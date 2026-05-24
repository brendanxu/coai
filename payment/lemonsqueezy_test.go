package payment

import (
	"bytes"
	"chat/globals"
	"chat/plans"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
)

// computeHMAC produces a hex-encoded HMAC-SHA256 of body using secret.
// Test helper — mirrors what LS computes server-side before sending a webhook.
func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- verifySignature ---

func TestVerifySignature_ValidSignature(t *testing.T) {
	secret := "unit-test-webhook-fixture"
	body := []byte(`{"meta":{"event_name":"subscription_created"}}`)
	sig := computeHMAC(body, secret)
	if !verifySignature(body, sig, secret) {
		t.Fatal("valid signature was rejected")
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	body := []byte("payload")
	sig := computeHMAC(body, "real-secret")
	if verifySignature(body, sig, "different-secret") {
		t.Fatal("signature verified against wrong secret")
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	secret := "test-secret"
	original := []byte("original payload")
	sig := computeHMAC(original, secret)
	if verifySignature([]byte("tampered payload"), sig, secret) {
		t.Fatal("signature verified after body was tampered")
	}
}

func TestVerifySignature_EmptyBody(t *testing.T) {
	if verifySignature(nil, "abc", "secret") {
		t.Fatal("empty body should fail verification")
	}
	if verifySignature([]byte{}, "abc", "secret") {
		t.Fatal("zero-length body should fail verification")
	}
}

func TestVerifySignature_EmptySignature(t *testing.T) {
	if verifySignature([]byte("body"), "", "secret") {
		t.Fatal("empty signature should fail verification")
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	body := []byte("body")
	sig := computeHMAC(body, "")
	if verifySignature(body, sig, "") {
		t.Fatal("empty secret should fail verification (no real auth)")
	}
}

// --- isDupErr ---

func TestIsDupErr_MatchesMySQLDuplicate(t *testing.T) {
	err := sqlError("Error 1062: Duplicate entry 'evt_abc' for key 'PRIMARY'")
	if !isDupErr(err) {
		t.Fatal("MySQL Error 1062 should be classified as duplicate")
	}
}

func TestIsDupErr_MatchesSqliteUnique(t *testing.T) {
	if !isDupErr(sqlError("UNIQUE constraint failed: gtk_webhook_event.event_id")) {
		t.Fatal("sqlite UNIQUE constraint should be classified as duplicate")
	}
	if !isDupErr(sqlError("PRIMARY KEY must be unique")) {
		t.Fatal("sqlite PRIMARY KEY error should be classified as duplicate")
	}
}

func TestIsDupErr_NotADuplicate(t *testing.T) {
	if isDupErr(sqlError("connection refused")) {
		t.Fatal("connection error mis-classified as duplicate")
	}
	if isDupErr(nil) {
		t.Fatal("nil error mis-classified as duplicate")
	}
}

func sqlError(msg string) error { return &fakeErr{msg} }

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

// --- userIDFromCustomData ---

func TestUserIDFromCustomData_String(t *testing.T) {
	id, err := userIDFromCustomData(map[string]interface{}{"user_id": "42"})
	if err != nil {
		t.Fatalf("string user_id rejected: %v", err)
	}
	if id != 42 {
		t.Fatalf("got %d want 42", id)
	}
}

func TestUserIDFromCustomData_Number(t *testing.T) {
	// json.Unmarshal into interface{} produces float64 for numeric values.
	id, err := userIDFromCustomData(map[string]interface{}{"user_id": float64(7)})
	if err != nil {
		t.Fatalf("numeric user_id rejected: %v", err)
	}
	if id != 7 {
		t.Fatalf("got %d want 7", id)
	}
}

func TestUserIDFromCustomData_Missing(t *testing.T) {
	if _, err := userIDFromCustomData(map[string]interface{}{}); err == nil {
		t.Fatal("missing user_id should error")
	}
}

// --- HandleWebhook integration ---

// newTestEngine wires gin with a sqlite-backed db middleware exactly the way
// the production middleware/auth.go does (c.Set("db", db)). Lets HandleWebhook
// run against a real DB without spinning up MySQL.
func newTestEngine(t *testing.T) (*gin.Engine, *sql.DB) {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Pin to a single connection so the in-memory DB is shared across
	// goroutines (the webhook handler may launch a cleanupOldEvents
	// background goroutine that otherwise grabs a fresh empty :memory:
	// DB). Same fix used by commerce/entitlement_test.go +
	// payment/dispatch_token_test.go.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}
	// CoAI's CreateSubscriptionTable schema, sqlite-compatible via PreflightSql.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS subscription (
		  id INT PRIMARY KEY AUTO_INCREMENT,
		  level INT DEFAULT 1,
		  user_id INT UNIQUE,
		  expired_at DATETIME,
		  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  total_month INT DEFAULT 0,
		  enterprise BOOLEAN DEFAULT FALSE,
		  FOREIGN KEY (user_id) REFERENCES auth(id)
		);
	`); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("db", db); c.Next() })
	r.POST("/webhook/lemonsqueezy", HandleWebhook)
	return r, db
}

func samplePayload(eventName string) []byte {
	p := map[string]interface{}{
		"meta": map[string]interface{}{
			"event_name":  eventName,
			"test_mode":   true,
			"custom_data": map[string]interface{}{"user_id": "42"},
		},
		"data": map[string]interface{}{
			"id": "ls-sub-001",
			"attributes": map[string]interface{}{
				"variant_id": 999,
				"status":     "active",
				"renews_at":  "2026-05-27T12:00:00Z",
				"test_mode":  true,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func samplePlanPayload(eventName string) []byte {
	p := map[string]interface{}{
		"meta": map[string]interface{}{
			"event_name": eventName,
			"test_mode":  true,
			"custom_data": map[string]interface{}{
				"type":      "plan",
				"plan_code": "starter-100k",
				"user_id":   "42",
			},
		},
		"data": map[string]interface{}{
			"id": "ls-order-plan-001",
			"attributes": map[string]interface{}{
				"variant_id": 999,
				"status":     "paid",
				"renews_at":  "2026-05-27T12:00:00Z",
				"test_mode":  true,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func seedPlanRechargeTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE quota (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER UNIQUE,
			quota REAL,
			used REAL
		)
	`); err != nil {
		t.Fatalf("seed quota table: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan (code, name, type, price_cents, duration_days, quota_config, is_active)
		VALUES ('starter-100k', 'Starter 100K', 'pack', 9900, 30, '{"quota": 100000}', TRUE)
	`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func TestHandleWebhook_HappyPath_SubscriptionCreated(t *testing.T) {
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200; body=%s", w.Code, w.Body.String())
	}

	var level int
	err := db.QueryRow(`SELECT level FROM subscription WHERE user_id = 42`).Scan(&level)
	if err != nil {
		t.Fatalf("subscription row not created: %v", err)
	}
	if level != 1 {
		t.Fatalf("got level=%d want 1 (Starter)", level)
	}

	var lsID string
	err = db.QueryRow(`SELECT ls_subscription_id FROM gtk_ls_subscription WHERE user_id = 42`).Scan(&lsID)
	if err != nil {
		t.Fatalf("mapping row not created: %v", err)
	}
	if lsID != "ls-sub-001" {
		t.Fatalf("got ls_subscription_id=%q want ls-sub-001", lsID)
	}
}

func TestHandleWebhook_PlanCustomDataRedeemsQuota(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePlanPayload("order_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200; body=%s", w.Code, w.Body.String())
	}

	var planRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_user_plan WHERE order_id = 'ls-order-plan-001'`).Scan(&planRows); err != nil {
		t.Fatalf("count gtk_user_plan: %v", err)
	}
	if planRows != 1 {
		t.Fatalf("got %d gtk_user_plan rows, want 1", planRows)
	}

	var quota float64
	if err := db.QueryRow(`SELECT quota FROM quota WHERE user_id = 42`).Scan(&quota); err != nil {
		t.Fatalf("read quota: %v", err)
	}
	if quota != 100000 {
		t.Fatalf("got quota=%f want 100000", quota)
	}

	var legacyRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription WHERE user_id = 42`).Scan(&legacyRows); err != nil {
		t.Fatalf("count legacy subscription: %v", err)
	}
	if legacyRows != 0 {
		t.Fatalf("plan checkout should skip legacy subscription path; got %d rows", legacyRows)
	}
}

// BL-01 (REVIEW.md 2026-05-13): LS fires 2-3 events per first-month
// subscription purchase, all carrying the same custom_data. Before the fix,
// the dispatcher accepted all three event names → 3 RedeemPlanForOrder calls
// with 3 different data.id values → triple credit grant (15,000 instead of
// 5,000). After the fix, only `order_created` redeems; sibling events ack
// with a log and create no rows.
//
// These two tests pin that behavior. Together with the existing
// TestHandleWebhook_PlanCustomDataRedeemsQuota (order_created → 1 row),
// they cover all three plan-bearing event shapes LS produces.
func TestHandleWebhook_PlanCustomData_SubscriptionCreatedDoesNotRedeem(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePlanPayload("subscription_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200 (sibling event must ack); body=%s", w.Code, w.Body.String())
	}

	// Critical assertion: NO gtk_user_plan row created — this event must NOT redeem.
	var planRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_user_plan`).Scan(&planRows); err != nil {
		t.Fatalf("count gtk_user_plan: %v", err)
	}
	if planRows != 0 {
		t.Fatalf("subscription_created plan event must NOT create gtk_user_plan; got %d rows", planRows)
	}

	// And no quota mutation either.
	var quotaRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM quota WHERE user_id = 42`).Scan(&quotaRows); err != nil {
		t.Fatalf("count quota: %v", err)
	}
	if quotaRows != 0 {
		t.Fatalf("subscription_created plan event must NOT touch quota; got %d rows", quotaRows)
	}
}

func TestHandleWebhook_PlanCustomData_SubscriptionPaymentSuccessDoesNotRedeem(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePlanPayload("subscription_payment_success")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200 (sibling event must ack); body=%s", w.Code, w.Body.String())
	}

	var planRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_user_plan`).Scan(&planRows); err != nil {
		t.Fatalf("count gtk_user_plan: %v", err)
	}
	if planRows != 0 {
		t.Fatalf("subscription_payment_success plan event must NOT create gtk_user_plan; got %d rows", planRows)
	}
}

