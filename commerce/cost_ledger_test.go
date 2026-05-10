// cost_ledger_test.go — dual-engine tests for the unified usage write path
// (PKG-2 Wave 2 B2). Mirrors plans/migration_test.go's SQLite in-memory
// pattern: stub auth(id) → run plans.Migrate(db) → exercise WriteUsageCost
// and ComputeAndWriteUsageCost → assert row state.
//
// We deliberately use the real plans.Migrate to create gtk_app_usage_log
// (rather than a hand-crafted CREATE TABLE in the test) so any future
// schema drift in plans/migration.go is caught here too.

package commerce

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newSqliteWithDeps opens an in-memory SQLite with globals.SqliteEngine
// flipped on for the duration of the test, stubs auth(id) (FK target for
// gtk_user_plan in plans.Migrate), and runs plans.Migrate so
// gtk_app_usage_log exists. Returns a ready-to-use *sql.DB.
func newSqliteWithDeps(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans.Migrate: %v", err)
	}
	// Insert a single auth row so any future FK-style assertions on user_id
	// pass; gtk_app_usage_log itself has no FK to auth, but keeping a real
	// row makes the test data feel honest.
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}
	return db
}

// usageRow mirrors the columns we read back per assertion. Kept narrow to
// what the tests check; created_at is not asserted (DB DEFAULT, not under
// test).
type usageRow struct {
	UserID     int64
	PlanID     sql.NullInt64
	Service    string
	Source     string
	OrderID    sql.NullString
	Provider   sql.NullString
	TokensUsed int64
	CostCents  int64
}

