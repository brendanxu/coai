// margin_view_test.go — exercises gtk_service_margin_v VIEW + reader
// (PKG-2 Wave 4 D6).

package commerce

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newMarginTestDB stands up an in-memory SQLite with the three tables the
// VIEW joins over (gtk_service_order, gtk_app_usage_log) plus the VIEW
// itself created via commerce.Migrate. Uses applyInlineServiceSchema (the
// helper introduced in entitlement_test.go to break the import cycle) so
// we don't pull in chat/service.
func newMarginTestDB(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)

	// auth + service tables (gtk_service_order has FK to auth via
	// service.migration.go's MySQL branch; SQLite ignores FK without
	// PRAGMA foreign_keys=ON, but we seed the auth table anyway for
	// shape parity).
	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}
	if err := applyInlineServiceSchema(db); err != nil {
		t.Fatalf("applyInlineServiceSchema: %v", err)
	}

	// gtk_app_usage_log — mirror of commerce/cost_ledger.go's INSERT shape.
	if _, err := db.Exec(`
		CREATE TABLE gtk_app_usage_log (
		  id           INTEGER PRIMARY KEY AUTOINCREMENT,
		  user_id      INTEGER NOT NULL,
		  plan_id      INTEGER,
		  service      TEXT    NOT NULL,
		  source       TEXT    NOT NULL DEFAULT 'chat'
		                CHECK (source IN ('chat','api','service_order','admin_test')),
		  order_id     TEXT,
		  provider     TEXT,
		  tokens_used  INTEGER NOT NULL DEFAULT 0,
		  cost_cents   INTEGER NOT NULL DEFAULT 0,
		  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("seed gtk_app_usage_log: %v", err)
	}

	// commerce.Migrate creates gtk_payment_session + the VIEW.
	if err := Migrate(db); err != nil {
		t.Fatalf("commerce.Migrate: %v", err)
	}
	return db
}

// TestMarginView_HappyPath: 2 services, known revenue + cost → expected
// margin pcts surface in the right order (worst margin first per ORDER BY).
func TestMarginView_HappyPath(t *testing.T) {
	db := newMarginTestDB(t)

	// Seed two completed orders: one healthy (90% margin), one thin (10%).
	if _, err := db.Exec(`
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status, completed_at)
		VALUES
		  ('SVC-HEALTHY', 1, 1, 'svc-healthy',  10000, 'lemonsqueezy', 'completed', CURRENT_TIMESTAMP),
		  ('SVC-THIN',    1, 1, 'svc-thin',     10000, 'lemonsqueezy', 'completed', CURRENT_TIMESTAMP)
	`); err != nil {
		t.Fatalf("seed orders: %v", err)
	}

	// Healthy: 1000 cents cost (margin 90%).
	// Thin:    9000 cents cost (margin 10%).
	if _, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, source, order_id, cost_cents, tokens_used)
		VALUES
		  (1, 'deepseek-chat', 'service_order', 'SVC-HEALTHY', 1000, 100),
		  (1, 'deepseek-chat', 'service_order', 'SVC-THIN',    9000, 100)
	`); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	rows, err := QueryMarginByService(db, 30)
	if err != nil {
		t.Fatalf("QueryMarginByService: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (%v)", len(rows), rows)
	}

	// Worst-margin-first ordering: thin (10%) before healthy (90%).
	if rows[0].ServiceSlug != "svc-thin" {
		t.Errorf("first row should be 'svc-thin' (worst margin), got %q",
			rows[0].ServiceSlug)
	}
	if rows[0].RollingRevenueCents != 10000 || rows[0].RollingCostCents != 9000 {
		t.Errorf("svc-thin: rev=%d cost=%d, want 10000/9000",
			rows[0].RollingRevenueCents, rows[0].RollingCostCents)
	}
	if rows[0].RollingMarginPct < 9.99 || rows[0].RollingMarginPct > 10.01 {
		t.Errorf("svc-thin margin = %.2f, want ~10.00", rows[0].RollingMarginPct)
	}

	if rows[1].ServiceSlug != "svc-healthy" {
		t.Errorf("second row should be 'svc-healthy', got %q", rows[1].ServiceSlug)
	}
	if rows[1].RollingMarginPct < 89.99 || rows[1].RollingMarginPct > 90.01 {
		t.Errorf("svc-healthy margin = %.2f, want ~90.00", rows[1].RollingMarginPct)
	}
}

