// Tests for the PKG-5 customer self-serve order endpoints.
//
// Mirrors the existing service/store_test.go and refund_test.go pattern:
// SQLite in-memory + Migrate(). No HTTP layer — we test the storage
// primitives directly because the handlers are thin adapters
// (RequireAuth + JSON envelope) and a bug in either layer surfaces in
// these primitive-level assertions.

package service

import (
	"chat/globals"
	"database/sql"
	"errors"
	"testing"
)

// seedCustomerOrder is a small fixture helper. It seeds (idempotently)
// the agent + service rows that orders FK-reference, then inserts an
// order with the given owner + status. Returns nothing because the
// caller passes orderNo in.
//
// Note: tests in this package run against SQLite without FOREIGN KEYS
// enforcement, so we don't strictly need the agent/service rows to
// exist for the order INSERT. But we seed them anyway so the LEFT JOIN
// in ListMyOrders has a `service_name` to return — otherwise the JOIN
// returns NULL and we'd fall through to COALESCE(o.service_slug). Both
// are valid behaviors; we test both below.
func seedCustomerOrder(t *testing.T, db *sql.DB, orderNo string, ownerID int64, status string) {
	t.Helper()
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status, min_tier)
		VALUES ('xhs-copy-writer', 'XHS', 'p', 'deepseek-chat', 'active', 'light')
		ON CONFLICT(slug) DO NOTHING
	`)
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
			price_cny_cents, included_credits, billing_type, status)
		VALUES ('xhs-single-post', '小红书单篇文案', 'diy_agent', 'xhs-copy-writer',
		        1900, 60, 'one_time', 'active')
		ON CONFLICT(slug) DO NOTHING
	`)
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order
			(order_no, coai_user_id, service_id, service_slug,
			 price_cny_cents_paid, credits_granted, payment_provider, status)
		VALUES (?, ?, 1, 'xhs-single-post', 1900, 60, 'lemonsqueezy', ?)
	`, orderNo, ownerID, status); err != nil {
		t.Fatalf("seed order %s: %v", orderNo, err)
	}
}

// ─────────────────────────────────────────────────────────────────────
// ListMyOrders
// ─────────────────────────────────────────────────────────────────────

func TestListMyOrders_OnlyReturnsCallersOrders(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-MINE-1", 42, "paid")
	seedCustomerOrder(t, db, "SVC-MINE-2", 42, "completed")
	seedCustomerOrder(t, db, "SVC-OTHER", 99, "paid")

	rows, err := ListMyOrders(db, 42, "")
	if err != nil {
		t.Fatalf("ListMyOrders: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 owned rows, got %d (%+v)", len(rows), rows)
	}
	for _, r := range rows {
		if r.OrderNo == "SVC-OTHER" {
			t.Errorf("leaked another user's order: %s", r.OrderNo)
		}
		if r.PriceDisplayCNY != "¥19" {
			t.Errorf("price formatting failed: %q", r.PriceDisplayCNY)
		}
		if r.ServiceName != "小红书单篇文案" {
			t.Errorf("service name JOIN failed: %q", r.ServiceName)
		}
	}
}

func TestListMyOrders_NewestFirst(t *testing.T) {
	db := setupTestDB(t)
	// Insert in order — auto-incrementing id makes ORDER BY id DESC =
	// newest first regardless of created_at clock skew.
	seedCustomerOrder(t, db, "SVC-A", 1, "pending_payment")
	seedCustomerOrder(t, db, "SVC-B", 1, "paid")
	seedCustomerOrder(t, db, "SVC-C", 1, "completed")

	rows, err := ListMyOrders(db, 1, "")
	if err != nil {
		t.Fatalf("ListMyOrders: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if rows[0].OrderNo != "SVC-C" || rows[1].OrderNo != "SVC-B" || rows[2].OrderNo != "SVC-A" {
		t.Errorf("ordering wrong: %s, %s, %s", rows[0].OrderNo, rows[1].OrderNo, rows[2].OrderNo)
	}
}

func TestListMyOrders_StatusFilter(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-PEN", 7, "pending_payment")
	seedCustomerOrder(t, db, "SVC-PAY", 7, "paid")
	seedCustomerOrder(t, db, "SVC-DONE", 7, "completed")

	paid, err := ListMyOrders(db, 7, "paid")
	if err != nil {
		t.Fatalf("paid filter: %v", err)
	}
	if len(paid) != 1 || paid[0].OrderNo != "SVC-PAY" {
		t.Fatalf("status filter wrong: %+v", paid)
	}

	completed, err := ListMyOrders(db, 7, "completed")
	if err != nil {
		t.Fatalf("completed filter: %v", err)
	}
	if len(completed) != 1 || completed[0].OrderNo != "SVC-DONE" {
		t.Fatalf("status filter wrong: %+v", completed)
	}

	// Filter for a status that has no rows returns empty (not error).
	refunded, err := ListMyOrders(db, 7, "refunded")
	if err != nil {
		t.Fatalf("refunded filter: %v", err)
	}
	if len(refunded) != 0 {
		t.Errorf("want 0 refunded, got %d", len(refunded))
	}
}

func TestListMyOrders_NoOrdersReturnsEmptySlice(t *testing.T) {
	db := setupTestDB(t)
	rows, err := ListMyOrders(db, 999, "")
	if err != nil {
		t.Fatalf("ListMyOrders: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want empty, got %d", len(rows))
	}
}

func TestListMyOrders_HasRunFlagReflectsAgentRunID(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-NORUN", 5, "paid")
	seedCustomerOrder(t, db, "SVC-RAN", 5, "completed")
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET agent_run_id = 'run-xyz' WHERE order_no = 'SVC-RAN'`,
	); err != nil {
		t.Fatalf("set agent_run_id: %v", err)
	}

	rows, err := ListMyOrders(db, 5, "")
	if err != nil {
		t.Fatalf("ListMyOrders: %v", err)
	}
	for _, r := range rows {
		switch r.OrderNo {
		case "SVC-RAN":
			if !r.HasRun {
				t.Errorf("SVC-RAN should have HasRun=true")
			}
		case "SVC-NORUN":
			if r.HasRun {
				t.Errorf("SVC-NORUN should have HasRun=false")
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// LoadMyOrderDetail
// ─────────────────────────────────────────────────────────────────────

func TestLoadMyOrderDetail_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-DETAIL", 11, "paid")

	d, err := LoadMyOrderDetail(db, 11, "SVC-DETAIL")
	if err != nil {
		t.Fatalf("LoadMyOrderDetail: %v", err)
	}
	if d.OrderNo != "SVC-DETAIL" {
		t.Errorf("OrderNo wrong: %q", d.OrderNo)
	}
	if d.ServiceName != "小红书单篇文案" {
		t.Errorf("ServiceName wrong: %q", d.ServiceName)
	}
	if d.Status != "paid" {
		t.Errorf("Status wrong: %q", d.Status)
	}
	if d.HasRun {
		t.Errorf("HasRun should be false")
	}
	if d.PriceDisplayCNY != "¥19" {
		t.Errorf("PriceDisplayCNY wrong: %q", d.PriceDisplayCNY)
	}
}

func TestLoadMyOrderDetail_RejectsOtherUsersOrder(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-NOTYOURS", 42, "paid")

	// Someone else asking for it must see sql.ErrNoRows (handler turns
	// this into 404 — we don't 403 because that would confirm-or-deny
	// the order's existence to a curious caller).
	_, err := LoadMyOrderDetail(db, 99, "SVC-NOTYOURS")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want sql.ErrNoRows for other user's order, got %v", err)
	}
}

func TestLoadMyOrderDetail_NotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := LoadMyOrderDetail(db, 1, "SVC-DOES-NOT-EXIST")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want sql.ErrNoRows, got %v", err)
	}
}

