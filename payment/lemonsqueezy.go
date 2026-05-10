package payment

import (
	"chat/globals"
	"chat/service"
	"chat/utils"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// logf emits a single-line key=value structured log via CoAI's globals logger.
// Format: "[payment] <event> k1=v1 k2=v2 ..."
//
// Reasons for inline kv (vs. zap/zerolog): CoAI's logger has no structured
// API and adding a new logging dep risks rebase pain. Single-line k=v is
// `grep | awk` friendly and lints clean in production aggregators.
func logf(level func(args ...interface{}), event string, fields ...interface{}) {
	parts := make([]string, 0, 2+len(fields)/2)
	parts = append(parts, "[payment]", event)
	for i := 0; i+1 < len(fields); i += 2 {
		parts = append(parts, fmt.Sprintf("%v=%v", fields[i], fields[i+1]))
	}
	level(strings.Join(parts, " "))
}

// levelStarter is the only paid tier shipping in v0.6 ($15/mo Starter).
// Maps directly to CoAI's `subscription.level` column. v0.7+ may add
// additional tiers; the mapping happens in `dispatch` based on variant_id.
const levelStarter = 1

// LS webhook event names we handle. Anything else is acked with 200 + log.
const (
	eventCreated       = "subscription_created"
	eventUpdated       = "subscription_updated"
	eventResumed       = "subscription_resumed"
	eventCancelled     = "subscription_cancelled"
	eventPaymentFailed = "subscription_payment_failed"
	eventOrderCreated  = "order_created" // v0.10 ③ — fired for service-order one-shots AND for first-month subscriptions
	eventSubPayment    = "subscription_payment_success" // v0.10 ③ — fired on monthly renewals; deferred handling
)

// webhookPayload is a partial map of LS's webhook envelope. We only decode
// the fields we use; LS adds optional fields without breaking us.
//
// LS reference: https://docs.lemonsqueezy.com/help/webhooks
type webhookPayload struct {
	Meta struct {
		EventName  string                 `json:"event_name"`
		TestMode   bool                   `json:"test_mode"`
		CustomData map[string]interface{} `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			VariantID int    `json:"variant_id"`
			Status    string `json:"status"`
			RenewsAt  string `json:"renews_at"`
			TestMode  bool   `json:"test_mode"`
		} `json:"attributes"`
	} `json:"data"`
}

// verifySignature checks the LS webhook HMAC SHA-256 signature using
// constant-time comparison to defeat timing attacks. Empty inputs return
// false defensively — never trust an unsigned/unsecreted webhook.
func verifySignature(body []byte, signature, secret string) bool {
	if len(body) == 0 || signature == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// HandleWebhook is the LS webhook entry point.
//
// HTTP responses:
//   401 — bad/missing signature, or webhook_secret env not configured
//   500 — DB write failed (LS will retry; idempotency table catches the retry)
//   200 — processed OR duplicate event_id (idempotent)
//
// We always read the raw body BEFORE binding so HMAC verification sees the
// exact bytes LS signed. Gin's binding consumes the body; doing it after
// json.Unmarshal would leave verification with empty bytes.
func HandleWebhook(c *gin.Context) {
	start := time.Now()
	secret := viper.GetString("lemonsqueezy.webhook_secret")
	if secret == "" {
		logf(globals.Warn, "secret_unconfigured")
		c.AbortWithStatusJSON(401, gin.H{"error": "webhook secret not configured"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logf(globals.Warn, "body_read_failed", "error", err)
		c.AbortWithStatusJSON(500, gin.H{"error": "body read failed"})
		return
	}

	sig := c.GetHeader("X-Signature")
	if !verifySignature(body, sig, secret) {
		logf(globals.Warn, "signature_invalid", "len", len(body), "sig_len", len(sig))
		c.AbortWithStatusJSON(401, gin.H{"error": "invalid signature"})
		return
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		// Verified-but-malformed payload. Codex P1 (2026-04-27): returning 200
		// silently loses LS-bug events that would parse on retry (truncation,
		// provider-side regression). Return 500 so LS retries within its bounded
		// window (~24h). Persistent malformed payloads will eventually exhaust
		// LS retries — that's the correct durable failure signal.
		logf(globals.Warn, "payload_unparseable", "error", err)
		c.AbortWithStatusJSON(500, gin.H{"error": "payload unparseable"})
		return
	}

	// Idempotency. SHA256 of the body is the event ID.
	//   * Same body re-delivered (LS retry on our 5xx) → same hash.
	//   * Different events → different bodies (LS includes timestamps).
	//
	// Two-phase classification (Codex P1 fix, 2026-04-27):
	//   * Insert succeeded → first-time delivery; proceed to dispatch.
	//   * Insert dup-key + existing row's processed_at IS NOT NULL → fully processed
	//     duplicate; ack 200 OK.
	//   * Insert dup-key + processed_at IS NULL → first attempt is in-flight
	//     OR crashed mid-dispatch. Return 503 so LS retries; by then either
	//     the in-flight attempt completes (next retry sees processed_at and
	//     200s) or has been cleaned up (next retry inserts cleanly).
	eventID := sha256Hex(body)
	db := utils.GetDBFromContext(c)

	classification, err := classifyEvent(db, eventID, payload.Meta.EventName)
	if err != nil {
		logf(globals.Error, "idempotency_table_error", "event_id", eventID, "error", err)
		c.AbortWithStatusJSON(500, gin.H{"error": "idempotency table"})
		return
	}
	switch classification {
	case eventClassFresh:
		// fall through to dispatch
	case eventClassProcessed:
		logf(globals.Info, "duplicate_event", "event_id", eventID, "event_type", payload.Meta.EventName)
		c.JSON(200, gin.H{"status": "duplicate"})
		return
	case eventClassInFlight:
		logf(globals.Warn, "in_flight_retry", "event_id", eventID, "event_type", payload.Meta.EventName)
		c.AbortWithStatusJSON(503, gin.H{"error": "in flight, retry"})
		return
	}

	if err := dispatch(db, &payload); err != nil {
		// Roll back the idempotency row so a future LS retry can succeed.
		// If the DELETE itself fails, log loudly — the row remains as
		// "in-flight" and the in_flight_retry path above will keep returning
		// 503 until LS retries successfully or gives up.
		if _, delErr := globals.ExecDb(db,
			`DELETE FROM gtk_webhook_event WHERE event_id = ?`, eventID); delErr != nil {
			logf(globals.Error, "rollback_delete_failed",
				"event_id", eventID, "error", delErr)
		}
		logf(globals.Warn, "dispatch_failed",
			"event_id", eventID,
			"event_type", payload.Meta.EventName,
			"error", err)
		c.AbortWithStatusJSON(500, gin.H{"error": "dispatch failed"})
		return
	}

	_, _ = globals.ExecDb(db,
		`UPDATE gtk_webhook_event SET processed_at = ? WHERE event_id = ?`,
		utils.ConvertSqlTime(time.Now()), eventID)

	latencyMs := time.Since(start).Milliseconds()
	logf(globals.Info, "processed",
		"event_id", eventID,
		"event_type", payload.Meta.EventName,
		"test_mode", payload.Meta.TestMode,
		"latency_ms", latencyMs)

	// Probabilistic background cleanup of old idempotency rows.
	// 5% sampling rate at webhook traffic of ~10/min keeps the table
	// bounded without a separate cron worker. The async DELETE doesn't
	// block our 200-response — LS only needs the status code.
	if rand.Intn(100) < 5 {
		go cleanupOldEvents(db)
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// cleanupOldEvents trims gtk_webhook_event rows older than the idempotency
// window (90d). LS retries within ~24h on failures, so 90d is generous.
//
// Runs on its own goroutine — never logs success (success is silent and
// frequent). Logs DB errors so a stuck cleanup is visible.
func cleanupOldEvents(db *sql.DB) {
	cutoff := utils.ConvertSqlTime(time.Now().AddDate(0, 0, -90))
	_, err := globals.ExecDb(db,
		`DELETE FROM gtk_webhook_event WHERE received_at < ?`, cutoff)
	if err != nil {
		logf(globals.Warn, "cleanup_failed", "error", err)
	}
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Event classification for the idempotency state machine. See HandleWebhook
// for the full flow rationale.
type eventClass int

const (
	eventClassFresh     eventClass = iota // first-time delivery; row freshly inserted
	eventClassProcessed                   // row exists with processed_at set; safe duplicate
	eventClassInFlight                    // row exists with processed_at NULL; first attempt mid-flight or crashed
)

// classifyEvent attempts to insert a webhook event row. On dup-key it
// inspects the existing row's processed_at to distinguish between safely
// processed events (Fresh→Processed) and events whose first attempt is
// either still running or died before reaching the UPDATE that sets
// processed_at.
//
// Returns (eventClassFresh, nil) only if the INSERT succeeded — the caller
// owns the subsequent dispatch. (eventClassProcessed/InFlight, nil) means
// no INSERT happened; the caller must NOT dispatch.
func classifyEvent(db *sql.DB, eventID, eventType string) (eventClass, error) {
	_, err := globals.ExecDb(db,
		`INSERT INTO gtk_webhook_event (event_id, event_type) VALUES (?, ?)`,
		eventID, eventType)
	if err == nil {
		return eventClassFresh, nil
	}
	if !isDupErr(err) {
		return eventClassFresh, err
	}

	// Dup-key. Inspect the existing row.
	var processedAt sql.NullString
	if scanErr := globals.QueryRowDb(db,
		`SELECT processed_at FROM gtk_webhook_event WHERE event_id = ?`,
		eventID).Scan(&processedAt); scanErr != nil {
		// Row vanished between INSERT and SELECT — race with a concurrent
		// rollback DELETE. Treat as fresh on the next call (the LS retry).
		return eventClassInFlight, nil
	}
	if processedAt.Valid && processedAt.String != "" {
		return eventClassProcessed, nil
	}
	return eventClassInFlight, nil
}

// isDupErr matches duplicate-key errors across MySQL (1062) and sqlite
// (UNIQUE constraint failed / PRIMARY KEY).
func isDupErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Error 1062") ||
		strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "PRIMARY KEY")
}

// dispatch routes an LS webhook payload to the right product-type handler.
// Two-level router (PKG-2 Wave 3 C1):
//
//  1. greentokey_order_no in custom_data  → service-order path
//     (dispatchServiceOrder until Wave 3 C2 splits it into
//      dispatch_service.go::handleServiceEvent).
//  2. otherwise                            → token-plan path
//     (dispatch_token.go::handleTokenPlanEvent).
//
// Both paths are idempotent at their state-flip layers. The split into
// dispatch_token.go / dispatch_service.go is the "blast-radius reduction"
// from autoplan H2: a token-plan change can no longer accidentally touch
// service code paths and vice-versa.
func dispatch(db *sql.DB, p *webhookPayload) error {
	if orderNo := serviceOrderNoFromCustomData(p.Meta.CustomData); orderNo != "" {
		return dispatchServiceOrder(db, p, orderNo)
	}
	return handleTokenPlanEvent(db, p)
}

// serviceOrderNoFromCustomData extracts greentokey_order_no if present.
// Returns empty string if absent (Layer 2 套餐 path).
func serviceOrderNoFromCustomData(custom map[string]interface{}) string {
	raw, ok := custom["greentokey_order_no"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// dispatchServiceOrder handles webhooks belonging to Layer 3 service
// orders (custom_data has greentokey_order_no).
//
// v0.10 first-cut handling:
//   - order_created           → MarkOrderPaid (one-shot or first-month)
//   - subscription_created    → MarkOrderPaid (covers first-month);
//                                NOTE: does NOT call upsertSubscription
//                                because that path is for Layer 2 token
//                                packs, not Layer 3 service orders.
//   - subscription_cancelled  → log only; the order itself is already
//                                paid, founder handles refund via
//                                /api/gtk/v1/admin/refund if needed.
//   - subscription_payment_success → DEFERRED. Each monthly renewal
//                                should create a NEW gtk_service_order
//                                row + flip it to paid. v0.11 cron job
//                                or follow-up commit handles this. For
//                                v0.10 first deploy: log + ack so LS
//                                stops retrying.
//   - other                   → log + ack.
func dispatchServiceOrder(db *sql.DB, p *webhookPayload, orderNo string) error {
	switch p.Meta.EventName {
	case eventOrderCreated, eventCreated:
		return service.MarkOrderPaid(db, orderNo, p.Data.ID, "lemonsqueezy")
	case eventCancelled:
		logf(globals.Info, "service_order_subscription_cancelled",
			"order_no", orderNo, "ls_subscription_id", p.Data.ID,
			"note", "order itself is already paid; refund via /admin/refund if needed")
		return nil
	case eventSubPayment:
		// TODO v0.11: create new gtk_service_order row for this month
		// of the existing subscription, flip to paid.
		logf(globals.Info, "service_order_monthly_renewal_deferred",
			"order_no", orderNo, "ls_subscription_id", p.Data.ID,
			"todo", "v0.11 cron creates new monthly order row")
		return nil
	default:
		logf(globals.Info, "service_order_event_acked",
			"order_no", orderNo, "event", p.Meta.EventName)
		return nil
	}
}

// userIDFromCustomData extracts the greentokey user_id we embedded in the
// LS Checkout URL via `checkout[custom][user_id]`. LS preserves it in
// every webhook for the resulting subscription.
//
// LS sends custom_data values as JSON strings or numbers depending on
// how the URL encoded them. Accept both.
func userIDFromCustomData(custom map[string]interface{}) (int64, error) {
	raw, ok := custom["user_id"]
	if !ok {
		return 0, errors.New("custom_data.user_id missing")
	}
	switch v := raw.(type) {
	case string:
		var id int64
		if _, err := fmt.Sscanf(v, "%d", &id); err != nil {
			return 0, fmt.Errorf("custom_data.user_id not numeric: %q", v)
		}
		return id, nil
	case float64:
		return int64(v), nil
	case int:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("custom_data.user_id wrong type: %T", v)
	}
}

// upsertSubscription, upsertLsMapping, markCancelled, activateExternalSubscription
// all moved to dispatch_token.go (PKG-2 Wave 3 C1). They preserve invariants
// I3 (monotonic renews_at guard) and I4 (monotonic expired_at guard) — both
// hard-won Codex P1 fixes from 2026-04-27. See dispatch_token.go for the
// authoritative implementations and the inline I3/I4 reminders.

// creditsForLevel + quotaUnitsForLevel translate a subscription level
// into the user-facing credit allowance and the corresponding NewAPI
// quota unit count.
//
// Credit semantics (locked 2026-04-30, see newapi/credit.go):
//   1 credit = 1500 NewAPI quota units
//   ¥99/月 = $15/月 = 5000 credits = 7,500,000 quota units
//
// Per-call burn (assuming 1k input + 1k output):
//   轻量 (light)    0.5 credits   — DeepSeek-chat / Qwen-flash etc.
//   标准 (standard) 1.0 credits   — DeepSeek-r1 / Claude Haiku / GPT-4o-mini
//   高级 (premium)  3.0 credits   — GPT-4o / Claude Sonnet / Claude Opus
//
// v0.9 levels (placeholder; v1 will move to gtk_plan.credits):
//
//   levelStarter (1)  →  5,000 credits / month  → ¥99 or $15
//   levelPro     (2)  → 20,000 credits / month  → ¥299 or $45
//   levelScale   (3)  → 80,000 credits / month  → ¥999 or $145
//
// Only level 1 is wired in v0.9; 2/3 documented for forward compat.
func creditsForLevel(level int) int64 {
	switch level {
	case 1: // Starter ¥99 / $15
		return 5_000
	case 2: // Pro ¥299 / $45
		return 20_000
	case 3: // Scale ¥999 / $145
		return 80_000
	default:
		return 0
	}
}

// quotaUnitsForLevel returns NewAPI internal quota for a level, derived
// from creditsForLevel(level) * QuotaPerCredit. Single source of truth:
// edit creditsForLevel and the conversion stays consistent.
//
// We intentionally hardcode 1500 here (= newapi.QuotaPerCredit) rather
// than import the constant — keeps payment package free of newapi build
// dependency for unit tests. If the constant ever changes (it shouldn't,
// it's a pricing decision not an engineering knob), update both places.
func quotaUnitsForLevel(level int) int64 {
	const quotaPerCredit = 1500 // mirror of newapi.QuotaPerCredit
	return creditsForLevel(level) * quotaPerCredit
}

// (upsertLsMapping / markCancelled / activateExternalSubscription moved to
//  dispatch_token.go in PKG-2 Wave 3 C1.)
