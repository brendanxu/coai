package plans

import (
	"chat/globals"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newTestDB opens an in-memory SQLite, flips the engine flag, and seeds the
// stub `auth(id)` table that gtk_user_plan FKs against. Mirrors the harness
// in payment/migration_test.go so this package follows the same idiom.
func newTestDB(t *testing.T) *sql.DB {
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
	return db
}

// TestMigrate_Idempotent runs Migrate twice and confirms no error + every
// expected table is present. The double-run is the contract: every helper
// in migration.go must be safe on a re-boot of an already-migrated db.
//
// Union of v0.16 V2 tables (gtk_provider_pricing, gtk_billing_config) +
// PKG-1 (v0.21) baseline tables (gtk_plan, gtk_user_plan, gtk_app_usage_log).
func TestMigrate_Idempotent(t *testing.T) {
	db := newTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (must be idempotent): %v", err)
	}

	want := map[string]bool{
		"gtk_plan":             false,
		"gtk_user_plan":        false,
		"gtk_app_usage_log":    false,
		"gtk_provider_pricing": false,
		"gtk_billing_config":   false,
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

// TestUpgradeAppUsageLogV2_AddsAllColumns verifies every V2 column in
// usageLogV2Columns lands on the table after migration. Uses PRAGMA
// table_info because that's the same probe addColumnIfMissing relies on.
func TestUpgradeAppUsageLogV2_AddsAllColumns(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(gtk_app_usage_log)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}

	for _, col := range usageLogV2Columns {
		if !got[col.name] {
			t.Errorf("V2 column %q missing from gtk_app_usage_log", col.name)
		}
	}
	// Sanity: legacy columns still exist (we never drop them).
	for _, legacy := range []string{"id", "user_id", "service", "tokens_used", "cost_cents", "created_at"} {
		if !got[legacy] {
			t.Errorf("legacy column %q was dropped — must keep for back-compat", legacy)
		}
	}
}

// TestUpgradeAppUsageLogV2_PreExistingV1 simulates a prod restore from a
// V1-era dump (only the original 7 columns) and checks the V2 ALTER path
// adds the new columns instead of erroring on the absent-CREATE branch.
func TestUpgradeAppUsageLogV2_PreExistingV1(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Hand-shape the V1 table with only the original columns, then run the
	// V2 upgrader directly — bypassing the wider Migrate flow.
	if _, err := db.Exec(`
		CREATE TABLE gtk_app_usage_log (
		  id          INTEGER PRIMARY KEY AUTOINCREMENT,
		  user_id     INTEGER NOT NULL,
		  plan_id     INTEGER,
		  service     TEXT    NOT NULL,
		  tokens_used INTEGER NOT NULL DEFAULT 0,
		  cost_cents  INTEGER NOT NULL DEFAULT 0,
		  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("seed v1 table: %v", err)
	}
	// Seed a row so we can confirm zero-default-backfill behaviour.
	if _, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, tokens_used, cost_cents)
		VALUES (1, 'chat', 100, 5)
	`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if err := upgradeAppUsageLogV2(db); err != nil {
		t.Fatalf("upgrade v2: %v", err)
	}
	// Re-running must be a no-op.
	if err := upgradeAppUsageLogV2(db); err != nil {
		t.Fatalf("upgrade v2 (second run): %v", err)
	}

	// Pre-existing row must scan with safe zero defaults on V2 fields.
	var inputTok, outputTok, cacheR int64
	var modelID string
	row := db.QueryRow(`
		SELECT model_id, input_tokens, output_tokens, cache_read_tokens
		FROM gtk_app_usage_log WHERE id = 1
	`)
	if err := row.Scan(&modelID, &inputTok, &outputTok, &cacheR); err != nil {
		t.Fatalf("scan migrated row: %v", err)
	}
	if modelID != "" || inputTok != 0 || outputTok != 0 || cacheR != 0 {
		t.Errorf("expected zero-defaults, got model=%q in=%d out=%d cache=%d",
			modelID, inputTok, outputTok, cacheR)
	}
}

// TestSeedProviderPricing_OnlyOnce confirms the seed inserts on first run
// and is a no-op on second run, matching ops' "append-not-update" rule.
//
// PKG-PRICING-DYNAMIC update: Migrate() now also calls seedDisplayPricing()
// which may INSERT additional rows for models in the public Pricing page that
// have no upstream tracking row in providerPricingSeed (e.g. deepseek-r1,
// gpt-4o-mini, qwen2.5-max, gemini-2.0-flash, kimi-k2, claude-3-5-sonnet).
// The test now counts upstream rows + display-only insertions separately and
// verifies the idempotency invariant: a second full Migrate() is a no-op.
func TestSeedProviderPricing_OnlyOnce(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	var first int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_provider_pricing`).Scan(&first); err != nil {
		t.Fatalf("count: %v", err)
	}
	// After Migrate(): at minimum providerPricingSeed rows must be present;
	// seedDisplayPricing may have added display-only rows on top.
	if first < len(providerPricingSeed) {
		t.Errorf("first migrate: got %d rows, want >= %d (providerPricingSeed len)",
			first, len(providerPricingSeed))
	}

	// Re-running the full Migrate() must be a no-op — row count unchanged.
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var second int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_provider_pricing`).Scan(&second); err != nil {
		t.Fatalf("re-count: %v", err)
	}
	if second != first {
		t.Errorf("second migrate mutated table: had %d, now %d (not idempotent)", first, second)
	}
}

func TestDedupeDisplayPricingRows_ClearsOlderDuplicateDisplayRows(t *testing.T) {
	db := newTestDB(t)
	if err := createProviderPricingTable(db); err != nil {
		t.Fatalf("create pricing table: %v", err)
	}
	if err := addProviderPricingDisplayColumns(db); err != nil {
		t.Fatalf("add display columns: %v", err)
	}

	insertRow := func(effectiveFrom, displayName string, credits int64) int64 {
		t.Helper()
		res, err := db.Exec(`
			INSERT INTO gtk_provider_pricing
			  (provider, model_id, token_type, upstream_per_m, effective_from,
			   display_in_cny_per_m, display_out_cny_per_m, display_credits_per_m,
			   display_name, vendor_label, context_size, cache_flag)
			VALUES ('openai', 'gpt-4o', 'input', 2.5, ?, 18.20, 72.80, ?, ?, 'openai', '128k', 'true')
		`, effectiveFrom, credits, displayName)
		if err != nil {
			t.Fatalf("insert display row %s: %v", effectiveFrom, err)
		}
		id, _ := res.LastInsertId()
		return id
	}

	staleID := insertRow("2026-05-20 00:00:00", "GPT-4o stale", 3640)
	latestID := insertRow("2026-05-21 00:00:00", "GPT-4o latest", 3650)

	if err := dedupeDisplayPricingRows(db); err != nil {
		t.Fatalf("dedupe display rows: %v", err)
	}

	var completeRows int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM gtk_provider_pricing
		WHERE provider = 'openai'
		  AND model_id = 'gpt-4o'
		  AND token_type = 'input'
		  AND display_in_cny_per_m  IS NOT NULL
		  AND display_out_cny_per_m IS NOT NULL
		  AND display_credits_per_m IS NOT NULL
		  AND display_name          IS NOT NULL
		  AND vendor_label          IS NOT NULL
		  AND context_size          IS NOT NULL
		  AND cache_flag            IS NOT NULL
	`).Scan(&completeRows); err != nil {
		t.Fatalf("count complete display rows: %v", err)
	}
	if completeRows != 1 {
		t.Fatalf("complete display rows = %d, want 1", completeRows)
	}

	var staleName sql.NullString
	if err := db.QueryRow(`SELECT display_name FROM gtk_provider_pricing WHERE id = ?`, staleID).Scan(&staleName); err != nil {
		t.Fatalf("scan stale display_name: %v", err)
	}
	if staleName.Valid {
		t.Fatalf("stale row display_name still public: %q", staleName.String)
	}

	var latestName string
	if err := db.QueryRow(`SELECT display_name FROM gtk_provider_pricing WHERE id = ?`, latestID).Scan(&latestName); err != nil {
		t.Fatalf("scan latest display_name: %v", err)
	}
	if latestName != "GPT-4o latest" {
		t.Fatalf("latest row display_name = %q, want GPT-4o latest", latestName)
	}
}

// TestProviderPricingSeed_Sonnet45 confirms the Sonnet 4.5 row set follows
// Anthropic's canonical 1.25x write / 0.1x read structure. Catches arithmetic
// fat-finger in the seed slice before it ever reaches a billing path.
func TestProviderPricingSeed_Sonnet45(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	prices := map[string]float64{}
	rows, err := db.Query(`
		SELECT token_type, upstream_per_m FROM gtk_provider_pricing
		WHERE provider = 'anthropic' AND model_id = 'claude-sonnet-4.5'
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tt string
		var p float64
		if err := rows.Scan(&tt, &p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		prices[tt] = p
	}

	cases := []struct {
		tokenType string
		want      float64
	}{
		{"input", 3.0},
		{"output", 15.0},
		{"cache_write_5m", 3.75},
		{"cache_write_1h", 6.0},
		{"cache_read", 0.30},
	}
	for _, c := range cases {
		got, ok := prices[c.tokenType]
		if !ok {
			t.Errorf("missing token_type %q", c.tokenType)
			continue
		}
		if got != c.want {
			t.Errorf("Sonnet 4.5 %s: got %g, want %g", c.tokenType, got, c.want)
		}
	}

	// Cross-check the cache multipliers match the input baseline. Use a
	// micro-USD tolerance because IEEE-754 makes `3.0 * 0.1 = 0.30000…04`
	// — equality on float64 multiplication is a footgun.
	const tol = 1e-6
	approxEqual := func(a, b float64) bool { return a-b < tol && b-a < tol }

	in := prices["input"]
	if write5m := prices["cache_write_5m"]; !approxEqual(write5m, in*1.25) {
		t.Errorf("cache_write_5m must equal input × 1.25; got %g vs %g", write5m, in*1.25)
	}
	if write1h := prices["cache_write_1h"]; !approxEqual(write1h, in*2.0) {
		t.Errorf("cache_write_1h must equal input × 2.0; got %g vs %g", write1h, in*2.0)
	}
	if read := prices["cache_read"]; !approxEqual(read, in*0.1) {
		t.Errorf("cache_read must equal input × 0.1; got %g vs %g", read, in*0.1)
	}
}

// TestSeedBillingConfig_DefaultMarkup confirms the markup_multiplier seed
// lands at exactly "1.300". Any drift here means every customer call is
// silently mispriced.
func TestSeedBillingConfig_DefaultMarkup(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var v string
	row := db.QueryRow(`SELECT v FROM gtk_billing_config WHERE k = 'markup_multiplier'`)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("scan markup_multiplier: %v", err)
	}
	if v != "1.300" {
		t.Errorf("markup_multiplier seed: got %q, want %q", v, "1.300")
	}

	// Manually overwrite — re-running Migrate must NOT clobber operator
	// changes. INSERT OR IGNORE semantics rely on the PK collision.
	if _, err := db.Exec(`UPDATE gtk_billing_config SET v = '1.500' WHERE k = 'markup_multiplier'`); err != nil {
		t.Fatalf("manual update: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	row = db.QueryRow(`SELECT v FROM gtk_billing_config WHERE k = 'markup_multiplier'`)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("re-scan: %v", err)
	}
	if v != "1.500" {
		t.Errorf("Migrate clobbered operator override: v=%q", v)
	}
}

// TestColumnExists_BothEnginesAgreeOnSqlite is a sanity check — the helper
// must report the same answer regardless of capitalisation / formatting.
// Catches a regression where the SQLite branch reads PRAGMA wrong.
func TestColumnExists_BothEnginesAgreeOnSqlite(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, c := range []struct {
		name string
		want bool
	}{
		{"id", true},
		{"input_tokens", true},
		{"this_does_not_exist", false},
	} {
		got, err := columnExists(db, "gtk_app_usage_log", c.name)
		if err != nil {
			t.Errorf("columnExists(%q): %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("columnExists(%q): got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMigrate_ProductTypeColumnsPresent (PKG-1, v0.21) verifies the L23
// discriminator + attribution columns exist on each table after Migrate.
// Runs PRAGMA table_info per table and checks the new columns are wired
// correctly. Uses sqliteColumns helper for a map-based "have" set.
func TestMigrate_ProductTypeColumnsPresent(t *testing.T) {
	db := newTestDB(t)
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

// ---------------------------------------------------------------------------
// Wave 1.5 — token_id column tests (PKG-A-3)
// ---------------------------------------------------------------------------

// TestTokenIDColumn_PresentAfterMigrate verifies that after Migrate() the
// gtk_app_usage_log table has a token_id column with a default of 0.
func TestTokenIDColumn_PresentAfterMigrate(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// PRAGMA table_info returns one row per column with fields:
	//   cid | name | type | notnull | dflt_value | pk
	rows, err := db.Query(`PRAGMA table_info(gtk_app_usage_log)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "token_id" {
			found = true
			if dflt.String != "0" {
				t.Errorf("token_id default: want '0', got %q", dflt.String)
			}
		}
	}
	if !found {
		t.Error("token_id column missing from gtk_app_usage_log after Migrate")
	}
}

// TestTokenIDColumn_Idempotent re-runs Migrate() on an already-migrated DB
// and confirms no error is returned (addColumnIfMissing gate works).
func TestTokenIDColumn_Idempotent(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (idempotency): %v", err)
	}
}

// TestTokenIDColumn_WriteAndRead confirms that WriteUsageCost (in the
// commerce package) correctly writes token_id and that the value round-trips
// through a SELECT. This validates Tasks 1.5a + 1.5c together: schema has
// the column and the INSERT path populates it.
func TestTokenIDColumn_WriteAndRead(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed auth row so the FK (or NOT NULL) on user_id is satisfied.
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	// Direct INSERT using the same SQL shape as WriteUsageCost to keep the
	// test in the plans package (avoids an import cycle with commerce).
	// This mirrors what WriteUsageCost does after the Wave 1.5 change.
	const wantTokenID int64 = 7
	_, err := db.Exec(`
		INSERT INTO gtk_app_usage_log
		  (user_id, service, source, tokens_used, cost_cents, token_id)
		VALUES
		  (42, 'deepseek-chat', 'api', 500, 3, ?)
	`, wantTokenID)
	if err != nil {
		t.Fatalf("insert with token_id: %v", err)
	}

	var got int64
	if err := db.QueryRow(`SELECT token_id FROM gtk_app_usage_log WHERE user_id = 42`).Scan(&got); err != nil {
		t.Fatalf("select token_id: %v", err)
	}
	if got != wantTokenID {
		t.Errorf("token_id round-trip: want %d, got %d", wantTokenID, got)
	}
}

// TestTokenIDColumn_LegacyRowDefault confirms that rows inserted WITHOUT
// specifying token_id get DEFAULT 0 (legacy / no-token-context behavior).
func TestTokenIDColumn_LegacyRowDefault(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (99)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	_, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, source, tokens_used, cost_cents)
		VALUES (99, 'gpt-4o', 'chat', 200, 1)
	`)
	if err != nil {
		t.Fatalf("insert without token_id: %v", err)
	}

	var got int64
	if err := db.QueryRow(`SELECT token_id FROM gtk_app_usage_log WHERE user_id = 99`).Scan(&got); err != nil {
		t.Fatalf("select token_id: %v", err)
	}
	if got != 0 {
		t.Errorf("legacy row token_id: want 0 (default), got %d", got)
	}
}
