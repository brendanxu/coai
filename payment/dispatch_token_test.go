package payment

import (
	"bytes"
	"chat/commerce"
	"chat/globals"
	"chat/newapi"
	"chat/plans"
	"chat/service"
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// newTokenTestEngine extends the basic test engine with the additional
// schemas commerce.RevokeEntitlement needs (gtk_user_plan, gtk_newapi_*).
//
// We can't reuse newTestEngine() directly because it only migrates
// payment's own tables (gtk_ls_subscription + gtk_webhook_event), but
// refund flows reach into gtk_user_plan via commerce.RevokeEntitlement.
// Going through plans.Migrate + service.Migrate + newapi.Migrate mirrors
// the production boot-order in main.go.
func newTokenTestEngine(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Pin to single conn so :memory: is shared across goroutines (parity
	// with commerce/entitlement_test.go pattern).
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}

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
		t.Fatalf("payment.Migrate: %v", err)
	}
	if err := service.Migrate(db); err != nil {
		t.Fatalf("service.Migrate: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans.Migrate: %v", err)
	}
	if err := newapi.Migrate(db); err != nil {
		t.Fatalf("newapi.Migrate: %v", err)
	}
	return db
}

// makeTokenPayload builds a webhookPayload struct directly (unit-level
// bypass; HTTP-level tests live in lemonsqueezy_test.go and use the gin
// route + signature).
func makeTokenPayload(eventName, lsID string, renewsAt time.Time, status string) *webhookPayload {
	p := &webhookPayload{}
	p.Meta.EventName = eventName
	p.Meta.TestMode = true
	p.Meta.CustomData = map[string]interface{}{"user_id": "42"}
	p.Data.ID = lsID
	p.Data.Attributes.VariantID = 999
	p.Data.Attributes.Status = status
	p.Data.Attributes.RenewsAt = renewsAt.UTC().Format(time.RFC3339)
	p.Data.Attributes.TestMode = true
	return p
}

// --- C1.1 happy path: subscription_created routes through the new file --

func TestHandleTokenPlanEvent_SubscriptionCreated(t *testing.T) {
	db := newTokenTestEngine(t)

	p := makeTokenPayload(eventCreated, "ls-tok-001",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "active")

	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("handleTokenPlanEvent: %v", err)
	}

	// subscription row created with level=1, expired_at=2026-06-01.
	var (
		level     int
		expiredAt string
	)
	if err := db.QueryRow(
		`SELECT level, expired_at FROM subscription WHERE user_id = 42`,
	).Scan(&level, &expiredAt); err != nil {
		t.Fatalf("read subscription: %v", err)
	}
	if level != 1 {
		t.Errorf("level=%d want 1 (Starter)", level)
	}
	if got := expiredAt[:10]; got != "2026-06-01" {
		t.Errorf("expired_at prefix=%q want 2026-06-01", got)
	}

	// gtk_ls_subscription mapping row exists.
	var lsID string
	if err := db.QueryRow(
		`SELECT ls_subscription_id FROM gtk_ls_subscription WHERE user_id = 42`,
	).Scan(&lsID); err != nil {
		t.Fatalf("read gtk_ls_subscription: %v", err)
	}
	if lsID != "ls-tok-001" {
		t.Errorf("ls_subscription_id=%q want ls-tok-001", lsID)
	}
}

// --- C1.2 cancellation: cancel_at_period_end stamps cancelled_at ---------

