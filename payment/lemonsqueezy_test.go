package payment

import (
	"bytes"
	"chat/globals"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	_ "github.com/mattn/go-sqlite3"
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
	secret := "test-secret-1234567890"
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
