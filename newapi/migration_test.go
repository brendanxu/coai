package newapi

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrate_Idempotent verifies Migrate creates both gtk_newapi_binding
// and gtk_newapi_pending_provisions and is safe to call twice.
func TestMigrate_Idempotent(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (must be idempotent): %v", err)
	}

	want := map[string]bool{
		"gtk_newapi_binding":            false,
		"gtk_newapi_pending_provisions": false,
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'gtk_newapi_%'`)
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

// TestPendingProvisions_InsertAndRead exercises the full PendingProvision
// lifecycle so the struct + schema stay in sync. Inserts a 'pending' row
// then transitions it to 'succeeded' to verify both the CHECK constraint
// (status enum) accepts the legal values and the column types are scannable
// into the Go struct.
func TestPendingProvisions_InsertAndRead(t *testing.T) {
	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO gtk_plan (code, name, type, price_cents, duration_days) VALUES ('plan-1','Plan One','subscription',1500,30)`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	res, err := db.Exec(`INSERT INTO gtk_newapi_pending_provisions (user_id, plan_id, provision_type) VALUES (?, ?, ?)`,
		42, 1, "token_plan")
	if err != nil {
		t.Fatalf("insert pending: %v", err)
	}
	rowID, _ := res.LastInsertId()

	var p PendingProvision
	if err := db.QueryRow(`
		SELECT id, user_id, plan_id, provision_type, status, retry_count,
		       last_attempt_at, last_error, succeeded_at, failed_at
		FROM gtk_newapi_pending_provisions WHERE id = ?
	`, rowID).Scan(
		&p.ID, &p.UserID, &p.PlanID, &p.ProvisionType, &p.Status, &p.RetryCount,
		&p.LastAttemptAt, &p.LastError, &p.SucceededAt, &p.FailedAt,
	); err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if p.UserID != 42 || p.PlanID != 1 {
		t.Errorf("read user/plan = %d/%d, want 42/1", p.UserID, p.PlanID)
	}
	if p.ProvisionType != "token_plan" {
		t.Errorf("provision_type = %q, want 'token_plan'", p.ProvisionType)
	}
	if p.Status != "pending" {
		t.Errorf("status default = %q, want 'pending'", p.Status)
	}
	if p.RetryCount != 0 {
		t.Errorf("retry_count default = %d, want 0", p.RetryCount)
	}
	if p.LastAttemptAt.Valid || p.LastError.Valid || p.SucceededAt.Valid || p.FailedAt.Valid {
		t.Errorf("nullable cols should all be NULL on insert; got %+v", p)
	}

	if _, err := db.Exec(`UPDATE gtk_newapi_pending_provisions SET status='succeeded', succeeded_at=CURRENT_TIMESTAMP WHERE id=?`, rowID); err != nil {
		t.Fatalf("update status: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM gtk_newapi_pending_provisions WHERE id=?`, rowID).Scan(&status); err != nil {
		t.Fatalf("re-read status: %v", err)
	}
	if status != "succeeded" {
		t.Errorf("status after update = %q, want 'succeeded'", status)
	}
}

// TestPendingProvisions_RejectsInvalidStatus verifies the CHECK constraint
// fires when caller writes a status outside the enum. Defends against a
// future code change that adds a new state without updating the schema.
func TestPendingProvisions_RejectsInvalidStatus(t *testing.T) {
	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO gtk_plan (code, name, type, price_cents, duration_days) VALUES ('plan-1','Plan One','subscription',1500,30)`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	_, err := db.Exec(`INSERT INTO gtk_newapi_pending_provisions (user_id, plan_id, provision_type, status) VALUES (?, ?, ?, ?)`,
		1, 1, "token_plan", "BOGUS_STATE")
	if err == nil {
		t.Fatalf("expected CHECK constraint failure for status='BOGUS_STATE', got nil")
	}
}

// TestBinding_GroupDefault verifies the PKG-2 Wave 1 (Q2 / CR8)
// newapi_group plumbing on the binding row:
//
//   - SaveBinding without a Group field set writes 'default' (helper
//     normalizes empty string to NewAPI's own column default).
//   - LoadBinding reads the column back into Binding.Group.
//
// This is the unit-level confirmation of CR8's "~50 LOC" plumbing path;
// live NewAPI roundtrip via SyncBindingGroup is gated behind
// newapi.IsConfigured() at call time and exercised in higher-level
// integration tests (PKG-3+).
func TestBinding_GroupDefault(t *testing.T) {
	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	// Insert without Group → expect 'default' on read.
	if err := SaveBinding(db, &Binding{
		CoaiUserID:     42,
		NewapiUserID:   100,
		NewapiTokenID:  200,
		NewapiTokenKey: "sk-test-default",
		LastKnownQuota: 500_000,
	}); err != nil {
		t.Fatalf("save binding (no group): %v", err)
	}
	got, err := LoadBinding(db, 42)
	if err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if got.Group != "default" {
		t.Errorf("group default = %q, want 'default'", got.Group)
	}
}

// TestBinding_GroupPreserved verifies an explicit Group survives the
// upsert + readback round-trip — important for the SyncBindingGroup
// flow (write to DB only after NewAPI ack) so admin-driven group flips
// stick.
func TestBinding_GroupPreserved(t *testing.T) {
	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (43)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	// First write: group='service-runtime' (e.g. service-product user).
	if err := SaveBinding(db, &Binding{
		CoaiUserID:     43,
		NewapiUserID:   101,
		NewapiTokenID:  201,
		NewapiTokenKey: "sk-test-service",
		Group:          "service-runtime",
		LastKnownQuota: 1_000_000,
	}); err != nil {
		t.Fatalf("save binding (service-runtime): %v", err)
	}
	got, err := LoadBinding(db, 43)
	if err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if got.Group != "service-runtime" {
		t.Errorf("group = %q, want 'service-runtime'", got.Group)
	}

	// Upsert with a different group flips the stored value.
	if err := SaveBinding(db, &Binding{
		CoaiUserID:     43,
		NewapiUserID:   101,
		NewapiTokenID:  201,
		NewapiTokenKey: "sk-test-service",
		Group:          "premium",
		LastKnownQuota: 1_000_000,
	}); err != nil {
		t.Fatalf("re-save binding (premium): %v", err)
	}
	got, err = LoadBinding(db, 43)
	if err != nil {
		t.Fatalf("load binding (after flip): %v", err)
	}
	if got.Group != "premium" {
		t.Errorf("group after upsert = %q, want 'premium'", got.Group)
	}
}

// newSqliteWithFKDeps spins an in-memory SQLite DB with the FK targets
// gtk_newapi_pending_provisions needs (auth + gtk_plan). Calling
// plans.Migrate would create an import cycle, so we hand-roll a minimal
// gtk_plan stub matching the columns inserted in the test bodies above.
// Full plans schema is exercised in plans/migration_test.go.
func newSqliteWithFKDeps(t *testing.T) *sql.DB {
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
	if _, err := db.Exec(`
		CREATE TABLE gtk_plan (
		  id            INTEGER PRIMARY KEY AUTOINCREMENT,
		  code          TEXT    NOT NULL UNIQUE,
		  name          TEXT    NOT NULL,
		  type          TEXT    NOT NULL,
		  price_cents   INTEGER NOT NULL,
		  duration_days INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("seed gtk_plan stub: %v", err)
	}
	return db
}
