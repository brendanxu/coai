package service

import (
	"chat/globals"
	"database/sql"
	"testing"
)

// TestSeedCatalog_FreshAndIdempotent verifies first run inserts all
// agents+services and second run is a no-op.
func TestSeedCatalog_FreshAndIdempotent(t *testing.T) {
	db := setupTestDB(t)

	// First run — should insert.
	if err := SeedCatalog(db); err != nil {
		t.Fatalf("first SeedCatalog: %v", err)
	}

	expectAgentCount := len(agentSeeds)
	expectServiceCount := len(serviceSeeds)

	got := countRows(t, db, "gtk_agent")
	if got != expectAgentCount {
		t.Errorf("after first seed: gtk_agent count = %d, want %d", got, expectAgentCount)
	}
	got = countRows(t, db, "gtk_service")
	if got != expectServiceCount {
		t.Errorf("after first seed: gtk_service count = %d, want %d", got, expectServiceCount)
	}

	// Second run — must be no-op (no duplicate rows).
	if err := SeedCatalog(db); err != nil {
		t.Fatalf("second SeedCatalog: %v", err)
	}
	got = countRows(t, db, "gtk_agent")
	if got != expectAgentCount {
		t.Errorf("after second seed: gtk_agent count = %d, want %d (idempotency violated)", got, expectAgentCount)
	}
	got = countRows(t, db, "gtk_service")
	if got != expectServiceCount {
		t.Errorf("after second seed: gtk_service count = %d, want %d (idempotency violated)", got, expectServiceCount)
	}
}

// TestSeedCatalog_PreservesOperatorEdits verifies the seed never
// overwrites existing rows. If an operator edited a service price via
// SQL, the seed must respect that.
func TestSeedCatalog_PreservesOperatorEdits(t *testing.T) {
	db := setupTestDB(t)

	// Pre-insert a service row matching one of the seeds, but with a
	// different (operator-edited) price.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('xhs-copy-writer', 'OPERATOR EDIT', 'OPERATOR PROMPT', 'gpt-4o', 'active')
	`); err != nil {
		t.Fatalf("pre-insert agent: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
		                         price_cny_cents, billing_type, status)
		VALUES ('xhs-single-post', 'OPERATOR EDIT NAME', 'diy_agent',
		        'xhs-copy-writer', 99999, 'one_time', 'draft')
	`); err != nil {
		t.Fatalf("pre-insert service: %v", err)
	}

	if err := SeedCatalog(db); err != nil {
		t.Fatalf("SeedCatalog: %v", err)
	}

	// Confirm operator's edit survived.
	var (
		name  string
		model string
		price int64
	)
	if err := db.QueryRow(`SELECT name, preferred_model FROM gtk_agent WHERE slug='xhs-copy-writer'`).
		Scan(&name, &model); err != nil {
		t.Fatalf("read back agent: %v", err)
	}
	if name != "OPERATOR EDIT" || model != "gpt-4o" {
		t.Errorf("agent operator-edit clobbered: name=%q model=%q", name, model)
	}

	if err := db.QueryRow(`SELECT name, price_cny_cents FROM gtk_service WHERE slug='xhs-single-post'`).
		Scan(&name, &price); err != nil {
		t.Fatalf("read back service: %v", err)
	}
	if name != "OPERATOR EDIT NAME" || price != 99999 {
		t.Errorf("service operator-edit clobbered: name=%q price=%d", name, price)
	}

	// But OTHER seeds should still have been inserted.
	if c := countRows(t, db, "gtk_service"); c != len(serviceSeeds) {
		t.Errorf("expected %d total services after seed, got %d", len(serviceSeeds), c)
	}
}

// TestSeedServices_AllReferToValidAgents — sanity check on the seed
// data itself: every service.AgentSlug must match a seeded agent.
func TestSeedServices_AllReferToValidAgents(t *testing.T) {
	agents := make(map[string]bool, len(agentSeeds))
	for _, a := range agentSeeds {
		agents[a.Slug] = true
	}
	for _, s := range serviceSeeds {
		if !agents[s.AgentSlug] {
			t.Errorf("service %q references missing agent %q (seed data is internally inconsistent)",
				s.Slug, s.AgentSlug)
		}
	}
}

// TestSeedAgents_PromptNonEmpty — guard against accidentally shipping
// a service with an empty system_prompt (would produce garbage output).
func TestSeedAgents_PromptNonEmpty(t *testing.T) {
	for _, a := range agentSeeds {
		if a.SystemPrompt == "" {
			t.Errorf("agent %q has empty system_prompt", a.Slug)
		}
		if a.PreferredModel == "" {
			t.Errorf("agent %q has empty preferred_model", a.Slug)
		}
	}
}

// TestSeedServices_PriceConsistency — recommendation 11.Q3 strawman
// prices: ¥19 / ¥299 / ¥1,980. Tests pin them so accidental edits
// (forgetting a zero) trip the test.
func TestSeedServices_PriceConsistency(t *testing.T) {
	expectedPrices := map[string]int64{
		"xhs-single-post":   1900,   // ¥19
		"xhs-monthly-pack":  29900,  // ¥299
		"mansu-managed-ops": 198000, // ¥1,980
	}
	for _, s := range serviceSeeds {
		want, ok := expectedPrices[s.Slug]
		if !ok {
			t.Logf("service %q not in pinned prices — add to test if newly added", s.Slug)
			continue
		}
		if s.PriceCNYCents != want {
			t.Errorf("service %q price = %d, want %d", s.Slug, s.PriceCNYCents, want)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