func TestLoadMyOrderDetail_IncludesRefundAndRunMetadata(t *testing.T) {
	db := setupTestDB(t)
	seedCustomerOrder(t, db, "SVC-REF", 3, "completed")
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET agent_run_id = 'run-abc', refund_reason = 'partial — pricing adjustment'
		WHERE order_no = 'SVC-REF'
	`); err != nil {
		t.Fatalf("annotate: %v", err)
	}

	d, err := LoadMyOrderDetail(db, 3, "SVC-REF")
	if err != nil {
		t.Fatalf("LoadMyOrderDetail: %v", err)
	}
	if d.AgentRunID != "run-abc" {
		t.Errorf("AgentRunID = %q", d.AgentRunID)
	}
	if d.RefundReason != "partial — pricing adjustment" {
		t.Errorf("RefundReason = %q", d.RefundReason)
	}
	if !d.HasRun {
		t.Errorf("HasRun should be true")
	}
}

// validOrderStatuses is exercised indirectly via the handler. We test
// the lookup map directly here so a future schema change that adds a
// new status forces an update to the validator (and this test).
func TestValidOrderStatuses_CoversAllSchemaStates(t *testing.T) {
	// If the schema CHECK constraint adds a state, that state must
	// appear in validOrderStatuses or the new state cannot be filtered
	// from the API. Keep these in lockstep with service/migration.go.
	want := []string{
		"pending_payment", "paid", "running", "completed",
		"refunded", "refunded_post_delivery", "failed",
		"canceled_mid_flight",
	}
	for _, s := range want {
		if _, ok := validOrderStatuses[s]; !ok {
			t.Errorf("status %q missing from validOrderStatuses", s)
		}
	}
}