// readSingleUsageRow loads the single row in gtk_app_usage_log. Fails
// the test if the count isn't exactly 1 — every test inserts exactly
// one row, so a mismatch points to a regression.
func readSingleUsageRow(t *testing.T, db *sql.DB) usageRow {
	t.Helper()
	rows, err := db.Query(`
		SELECT user_id, plan_id, service, source, order_id, provider, tokens_used, cost_cents
		FROM gtk_app_usage_log
	`)
	if err != nil {
		t.Fatalf("query usage: %v", err)
	}
	defer rows.Close()
	var out []usageRow
	for rows.Next() {
		var r usageRow
		if err := rows.Scan(&r.UserID, &r.PlanID, &r.Service, &r.Source,
			&r.OrderID, &r.Provider, &r.TokensUsed, &r.CostCents); err != nil {
			t.Fatalf("scan usage: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected exactly 1 usage row, got %d", len(out))
	}
	return out[0]
}

// TestWriteUsageCost_ServiceOrder covers the service-order origin: source
// is 'service_order' and order_id is set so the per-order cost rollup
// query (idx_gtk_usage_source_order) finds this row.
func TestWriteUsageCost_ServiceOrder(t *testing.T) {
	db := newSqliteWithDeps(t)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "deepseek-chat",
		Source:     "service_order",
		OrderID:    sql.NullString{String: "ord_test_001", Valid: true},
		Provider:   sql.NullString{String: "deepseek", Valid: true},
		TokensUsed: 1500,
		CostCents:  3, // caller-computed; WriteUsageCost trusts it
	}
	if err := WriteUsageCost(db, entry); err != nil {
		t.Fatalf("WriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.Source != "service_order" {
		t.Errorf("source = %q, want 'service_order'", got.Source)
	}
	if !got.OrderID.Valid || got.OrderID.String != "ord_test_001" {
		t.Errorf("order_id = %+v, want {ord_test_001, Valid=true}", got.OrderID)
	}
	if got.CostCents != 3 {
		t.Errorf("cost_cents = %d, want 3 (caller-supplied trust)", got.CostCents)
	}
}

// TestWriteUsageCost_Chat covers the chat origin: source='chat' and
// OrderID Valid=false should persist as NULL in the order_id column.
func TestWriteUsageCost_Chat(t *testing.T) {
	db := newSqliteWithDeps(t)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "deepseek-chat",
		Source:     "chat",
		OrderID:    sql.NullString{}, // Valid=false → NULL
		Provider:   sql.NullString{String: "deepseek", Valid: true},
		TokensUsed: 800,
		CostCents:  1,
	}
	if err := WriteUsageCost(db, entry); err != nil {
		t.Fatalf("WriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.Source != "chat" {
		t.Errorf("source = %q, want 'chat'", got.Source)
	}
	if got.OrderID.Valid {
		t.Errorf("order_id should be NULL for chat, got %+v", got.OrderID)
	}
}

// TestWriteUsageCost_KnownModel verifies the dumb-persister contract:
// caller's pre-computed CostCents is written exactly, regardless of
// whatever the pricing table would have computed. Critical for the case
// where chat / api callers have applied a markup before calling
// WriteUsageCost.
func TestWriteUsageCost_KnownModel(t *testing.T) {
	db := newSqliteWithDeps(t)
	const arbitraryCost = int64(999) // unrelated to deepseek-chat actual price
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "deepseek-chat",
		Source:     "api",
		Provider:   sql.NullString{String: "deepseek", Valid: true},
		TokensUsed: 1000,
		CostCents:  arbitraryCost,
	}
	if err := WriteUsageCost(db, entry); err != nil {
		t.Fatalf("WriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.CostCents != arbitraryCost {
		t.Errorf("cost_cents = %d, want %d (caller's value trusted as-is)", got.CostCents, arbitraryCost)
	}
}

// TestWriteUsageCost_NullProvider exercises the NULL provider path: an
// admin-test or unknown-provider call may leave Provider Valid=false;
// the row should persist with provider IS NULL (not the empty string).
func TestWriteUsageCost_NullProvider(t *testing.T) {
	db := newSqliteWithDeps(t)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "experimental-model",
		Source:     "admin_test",
		Provider:   sql.NullString{}, // Valid=false → NULL
		TokensUsed: 100,
		CostCents:  0,
	}
	if err := WriteUsageCost(db, entry); err != nil {
		t.Fatalf("WriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.Provider.Valid {
		t.Errorf("provider should be NULL when Valid=false, got %+v", got.Provider)
	}
}

// TestComputeAndWriteUsageCost_KnownModel: 1M in + 500k out on
// deepseek-chat = 28 cents (see pricing_test.go TestLookupCostCents_KnownModel
// for the math walkthrough). Provider should be auto-stamped to "deepseek".
func TestComputeAndWriteUsageCost_KnownModel(t *testing.T) {
	db := newSqliteWithDeps(t)
	const tokensIn, tokensOut = int64(1_000_000), int64(500_000)
	const wantCost = int64(28)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "deepseek-chat",
		Source:     "service_order",
		OrderID:    sql.NullString{String: "ord_test_002", Valid: true},
		TokensUsed: tokensIn + tokensOut,
		// CostCents intentionally zero; ComputeAndWriteUsageCost stamps it.
		// Provider intentionally Valid=false; ComputeAndWriteUsageCost stamps it.
	}
	if err := ComputeAndWriteUsageCost(db, entry, tokensIn, tokensOut); err != nil {
		t.Fatalf("ComputeAndWriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.CostCents != wantCost {
		t.Errorf("cost_cents = %d, want %d (deepseek-chat 1M in + 500k out)", got.CostCents, wantCost)
	}
	if !got.Provider.Valid || got.Provider.String != "deepseek" {
		t.Errorf("provider = %+v, want {deepseek, Valid=true} (auto-stamped from pricing.go)", got.Provider)
	}
}

// TestComputeAndWriteUsageCost_UnknownModel: a model not in pricing.go
// should still write the row (cost_cents=0, provider stays NULL) and
// must NOT return an error. This is the documented contract: caller's
// flow is never broken by a missing pricing entry.
func TestComputeAndWriteUsageCost_UnknownModel(t *testing.T) {
	db := newSqliteWithDeps(t)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "bogus-future-model-x",
		Source:     "service_order",
		OrderID:    sql.NullString{String: "ord_test_003", Valid: true},
		TokensUsed: 500,
		// Provider Valid=false; unknown-model branch should leave it that way.
	}
	if err := ComputeAndWriteUsageCost(db, entry, 300, 200); err != nil {
		t.Fatalf("ComputeAndWriteUsageCost on unknown model returned error (should not): %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.CostCents != 0 {
		t.Errorf("cost_cents = %d, want 0 (unknown model fallback)", got.CostCents)
	}
	if got.Provider.Valid {
		t.Errorf("provider should remain NULL for unknown model, got %+v", got.Provider)
	}
	if got.Service != "bogus-future-model-x" {
		t.Errorf("service = %q, want 'bogus-future-model-x' (row preserved for audit)", got.Service)
	}
}

// TestComputeAndWriteUsageCost_RoundsDown documents the sub-cent floor
// behavior in the integrated path. deepseek-chat at 1000 in + 500 out
// computes to 28 µ¢, which floors to 0 cents. The row is still written
// (audit), provider is still stamped (pricing.go knows the model).
func TestComputeAndWriteUsageCost_RoundsDown(t *testing.T) {
	db := newSqliteWithDeps(t)
	entry := UsageCostEntry{
		UserID:     1,
		Service:    "deepseek-chat",
		Source:     "chat",
		TokensUsed: 1500,
	}
	if err := ComputeAndWriteUsageCost(db, entry, 1000, 500); err != nil {
		t.Fatalf("ComputeAndWriteUsageCost: %v", err)
	}
	got := readSingleUsageRow(t, db)
	if got.CostCents != 0 {
		t.Errorf("cost_cents = %d, want 0 (sub-cent rounded down — documented behavior)", got.CostCents)
	}
	if !got.Provider.Valid || got.Provider.String != "deepseek" {
		t.Errorf("provider = %+v, want {deepseek, Valid=true} (model is known despite 0 cost)", got.Provider)
	}
}
