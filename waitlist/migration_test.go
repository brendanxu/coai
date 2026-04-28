package waitlist

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrate_Idempotent confirms Migrate creates gtk_waitlist and is safe to
// re-run. Mirrors payment.TestMigrate_Idempotent.
func TestMigrate_Idempotent(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (must be idempotent): %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='gtk_waitlist'`).Scan(&name)
	if err != nil {
		t.Fatalf("expected table gtk_waitlist not created: %v", err)
	}
}

// TestUniqueConstraint_Enforced verifies the design intent: a second INSERT
// with the same (email, service) fails. The handler relies on this to
// short-circuit duplicates into idempotent successes.
func TestUniqueConstraint_Enforced(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := globals.ExecDb(db,
		`INSERT INTO gtk_waitlist (email, service, source) VALUES (?, ?, ?)`,
		"alice@example.com", "tax-filing", "marketing-landing"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = globals.ExecDb(db,
		`INSERT INTO gtk_waitlist (email, service, source) VALUES (?, ?, ?)`,
		"alice@example.com", "tax-filing", "marketing-landing")
	if err == nil {
		t.Fatal("second insert with same (email, service) should fail (UNIQUE)")
	}
	if !isDuplicateKeyErr(err) {
		t.Fatalf("expected duplicate-key error, got: %v", err)
	}

	// Different service for the same email is allowed: independent rows.
	if _, err := globals.ExecDb(db,
		`INSERT INTO gtk_waitlist (email, service, source) VALUES (?, ?, ?)`,
		"alice@example.com", "video-editing", "marketing-landing"); err != nil {
		t.Fatalf("same email different service should be allowed: %v", err)
	}
}
