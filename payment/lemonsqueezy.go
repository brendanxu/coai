package payment

import (
	"chat/globals"
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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

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
	secret := viper.GetString("lemonsqueezy.webhook_secret")
	if secret == "" {
		globals.Warn("[payment] LEMONSQUEEZY_WEBHOOK_SECRET not configured")
		c.AbortWithStatusJSON(401, gin.H{"error": "webhook secret not configured"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		globals.Warn(fmt.Sprintf("[payment] read body: %s", err))
		c.AbortWithStatusJSON(500, gin.H{"error": "body read failed"})
		return
	}

	sig := c.GetHeader("X-Signature")
	if !verifySignature(body, sig, secret) {
		c.AbortWithStatusJSON(401, gin.H{"error": "invalid signature"})
		return
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		// Valid signature but unparseable body. Logging and 200-OK avoids
		// LS retry-loop hell; we already absorbed the verified-but-broken event.
		globals.Warn(fmt.Sprintf("[payment] webhook payload unparseable: %s", err))
		c.JSON(200, gin.H{"status": "ignored"})
		return
	}

	// Idempotency. SHA256 of the body is a safe event ID:
	//   * Same body re-delivered (LS retry on our 5xx) → same hash → duplicate.
	//   * Different events → different bodies (LS includes timestamps in payload) → fresh hash.
	eventID := sha256Hex(body)
	db := utils.GetDBFromContext(c)

	inserted, err := insertWebhookEvent(db, eventID, payload.Meta.EventName)
	if err != nil {
		c.AbortWithStatusJSON(500, gin.H{"error": "idempotency table"})
		return
	}
	if !inserted {
		c.JSON(200, gin.H{"status": "duplicate"})
		return
	}

	if err := dispatch(db, &payload); err != nil {
		// Roll back the idempotency row so a future LS retry can succeed.
		_, _ = globals.ExecDb(db,
			`DELETE FROM gtk_webhook_event WHERE event_id = ?`, eventID)
		globals.Warn(fmt.Sprintf("[payment] dispatch %s: %s", payload.Meta.EventName, err))
		c.AbortWithStatusJSON(500, gin.H{"error": "dispatch failed"})
		return
	}

	_, _ = globals.ExecDb(db,
		`UPDATE gtk_webhook_event SET processed_at = ? WHERE event_id = ?`,
		utils.ConvertSqlTime(time.Now()), eventID)
	c.JSON(200, gin.H{"status": "ok"})
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// insertWebhookEvent returns (inserted, err). inserted=false with err=nil
// means the row already existed (idempotent duplicate).
func insertWebhookEvent(db *sql.DB, eventID, eventType string) (bool, error) {
	_, err := globals.ExecDb(db,
		`INSERT INTO gtk_webhook_event (event_id, event_type) VALUES (?, ?)`,
		eventID, eventType)
	if err == nil {
		return true, nil
	}
	if isDupErr(err) {
		return false, nil
	}
	return false, err
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

func dispatch(db *sql.DB, p *webhookPayload) error {
	switch p.Meta.EventName {
	case eventCreated, eventUpdated, eventResumed:
		return upsertSubscription(db, p)
	case eventCancelled:
		return markCancelled(db, p)
	case eventPaymentFailed:
		// Log only; LS handles dunning. User keeps access until subscription_cancelled fires.
		globals.Warn(fmt.Sprintf("[payment] payment_failed for ls_sub=%s", p.Data.ID))
		return nil
	default:
		// Unknown event_name. Ack with 200 (don't 4xx — LS would mark endpoint broken).
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

// upsertSubscription handles created/updated/resumed events. The semantics:
//   * created: first time this user subscribes — INSERT subscription row + INSERT mapping
//   * updated: renewal or plan change — UPDATE subscription.expired_at to new renews_at
//   * resumed: user un-cancelled before period ended — clear cancelled_at, refresh status
//
// All three converge on "ensure subscription.expired_at == LS renews_at" and
// "upsert mapping row with current LS state". The DB primitives are the same.
func upsertSubscription(db *sql.DB, p *webhookPayload) error {
	userID, err := userIDFromCustomData(p.Meta.CustomData)
	if err != nil {
		return err
	}
	renewsAt, err := time.Parse(time.RFC3339, p.Data.Attributes.RenewsAt)
	if err != nil {
		return fmt.Errorf("parse renews_at %q: %w", p.Data.Attributes.RenewsAt, err)
	}

	if err := activateExternalSubscription(db, userID, levelStarter, renewsAt); err != nil {
		return fmt.Errorf("activate: %w", err)
	}

	// Upsert the audit/mapping row. Existing cancelled_at gets cleared on resumed
	// (resumed = un-cancellation), preserved on update (re-billing of an active sub).
	clearCancelled := p.Meta.EventName == eventResumed
	return upsertLsMapping(db, userID, p, renewsAt, clearCancelled)
}

func upsertLsMapping(db *sql.DB, userID int64, p *webhookPayload, renewsAt time.Time, clearCancelled bool) error {
	// Engine-agnostic upsert: try INSERT, swallow duplicate-key, then UPDATE
	// fields that can change. CoAI's globals.PreflightSql only translates
	// MySQL-specific ON DUPLICATE KEY UPDATE for the `quota` table, so we
	// can't rely on that syntax for new tables.
	variantID := fmt.Sprintf("%d", p.Data.Attributes.VariantID)
	renewsAtStr := utils.ConvertSqlTime(renewsAt)

	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
			(user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		userID, p.Data.ID, variantID,
		p.Data.Attributes.Status, renewsAtStr, p.Data.Attributes.TestMode)
	if err != nil && !isDupErr(err) {
		return err
	}

	_, err = globals.ExecDb(db, `
		UPDATE gtk_ls_subscription
		SET variant_id = ?, status = ?, renews_at = ?, test_mode = ?
		WHERE ls_subscription_id = ?
	`,
		variantID, p.Data.Attributes.Status, renewsAtStr,
		p.Data.Attributes.TestMode, p.Data.ID)
	if err != nil {
		return err
	}

	if clearCancelled {
		_, err = globals.ExecDb(db,
			`UPDATE gtk_ls_subscription SET cancelled_at = NULL WHERE ls_subscription_id = ?`,
			p.Data.ID)
	}
	return err
}

// markCancelled handles subscription_cancelled. Per LS docs, the user
// retains access until period end — so we DO NOT touch CoAI's subscription
// table. We only set cancelled_at + status on the mapping row. CoAI's
// existing IsSubscribe() returns false naturally once expired_at passes.
func markCancelled(db *sql.DB, p *webhookPayload) error {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_ls_subscription
		SET cancelled_at = ?, status = ?
		WHERE ls_subscription_id = ?
	`, utils.ConvertSqlTime(time.Now()), p.Data.Attributes.Status, p.Data.ID)
	return err
}

// activateExternalSubscription is the bridge between LS payment events and
// CoAI's authoritative `subscription` table. It mirrors auth.User.AddSubscription
// but takes an explicit expiredAt (from LS renews_at) and bypasses user.Pay()
// — money flowed through LS, not CoAI's wallet.
//
// total_month=1 because LS bills monthly; we record one increment per webhook.
// On renewal (subscription_updated), the same DB row is UPDATEd with a new
// expired_at; total_month is NOT incremented here (LS payload doesn't tell us
// "this is renewal #N"; treating each event as +1 month would double-count).
func activateExternalSubscription(db *sql.DB, userID int64, level int, expiredAt time.Time) error {
	if level < 1 {
		return fmt.Errorf("invalid plan level: %d", level)
	}
	date := utils.ConvertSqlTime(expiredAt)

	// Engine-agnostic upsert: INSERT (ignore dup-key) then UPDATE. We can't
	// use MySQL-specific ON DUPLICATE KEY UPDATE because CoAI's PreflightSql
	// doesn't translate it for non-quota tables.
	_, err := globals.ExecDb(db,
		`INSERT INTO subscription (user_id, expired_at, total_month, level) VALUES (?, ?, 1, ?)`,
		userID, date, level)
	if err != nil && !isDupErr(err) {
		return err
	}

	// Always UPDATE — covers both the just-inserted case (no-op effectively)
	// and the renewal case where the row pre-existed. total_month is NOT
	// touched here; LS payload has no signal of "this is renewal #N".
	_, err = globals.ExecDb(db,
		`UPDATE subscription SET expired_at = ?, level = ? WHERE user_id = ?`,
		date, level, userID)
	return err
}
