// m1_l2_renewal_test.go — PKG-M1-①-L2-followup integration tests.
//
// Validates the L2 token-plan renewal handler (handleL2TokenRenewal) added to
// close the gap identified in M1-①: the L2 dispatch branch in
// payment/lemonsqueezy.go was silently acking subscription_payment_success
// for month-2+ renewals. LS does not re-fire order_created on renewals, so
// without this fix L2 customers never received renewed credits past month 1.
//
// Three scenarios validated (mirrors the M1-① test pattern):
//
//	Test 1 (L2FirstMonth): order_created creates the initial gtk_user_plan row
//	        via the existing dispatch() L2 branch — unchanged behavior.
//
//	Test 2 (L2Renewal): 31 days later, subscription_payment_success arrives →
//	        new gtk_user_plan row created via handleL2TokenRenewal.
//
//	Test 3 (L2SameDayDup): subscription_payment_success fires within 24h of
//	        order_created (BL-01 first-month multi-fire pattern) → skip,
//	        no double row.
//
// All tests drive through the full HTTP path (same gin route as real LS) so
// the dispatch() routing is exercised end-to-end. userID=42 is used
// throughout because that is the only auth row seeded by newTestEngine.

package payment

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"chat/globals"

	"github.com/spf13/viper"
)

// buildL2OrderCreatedBody builds an order_created payload for the L2 path
// (custom_data carries type="plan" + plan_code + user_id).
func buildL2OrderCreatedBody(orderID, planCode string, userID int64) []byte {
	return lsPayload("order_created", orderID, planCode, userID)
}

// buildL2SubPaymentBody builds a subscription_payment_success payload for the
// L2 path. lsInvoiceID is the data.id of the invoice (unique per renewal
// cycle). userID must match the user_id embedded in the original
// order_created payload so the same gtk_user_plan rows are found.
func buildL2SubPaymentBody(lsInvoiceID, planCode string, userID int64) []byte {
	return lsPayload("subscription_payment_success", lsInvoiceID, planCode, userID)
}

// --- Test 1: L2 first-month — order_created creates the initial row ----------

// TestM1L2_FirstMonthOrderCreated verifies that the existing L2 dispatch
// behavior is unchanged: order_created with plan custom_data calls
// auth.RedeemPlanForOrder and creates exactly one gtk_user_plan row.
func TestM1L2_FirstMonthOrderCreated(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const (
		userID   int64 = 42 // seeded by newTestEngine
		planCode       = "starter-100k"
		orderID        = "ls-l2-order-month1-t1"
	)

	body := buildL2OrderCreatedBody(orderID, planCode, userID)
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("order_created: got %d want 200; body=%s", w.Code, w.Body.String())
	}

	// Exactly one gtk_user_plan row.
	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("L2 first-month: want 1 plan row, got %d", got)
	}

	// Quota was granted exactly once.
	if got := quotaCount(t, db, userID); got != 1 {
		t.Fatalf("L2 first-month: want 1 quota row, got %d", got)
	}
}

// --- Test 2: L2 renewal — subscription_payment_success creates new row -------

// TestM1L2_RenewalCreatesNewRow verifies the core fix: when
// subscription_payment_success arrives 31+ days after the initial
// order_created (month 2 renewal), handleL2TokenRenewal creates a new
// gtk_user_plan row so the customer's credits are topped up.
func TestM1L2_RenewalCreatesNewRow(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const (
		userID    int64 = 42 // seeded by newTestEngine
		planCode        = "starter-100k"
		orderID         = "ls-l2-order-month1-t2"
		invoiceID       = "ls-l2-invoice-month2-t2"
	)

	// Step 1: month-1 purchase via order_created.
	body1 := buildL2OrderCreatedBody(orderID, planCode, userID)
	req1 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body1))
	req1.Header.Set("X-Signature", computeHMAC(body1, secret))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("month-1 order_created: got %d", w1.Code)
	}
	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("after month-1: want 1 row, got %d", got)
	}

	// Back-date the initial row's purchased_at to simulate 31 days elapsed.
	purchasedAt31DaysAgo := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02 15:04:05")
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_user_plan SET purchased_at = ? WHERE user_id = ?`,
		purchasedAt31DaysAgo, userID,
	); err != nil {
		t.Fatalf("back-date gtk_user_plan: %v", err)
	}

	// Step 2: month-2 renewal via subscription_payment_success.
	body2 := buildL2SubPaymentBody(invoiceID, planCode, userID)
	req2 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body2))
	req2.Header.Set("X-Signature", computeHMAC(body2, secret))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("month-2 subscription_payment_success: got %d; body=%s", w2.Code, w2.Body.String())
	}

	// Expect 2 rows: month-1 + renewal.
	if got := planRowCount(t, db, userID); got != 2 {
		t.Fatalf("L2 renewal: want 2 plan rows (month-1 + renewal), got %d", got)
	}
}

// --- Test 3: L2 same-day dup — subscription_payment_success within 24h skips -

// TestM1L2_SameDaySubPaymentSkips verifies the BL-01 guard for the L2 path:
// when subscription_payment_success fires within 24h of order_created
// (normal LS first-month multi-fire), handleL2TokenRenewal detects that the
// latest gtk_user_plan row is fresh (<24h) and skips — no double row.
func TestM1L2_SameDaySubPaymentSkips(t *testing.T) {
	r, db := newTestEngine(t)
	seedPlanRechargeTables(t, db)

	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	const (
		userID    int64 = 42 // seeded by newTestEngine
		planCode        = "starter-100k"
		orderID         = "ls-l2-order-month1-t3"
		invoiceID       = "ls-l2-invoice-sameday-t3"
	)

	// Step 1: order_created — creates fresh row (purchased_at = CURRENT_TIMESTAMP).
	body1 := buildL2OrderCreatedBody(orderID, planCode, userID)
	req1 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body1))
	req1.Header.Set("X-Signature", computeHMAC(body1, secret))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("order_created: got %d", w1.Code)
	}
	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("after order_created: want 1 row, got %d", got)
	}

	// Step 2: subscription_payment_success fires immediately (same-day sibling).
	// purchased_at on the existing row is CURRENT_TIMESTAMP → within 24h → skip.
	body2 := buildL2SubPaymentBody(invoiceID, planCode, userID)
	req2 := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body2))
	req2.Header.Set("X-Signature", computeHMAC(body2, secret))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("subscription_payment_success: got %d; body=%s", w2.Code, w2.Body.String())
	}

	// Must still be exactly 1 row — renewal was skipped (BL-01 guard).
	if got := planRowCount(t, db, userID); got != 1 {
		t.Fatalf("L2 same-day dup: want 1 row (BL-01 skip), got %d", got)
	}
}
