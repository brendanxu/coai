// Package plans implements greentokey's 套餐 (subscription / pack) data model.
//
// It owns five tables that drive the user-facing pricing + billing layer:
//
//	gtk_plan              — plan catalog (subscription tier or one-shot pack)
//	gtk_user_plan         — user → plan binding with status + remaining quota
//	gtk_app_usage_log     — per-call usage record with 4-class token split
//	gtk_provider_pricing  — provider × model × token_type → upstream USD/M (V2, 2026-05-10)
//	gtk_billing_config    — global billing knobs (markup_multiplier) (V2, 2026-05-10)
//
// The package owns ONLY the schema + types in this dispatch. Business logic —
// buying flows, quota deduction, admin CRUD, billing calculator — belongs to
// other packages (billing/, payment/).
//
// All greentokey-specific tables are prefixed `gtk_*` (decision D2 in
// PROJECT_BRIEF.md §"主线 locked decisions") so this package can rebase
// against upstream CoAI without table-name collisions.
//
// V2 migration (2026-05-10): added 9 cache-aware columns to gtk_app_usage_log
// + two new tables (gtk_provider_pricing, gtk_billing_config). Drives the
// "we never lose money" billing rule documented in
// docs/research/token-cache-AUDIT-and-billing-design.md.
package plans

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates / upgrades the plans + billing tables. Idempotent: safe to
// call on every boot. Dispatches MySQL vs SQLite DDL based on
// globals.SqliteEngine because:
//
//   - gtk_plan uses ENUM + JSON which SQLite doesn't have — translate to
//     TEXT + CHECK and TEXT.
//   - INDEX clauses inside CREATE TABLE work in MySQL but cause syntax errors
//     in SQLite, which requires separate CREATE INDEX IF NOT EXISTS.
//   - V2 columns are added via addColumnIfMissing so existing prod tables
//     get backfilled without dropping data.
//
// Each helper wraps its DDL with fmt.Errorf so a failure clearly identifies
// which table failed (matches the payment + waitlist pattern).
func Migrate(db *sql.DB) error {
	if err := createPlanTable(db); err != nil {
		return fmt.Errorf("create gtk_plan: %w", err)
	}
	if err := createUserPlanTable(db); err != nil {
		return fmt.Errorf("create gtk_user_plan: %w", err)
	}
	if err := createAppUsageLogTable(db); err != nil {
		return fmt.Errorf("create gtk_app_usage_log: %w", err)
	}
	if err := upgradeAppUsageLogV2(db); err != nil {
		return fmt.Errorf("upgrade gtk_app_usage_log v2: %w", err)
	}
	if err := createProviderPricingTable(db); err != nil {
		return fmt.Errorf("create gtk_provider_pricing: %w", err)
	}
	if err := createBillingConfigTable(db); err != nil {
		return fmt.Errorf("create gtk_billing_config: %w", err)
	}
	if err := seedProviderPricing(db); err != nil {
		return fmt.Errorf("seed gtk_provider_pricing: %w", err)
	}
	if err := seedBillingConfig(db); err != nil {
		return fmt.Errorf("seed gtk_billing_config: %w", err)
	}
	return nil
}

