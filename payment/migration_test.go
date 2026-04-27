package payment

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrate_Idempotent verifies that Migrate can be called twice without
// errors and that the expected tables exist after running. Uses an in-memory
// sqlite DB via globals.PreflightSql (which translates MySQL DDL → sqlite).
func TestMigrate_Idempotent(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// gtk_ls_subscription has a FK to auth(id); create a stub.
	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (must be idempotent): %v", err)
	}

	want := map[string]bool{"gtk_ls_subscription": false, "gtk_webhook_event": false}
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

// TestWebhookEvent_PrimaryKeyIsIdempotent verifies the design intent: inserting
// the same event_id twice fails with a duplicate-key error. The webhook handler
// will catch this error and treat it as "already processed".
func TestWebhookEvent_PrimaryKeyIsIdempotent(t *testing.T) {
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

	if _, err := globals.ExecDb(db,
		`INSERT INTO gtk_webhook_event (event_id, event_type) VALUES (?, ?)`,
		"evt_abc", "subscription_created"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := globals.ExecDb(db,
		`INSERT INTO gtk_webhook_event (event_id, event_type) VALUES (?, ?)`,
		"evt_abc", "subscription_created"); err == nil {
		t.Fatal("second insert with same event_id should fail (duplicate primary key)")
	}
}