func TestHandleWebhook_BadSignature_Returns401(t *testing.T) {
	r, _ := newTestEngine(t)
	viper.Set("lemonsqueezy.webhook_secret", "real-secret")
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, "WRONG-SECRET"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("got %d want 401", w.Code)
	}
}

func TestHandleWebhook_DuplicateEvent_IsIdempotent(t *testing.T) {
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	sig := computeHMAC(body, secret)

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := send(); w.Code != 200 {
		t.Fatalf("first send: got %d want 200; body=%s", w.Code, w.Body.String())
	}
	if w := send(); w.Code != 200 {
		t.Fatalf("duplicate send: got %d want 200; body=%s", w.Code, w.Body.String())
	}

	// Critical: duplicate webhook must NOT create a second subscription row
	// (or extend total_month). Verify subscription table has exactly 1 row for user 42.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription WHERE user_id = 42`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d subscription rows want 1 (idempotency violated)", count)
	}

	var totalMonth int
	if err := db.QueryRow(`SELECT total_month FROM subscription WHERE user_id = 42`).Scan(&totalMonth); err != nil {
		t.Fatalf("read total_month: %v", err)
	}
	if totalMonth != 1 {
		t.Fatalf("got total_month=%d want 1 (no double-counting on retry)", totalMonth)
	}
}

