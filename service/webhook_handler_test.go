package service

import (
	"chat/globals"
	"database/sql"
	"strings"
	"testing"
)

// seedWebhookFixture creates an agent + service + order in given status
// + ls_order_id. Returns the gtk_service.id of the seeded service for
// callers needing it.
func seedWebhookFixture(t *testing.T, db *sql.DB, orderNo, status, lsOrderID string) {
	t.Helper()
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('a', 'A', 'p', 'm', 'active')
	`)
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
			price_cny_cents, billing_type, status)
		VALUES ('s', 'S', 'diy_agent', 'a', 1900, 'one_time', 'active')
	`)
	if lsOrderID != "" {
		if _, err := globals.ExecDb(db, `
			INSERT INTO gtk_service_order
				(order_no, coai_user_id, service_id, service_slug,
				 price_cny_cents_paid, payment_provider, ls_order_id, status)
			VALUES (?, 1, 1, 's', 1900, 'lemonsqueezy', ?, ?)
		`, orderNo, lsOrderID, status); err != nil {
			t.Fatalf("seed order: %v", err)
		}
	} else {
		if _, err := globals.ExecDb(db, `
			INSERT INTO gtk_service_order
				(order_no, coai_user_id, service_id, service_slug,
				 price_cny_cents_paid, payment_provider, status)
			VALUES (?, 1, 1, 's', 1900, 'lemonsqueezy', ?)
		`, orderNo, status); err != nil {
			t.Fatalf("seed order: %v", err)
		}
	}
}

func TestMarkOrderPaid_HappyPathLemonSqueezy(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-A", "pending_payment", "")

	if err := MarkOrderPaid(db, "SVC-A", "ls-order-123", "lemonsqueezy"); err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}

	var status, lsOrderID string
	if err := db.QueryRow(`SELECT status, ls_order_id FROM gtk_service_order WHERE order_no='SVC-A'`).
		Scan(&status, &lsOrderID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paid" {
		t.Errorf("status=%s, want paid", status)
	}
	if lsOrderID != "ls-order-123" {
		t.Errorf("ls_order_id=%s", lsOrderID)
	}
}

func TestMarkOrderPaid_HappyPathHupijiao(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-HJ", "pending_payment", "")

	if err := MarkOrderPaid(db, "SVC-HJ", "hupijiao-tx-xyz", "hupijiao"); err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}

	var status, hupijiaoTradeNo string
	if err := db.QueryRow(`SELECT status, COALESCE(hupijiao_trade_no, '') FROM gtk_service_order WHERE order_no='SVC-HJ'`).
		Scan(&status, &hupijiaoTradeNo); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paid" {
		t.Errorf("status=%s", status)
	}
	if hupijiaoTradeNo != "hupijiao-tx-xyz" {
		t.Errorf("hupijiao_trade_no=%s", hupijiaoTradeNo)
	}
}

func TestMarkOrderPaid_IdempotentDoubleCall(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-DUP", "pending_payment", "")

	if err := MarkOrderPaid(db, "SVC-DUP", "ls-order-1", "lemonsqueezy"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call — same external id. Should be no-op without error.
	if err := MarkOrderPaid(db, "SVC-DUP", "ls-order-1", "lemonsqueezy"); err != nil {
		t.Errorf("second call should be idempotent: %v", err)
	}
}

func TestMarkOrderPaid_RefusesRefundedOrder(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-REF", "refunded", "")

	err := MarkOrderPaid(db, "SVC-REF", "ls-order-x", "lemonsqueezy")
	if err == nil {
		t.Error("expected error for refunded order")
	}
	if err != nil && !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should explain terminal state: %v", err)
	}
}

func TestMarkOrderPaid_RefusesFailedOrder(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-F", "failed", "")
	if err := MarkOrderPaid(db, "SVC-F", "x", "lemonsqueezy"); err == nil {
		t.Error("expected error for failed order")
	}
}

func TestMarkOrderPaid_RefusesUnknownProvider(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-X", "pending_payment", "")
	if err := MarkOrderPaid(db, "SVC-X", "id", "stripe"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestMarkOrderPaid_RefusesMissingOrder(t *testing.T) {
	db := setupTestDB(t)
	if err := MarkOrderPaid(db, "SVC-NONEXIST", "id", "lemonsqueezy"); err == nil {
		t.Error("expected error for missing order")
	}
}

func TestMarkOrderPaid_RefusesEmptyOrderNo(t *testing.T) {
	db := setupTestDB(t)
	if err := MarkOrderPaid(db, "", "id", "lemonsqueezy"); err == nil {
		t.Error("expected error for empty order_no")
	}
}

func TestLinkSubscription_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-SUB", "paid", "ls-order-1")

	if err := LinkSubscription(db, "SVC-SUB", 42); err != nil {
		t.Fatalf("LinkSubscription: %v", err)
	}

	var subID sql.NullInt64
	if err := db.QueryRow(`SELECT subscription_id FROM gtk_service_order WHERE order_no='SVC-SUB'`).
		Scan(&subID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !subID.Valid || subID.Int64 != 42 {
		t.Errorf("subscription_id should be 42, got %v", subID)
	}
}

func TestLinkSubscription_IdempotentSecondCall(t *testing.T) {
	db := setupTestDB(t)
	seedWebhookFixture(t, db, "SVC-SUB2", "paid", "ls-order-1")

	if err := LinkSubscription(db, "SVC-SUB2", 1); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// Second link — different id. Should be no-op (we only set when NULL).
	if err := LinkSubscription(db, "SVC-SUB2", 999); err != nil {
		t.Fatalf("second link: %v", err)
	}

	var subID int64
	if err := db.QueryRow(`SELECT subscription_id FROM gtk_service_order WHERE order_no='SVC-SUB2'`).
		Scan(&subID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if subID != 1 {
		t.Errorf("subscription_id should still be 1 (idempotent), got %d", subID)
	}
}

func TestLinkSubscription_RejectsZero(t *testing.T) {
	db := setupTestDB(t)
	if err := LinkSubscription(db, "SVC-X", 0); err == nil {
		t.Error("expected error for zero subID")
	}
}

func TestHupijiaoVerifyHash_DeterministicAndSensitive(t *testing.T) {
	params := map[string]string{
		"appid":          "merchant1",
		"trade_order_id": "SVC-ABCD",
		"total_fee":      "19.00",
		"status":         "OD",
	}
	h1 := hupijiaoVerifyHash(params, "secret")
	h2 := hupijiaoVerifyHash(params, "secret")
	if h1 != h2 {
		t.Error("hash must be deterministic")
	}

	// Different secret → different hash.
	if h1 == hupijiaoVerifyHash(params, "differentsecret") {
		t.Error("hash should depend on secret")
	}

	// Different param value → different hash.
	params2 := map[string]string{
		"appid":          "merchant1",
		"trade_order_id": "SVC-ABCD",
		"total_fee":      "299.00", // changed
		"status":         "OD",
	}
	if h1 == hupijiaoVerifyHash(params2, "secret") {
		t.Error("hash should be sensitive to param values")
	}
}