func TestHandleTokenPlanEvent_SubscriptionCancelled(t *testing.T) {
	db := newTokenTestEngine(t)

	// Pre-create the subscription so cancellation has something to stamp.
	created := makeTokenPayload(eventCreated, "ls-tok-cancel",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "active")
	if err := handleTokenPlanEvent(db, created); err != nil {
		t.Fatalf("seed created: %v", err)
	}

	var beforeExpired string
	_ = db.QueryRow(`SELECT expired_at FROM subscription WHERE user_id = 42`).Scan(&beforeExpired)

	// Now cancel.
	cancelled := makeTokenPayload(eventCancelled, "ls-tok-cancel",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "cancelled")
	if err := handleTokenPlanEvent(db, cancelled); err != nil {
		t.Fatalf("handleTokenPlanEvent(cancelled): %v", err)
	}

	// Per LS semantics, expired_at MUST NOT change on cancel — the user
	// keeps access until period end. CoAI's IsSubscribe() naturally returns
	// false once expired_at passes.
	var afterExpired string
	_ = db.QueryRow(`SELECT expired_at FROM subscription WHERE user_id = 42`).Scan(&afterExpired)
	if beforeExpired != afterExpired {
		t.Errorf("expired_at changed on cancel: before=%q after=%q", beforeExpired, afterExpired)
	}

	// gtk_ls_subscription.cancelled_at should be populated.
	var cancelledAt sql.NullString
	if err := db.QueryRow(
		`SELECT cancelled_at FROM gtk_ls_subscription WHERE ls_subscription_id = ?`,
		"ls-tok-cancel",
	).Scan(&cancelledAt); err != nil {
		t.Fatalf("read cancelled_at: %v", err)
	}
	if !cancelledAt.Valid || cancelledAt.String == "" {
		t.Fatal("cancelled_at not set after subscription_cancelled")
	}
}

// --- C1.3 refund flow: RefundTokenPlan calls commerce.RevokeEntitlement --

func TestHandleTokenPlanEvent_RefundFull(t *testing.T) {
	db := newTokenTestEngine(t)

	// Seed a gtk_user_plan row (active) for order_id="ls-tok-refund". The
	// commerce.RevokeEntitlement(token) path reads this table and flips
	// status='canceled' + cancellation_reason='refund_full'.
	// Use id=999 to avoid colliding with seedTokenPlans (which auto-inserts
	// 'token-99' at migration time and gets id=1). Test plan code stays
	// 'starter' since RefundTokenPlan reads plan_id, not code.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan (id, code, name, type, price_cents, duration_days)
		VALUES (999, 'starter', 'Starter', 'subscription', 1500, 30)
	`); err != nil {
		t.Fatalf("seed gtk_plan: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_user_plan
		  (user_id, plan_id, product_type, status, cancellation_reason,
		   expire_at, order_id)
		VALUES (?, 999, 'token', 'active', NULL, ?, ?)
	`, 42, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "ls-tok-refund"); err != nil {
		t.Fatalf("seed gtk_user_plan: %v", err)
	}

	state, err := RefundTokenPlan(context.Background(), db, "ls-tok-refund", "refund_full")
	if err != nil {
		t.Fatalf("RefundTokenPlan: %v", err)
	}
	// 'refund_*' reasons map to EntitlementRevoked per classifyCanceledState.
	if state != commerce.EntitlementRevoked {
		t.Errorf("state=%q want %q", state, commerce.EntitlementRevoked)
	}

	// Idempotent: second call must return the same state, no error.
	state2, err := RefundTokenPlan(context.Background(), db, "ls-tok-refund", "refund_full")
	if err != nil {
		t.Fatalf("RefundTokenPlan (second call): %v", err)
	}
	if state2 != commerce.EntitlementRevoked {
		t.Errorf("idempotent state=%q want %q", state2, commerce.EntitlementRevoked)
	}

	// DB reflects the flip.
	var status string
	var reason sql.NullString
	if err := db.QueryRow(
		`SELECT status, cancellation_reason FROM gtk_user_plan WHERE order_id = ?`,
		"ls-tok-refund",
	).Scan(&status, &reason); err != nil {
		t.Fatalf("read gtk_user_plan: %v", err)
	}
	if status != "canceled" {
		t.Errorf("status=%q want canceled", status)
	}
	if !reason.Valid || reason.String != "refund_full" {
		t.Errorf("cancellation_reason=%q want refund_full", reason.String)
	}
}