func TestHandleWebhook_MissingSecretEnv_Returns401(t *testing.T) {
	r, _ := newTestEngine(t)
	viper.Set("lemonsqueezy.webhook_secret", "")
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", "anything")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("got %d want 401", w.Code)
	}
}

func TestHandleWebhook_UnknownEventType_AcksWith200(t *testing.T) {
	r, _ := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_plan_changed_to_unknown_thing")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Unknown events must 200 — anything else makes LS retry forever.
	if w.Code != 200 {
		t.Fatalf("got %d want 200 (unknown events should be silently acked); body=%s", w.Code, w.Body.String())
	}
}

func TestHandleWebhook_Cancelled_LeavesSubscriptionExpiredAtUnchanged(t *testing.T) {
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	// First create the subscription.
	body := samplePayload("subscription_created")
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var beforeExpiredAt string
	_ = db.QueryRow(`SELECT expired_at FROM subscription WHERE user_id = 42`).Scan(&beforeExpiredAt)

	// Now cancel.
	body2 := samplePayload("subscription_cancelled")
	req2 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body2))
	req2.Header.Set("X-Signature", computeHMAC(body2, secret))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("cancel failed: %d %s", w2.Code, w2.Body.String())
	}

	// expired_at must NOT change — user keeps access until period end.
	var afterExpiredAt string
	_ = db.QueryRow(`SELECT expired_at FROM subscription WHERE user_id = 42`).Scan(&afterExpiredAt)
	if beforeExpiredAt != afterExpiredAt {
		t.Fatalf("expired_at changed on cancel: before=%q after=%q", beforeExpiredAt, afterExpiredAt)
	}

	// gtk_ls_subscription.cancelled_at must be set.
	var cancelledAt sql.NullString
	if err := db.QueryRow(`SELECT cancelled_at FROM gtk_ls_subscription WHERE user_id = 42`).Scan(&cancelledAt); err != nil {
		t.Fatalf("read cancelled_at: %v", err)
	}
	if !cancelledAt.Valid || cancelledAt.String == "" {
		t.Fatal("cancelled_at not set after subscription_cancelled event")
	}
}