func createPlanTable(db *sql.DB) error {
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_plan (
			  id            INTEGER PRIMARY KEY AUTOINCREMENT,
			  code          TEXT    NOT NULL UNIQUE,
			  name          TEXT    NOT NULL,
			  type          TEXT    NOT NULL CHECK (type IN ('subscription','pack')),
			  price_cents   INTEGER NOT NULL,
			  duration_days INTEGER NOT NULL,
			  quota_config  TEXT,
			  is_active     INTEGER NOT NULL DEFAULT 1,
			  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_plan (
		  id            INT          PRIMARY KEY AUTO_INCREMENT,
		  code          VARCHAR(50)  NOT NULL UNIQUE,
		  name          VARCHAR(100) NOT NULL,
		  type          ENUM('subscription','pack') NOT NULL,
		  price_cents   INT          NOT NULL,
		  duration_days INT          NOT NULL,
		  quota_config  JSON         NULL,
		  is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
		  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

func createUserPlanTable(db *sql.DB) error {
	// FK to auth(id) uses ON DELETE CASCADE for the same GDPR-friendly reason
	// payment/migration.go does (Codex P2 fix 2026-04-27): when a user is
	// deleted, their plan binding row goes with them.
	//
	// FK to gtk_plan(id) uses ON DELETE RESTRICT: a plan with active
	// user_plan rows must not be silently deletable — otherwise admin tooling
	// can orphan billing state. Founder retires plans by setting
	// gtk_plan.is_active = false, not by DELETE.
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_user_plan (
			  id           INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id      INTEGER NOT NULL,
			  plan_id      INTEGER NOT NULL,
			  status       TEXT    NOT NULL DEFAULT 'active'
			               CHECK (status IN ('active','expired','canceled')),
			  expire_at    DATETIME,
			  remaining    TEXT,
			  purchased_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  order_id     TEXT    NOT NULL UNIQUE,
			  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
			  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_user_status ON gtk_user_plan(user_id, status);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE UNIQUE INDEX IF NOT EXISTS idx_gtk_user_plan_order ON gtk_user_plan(order_id);`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_user_plan (
		  id           INT          PRIMARY KEY AUTO_INCREMENT,
		  user_id      INT          NOT NULL,
		  plan_id      INT          NOT NULL,
		  status       ENUM('active','expired','canceled') NOT NULL DEFAULT 'active',
		  expire_at    DATETIME     NULL,
		  remaining    JSON         NULL,
		  purchased_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  order_id     VARCHAR(100) NOT NULL,
		  INDEX idx_user_status (user_id, status),
		  UNIQUE KEY idx_order (order_id),
		  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
		  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
		);
	`)
	return err
}

func createAppUsageLogTable(db *sql.DB) error {
	// plan_id is intentionally nullable AND has no FK: a usage row records
	// what happened, not what billed. A user calling the chat endpoint with
	// no active plan still produces a usage row for cost accounting + future
	// pay-as-you-go billing. Keeping plan_id FK-free also lets this table
	// scale without join contention on the hot path.
	//
	// V2 (2026-05-10): the CREATE statement still ships the original 7
	// columns to keep restore-from-old-dump idempotent. New columns are
	// applied by upgradeAppUsageLogV2 below using addColumnIfMissing logic
	// that works on both SQLite (PRAGMA table_info) and MySQL
	// (INFORMATION_SCHEMA.COLUMNS).
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
			  id          INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id     INTEGER NOT NULL,
			  plan_id     INTEGER,
			  service     TEXT    NOT NULL,
			  tokens_used INTEGER NOT NULL DEFAULT 0,
			  cost_cents  INTEGER NOT NULL DEFAULT 0,
			  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_usage_user_created ON gtk_app_usage_log(user_id, created_at);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_usage_service_created ON gtk_app_usage_log(service, created_at);`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
		  id          BIGINT       PRIMARY KEY AUTO_INCREMENT,
		  user_id     INT          NOT NULL,
		  plan_id     INT          NULL,
		  service     VARCHAR(50)  NOT NULL,
		  tokens_used INT          NOT NULL DEFAULT 0,
		  cost_cents  INT          NOT NULL DEFAULT 0,
		  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  INDEX idx_user_created (user_id, created_at),
		  INDEX idx_service_created (service, created_at)
		);
	`)
	return err
}

// columnExists checks whether a column is already present on a table. Works
// on both engines: SQLite uses `PRAGMA table_info`, MySQL uses
// INFORMATION_SCHEMA. Returns false on any error so the caller can attempt
// the ADD COLUMN safely (a duplicate-column error is the desired backstop).
func columnExists(db *sql.DB, table, column string) (bool, error) {
	if globals.SqliteEngine {
		rows, err := globals.QueryDb(db, fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
				return false, err
			}
			if name == column {
				return true, nil
			}
		}
		return false, rows.Err()
	}
	var count int
	row := globals.QueryRowDb(db, `
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, table, column)
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// addColumnIfMissing runs ALTER TABLE ... ADD COLUMN only if absent. Used so
// upgradeAppUsageLogV2 is safe to re-run on every boot. Mirrors the helper
// in service/migration.go but supports both engines (the service version
// short-circuits SQLite because that package's CREATE always wins).
func addColumnIfMissing(db *sql.DB, table, column, columnDef string) error {
	exists, err := columnExists(db, table, column)
	if err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if exists {
		return nil
	}
	_, err = globals.ExecDb(db, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s %s", table, column, columnDef))
	return err
}

// V2 columns added to gtk_app_usage_log on 2026-05-10. The DEFAULT clauses
// matter: existing rows get safe zero values, new writes need every column
// or the calculator must explicitly null them.
//
// Pattern uses three knobs per engine:
//
//   - mysqlDef: full MySQL column definition (typed)
//   - sqliteDef: SQLite-flavoured definition (INTEGER replaces INT/BIGINT,
//     REAL replaces DECIMAL — SQLite stores everything as TEXT/INTEGER/REAL/
//     BLOB internally so type affinity is the closest match)
type usageLogV2Column struct {
	name      string
	mysqlDef  string
	sqliteDef string
}

var usageLogV2Columns = []usageLogV2Column{
	{"model_id", "VARCHAR(80) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
	{"provider", "VARCHAR(40) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
	{"input_tokens", "INT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"output_tokens", "INT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"cache_write_tokens", "INT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"cache_read_tokens", "INT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"cache_ttl", "VARCHAR(8) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
	{"upstream_cost_micro", "BIGINT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"client_charge_micro", "BIGINT NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
	{"markup_multiplier", "DECIMAL(4,3) NOT NULL DEFAULT 1.300", "REAL NOT NULL DEFAULT 1.300"},
}

// upgradeAppUsageLogV2 brings an existing gtk_app_usage_log up to the V2
// shape (cache-aware billing). Idempotent: every column add is gated by
// addColumnIfMissing.
//
// Why a separate function (vs amending createAppUsageLogTable):
//
//   - Existing prod data must survive — DROP+CREATE is unacceptable.
//   - SQLite's CREATE TABLE IF NOT EXISTS is a no-op when the table is
//     present, so widening the CREATE block alone wouldn't backfill.
//   - Splitting columns into a versioned slice (usageLogV2Columns) lets
//     future V3 work follow the same pattern.
func upgradeAppUsageLogV2(db *sql.DB) error {
	for _, col := range usageLogV2Columns {
		def := col.mysqlDef
		if globals.SqliteEngine {
			def = col.sqliteDef
		}
		if err := addColumnIfMissing(db, "gtk_app_usage_log", col.name, def); err != nil {
			return fmt.Errorf("add %s: %w", col.name, err)
		}
	}
	// Index on (provider, model_id, created_at) accelerates per-channel
	// billing audits and cache-hit-rate reports without touching the
	// existing two indexes.
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			CREATE INDEX IF NOT EXISTS idx_gtk_usage_provider_model_created
			ON gtk_app_usage_log(provider, model_id, created_at);
		`)
		return err
	}
	// MySQL has no IF NOT EXISTS for CREATE INDEX; check INFORMATION_SCHEMA
	// just like service/migration.go does for cross-table indexes.
	var count int
	row := globals.QueryRowDb(db, `
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME   = 'gtk_app_usage_log'
		  AND INDEX_NAME   = 'idx_gtk_usage_provider_model_created'
	`)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("check provider+model index: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err := globals.ExecDb(db, `
		ALTER TABLE gtk_app_usage_log
		  ADD INDEX idx_gtk_usage_provider_model_created (provider, model_id, created_at);
	`)
	return err
}

// createProviderPricingTable defines the operations-owned price book.
//
// One row = one (provider, model_id, token_type, effective_from). New prices
// are appended (never UPDATE) so historical bills can always be replayed
// using the rate that was effective at the moment of the call. The
// calculator selects the most recent row with `effective_from <= call_ts`.
//
// token_type values (controlled vocabulary, validated at INSERT site):
//
//	input
//	output
//	cache_write_5m   — Anthropic 5-minute TTL write multiplier
//	cache_write_1h   — Anthropic 1-hour TTL write multiplier
//	cache_read       — applies to all providers' cache reads
func createProviderPricingTable(db *sql.DB) error {
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_provider_pricing (
			  id              INTEGER PRIMARY KEY AUTOINCREMENT,
			  provider        TEXT     NOT NULL,
			  model_id        TEXT     NOT NULL,
			  token_type      TEXT     NOT NULL,
			  upstream_per_m  REAL     NOT NULL,
			  effective_from  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  notes           TEXT,
			  UNIQUE (provider, model_id, token_type, effective_from)
			);
		`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `
			CREATE INDEX IF NOT EXISTS idx_gtk_pricing_lookup
			ON gtk_provider_pricing(provider, model_id, token_type, effective_from);
		`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_provider_pricing (
		  id              INT           PRIMARY KEY AUTO_INCREMENT,
		  provider        VARCHAR(40)   NOT NULL,
		  model_id        VARCHAR(80)   NOT NULL,
		  token_type      VARCHAR(20)   NOT NULL,
		  upstream_per_m  DECIMAL(10,6) NOT NULL,
		  effective_from  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  notes           VARCHAR(255)  NULL,
		  UNIQUE KEY uk_pricing (provider, model_id, token_type, effective_from),
		  INDEX idx_lookup (provider, model_id, token_type, effective_from)
		);
	`)
	return err
}

// createBillingConfigTable holds global billing knobs as a tiny key-value
// store. Today there's exactly one knob (markup_multiplier) but the table
// is shaped to accept future flags (e.g. min_charge_micro,
// rounding_strategy) without another migration.
func createBillingConfigTable(db *sql.DB) error {
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_billing_config (
			  k TEXT PRIMARY KEY,
			  v TEXT NOT NULL
			);
		`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_billing_config (
		  k VARCHAR(40) PRIMARY KEY,
		  v VARCHAR(40) NOT NULL
		);
	`)
	return err
}

// providerPricingSeed lists the upstream USD/M numbers that are true on
// 2026-05-10. Sources cited in
// docs/research/token-cache-A-provider-api-survey.md.
//
// Operations adds new rows when prices change — never UPDATE — so historical
// bills replay correctly. To honor that, this seed is a one-shot insert
// guarded by a row-exists check; subsequent boots are no-ops.
var providerPricingSeed = []struct {
	provider, modelID, tokenType string
	upstreamPerM                 float64
	notes                        string
}{
	// Anthropic Claude Sonnet 4.5
	{"anthropic", "claude-sonnet-4.5", "input", 3.000000, "2026-05-10 base"},
	{"anthropic", "claude-sonnet-4.5", "output", 15.000000, "2026-05-10 base"},
	{"anthropic", "claude-sonnet-4.5", "cache_write_5m", 3.750000, "1.25x input"},
	{"anthropic", "claude-sonnet-4.5", "cache_write_1h", 6.000000, "2.0x input"},
	{"anthropic", "claude-sonnet-4.5", "cache_read", 0.300000, "0.1x input"},

	// Anthropic Claude Haiku 3.5
	{"anthropic", "claude-haiku-3.5", "input", 0.800000, "2026-05-10 base"},
	{"anthropic", "claude-haiku-3.5", "output", 4.000000, "2026-05-10 base"},
	{"anthropic", "claude-haiku-3.5", "cache_write_5m", 1.000000, "1.25x input"},
	{"anthropic", "claude-haiku-3.5", "cache_write_1h", 1.600000, "2.0x input"},
	{"anthropic", "claude-haiku-3.5", "cache_read", 0.080000, "0.1x input"},

	// DeepSeek V3 (auto KV cache, no explicit write tier)
	{"deepseek", "deepseek-v3", "input", 0.280000, "2026-05-10 miss"},
	{"deepseek", "deepseek-v3", "output", 1.100000, "2026-05-10 base"},
	{"deepseek", "deepseek-v3", "cache_read", 0.028000, "auto KV cache hit"},

	// OpenAI GPT-4o (auto cache, 25% read discount, no write surcharge)
	{"openai", "gpt-4o", "input", 2.500000, "2026-05-10 base"},
	{"openai", "gpt-4o", "output", 10.000000, "2026-05-10 base"},
	{"openai", "gpt-4o", "cache_read", 1.250000, "0.5x input (auto cache)"},
}

// seedProviderPricing inserts the V2 baseline. Idempotent: only inserts when
// the table is empty (operations adds prices manually after this).
func seedProviderPricing(db *sql.DB) error {
	var count int
	row := globals.QueryRowDb(db, `SELECT COUNT(*) FROM gtk_provider_pricing`)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("count gtk_provider_pricing: %w", err)
	}
	if count > 0 {
		return nil
	}
	for _, r := range providerPricingSeed {
		if _, err := globals.ExecDb(db, `
			INSERT INTO gtk_provider_pricing
			  (provider, model_id, token_type, upstream_per_m, notes)
			VALUES (?, ?, ?, ?, ?)
		`, r.provider, r.modelID, r.tokenType, r.upstreamPerM, r.notes); err != nil {
			return fmt.Errorf("seed %s/%s/%s: %w",
				r.provider, r.modelID, r.tokenType, err)
		}
	}
	return nil
}

// seedBillingConfig writes the default markup_multiplier. Idempotent via
// INSERT-OR-IGNORE semantics expressed differently per engine (SQLite uses
// INSERT OR IGNORE, MySQL uses INSERT IGNORE).
func seedBillingConfig(db *sql.DB) error {
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			INSERT OR IGNORE INTO gtk_billing_config (k, v)
			VALUES ('markup_multiplier', '1.300')
		`)
		return err
	}
	_, err := globals.ExecDb(db, `
		INSERT IGNORE INTO gtk_billing_config (k, v)
		VALUES ('markup_multiplier', '1.300')
	`)
	return err
}