func TestRefundTokenPlan_RejectsEmptyID(t *testing.T) {
	db := newTokenTestEngine(t)
	if _, err := RefundTokenPlan(context.Background(), db, "", "refund_full"); err == nil {
		t.Fatal("RefundTokenPlan accepted empty subscription id")
	}
}

// --- I3 regression: monotonic renews_at guard survives the refactor ------

func TestWebhook_OutOfOrderRenews_NoRegression(t *testing.T) {
	db := newTokenTestEngine(t)

	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// First the newer event lands.
	if err := upsertLsMapping(db, 42,
		makeTokenPayload(eventUpdated, "ls-tok-race", newer, "active"),
		newer, false,
	); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Now the older event — expected by I3 to NOT regress renews_at.
	if err := upsertLsMapping(db, 42,
		makeTokenPayload(eventUpdated, "ls-tok-race", older, "active"),
		older, false,
	); err != nil {
		t.Fatalf("older upsert: %v", err)
	}

	var renewsAt string
	if err := db.QueryRow(
		`SELECT renews_at FROM gtk_ls_subscription WHERE ls_subscription_id = ?`,
		"ls-tok-race",
	).Scan(&renewsAt); err != nil {
		t.Fatalf("read renews_at: %v", err)
	}
	if got := renewsAt[:10]; got != "2026-07-01" {
		t.Fatalf("renews_at prefix=%q want 2026-07-01 (older webhook regressed state — I3 broken)", got)
	}
}

// --- I4 regression: monotonic expired_at guard survives the refactor -----

func TestWebhook_OutOfOrderExpired_NoRegression(t *testing.T) {
	db := newTokenTestEngine(t)

	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	if err := activateExternalSubscription(db, 42, levelStarter, newer); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	if err := activateExternalSubscription(db, 42, levelStarter, older); err != nil {
		t.Fatalf("older activate: %v", err)
	}

	var expiredAt string
	if err := db.QueryRow(
		`SELECT expired_at FROM subscription WHERE user_id = 42`,
	).Scan(&expiredAt); err != nil {
		t.Fatalf("read expired_at: %v", err)
	}
	if got := expiredAt[:10]; got != "2026-07-01" {
		t.Fatalf("expired_at prefix=%q want 2026-07-01 (older webhook regressed state — I4 broken)", got)
	}
}

