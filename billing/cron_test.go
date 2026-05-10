package billing

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newCronTestDB(t *testing.T) *sql.DB {
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
		t.Fatalf("seed auth table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan (code, name, type, price_cents, duration_days, quota_config)
		VALUES ('cron-test', 'Cron Test', 'subscription', 100, 30, '{}')
	`); err != nil {
		t.Fatalf("seed gtk_plan: %v", err)
	}

	return db
}

func seedUserPlan(t *testing.T, db *sql.DB, orderID, status string, expireAt time.Time) {
	t.Helper()

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_user_plan (user_id, plan_id, status, expire_at, remaining, order_id)
		VALUES (1, 1, ?, ?, '{}', ?)
	`, status, formatSQLiteTime(expireAt), orderID); err != nil {
		t.Fatalf("seed user plan %s: %v", orderID, err)
	}
}

func formatSQLiteTime(ts time.Time) string {
	return ts.UTC().Format("2006-01-02 15:04:05")
}

func planStatus(t *testing.T, db *sql.DB, orderID string) string {
	t.Helper()

	var status string
	err := db.QueryRow(`SELECT status FROM gtk_user_plan WHERE order_id = ?`, orderID).Scan(&status)
	if err != nil {
		t.Fatalf("read status %s: %v", orderID, err)
	}
	return status
}

func TestExpirePlans_HappyPath(t *testing.T) {
	db := newCronTestDB(t)
	now := time.Now().UTC()

	seedUserPlan(t, db, "past-active", "active", now.Add(-time.Hour))
	seedUserPlan(t, db, "future-active", "active", now.Add(time.Hour))
	seedUserPlan(t, db, "already-expired", "expired", now.Add(-2*time.Hour))

	affected, err := ExpirePlans(db)
	if err != nil {
		t.Fatalf("ExpirePlans: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected rows: want 1, got %d", affected)
	}
	if got := planStatus(t, db, "past-active"); got != "expired" {
		t.Errorf("past active plan status: want expired, got %s", got)
	}
	if got := planStatus(t, db, "future-active"); got != "active" {
		t.Errorf("future active plan status: want active, got %s", got)
	}
	if got := planStatus(t, db, "already-expired"); got != "expired" {
		t.Errorf("already expired plan status: want expired, got %s", got)
	}
}

func TestExpirePlans_Idempotent(t *testing.T) {
	db := newCronTestDB(t)
	seedUserPlan(t, db, "past-active", "active", time.Now().UTC().Add(-time.Hour))

	affected, err := ExpirePlans(db)
	if err != nil {
		t.Fatalf("first ExpirePlans: %v", err)
	}
	if affected != 1 {
		t.Fatalf("first affected rows: want 1, got %d", affected)
	}

	affected, err = ExpirePlans(db)
	if err != nil {
		t.Fatalf("second ExpirePlans: %v", err)
	}
	if affected != 0 {
		t.Fatalf("second affected rows: want 0, got %d", affected)
	}
}

func TestExpirePlans_EmptyTable(t *testing.T) {
	db := newCronTestDB(t)

	affected, err := ExpirePlans(db)
	if err != nil {
		t.Fatalf("ExpirePlans: %v", err)
	}
	if affected != 0 {
		t.Fatalf("affected rows: want 0, got %d", affected)
	}
}

func TestExpirePlans_CanceledPlansAreTerminal(t *testing.T) {
	db := newCronTestDB(t)
	seedUserPlan(t, db, "past-canceled", "canceled", time.Now().UTC().Add(-time.Hour))

	affected, err := ExpirePlans(db)
	if err != nil {
		t.Fatalf("ExpirePlans: %v", err)
	}
	if affected != 0 {
		t.Fatalf("affected rows: want 0, got %d", affected)
	}
	if got := planStatus(t, db, "past-canceled"); got != "canceled" {
		t.Errorf("canceled plan status: want canceled, got %s", got)
	}
}

func TestNextTickDelay_BeforeToday3AM(t *testing.T) {
	now := time.Date(2026, 5, 10, 2, 0, 0, 0, SGT)

	if got, want := nextTickDelay(now), time.Hour; got != want {
		t.Fatalf("nextTickDelay: want %s, got %s", want, got)
	}
}

func TestNextTickDelay_AfterToday3AM(t *testing.T) {
	now := time.Date(2026, 5, 10, 4, 0, 0, 0, SGT)

	if got, want := nextTickDelay(now), 23*time.Hour; got != want {
		t.Fatalf("nextTickDelay: want %s, got %s", want, got)
	}
}

func TestNextTickDelay_Exactly3AM(t *testing.T) {
	now := time.Date(2026, 5, 10, 3, 0, 0, 0, SGT)

	if got, want := nextTickDelay(now), 24*time.Hour; got != want {
		t.Fatalf("nextTickDelay: want %s, got %s", want, got)
	}
}