// ----------------------------------------------------------------------
// PKG-2 Wave 3 C3 invariant locks
// ----------------------------------------------------------------------

// I1: SHA256 raw-body event ID dedup via gtk_webhook_event.
//
// Same body delivered twice — first dispatches, second is a no-op (200 OK
// with status=duplicate, no second subscription row). Locks in the
// idempotency contract on the gtk_webhook_event table that survived
// Wave 3 refactor.
func TestWebhookEvent_DuplicateBody_NoOp(t *testing.T) {
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	sig := computeHMAC(body, secret)

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := send(); w.Code != 200 {
		t.Fatalf("first send: got %d want 200; body=%s", w.Code, w.Body.String())
	}
	if w := send(); w.Code != 200 {
		t.Fatalf("duplicate send: got %d want 200; body=%s", w.Code, w.Body.String())
	}

	// Subscription table must have exactly 1 row + total_month==1 (no
	// second activation; no double-counting).
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription WHERE user_id = 42`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d subscription rows want 1 (I1: gtk_webhook_event idempotency violated)", count)
	}

	// gtk_webhook_event must have one row with processed_at populated.
	var procAt sql.NullString
	if err := db.QueryRow(
		`SELECT processed_at FROM gtk_webhook_event WHERE event_id = ?`,
		sha256Hex(body),
	).Scan(&procAt); err != nil {
		t.Fatalf("read gtk_webhook_event: %v", err)
	}
	if !procAt.Valid || procAt.String == "" {
		t.Fatal("processed_at not set after first delivery (I1 invariant violated)")
	}
}

// I2: processed_at rollback on dispatch failure.
//
// Dispatch returns err → the gtk_webhook_event row inserted by
// classifyEvent must be DELETEd (rolled back) so a subsequent LS retry
// with the same body re-fires dispatch instead of being silently 200-acked.
//
// We force a dispatch failure by injecting an unparseable renews_at into
// the payload AFTER signature verification (signature is on the wire
// bytes; verification can't tell semantic vs syntactic invalidity).
func TestWebhookFailure_ProcessedAtRolledBack(t *testing.T) {
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	// Build a payload with a bad renews_at — upsertSubscription will fail
	// when time.Parse rejects it, which propagates as a dispatch error.
	p := map[string]interface{}{
		"meta": map[string]interface{}{
			"event_name":  "subscription_created",
			"test_mode":   true,
			"custom_data": map[string]interface{}{"user_id": "42"},
		},
		"data": map[string]interface{}{
			"id": "ls-tok-bad-date",
			"attributes": map[string]interface{}{
				"variant_id": 999,
				"status":     "active",
				"renews_at":  "NOT-A-TIMESTAMP",
				"test_mode":  true,
			},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Dispatch failed → handler returned 500.
	if w.Code != 500 {
		t.Fatalf("got %d want 500 (dispatch failure expected); body=%s", w.Code, w.Body.String())
	}

	// CRITICAL: the gtk_webhook_event row must NOT exist (rolled back).
	// A retry of the same bytes must be classifyEventFresh, not
	// classifyEventProcessed/InFlight.
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_webhook_event WHERE event_id = ?`,
		sha256Hex(body),
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("I2: gtk_webhook_event row not rolled back after dispatch failure (count=%d)", n)
	}
}

