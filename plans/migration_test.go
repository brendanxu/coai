package plans

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrate_Idempotent verifies Migrate can be called twice without errors
// and that the three plans tables exist after running. SQLite in-memory.
// PKG-1 (v0.17, L23): also confirms the new product_type / billing_mode /
// quota_grant / service_id columns are present on gtk_plan, plus the
// product_type / cancellation_reason columns on gtk_user_plan, plus the
// source / order_id / provider columns on gtk_app_usage_log.
func TestMigrate_Idempotent(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// gtk_user_plan FK targets auth(id); stub it so CREATE TABLE accepts the
	// FOREIGN KEY clause. Same pattern as payment/migration_test.go.
	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (must be idempotent): %v", err)
	}

	want := map[string]bool{
		"gtk_plan":          false,
		"gtk_user_plan":     false,
		"gtk_app_usage_log": false,
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'gtk_%'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected table %q not created", name)
		}
	}
}

// TestMigrate_ProductTypeColumnsPresent verifies the PKG-1 columns exist
// on each table after Migrate. Runs PRAGMA table_info per table and checks
// the discriminator + attribution columns are wired correctly.
func TestMigrate_ProductTypeColumnsPresent(t *testing.T) {
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
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tests := []struct {
		table    string
		expected []string
	}{
		{"gtk_plan", []string{"product_type", "billing_mode", "quota_grant", "service_id"}},
		{"gtk_user_plan", []string{"product_type", "cancellation_reason"}},
		{"gtk_app_usage_log", []string{"source", "order_id", "provider"}},
	}
	for _, tc := range tests {
		t.Run(tc.table, func(t *testing.T) {
			cols, err := sqliteColumns(db, tc.table)
			if err != nil {
				t.Fatalf("read columns for %s: %v", tc.table, err)
			}
			for _, want := range tc.expected {
				if _, ok := cols[want]; !ok {
					t.Errorf("table %s missing column %q (have: %v)", tc.table, want, keysOf(cols))
				}
			}
		})
	}
}

// TestMigrate_DefaultsApplied verifies a freshly inserted gtk_plan row
// without explicit product_type / billing_mode picks up the schema defaults
// ('token' / 'subscription'). Catches a class of regression where the
// SQLite branch forgets the DEFAULT clause.
func TestMigrate_DefaultsApplied(t *testing.T) {
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
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO gtk_plan (code, name, type, price_cents, duration_days) VALUES ('test-token','Test Token Plan','subscription',1500,30)`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	var productType, billingMode string
	if err := db.QueryRow(`SELECT product_type, billing_mode FROM gtk_plan WHERE code='test-token'`).Scan(&productType, &billingMode); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if productType != "token" {
		t.Errorf("product_type default = %q, want 'token'", productType)
	}
	if billingMode != "subscription" {
		t.Errorf("billing_mode default = %q, want 'subscription'", billingMode)
	}
}

func sqliteColumns(db *sql.DB, table string) (map[string]struct{}, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
