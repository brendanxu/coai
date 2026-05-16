// m1_renewal_integration_test.go — PKG-M1-① end-to-end renewal coverage.
//
// Tests in this file exercise the full "initial purchase → time passes →
// renewal webhook → new row created" lifecycle for both token and service
// product paths, plus:
//   - Legacy back-fill: pre-PKG-M1 rows with NULL subscription_id get
//     populated on first renewal.
//   - Double-fire idempotency: firing the same renewal webhook body twice
//     creates only one new row (gtk_webhook_event SHA256 outer dedup OR
//     gtk_user_plan.order_id UNIQUE inner dedup).
//
// These tests use the same SQLite in-memory engines as the unit tests
// (newTokenRenewalTestEngine / newServiceTestEngine) so no real DB is needed.

package payment

import (
	"chat/globals"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

// buildSubPaymentBody constructs a minimal subscription_payment_success JSON
// body that HandleWebhook can parse (used for idempotency / double-fire tests
// that exercise the full HTTP path via dispatch()).
func buildSubPaymentBody(lsSubID, orderNo, planCode, renewsAt string) []byte {
	custom := map[string]interface{}{
		"user_id": "42",
	}
	if orderNo != "" {
		custom["greentokey_order_no"] = orderNo
	}
	if planCode != "" {
		custom["type"] = "plan"
		custom["plan_code"] = planCode
	}
	payload := map[string]interface{}{
		"meta": map[string]interface{}{
			"event_name":  "subscription_payment_success",
			"test_mode":   true,
			"custom_data": custom,
		},
		"data": map[string]interface{}{
			"id": lsSubID,
			"attributes": map[string]interface{}{
				"variant_id": 999,
				"status":     "active",
				"renews_at":  renewsAt,
				"test_mode":  true,
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

// sha256HexOf returns the SHA-256 hex of b (mirrors lemonsqueezy.go sha256Hex).
func sha256HexOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// --- Test 1: Token renewal full lifecycle --------------------------------

// TestM1_TokenRenewalLifecycle verifies the complete flow:
//  1. Seed a month-1 gtk_user_plan (31 days old, subscription_id set).
//  2. Fire subscription_payment_success.
//  3. Assert a new gtk_user_plan row appears with status='active' and
//     the same subscription_id.
func TestM1_TokenRenewalLifecycle(t *testing.T) {
	db := newTokenRenewalTestEngine(t)
	seedRenewalPlan(t, db)

	const lsSubID = "ls-m1-tok-lifecycle"

	// Month 1 row — 31 days old.
	seedUserPlanWithSub(t, db, "order-m1-tok-lc-1", lsSubID, 31)

	// Seed mapping row so userIDFromCustomData can resolve.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
		  (user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (42, ?, '999', 'active', datetime('now','+30 days'), 1)
	`, lsSubID); err != nil {
		t.Fatalf("seed gtk_ls_subscription: %v", err)
	}

	renewsAt := time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339)
	p := makeTokenPayload(eventSubPayment, lsSubID,
		time.Now().UTC().AddDate(0, 1, 0), "active")
	_ = renewsAt

	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("handleTokenPlanEvent(renewal): %v", err)
	}

	// Expect 2 rows: month-1 + renewal.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_user_plan WHERE subscription_id = ?`, lsSubID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("token lifecycle: want 2 rows, got %d", count)
	}

	// Renewal row must be active.
	var status string
	if err := db.QueryRow(`
		SELECT status FROM gtk_user_plan
		WHERE subscription_id = ? ORDER BY id DESC LIMIT 1
	`, lsSubID).Scan(&status); err != nil {
		t.Fatalf("read newest: %v", err)
	}
	if status != "active" {
		t.Errorf("renewal row status=%q want active", status)
	}
}

// --- Test 2: Service renewal full lifecycle ------------------------------

// TestM1_ServiceRenewalLifecycle verifies the complete service order renewal:
//  1. Seed a month-1 gtk_service_order (31 days old, subscription_id set).
//  2. Fire subscription_payment_success.
//  3. Assert a new gtk_service_order row appears with status='paid'.
func TestM1_ServiceRenewalLifecycle(t *testing.T) {
	db := newServiceTestEngine(t)

	const (
		origOrderNo = "SVC-M1-LC-001"
		lsSubID     = "ls-m1-svc-lifecycle"
	)

	// Month 1 — 31 days old.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, subscription_id,
		   ls_order_id, status, paid_at, created_at)
		VALUES (?, 1, 1, 'svc-stub', 198000, 'lemonsqueezy', ?,
		        'ls-order-m1-lc', 'paid', datetime('now','-31 days'),
		        datetime('now','-31 days'))
	`, origOrderNo, lsSubID); err != nil {
		t.Fatalf("seed month-1 order: %v", err)
	}

	p := makeServicePayload(eventSubPayment, lsSubID, origOrderNo, "")
	if err := handleServiceEvent(db, p, origOrderNo); err != nil {
		t.Fatalf("handleServiceEvent(renewal): %v", err)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_service_order WHERE subscription_id = ?`, lsSubID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("service lifecycle: want 2 rows, got %d", count)
	}

	var newStatus string
	if err := db.QueryRow(`
		SELECT status FROM gtk_service_order
		WHERE subscription_id = ? ORDER BY id DESC LIMIT 1
	`, lsSubID).Scan(&newStatus); err != nil {
		t.Fatalf("read newest: %v", err)
	}
	if newStatus != "paid" {
		t.Errorf("renewal status=%q want paid", newStatus)
	}
}

// --- Test 3: Legacy back-fill (token) -----------------------------------

// TestM1_TokenLegacyBackfill verifies that a pre-PKG-M1 gtk_user_plan row
// (subscription_id=NULL) gets its subscription_id populated on first renewal.
func TestM1_TokenLegacyBackfill(t *testing.T) {
	db := newTokenRenewalTestEngine(t)
	seedRenewalPlan(t, db)

	const lsSubID = "ls-m1-tok-backfill"

	// Legacy row: subscription_id IS NULL, purchased 31 days ago.
	purchasedAt := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02 15:04:05")
	var legacyID int64
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_user_plan
		  (user_id, plan_id, subscription_id, product_type, status,
		   expire_at, order_id, purchased_at)
		VALUES (42, 888, NULL, 'token', 'active',
		        datetime('now','+30 days'), 'order-legacy-tok', ?)
	`, purchasedAt); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM gtk_user_plan WHERE order_id='order-legacy-tok'`).Scan(&legacyID); err != nil {
		t.Fatalf("read legacy id: %v", err)
	}

	// Seed gtk_ls_subscription mapping for userIDFromCustomData.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
		  (user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (42, ?, '999', 'active', datetime('now','+30 days'), 1)
	`, lsSubID); err != nil {
		t.Fatalf("seed gtk_ls_subscription: %v", err)
	}

	p := makeTokenPayload(eventSubPayment, lsSubID,
		time.Now().UTC().AddDate(0, 1, 0), "active")

	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("handleTokenPlanEvent(backfill): %v", err)
	}

	// The legacy row must now have subscription_id populated.
	var subIDAfter interface{}
	if err := db.QueryRow(
		`SELECT subscription_id FROM gtk_user_plan WHERE id = ?`, legacyID,
	).Scan(&subIDAfter); err != nil {
		t.Fatalf("read legacy row after: %v", err)
	}
	if subIDAfter == nil {
		t.Error("legacy row subscription_id still NULL after renewal back-fill")
	}

	// Also expect a new renewal row (total = 2: legacy + renewal).
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_user_plan WHERE user_id = 42 AND product_type = 'token'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("back-fill lifecycle: want 2 rows, got %d", count)
	}
}

// --- Test 4: Double-fire idempotency ------------------------------------

// TestM1_TokenRenewalIdempotency verifies that firing the same
// subscription_payment_success webhook twice (LS retry) does NOT create two
// new gtk_user_plan rows. The inner dedup key (order_id UNIQUE) catches the
// second call even if the outer gtk_webhook_event row is bypassed at the
// direct-handler level.
func TestM1_TokenRenewalIdempotency(t *testing.T) {
	db := newTokenRenewalTestEngine(t)
	seedRenewalPlan(t, db)

	const lsSubID = "ls-m1-tok-idem"

	// Month-1 row, 31 days old.
	seedUserPlanWithSub(t, db, "order-m1-tok-idem-1", lsSubID, 31)

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
		  (user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (42, ?, '999', 'active', datetime('now','+30 days'), 1)
	`, lsSubID); err != nil {
		t.Fatalf("seed gtk_ls_subscription: %v", err)
	}

	p := makeTokenPayload(eventSubPayment, lsSubID,
		time.Now().UTC().AddDate(0, 1, 0), "active")

	// Fire twice — should be idempotent.
	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("first renewal: %v", err)
	}
	if err := handleTokenPlanEvent(db, p); err != nil {
		t.Fatalf("second renewal (idempotent): %v", err)
	}

	// Still exactly 2 rows (month-1 + one renewal, not month-1 + two renewals).
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM gtk_user_plan WHERE subscription_id = ?`, lsSubID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("idempotency: want 2 rows after double-fire, got %d", count)
	}
}
