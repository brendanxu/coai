// End-to-end webhook integration tests for the LS plan-redemption path.
//
// Why this file exists:
// The original v0.22 unit tests covered RedeemPlanForOrder idempotency
// (auth/recharge_test.go) AND a single-event LS webhook → redeem flow
// (TestHandleWebhook_PlanCustomDataRedeemsQuota). What they did NOT cover
// was the multi-event delivery pattern LemonSqueezy actually uses —
// order_created + subscription_created + subscription_payment_success
// all fire for a single first-month purchase with different data.id values.
// The dispatcher accepted all three. RedeemPlanForOrder's idempotency
// keyed on order_id couldn't dedup three different data.ids → triple
// credit grant (BL-01 in REVIEW.md 2026-05-13).
//
// This integration test driver mocks the LS multi-event delivery pattern
// + asserts end-to-end gtk_user_plan + quota state. Had this existed
// at v0.22.0 ship time, BL-01 would have been impossible to merge.
//
// The driver is intentionally minimal — no test fixtures library, no
// snapshot diffing. Just: build a payload, send it through the same
// /webhook/lemonsqueezy gin route the real LS uses, assert SQL state.
//
// New money-path features SHOULD add cases to TestLSPaymentLifecycle_*
// rather than reinventing payload construction.

package payment

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"chat/globals"
	"chat/plans"

	"github.com/spf13/viper"
)

// lsPayload builds a deterministic LS webhook payload for the plan-
// redemption path. eventName + dataID vary per call; everything else
// (custom_data.user_id / plan_code / type) stays stable, simulating
// the LS "same purchase, three events" delivery.
func lsPayload(eventName, dataID, planCode string, userID int64) []byte {
	p := map[string]interface{}{
		"meta": map[string]interface{}{
			"event_name": eventName,
			"test_mode":  true,
			"custom_data": map[string]interface{}{
				"type":      "plan",
				"plan_code": planCode,
				"user_id":   fmt.Sprintf("%d", userID),
			},
		},
		"data": map[string]interface{}{
			"id": dataID,
			"attributes": map[string]interface{}{
				"variant_id": 999,
				"status":     "paid",
				"renews_at":  "2026-06-12T12:00:00Z",
				"test_mode":  true,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// quotaCount reads the quota table to verify how many quota records exist
// for a given user. Used to assert "no extra credit grants" after sibling
// events.
func quotaCount(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	row := globals.QueryRowDb(db, `SELECT COUNT(*) FROM quota WHERE user_id = ?`, userID)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count quota: %v", err)
	}
	return n
}

// planRowCount reads gtk_user_plan to verify how many plan-bindings exist
// for a given user. Single-purchase = 1 row. Triple-redeem bug = 3 rows.
func planRowCount(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	row := globals.QueryRowDb(db,
		`SELECT COUNT(*) FROM gtk_user_plan WHERE user_id = ?`, userID)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count gtk_user_plan: %v", err)
	}
	return n
}

// quotaTotal reads quota.quota for the user (test schema has it as REAL).
func quotaTotal(t *testing.T, db *sql.DB, userID int64) float64 {
	t.Helper()
	var q float64
	row := globals.QueryRowDb(db,
		`SELECT quota FROM quota WHERE user_id = ?`, userID)
	if err := row.Scan(&q); err != nil {
		// No row = 0 quota
		return 0
	}
	return q
}

// TestLSPaymentLifecycle_TripleEventNoMultiCreditGrant is the BL-01
// regression guard. LS fires 3 events per first-month purchase with the
// SAME custom_data but 3 DIFFERENT data.id values. The dispatcher must
// redeem on order_created ONLY; the sibling events must ack without
// touching gtk_user_plan or quota.
//
// Without BL-01 fix: this test asserts 1 plan row, gets 3 → FAIL.
// With BL-01 fix:    1 plan row + 1 quota grant of 100000 → PASS.
func TestLSPaymentLifecycle_TripleEventNoMultiCreditGrant(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const userID int64 = 42
	const planCode = "starter-100k" // seeded by seedPlanRechargeTables
	const quotaPerRedeem = 100000.0 // seeded JSON: {"quota": 100000}

	// Simulate the actual LS delivery sequence: 3 events, 3 different
	// data.id values, same custom_data. Real LS doesn't guarantee order
	// but in practice order_created arrives first.
	events := []struct {
		eventName string
		dataID    string
	}{
		{"order_created", "ls-order-aaa-001"},
		{"subscription_created", "ls-sub-bbb-002"},
		{"subscription_payment_success", "ls-invoice-ccc-003"},
	}

	for _, e := range events {
		body := lsPayload(e.eventName, e.dataID, planCode, userID)
		req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
		req.Header.Set("X-Signature", computeHMAC(body, secret))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("event %s: got %d want 200; body=%s",
				e.eventName, w.Code, w.Body.String())
		}
	}

	// CRITICAL assertion: exactly 1 plan row (not 3).
	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("BL-01 regression: 1 purchase triggered %d gtk_user_plan rows "+
			"(expected 1). The dispatcher is redeeming on sibling events again.", got)
	}

	// CRITICAL assertion: quota grant happened exactly once.
	if got := quotaCount(t, db, userID); got != 1 {
		t.Fatalf("BL-01 regression: 1 purchase produced %d quota rows (expected 1)", got)
	}
	if got := quotaTotal(t, db, userID); got != quotaPerRedeem {
		t.Fatalf("BL-01 regression: quota = %f, want %f (triple-redeem multiplier "+
			"would yield %f)", got, quotaPerRedeem, 3*quotaPerRedeem)
	}
}

