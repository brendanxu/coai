// admin_ops_test.go — exercises commerce.MarkPaid (PKG-2 Wave 4 D7).
// Reuses the entitlement test fixture (newSqliteEntitlementDB) since
// MarkPaid is a thin wrapper over GrantEntitlement(service).

package commerce

import (
	"context"
	"testing"
)

func TestMarkPaid_HappyPath_ServiceOrder(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	// seedServiceOrder is defined in entitlement_test.go.
	seedServiceOrder(t, db, "ADMIN-MP-1", 1, "pending_payment")

	if err := MarkPaid(context.Background(), db, "ADMIN-MP-1", ProductService, 999); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM gtk_service_order WHERE order_no = ?`,
		"ADMIN-MP-1").Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paid" {
		t.Errorf("status = %q, want 'paid'", status)
	}
}

func TestMarkPaid_RejectsTokenProductType(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	err := MarkPaid(context.Background(), db, "TOK-1", ProductToken, 999)
	if err == nil {
		t.Fatal("MarkPaid(token) should error in v0 — only ProductService supported")
	}
}

func TestMarkPaid_RejectsEmptyOrderNo(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	err := MarkPaid(context.Background(), db, "", ProductService, 999)
	if err == nil {
		t.Fatal("empty orderNo should error")
	}
}

// IDEMPOTENT contract: marking an already-paid order is a no-op.
func TestMarkPaid_Idempotent(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ADMIN-MP-IDEM", 1, "paid")

	// Should not error — GrantEntitlement(service) CAS observes status='paid'
	// and returns nil per its idempotent contract.
	if err := MarkPaid(context.Background(), db, "ADMIN-MP-IDEM", ProductService, 999); err != nil {
		t.Fatalf("MarkPaid on already-paid order should be no-op: %v", err)
	}

	var status string
	_ = db.QueryRow(`SELECT status FROM gtk_service_order WHERE order_no = ?`,
		"ADMIN-MP-IDEM").Scan(&status)
	if status != "paid" {
		t.Errorf("status should remain 'paid', got %q", status)
	}
}
