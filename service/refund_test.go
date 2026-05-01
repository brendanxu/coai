package service

import (
	"chat/globals"
	"database/sql"
	"errors"
	"testing"
)

// seedRefundFixture inserts an agent + service + order with the given
// initial status, returns the order_no for tests to act on.
func seedRefundFixture(t *testing.T, db *sql.DB, orderNo, status string) {
	t.Helper()
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('a', 'A', 'p', 'm', 'active')
		ON CONFLICT(slug) DO NOTHING
	`); err != nil {
		// If ON CONFLICT not supported (rare), ignore the duplicate.
		// Test should still proceed.
		t.Logf("seed agent (may already exist): %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
			price_cny_cents, billing_type, status)
		VALUES ('s', 'S', 'diy_agent', 'a', 1900, 'one_time', 'active')
		ON CONFLICT(slug) DO NOTHING
	`); err != nil {
		t.Logf("seed service (may already exist): %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order
			(order_no, coai_user_id, service_id, service_slug,
			 price_cny_cents_paid, payment_provider, status)
		VALUES (?, 1, 1, 's', 1900, 'lemonsqueezy', ?)
	`, orderNo, status); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func TestRefundOrder_FlipsPaidToRefunded(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-PAID", "paid")

	res, err := refundOrder(db, "SVC-PAID", "customer changed mind", 0)
	if err != nil {
		t.Fatalf("refundOrder: %v", err)
	}
	if res.AlreadyRefunded {
		t.Error("first refund should NOT be marked already_refunded")
	}

	var status, reason string
	if err := db.QueryRow(
		`SELECT status, refund_reason FROM gtk_service_order WHERE order_no=?`, "SVC-PAID",
	).Scan(&status, &reason); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "refunded" {
		t.Errorf("status = %q, want refunded", status)
	}
	if reason != "customer changed mind" {
		t.Errorf("reason = %q", reason)
	}
}

func TestRefundOrder_IdempotentSameReason(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-DUP", "paid")

	if _, err := refundOrder(db, "SVC-DUP", "reason A", 0); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	res, err := refundOrder(db, "SVC-DUP", "reason A", 0)
	if err != nil {
		t.Fatalf("second refund: %v", err)
	}
	if !res.AlreadyRefunded {
		t.Error("second call should report already_refunded=true")
	}
}

func TestRefundOrder_IdempotentReasonUpdate(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-NOTE", "paid")

	if _, err := refundOrder(db, "SVC-NOTE", "reason A", 0); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	res, err := refundOrder(db, "SVC-NOTE", "updated: refund per founder review", 0)
	if err != nil {
		t.Fatalf("second refund: %v", err)
	}
	if !res.AlreadyRefunded {
		t.Error("expected already_refunded=true on reason update")
	}

	var reason string
	if err := db.QueryRow(
		`SELECT refund_reason FROM gtk_service_order WHERE order_no=?`, "SVC-NOTE",
	).Scan(&reason); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if reason != "updated: refund per founder review" {
		t.Errorf("reason not updated: %q", reason)
	}
}

func TestRefundOrder_RejectsFailedStatus(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-FAIL", "failed")
	_, err := refundOrder(db, "SVC-FAIL", "trying", 0)
	if !errors.Is(err, ErrOrderNotRefundable) {
		t.Errorf("want ErrOrderNotRefundable, got %v", err)
	}
}

func TestRefundOrder_NotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := refundOrder(db, "SVC-MISSING", "reason", 0)
	if !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("want ErrOrderNotFound, got %v", err)
	}
}

func TestRefundOrder_AllowsRefundOfCompletedOrder(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-DONE", "completed")
	if _, err := refundOrder(db, "SVC-DONE", "dispute despite delivery", 0); err != nil {
		t.Errorf("completed order should be refundable (founder discretion): %v", err)
	}
}

func TestRefundOrder_AllowsRefundOfPendingPayment(t *testing.T) {
	db := setupTestDB(t)
	seedRefundFixture(t, db, "SVC-PEND", "pending_payment")
	if _, err := refundOrder(db, "SVC-PEND", "customer abandoned, cleanup", 0); err != nil {
		t.Errorf("pending_payment should be refundable as cleanup: %v", err)
	}
	var status string
	if err := db.QueryRow(
		`SELECT status FROM gtk_service_order WHERE order_no=?`, "SVC-PEND",
	).Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "refunded" {
		t.Errorf("status = %q, want refunded", status)
	}
}