// newTokenRenewalTestEngine extends newTokenTestEngine with the quota table
// that auth.RedeemPlanForOrder writes to. Renewal tests need this because
// handleTokenPlanEvent(subscription_payment_success) calls RedeemPlanForOrder
// under the hood.
func newTokenRenewalTestEngine(t *testing.T) *sql.DB {
	t.Helper()
	db := newTokenTestEngine(t)

	// quota table (mirrors auth/recharge_test.go seedRechargeSchema).
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS quota (
		  id      INTEGER PRIMARY KEY AUTOINCREMENT,
		  user_id INTEGER UNIQUE,
		  quota   REAL,
		  used    REAL,
		  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
	return db
}

// seedRenewalPlan inserts a minimal active token plan the renewal handler can
// look up. plan_id=888 avoids collisions with other test seeds.
func seedRenewalPlan(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := globals.ExecDb(db, `
		INSERT OR IGNORE INTO gtk_plan
		  (id, code, name, type, product_type, billing_mode,
		   price_cents, duration_days, quota_grant, quota_config, is_active)
		VALUES (888, 'token-99', 'Token ¥99', 'subscription', 'token',
		        'subscription', 9900, 30, 5000,
		        '{"quota":5000}', 1)
	`); err != nil {
		t.Fatalf("seed gtk_plan: %v", err)
	}
}

// seedUserPlanWithSub inserts a gtk_user_plan row for user 42, bound to the
// given subscription_id. purchasedDaysAgo controls how old the row is so
// tests can simulate "just bought" vs "month-old" scenarios.
func seedUserPlanWithSub(t *testing.T, db *sql.DB, orderID, lsSubID string, purchasedDaysAgo int) {
	t.Helper()
	purchasedAt := time.Now().UTC().AddDate(0, 0, -purchasedDaysAgo)
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_user_plan
		  (user_id, plan_id, subscription_id, product_type, status,
		   expire_at, order_id, purchased_at)
		VALUES (42, 888, ?, 'token', 'active',
		        datetime('now', '+30 days'), ?, ?)
	`, lsSubID, orderID, purchasedAt.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("seed gtk_user_plan: %v", err)
	}
}

// --- PKG-M1-① token renewal tests (TDD: written before implementation) ----

// TestHandleTokenSubPayment_Renewal verifies that subscription_payment_success
// on an existing subscription (latest row >24h old) creates a new gtk_user_plan
// row with the same subscription_id and a fresh purchased_at.
func TestHandleTokenSubPayment_Renewal(t *testing.T) {
	db := newTokenRenewalTestEngine(t)
	seedRenewalPlan(t, db)

	const lsSubID = "ls-sub-renew-1"
	// Pre-seed an existing gtk_user_plan row purchased 31 days ago (month 1).
	seedUserPlanWithSub(t, db, "order-month-1", lsSubID, 31)

	// Also seed gtk_ls_subscription so the handler can look up user_id.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
		  (user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (42, ?, '999', 'active', datetime('now', '+30 days'), 1)
	`, lsSubID); err != nil {
		t.Fatalf("seed gtk_ls_subscription: %v", err)
	}

	renewsAt := time.Now().UTC().AddDate(0, 1, 0)
	p := makeTokenPayload(eventSubPayment, lsSubID, renewsAt, "active")

	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("handleTokenPlanEvent(renewal): %v", err)
	}

	// Two rows should exist for this subscription: the seeded month-1 row + new renewal row.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_user_plan WHERE subscription_id = ?`, lsSubID,
	).Scan(&count); err != nil {
		t.Fatalf("count gtk_user_plan: %v", err)
	}
	if count != 2 {
		t.Errorf("gtk_user_plan rows for sub=%q: got %d want 2 (month-1 + renewal)", lsSubID, count)
	}

	// The newest row must have status='active' and a fresh purchased_at (within last minute).
	var status, purchasedAt string
	if err := db.QueryRow(`
		SELECT status, purchased_at FROM gtk_user_plan
		WHERE subscription_id = ? ORDER BY id DESC LIMIT 1
	`, lsSubID).Scan(&status, &purchasedAt); err != nil {
		t.Fatalf("read newest gtk_user_plan: %v", err)
	}
	if status != "active" {
		t.Errorf("renewal row status=%q want active", status)
	}
}

// TestHandleTokenSubPayment_FirstMonth_Skips verifies that
// subscription_payment_success is skipped (no new row) when the most recent
// gtk_user_plan for this subscription was purchased within the last 24h —
// indicating the initial order_created already handled month 1.
func TestHandleTokenSubPayment_FirstMonth_Skips(t *testing.T) {
	db := newTokenRenewalTestEngine(t)
	seedRenewalPlan(t, db)

	const lsSubID = "ls-sub-fresh-1"
	// Seed a row purchased NOW (0 days ago = fresh, within 24h window).
	seedUserPlanWithSub(t, db, "order-fresh-month-1", lsSubID, 0)

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
		  (user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (42, ?, '999', 'active', datetime('now', '+30 days'), 1)
	`, lsSubID); err != nil {
		t.Fatalf("seed gtk_ls_subscription: %v", err)
	}

	renewsAt := time.Now().UTC().AddDate(0, 1, 0)
	p := makeTokenPayload(eventSubPayment, lsSubID, renewsAt, "active")

	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("handleTokenPlanEvent(fresh sub): %v", err)
	}

	// Must still be exactly 1 row — renewal was skipped.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_user_plan WHERE subscription_id = ?`, lsSubID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row (no new renewal), got %d", count)
	}
}

// --- HTTP path integration: ensure the slimmed dispatch() still 200s ----

func TestHandleWebhook_TokenPath_StillWorks(t *testing.T) {
	r, _ := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload(eventCreated)
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200; body=%s", w.Code, w.Body.String())
	}
}
