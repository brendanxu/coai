package service

import (
	"chat/globals"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB returns an in-memory SQLite *sql.DB with the service
// migrations applied. Mirrors waitlist/migration_test.go pattern.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
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
	return db
}

func TestMigrate_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate must be idempotent: %v", err)
	}

	for _, table := range []string{"gtk_agent", "gtk_service", "gtk_service_order"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestLoadActiveServices_FiltersAndOrders(t *testing.T) {
	db := setupTestDB(t)

	// Seed an agent so the agent_slug FK semantic holds (no enforcement
	// in SQLite-no-PRAGMA, but keep the test row realistic).
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('hero-copy', 'Hero Copy Writer', 'You write...', 'deepseek-chat', 'active')
	`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	seedSvc := func(slug, name, status string, order int, price int64) {
		t.Helper()
		if _, err := globals.ExecDb(db, `
			INSERT INTO gtk_service (slug, name, category, agent_slug,
			                         price_cny_cents, included_credits,
			                         billing_type, status, display_order)
			VALUES (?, ?, 'diy_agent', 'hero-copy', ?, 100, 'one_time', ?, ?)
		`, slug, name, price, status, order); err != nil {
			t.Fatalf("seed service %s: %v", slug, err)
		}
	}

	seedSvc("svc-active-2", "Second", "active", 2, 1900)
	seedSvc("svc-active-1", "First", "active", 1, 1900)
	seedSvc("svc-draft", "Draft", "draft", 99, 999900)
	seedSvc("svc-retired", "Retired", "retired", 99, 999900)

	got, err := LoadActiveServices(db)
	if err != nil {
		t.Fatalf("LoadActiveServices: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 active rows, got %d (%v)", len(got), got)
	}

	// Ordered by display_order ASC.
	if got[0].Slug != "svc-active-1" || got[1].Slug != "svc-active-2" {
		t.Errorf("display_order not honored: got %s, %s", got[0].Slug, got[1].Slug)
	}
	if got[0].PriceDisplayCNY != "¥19" {
		t.Errorf("price display not formatted: %q", got[0].PriceDisplayCNY)
	}
}

func TestLoadServiceBySlug_NotFoundOrInactive(t *testing.T) {
	db := setupTestDB(t)

	// Pre-seed agent + draft service.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('a', 'A', 'p', 'm', 'active')
	`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
		                         price_cny_cents, billing_type, status)
		VALUES ('paused', 'Paused', 'diy_agent', 'a', 1900, 'one_time', 'draft')
	`); err != nil {
		t.Fatalf("seed service: %v", err)
	}

	if _, err := LoadServiceBySlug(db, "missing"); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("missing slug: want ErrServiceNotFound, got %v", err)
	}
	if _, err := LoadServiceBySlug(db, "paused"); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("draft slug: want ErrServiceNotFound (inactive treated as missing), got %v", err)
	}
}

func TestCreateOrder_HappyPath(t *testing.T) {
	db := setupTestDB(t)

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('a', 'A', 'p', 'm', 'active')
	`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	res, err := globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
		                         price_cny_cents, included_credits,
		                         billing_type, status)
		VALUES ('test-svc', 'Test', 'diy_agent', 'a', 1900, 100, 'one_time', 'active')
	`)
	if err != nil {
		t.Fatalf("seed service: %v", err)
	}
	svcID, _ := res.LastInsertId()

	svc := &Service{
		ID:              svcID,
		Slug:            "test-svc",
		PriceCNYCents:   1900,
		IncludedCredits: 100,
	}
	orderNo, err := CreateOrder(db, 42, svc, "lemonsqueezy")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if len(orderNo) < 8 || orderNo[:4] != "SVC-" {
		t.Errorf("order_no shape unexpected: %q", orderNo)
	}

	var (
		uid      int64
		status   string
		provider string
		creds    int
	)
	err = db.QueryRow(`
		SELECT coai_user_id, status, payment_provider, credits_granted
		FROM gtk_service_order WHERE order_no = ?
	`, orderNo).Scan(&uid, &status, &provider, &creds)
	if err != nil {
		t.Fatalf("read order back: %v", err)
	}
	if uid != 42 || status != "pending_payment" || provider != "lemonsqueezy" || creds != 100 {
		t.Errorf("order row mismatch: uid=%d status=%s provider=%s creds=%d",
			uid, status, provider, creds)
	}
}

func TestCreateOrder_RejectsInvalidProvider(t *testing.T) {
	db := setupTestDB(t)
	svc := &Service{ID: 1, Slug: "x", PriceCNYCents: 100}
	_, err := CreateOrder(db, 1, svc, "stripe-direct")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestCreateOrder_RejectsNilService(t *testing.T) {
	db := setupTestDB(t)
	_, err := CreateOrder(db, 1, nil, "manual")
	if err == nil {
		t.Fatal("expected error for nil service")
	}
}