// I5 (NEW per autoplan H1): X-Event-Timestamp freshness window.
//
// HMAC validates the body bytes; freshness validates the *time* of
// delivery. A captured-payload replay with a valid signature but a
// timestamp older than 5 min is rejected with 401, never reaching
// dispatch.
func TestWebhook_StaleTimestamp_Rejected(t *testing.T) {
	r, _ := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	staleTimestamp := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)

	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("X-Event-Timestamp", staleTimestamp)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("got %d want 401 (stale timestamp must reject); body=%s",
			w.Code, w.Body.String())
	}
}

// I5 companion: a fresh timestamp (within window) is accepted normally.
func TestWebhook_FreshTimestamp_Accepted(t *testing.T) {
	r, _ := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	freshTimestamp := time.Now().UTC().Format(time.RFC3339)

	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("X-Event-Timestamp", freshTimestamp)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200 (fresh timestamp must accept); body=%s",
			w.Code, w.Body.String())
	}
}

// I5 unit-level: verifySignatureWithFreshness contract.

func TestVerifySignatureWithFreshness_AbsentTimestampAccepted(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := "abc"
	sig := computeHMAC(body, secret)
	ok, _ := verifySignatureWithFreshness(body, sig, "", secret, time.Now())
	if !ok {
		t.Fatal("absent timestamp must be permissively accepted (back-compat)")
	}
}

func TestVerifySignatureWithFreshness_StaleRejected(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := "abc"
	sig := computeHMAC(body, secret)
	stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	ok, reason := verifySignatureWithFreshness(body, sig, stale, secret, time.Now().UTC())
	if ok {
		t.Fatal("stale timestamp must reject")
	}
	if reason == "" {
		t.Errorf("expected non-empty reason on rejection")
	}
}

func TestVerifySignatureWithFreshness_FutureRejected(t *testing.T) {
	// Far-future timestamps (>5min ahead) are also rejected — a sign of
	// either clock skew or replay-with-mutated-timestamp attack.
	body := []byte(`{"x":1}`)
	secret := "abc"
	sig := computeHMAC(body, secret)
	future := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	ok, _ := verifySignatureWithFreshness(body, sig, future, secret, time.Now().UTC())
	if ok {
		t.Fatal("far-future timestamp must reject")
	}
}

func TestVerifySignatureWithFreshness_BadSigRejected(t *testing.T) {
	body := []byte(`{"x":1}`)
	ok, _ := verifySignatureWithFreshness(body, "not-a-real-sig", "", "secret", time.Now())
	if ok {
		t.Fatal("bad signature must reject regardless of timestamp")
	}
}

func TestParseWebhookTimestamp_RFC3339(t *testing.T) {
	got, err := parseWebhookTimestamp("2026-05-10T12:00:00Z")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 5 {
		t.Errorf("parse wrong: %v", got)
	}
}

func TestParseWebhookTimestamp_UnixSeconds(t *testing.T) {
	// 2026-05-10T00:00:00Z = 1778976000 unix seconds.
	got, err := parseWebhookTimestamp("1778976000")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Year() != 2026 {
		t.Errorf("parse wrong: %v", got)
	}
}

func TestParseWebhookTimestamp_Garbage(t *testing.T) {
	if _, err := parseWebhookTimestamp("hello"); err == nil {
		t.Fatal("garbage timestamp should error")
	}
}
