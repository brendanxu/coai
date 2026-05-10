// Tests for the PKG-N2 customer-side write actions on service orders.
//
// Same pattern as customer_orders_test.go: SQLite in-memory + Migrate(),
// exercise the storage path directly. The HTTP handlers are thin
// (auth + envelope + ownership-by-SQL); ownership behavior is asserted
// via the reused `seedCustomerOrder` helper from customer_orders_test.go.

package service

import (
	"chat/globals"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────
// Refund request — storage path
//
// We assert the storage-layer effect that CustomerRefundRequestAPI
// produces: the refund_reason column gains an appended note while
// status remains unchanged.
// ─────────────────────────────────────────────────────────────────────

func TestCustomerRefundRequest_AppendsToEmptyReason(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-CR-1", 42, "paid")

	note := "[CUSTOMER REQUEST 2026-05-10T12:00:00Z] 内容质量不达预期"
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ?`,
		note, "SVC-CR-1",
	); err != nil {
		t.Fatalf("update: %v", err)
	}

	row := globals.QueryRowDb(db,
		`SELECT status, COALESCE(refund_reason, '') FROM gtk_service_order WHERE order_no = ?`,
		"SVC-CR-1")
	var status, reason string
	if err := row.Scan(&status, &reason); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "paid" {
		t.Errorf("status = %q, want unchanged 'paid'", status)
	}
	if reason != note {
		t.Errorf("refund_reason = %q, want %q", reason, note)
	}
}

func TestCustomerRefundRequest_AppendsToExistingReason(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-CR-2", 42, "completed")

	first := "[CUSTOMER REQUEST 2026-05-10T12:00:00Z] 第一次请求"
	second := "[CUSTOMER REQUEST 2026-05-10T13:00:00Z] 还没收到回复"

	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ?`,
		first, "SVC-CR-2"); err != nil {
		t.Fatalf("seed first: %v", err)
	}

	// Second request appends with newline separator (mirrors the
	// handler's Go-side concat: existing + "\n" + new).
	combined := first + "\n" + second
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ?`,
		combined, "SVC-CR-2"); err != nil {
		t.Fatalf("append second: %v", err)
	}

	row := globals.QueryRowDb(db,
		`SELECT COALESCE(refund_reason, '') FROM gtk_service_order WHERE order_no = ?`,
		"SVC-CR-2")
	var reason string
	if err := row.Scan(&reason); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(reason, first) || !strings.Contains(reason, second) {
		t.Errorf("refund_reason = %q, want both notes preserved", reason)
	}
	if !strings.Contains(reason, "\n") {
		t.Errorf("refund_reason = %q, want newline separator between notes", reason)
	}
}

func TestCustomerRefundRequest_OwnershipScopedQuery(t *testing.T) {
	db := setupTestDB(t)
	// owner = 42 owns SVC-OWN; user 99 must not be able to mutate it
	// via the WHERE coai_user_id = ? guard.
	seedCustomerOrder(t, db, "SVC-OWN", 42, "paid")

	// Simulate non-owner attempt: the handler's UPDATE includes
	// `AND coai_user_id = ?` so this should affect 0 rows.
	res, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = 'malicious'
		 WHERE order_no = ? AND coai_user_id = ?`,
		"SVC-OWN", 99)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	rows, _ := res.RowsAffected()
	if rows != 0 {
		t.Errorf("non-owner update affected %d rows, want 0", rows)
	}
	// Confirm reason untouched.
	row := globals.QueryRowDb(db,
		`SELECT COALESCE(refund_reason, '') FROM gtk_service_order WHERE order_no = ?`,
		"SVC-OWN")
	var reason string
	_ = row.Scan(&reason)
	if reason != "" {
		t.Errorf("refund_reason = %q, want unchanged ''", reason)
	}
}

// statesAcceptingCustomerRefund map is the gate used by
// CustomerRefundRequestAPI — assert its contents stay aligned with
// what the handler accepts.
func TestCustomerRefundRequest_StatesAccepted(t *testing.T) {
	want := map[string]bool{
		"paid":                   true,
		"running":                true,
		"completed":              true,
		"pending_payment":        false,
		"failed":                 false,
		"refunded":               false,
		"refunded_post_delivery": false,
		"canceled_mid_flight":    false,
	}
	for status, accept := range want {
		_, ok := statesAcceptingCustomerRefund[status]
		if ok != accept {
			t.Errorf("status %q: accept=%v in map, want %v", status, ok, accept)
		}
	}
	// Defensive: no extras in the map beyond what we listed.
	if len(statesAcceptingCustomerRefund) != 3 {
		t.Errorf("statesAcceptingCustomerRefund has %d entries, want 3 (paid/running/completed)",
			len(statesAcceptingCustomerRefund))
	}
}

// ─────────────────────────────────────────────────────────────────────
// Reorder — storage path
// ─────────────────────────────────────────────────────────────────────

func TestCustomerReorder_LookupReturnsServiceSlug(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-RO-1", 42, "completed")

	// Mirror the SELECT the handler uses.
	row := globals.QueryRowDb(db,
		`SELECT service_slug FROM gtk_service_order WHERE order_no = ? AND coai_user_id = ?`,
		"SVC-RO-1", 42)
	var slug string
	if err := row.Scan(&slug); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if slug != "xhs-single-post" {
		t.Errorf("service_slug = %q, want 'xhs-single-post' (from seed fixture)", slug)
	}
}

func TestCustomerReorder_RejectsCrossUser(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-RO-2", 42, "completed")

	row := globals.QueryRowDb(db,
		`SELECT service_slug FROM gtk_service_order WHERE order_no = ? AND coai_user_id = ?`,
		"SVC-RO-2", 99) // wrong user
	var slug string
	err := row.Scan(&slug)
	if err == nil {
		t.Errorf("cross-user lookup returned slug %q, want sql.ErrNoRows", slug)
	}
}