// TestLSPaymentLifecycle_RenewalCreatesFreshPlanRow asserts the OTHER
// expected behavior: when LS fires order_created for a SUBSEQUENT cycle
// (month 2 renewal), it SHOULD create a new gtk_user_plan row because
// the data.id is different.
//
// This pins the "Option A" choice from the BL-01 fix design (drive on
// order_created which fires every renewal cycle) — if a future cleanup
// switches to "drive on subscription_payment_success", this test catches
// the renewal-loss regression.
func TestLSPaymentLifecycle_RenewalCreatesFreshPlanRow(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const userID int64 = 73
	const planCode = "starter-100k"

	// Month 1 purchase — one order_created event.
	month1 := lsPayload("order_created", "ls-order-month1-aaa", planCode, userID)
	req1 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(month1))
	req1.Header.Set("X-Signature", computeHMAC(month1, secret))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("month 1 order_created got %d want 200", w1.Code)
	}

	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("month 1: got %d plan rows, want 1", got)
	}

	// Month 2 renewal — another order_created, different data.id.
	// LS sends each renewal as a fresh "order_created" event with a
	// new order UUID.
	month2 := lsPayload("order_created", "ls-order-month2-bbb", planCode, userID)
	req2 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(month2))
	req2.Header.Set("X-Signature", computeHMAC(month2, secret))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("month 2 order_created got %d want 200", w2.Code)
	}

	// Renewal MUST create a new plan row.
	if got := planRowCount(t, db, userID); got != 2 {
		t.Fatalf("month 2 renewal: got %d plan rows, want 2 (renewal should "+
			"create a fresh gtk_user_plan binding)", got)
	}
}

// TestLSPaymentLifecycle_DuplicateOrderCreatedIsIdempotent asserts that
// LS retrying the SAME order_created event (same data.id) does NOT
// double-redeem. This is the inverse of the BL-01 multi-event case —
// here the event is identical, not sibling. Webhook idempotency via the
// SHA256 dedup table OR via gtk_user_plan.order_id UNIQUE.
//
// Both paths combined: the test asserts robustness against either
// idempotency mechanism failing.
func TestLSPaymentLifecycle_DuplicateOrderCreatedIsIdempotent(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const userID int64 = 99
	const planCode = "starter-100k"

	body := lsPayload("order_created", "ls-order-dup-001", planCode, userID)

	// Deliver the same payload twice (LS retry behavior).
	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
		req.Header.Set("X-Signature", computeHMAC(body, secret))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("attempt %d: got %d want 200", attempt, w.Code)
		}
	}

	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("duplicate webhook: got %d rows, want 1 (idempotency broken)", got)
	}
}

// _ keeps the plans import live for future test cases. Remove this once
// any TestLSPaymentLifecycle_* test actually references plans.* directly
// (e.g., to assert plan-lookup behavior pre/post redeem).
var _ = plans.ErrPlanNotFound