// TestMarginView_OrderWithNoUsage: an order with no usage rows shows
// 0 cost (and 100% margin per the COALESCE+SUM=0 contract).
func TestMarginView_OrderWithNoUsage(t *testing.T) {
	db := newMarginTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status, completed_at)
		VALUES
		  ('SVC-NOUSE', 1, 1, 'svc-pure-rev', 5000, 'lemonsqueezy', 'completed', CURRENT_TIMESTAMP)
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := QueryMarginByService(db, 30)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].RollingCostCents != 0 {
		t.Errorf("cost = %d, want 0 (no usage rows)", rows[0].RollingCostCents)
	}
	if rows[0].RollingMarginPct != 100.0 {
		t.Errorf("margin = %.2f, want 100.00 (zero cost)", rows[0].RollingMarginPct)
	}
}

// TestMarginView_ExcludesPendingOrders: the VIEW filters to status IN
// ('completed', 'refunded_post_delivery'). Pending / running / refunded
// (pre-delivery) rows must NOT show up.
func TestMarginView_ExcludesPendingOrders(t *testing.T) {
	db := newMarginTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status)
		VALUES
		  ('SVC-PENDING',  1, 1, 'svc-pending',  9999, 'lemonsqueezy', 'pending_payment'),
		  ('SVC-RUNNING',  1, 1, 'svc-running',  9999, 'lemonsqueezy', 'running'),
		  ('SVC-REFUND',   1, 1, 'svc-refund',   9999, 'lemonsqueezy', 'refunded'),
		  ('SVC-INCLUDED', 1, 1, 'svc-included', 5000, 'lemonsqueezy', 'completed')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := QueryMarginByService(db, 30)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("only 'completed' orders should show; got %d rows: %+v",
			len(rows), rows)
	}
	if rows[0].ServiceSlug != "svc-included" {
		t.Errorf("only svc-included should appear, got %q", rows[0].ServiceSlug)
	}
}

// TestMarginView_RefundPostDeliveryIncluded: the 'refunded_post_delivery'
// state is intentionally included so margin reports flag the loss.
func TestMarginView_RefundPostDeliveryIncluded(t *testing.T) {
	db := newMarginTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status)
		VALUES
		  ('SVC-LOSS', 1, 1, 'svc-postref', 5000, 'lemonsqueezy', 'refunded_post_delivery')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, source, order_id, cost_cents, tokens_used)
		VALUES (1, 'gpt-4o', 'service_order', 'SVC-LOSS', 6000, 500)
	`); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	rows, err := QueryMarginByService(db, 30)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0].ServiceSlug != "svc-postref" {
		t.Fatalf("want 1 row for svc-postref, got %+v", rows)
	}
	// Cost > revenue → negative margin (the alert signal).
	if rows[0].RollingMarginPct >= 0 {
		t.Errorf("margin = %.2f, want NEGATIVE for cost>revenue case", rows[0].RollingMarginPct)
	}
}

// TestQueryMarginByService_RejectsNonPositiveDays: contract guard —
// `days <= 0` returns an error instead of silently returning all rows
// or no rows.
func TestQueryMarginByService_RejectsNonPositiveDays(t *testing.T) {
	db := newMarginTestDB(t)
	for _, d := range []int{0, -1, -1000} {
		if _, err := QueryMarginByService(db, d); err == nil {
			t.Errorf("days=%d should error, got nil", d)
		}
	}
}

// TestCreateServiceMarginView_Idempotent: re-running Migrate (which
// re-creates the VIEW) doesn't error and leaves the VIEW queryable.
func TestCreateServiceMarginView_Idempotent(t *testing.T) {
	db := newMarginTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate must be idempotent: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("third Migrate must be idempotent: %v", err)
	}
	if _, err := QueryMarginByService(db, 30); err != nil {
		t.Errorf("VIEW should be queryable after re-migrate: %v", err)
	}
}
