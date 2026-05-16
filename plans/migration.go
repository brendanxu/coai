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
//
// PKG-1 migration (v0.17, L23 lock 2026-05-09): added product_type / billing_mode
// / quota_grant / service_id discriminators to gtk_plan, plus product_type /
// cancellation_reason to gtk_user_plan, plus source / order_id / provider
// attribution columns to gtk_app_usage_log. See
// docs/strategy/2026-05-09-token-product-service-product-architecture.md.
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
//   - PKG-1 ALTER passes go through the same addColumnIfMissing helper so
//     re-boot is a no-op on a migrated DB.
func Migrate(db *sql.DB) error {
	if err := createAuditDeletionTable(db); err != nil {
		return fmt.Errorf("create gtk_audit_deletion: %w", err)
	}
	if err := createPlanTable(db); err != nil {
		return fmt.Errorf("create gtk_plan: %w", err)
	}
	if err := alterPlanForProductType(db); err != nil {
		return fmt.Errorf("alter gtk_plan for product_type: %w", err)
	}
	if err := createUserPlanTable(db); err != nil {
		return fmt.Errorf("create gtk_user_plan: %w", err)
	}
	if err := alterUserPlanForProductType(db); err != nil {
		return fmt.Errorf("alter gtk_user_plan for product_type: %w", err)
	}
	if err := alterUserPlanForSubscriptionID(db); err != nil {
		return fmt.Errorf("alter gtk_user_plan for subscription_id: %w", err)
	}
	if err := createAppUsageLogTable(db); err != nil {
		return fmt.Errorf("create gtk_app_usage_log: %w", err)
	}
	if err := upgradeAppUsageLogV2(db); err != nil {
		return fmt.Errorf("upgrade gtk_app_usage_log v2: %w", err)
	}
	if err := alterAppUsageLogForAttribution(db); err != nil {
		return fmt.Errorf("alter gtk_app_usage_log for attribution: %w", err)
	}
	if err := createProviderPricingTable(db); err != nil {
		return fmt.Errorf("create gtk_provider_pricing: %w", err)
	}
	if err := addProviderPricingDisplayColumns(db); err != nil {
		return fmt.Errorf("add gtk_provider_pricing display columns: %w", err)
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
	if err := seedTokenPlans(db); err != nil {
		return fmt.Errorf("seed gtk_plan token plans: %w", err)
	}
	if err := seedDisplayPricing(db); err != nil {
		return fmt.Errorf("seed gtk_provider_pricing display rows: %w", err)
	}
	return nil
}

// tokenPlanSeed lists the public token-product plans shipped with v0.22
// (token-distribution self-serve launch). Operations adds new plans by
// INSERT into gtk_plan directly — never UPDATE these seed rows in place,
// because gtk_user_plan rows reference plan_id and historical billing
// must replay against the price that was effective at purchase time.
//
// Idempotent: only inserted when no row with matching code exists, so
// re-boot is a no-op and ops-side INSERTs (with code != these) survive.
//
// PriceCents stored in CNY (¥99 = 9900). LemonSqueezy variant maps the
// USD price separately (¥99 ≈ $14 — close-enough single-tier alignment).
// Mainland customers pay CNY via hupijiao; overseas pay USD via LS.
var tokenPlanSeed = []struct {
	code         string
	name         string
	planType     string // legacy 'subscription' or 'pack'
	productType  string // L23 'token' or 'service'
	billingMode  string // 'subscription' / 'one_time' / 'top_up' / 'manual'
	priceCents   int64  // in CNY 分
	durationDays int64
	quotaGrant   int64
	quotaConfig  string // raw JSON; consumed by auth.RedeemPlanForOrder
}{
	{
		code:         "token-99",
		name:         "Token 套餐 ¥99/月",
		planType:     "subscription",
		productType:  "token",
		billingMode:  "subscription",
		priceCents:   9900,
		durationDays: 30,
		quotaGrant:   5000,
		quotaConfig:  `{"quota":5000,"reset":"monthly"}`,
	},
}

// seedTokenPlans inserts the v0.22 launch token-plan catalog. Idempotent
// per-row: skips when gtk_plan.code already exists. Engines diverge on
// JSON column handling (MySQL JSON vs SQLite TEXT) but the parametrised
// INSERT is portable as-is — driver translates ? bind to native type.
//
// SQLite path is skipped: ~10 test files across commerce/service/usage
// hardcode INSERT INTO gtk_plan (id, ...) VALUES (1, ...) and would
// collide with this seed's auto-id. Production uses MySQL exclusively
// so the seed runs on real boot; tests opt into a token-99 row via
// their own INSERT when they need one. Trade-off: this seed is exercised
// only in prod boot (and in a planned docker-mysql migration drill, not
// SQLite unit tests).
func seedTokenPlans(db *sql.DB) error {
	if globals.SqliteEngine {
		return nil
	}
	for _, p := range tokenPlanSeed {
		var count int
		row := globals.QueryRowDb(db,
			`SELECT COUNT(*) FROM gtk_plan WHERE code = ?`, p.code)
		if err := row.Scan(&count); err != nil {
			return fmt.Errorf("count gtk_plan code=%q: %w", p.code, err)
		}
		if count > 0 {
			continue
		}
		// SQLite branch lacks the auto-default columns the MySQL ENUMs
		// provide via DEFAULT clauses on alter, so write all columns
		// explicitly. is_active defaults TRUE on both.
		if _, err := globals.ExecDb(db, `
			INSERT INTO gtk_plan
			  (code, name, type, product_type, billing_mode,
			   price_cents, duration_days, quota_grant, quota_config, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE)
		`, p.code, p.name, p.planType, p.productType, p.billingMode,
			p.priceCents, p.durationDays, p.quotaGrant, p.quotaConfig); err != nil {
			return fmt.Errorf("insert gtk_plan code=%q: %w", p.code, err)
		}
	}
	return nil
}

// alterPlanForProductType adds the four PKG-1 discriminator columns to
// gtk_plan plus the product_type lookup index plus the FK to gtk_service.
// MySQL only — SQLite branch in createPlanTable already includes the
// columns inline. The FK to gtk_service uses addForeignKeyIfMissing so
// re-boot is a no-op once the constraint is in place.
func alterPlanForProductType(db *sql.DB) error {
	if err := addColumnIfMissing(db, "gtk_plan", "product_type",
		"ENUM('token','service') NOT NULL DEFAULT 'token' AFTER type"); err != nil {
		return fmt.Errorf("add product_type: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_plan", "billing_mode",
		"ENUM('subscription','one_time','top_up','manual') NOT NULL DEFAULT 'subscription' AFTER product_type"); err != nil {
		return fmt.Errorf("add billing_mode: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_plan", "quota_grant",
		"BIGINT NULL AFTER duration_days"); err != nil {
		return fmt.Errorf("add quota_grant: %w", err)
	}
	// service_id BIGINT (NOT INT) — gtk_service.id is BIGINT per
	// service/migration.go. ON DELETE SET NULL: retiring a service should
	// orphan the plan row, not drop it.
	if err := addColumnIfMissing(db, "gtk_plan", "service_id",
		"BIGINT NULL AFTER quota_grant"); err != nil {
		return fmt.Errorf("add service_id: %w", err)
	}
	if err := addIndexIfMissing(db, "gtk_plan", "idx_gtk_plan_product_type",
		"(product_type)"); err != nil {
		return fmt.Errorf("add idx_gtk_plan_product_type: %w", err)
	}
	if err := addForeignKeyIfMissing(db, "gtk_plan", "fk_gtk_plan_service_id",
		"FOREIGN KEY (service_id) REFERENCES gtk_service(id) ON DELETE SET NULL"); err != nil {
		return fmt.Errorf("add fk_gtk_plan_service_id: %w", err)
	}
	return nil
}

func alterUserPlanForProductType(db *sql.DB) error {
	if err := addColumnIfMissing(db, "gtk_user_plan", "product_type",
		"ENUM('token','service') NOT NULL DEFAULT 'token' AFTER plan_id"); err != nil {
		return fmt.Errorf("add product_type: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_user_plan", "cancellation_reason",
		"VARCHAR(64) NULL AFTER status"); err != nil {
		return fmt.Errorf("add cancellation_reason: %w", err)
	}
	if err := addIndexIfMissing(db, "gtk_user_plan",
		"idx_gtk_user_plan_user_product",
		"(user_id, product_type, status)"); err != nil {
		return fmt.Errorf("add idx_gtk_user_plan_user_product: %w", err)
	}
	return nil
}

// alterUserPlanForSubscriptionID adds the nullable subscription_id column to
// gtk_user_plan so renewal handlers can look up "all gtk_user_plan rows
// belonging to LS subscription X". Added by PKG-M1-① (2026-05-16).
//
// Uses a plain BIGINT (not FK) because gtk_ls_subscription.id is an
// auto-increment INT on MySQL; an FK here would require the subscription row
// to exist before the renewal row — but back-fill for legacy rows (NULL →
// populated) happens in the webhook handler, not here. The INT NULL shape
// is safe for both engines.
//
// The composite index (user_id, subscription_id) lets the renewal handler
// efficiently find "most recent gtk_user_plan for this user + subscription"
// without a full-table scan.
func alterUserPlanForSubscriptionID(db *sql.DB) error {
	if err := addColumnIfMissing(db, "gtk_user_plan", "subscription_id",
		"BIGINT NULL AFTER plan_id"); err != nil {
		return fmt.Errorf("add subscription_id: %w", err)
	}
	if err := addIndexIfMissing(db, "gtk_user_plan",
		"idx_gtk_user_plan_sub",
		"(user_id, subscription_id)"); err != nil {
		return fmt.Errorf("add idx_gtk_user_plan_sub: %w", err)
	}
	return nil
}

func alterAppUsageLogForAttribution(db *sql.DB) error {
	if err := addColumnIfMissing(db, "gtk_app_usage_log", "source",
		"ENUM('chat','api','service_order','admin_test') NOT NULL DEFAULT 'chat' AFTER service"); err != nil {
		return fmt.Errorf("add source: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_app_usage_log", "order_id",
		"VARCHAR(100) NULL AFTER source"); err != nil {
		return fmt.Errorf("add order_id: %w", err)
	}
	// Note: provider column is also added by upgradeAppUsageLogV2 (V2 cache
	// fields). PKG-1 originally added it again — but addColumnIfMissing is
	// idempotent so the second call is a no-op. We keep this stub for
	// PKG-1 audit-trail symmetry but no ALTER actually runs.
	if err := addIndexIfMissing(db, "gtk_app_usage_log",
		"idx_gtk_usage_source_order", "(source, order_id)"); err != nil {
		return fmt.Errorf("add idx_gtk_usage_source_order: %w", err)
	}
	return nil
}

// createAuditDeletionTable creates gtk_audit_deletion, the permanent audit
// trail for customer data deletion requests (PKG-D5). One row per deletion
// attempt; the table itself is never deleted even when a customer is erased.
//
// Columns:
//
//	coai_user_id             — which greentokey user was deleted
//	deletion_request_at      — when the CLI script started
//	deletion_completed_at    — when the last DELETE committed (NULL if error)
//	tables_affected          — count of tables that had rows deleted
//	rows_deleted_total       — sum of all rows deleted across tables
//	newapi_user_id           — the NewAPI user ID that was revoked (NULL if not bound)
//	newapi_revocation_status — 'success'|'skipped'|'failed'|NULL (not yet attempted)
//	error_message            — first error encountered (NULL on full success)
//	created_by               — 'cli'|'admin_ui'|'api' (always 'cli' in v1)
//	created_at               — row insert time
//
// No FK to auth(id) — auth row is deleted as part of the erasure; this
// audit row must survive to prove the deletion happened.
func createAuditDeletionTable(db *sql.DB) error {
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_audit_deletion (
			  id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			  coai_user_id             INTEGER NOT NULL,
			  deletion_request_at      DATETIME NOT NULL,
			  deletion_completed_at    DATETIME,
			  tables_affected          INTEGER NOT NULL DEFAULT 0,
			  rows_deleted_total       INTEGER NOT NULL DEFAULT 0,
			  newapi_user_id           INTEGER,
			  newapi_revocation_status TEXT
			                            CHECK (newapi_revocation_status IN
			                              ('success','skipped','failed',NULL)),
			  error_message            TEXT,
			  created_by               TEXT NOT NULL DEFAULT 'cli',
			  created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`); err != nil {
			return fmt.Errorf("create gtk_audit_deletion (sqlite): %w", err)
		}
		_, err := globals.ExecDb(db, `
			CREATE INDEX IF NOT EXISTS idx_gtk_audit_deletion_user_created
			ON gtk_audit_deletion(coai_user_id, created_at);
		`)
		return err
	}
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_audit_deletion (
		  id                       BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  coai_user_id             INT          NOT NULL,
		  deletion_request_at      DATETIME     NOT NULL,
		  deletion_completed_at    DATETIME     NULL,
		  tables_affected          INT          NOT NULL DEFAULT 0,
		  rows_deleted_total       INT          NOT NULL DEFAULT 0,
		  newapi_user_id           INT          NULL,
		  newapi_revocation_status VARCHAR(32)  NULL
		                            COMMENT 'success|skipped|failed|NULL',
		  error_message            TEXT         NULL,
		  created_by               VARCHAR(64)  NOT NULL DEFAULT 'cli',
		  created_at               DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  INDEX idx_gtk_audit_deletion_user_created (coai_user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`); err != nil {
		return fmt.Errorf("create gtk_audit_deletion (mysql): %w", err)
	}
	return nil
}

// addIndexIfMissing creates an index only if it doesn't already exist.
// SQLite skipped (test schemas don't need the perf index — same convention
// as service/migration.go).
func addIndexIfMissing(db *sql.DB, table, indexName, columns string) error {
	if globals.SqliteEngine {
		return nil
	}
	var count int
	row := globals.QueryRowDb(db, `
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?
	`, table, indexName)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("check index %s.%s: %w", table, indexName, err)
	}
	if count > 0 {
		return nil
	}
	_, err := globals.ExecDb(db, fmt.Sprintf(
		"ALTER TABLE %s ADD INDEX %s %s", table, indexName, columns))
	return err
}

// addForeignKeyIfMissing creates a named FK only if it doesn't already
// exist in INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS. SQLite skipped —
// the SQLite branch wires FKs at CREATE TABLE time.
func addForeignKeyIfMissing(db *sql.DB, table, constraintName, fkClause string) error {
	if globals.SqliteEngine {
		return nil
	}
	var count int
	row := globals.QueryRowDb(db, `
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = ? AND CONSTRAINT_NAME = ?
	`, table, constraintName)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("check fk %s.%s: %w", table, constraintName, err)
	}
	if count > 0 {
		return nil
	}
	_, err := globals.ExecDb(db, fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s %s", table, constraintName, fkClause))
	return err
}

func createPlanTable(db *sql.DB) error {
	if globals.SqliteEngine {
		// PKG-1: product_type / billing_mode / quota_grant / service_id added
		// inline to the SQLite branch since addColumnIfMissing on SQLite goes
		// through PRAGMA-driven columnExists (which won't find them in a
		// freshly-created table without the inline CREATE). service_id has no
		// FK in SQLite test schema (gtk_service may not be created in
		// isolated unit tests; service.Migrate seeds it in integration-style
		// tests).
		_, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_plan (
			  id            INTEGER PRIMARY KEY AUTOINCREMENT,
			  code          TEXT    NOT NULL UNIQUE,
			  name          TEXT    NOT NULL,
			  type          TEXT    NOT NULL CHECK (type IN ('subscription','pack')),
			  product_type  TEXT    NOT NULL DEFAULT 'token'
			                 CHECK (product_type IN ('token','service')),
			  billing_mode  TEXT    NOT NULL DEFAULT 'subscription'
			                 CHECK (billing_mode IN ('subscription','one_time','top_up','manual')),
			  price_cents   INTEGER NOT NULL,
			  duration_days INTEGER NOT NULL,
			  quota_grant   INTEGER,
			  service_id    INTEGER,
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
		// PKG-1: product_type + cancellation_reason added inline.
		// V0.16: order_id kept UNIQUE (was relaxed in v0.21 — restoring uniqueness
		// because order_no must collide-check at write time).
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_user_plan (
			  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id             INTEGER NOT NULL,
			  plan_id             INTEGER NOT NULL,
			  subscription_id     INTEGER,
			  product_type        TEXT    NOT NULL DEFAULT 'token'
			                       CHECK (product_type IN ('token','service')),
			  status              TEXT    NOT NULL DEFAULT 'active'
			                       CHECK (status IN ('active','expired','canceled')),
			  cancellation_reason TEXT,
			  expire_at           DATETIME,
			  remaining           TEXT,
			  purchased_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  order_id            TEXT    NOT NULL UNIQUE,
			  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
			  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_user_status ON gtk_user_plan(user_id, status);`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE UNIQUE INDEX IF NOT EXISTS idx_gtk_user_plan_order ON gtk_user_plan(order_id);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_user_product ON gtk_user_plan(user_id, product_type, status);`)
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
	//
	// PKG-1: source / order_id / provider added inline on the SQLite branch
	// (same backfill rationale as gtk_plan); MySQL gets them via
	// alterAppUsageLogForAttribution.
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
			  id          INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id     INTEGER NOT NULL,
			  plan_id     INTEGER,
			  service     TEXT    NOT NULL,
			  source      TEXT    NOT NULL DEFAULT 'chat'
			               CHECK (source IN ('chat','api','service_order','admin_test')),
			  order_id    TEXT,
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
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_usage_service_created ON gtk_app_usage_log(service, created_at);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_usage_source_order ON gtk_app_usage_log(source, order_id);`)
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
// upgradeAppUsageLogV2 + alterPlanForProductType + alterUserPlanForProductType
// + alterAppUsageLogForAttribution are safe to re-run on every boot. This is
// the unified V2 helper — works on both engines (PRAGMA on SQLite,
// INFORMATION_SCHEMA on MySQL) and supersedes the SQLite-skipping helper that
// PKG-1 originally shipped. PKG-1 alters that target SQLite-inline columns
// become no-ops via columnExists.
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
	{"provider", "VARCHAR(40) NULL", "TEXT NULL"},
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

// providerPricingDisplayColumns defines the 7 nullable display columns added
// by PKG-PRICING-DYNAMIC (2026-05-15). They live on gtk_provider_pricing so
// the public Pricing.tsx page can be driven from DB rather than a hardcoded
// array. Columns are nullable: NULL means "not published to the public page".
//
// Two engine variants are needed because SQLite uses TEXT/REAL/INTEGER type
// affinity (no DECIMAL/VARCHAR), but addColumnIfMissing handles dispatch.
type providerPricingDisplayCol struct {
	name      string
	mysqlDef  string
	sqliteDef string
}

var providerPricingDisplayCols = []providerPricingDisplayCol{
	{"display_in_cny_per_m", "DECIMAL(10,2) NULL", "REAL"},
	{"display_out_cny_per_m", "DECIMAL(10,2) NULL", "REAL"},
	{"display_credits_per_m", "INT NULL", "INTEGER"},
	{"display_name", "VARCHAR(60) NULL", "TEXT"},
	{"vendor_label", "VARCHAR(40) NULL", "TEXT"},
	{"context_size", "VARCHAR(20) NULL", "TEXT"},
	{"cache_flag", "VARCHAR(20) NULL", "TEXT"},
}

// addProviderPricingDisplayColumns adds the 7 nullable display columns to
// gtk_provider_pricing. Idempotent: each column is added only if absent,
// using the unified addColumnIfMissing helper (PRAGMA on SQLite,
// INFORMATION_SCHEMA on MySQL). Re-running on a migrated DB is a no-op.
func addProviderPricingDisplayColumns(db *sql.DB) error {
	for _, col := range providerPricingDisplayCols {
		def := col.mysqlDef
		if globals.SqliteEngine {
			def = col.sqliteDef
		}
		if err := addColumnIfMissing(db, "gtk_provider_pricing", col.name, def); err != nil {
			return fmt.Errorf("add %s: %w", col.name, err)
		}
	}
	return nil
}

// displayPricingSeed maps the 8 models currently shown in Pricing.tsx to the
// canonical (provider, model_id, token_type='input') key in gtk_provider_pricing.
// On first boot these rows either UPDATE an existing upstream-tracking row's
// display fields, or INSERT a new row if no upstream row exists for that model.
//
// Values extracted from Pricing.tsx MODEL_ROWS (hardcoded as of v0.30.0).
// vendor_label stores the Pricing.tsx "vendor" string (friendly); provider
// stores the canonical slug used by upstream cost tracking.
var displayPricingSeed = []struct {
	provider    string // canonical slug (must match upstream seed or be new)
	modelID     string // upstream model_id slug
	displayName string // Pricing.tsx model column
	vendorLabel string // Pricing.tsx vendor column
	contextSize string // Pricing.tsx context column
	priceIn     float64
	priceOut    float64
	creditsPerM int64
	cacheFlag   string // "true" | "cache_control" | "false"
}{
	{"openai", "gpt-4o", "GPT-4o", "openai", "128k", 18.20, 72.80, 3640, "true"},
	{"openai", "gpt-4o-mini", "GPT-4o mini", "openai", "128k", 1.10, 4.40, 220, "true"},
	{"anthropic", "claude-3-5-sonnet", "Claude 3.5 Sonnet", "anthropic", "200k", 21.60, 108.00, 5400, "cache_control"},
	{"deepseek", "deepseek-v3", "DeepSeek V3", "deepseek", "64k", 1.00, 4.00, 200, "true"},
	{"deepseek", "deepseek-r1", "DeepSeek R1", "deepseek", "64k", 4.00, 16.00, 800, "true"},
	{"alibaba", "qwen2.5-max", "Qwen2.5-Max", "阿里", "32k", 8.00, 24.00, 1200, "true"},
	{"google", "gemini-2.0-flash", "Gemini 2.0 Flash", "google", "1M", 0.72, 2.88, 144, "true"},
	{"moonshot", "kimi-k2", "Kimi K2", "moonshot", "200k", 12.00, 12.00, 600, "true"},
}

// seedDisplayPricing populates the 7 display_* columns for the 8 baseline
// models. Idempotent: for each seed row it tries to UPDATE the existing
// (provider, model_id, token_type='input') row first; if no row matches it
// INSERTs a new one with both upstream_per_m (estimated from display_in) and
// all display fields populated.
//
// Re-running this function is safe: the UPDATE is a no-op when the display
// fields are already set to the same values; the INSERT path guards with
// INSERT IGNORE / INSERT OR IGNORE so duplicate unique-key violations are
// silently skipped.
//
// SQLite note: unlike seedTokenPlans, this seed DOES run under SQLite because
// plans/migration_test.go exercises the display-pricing seed and we want
// idempotency to be exercised in unit tests too.
func seedDisplayPricing(db *sql.DB) error {
	for _, r := range displayPricingSeed {
		// Try to UPDATE an existing input row first.
		var res sql.Result
		var err error
		if globals.SqliteEngine {
			res, err = globals.ExecDb(db, `
				UPDATE gtk_provider_pricing
				SET display_in_cny_per_m  = ?,
				    display_out_cny_per_m = ?,
				    display_credits_per_m = ?,
				    display_name          = ?,
				    vendor_label          = ?,
				    context_size          = ?,
				    cache_flag            = ?
				WHERE provider = ? AND model_id = ? AND token_type = 'input'
			`, r.priceIn, r.priceOut, r.creditsPerM,
				r.displayName, r.vendorLabel, r.contextSize, r.cacheFlag,
				r.provider, r.modelID)
		} else {
			res, err = globals.ExecDb(db, `
				UPDATE gtk_provider_pricing
				SET display_in_cny_per_m  = ?,
				    display_out_cny_per_m = ?,
				    display_credits_per_m = ?,
				    display_name          = ?,
				    vendor_label          = ?,
				    context_size          = ?,
				    cache_flag            = ?
				WHERE provider = ? AND model_id = ? AND token_type = 'input'
			`, r.priceIn, r.priceOut, r.creditsPerM,
				r.displayName, r.vendorLabel, r.contextSize, r.cacheFlag,
				r.provider, r.modelID)
		}
		if err != nil {
			return fmt.Errorf("update display seed %s/%s: %w", r.provider, r.modelID, err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			continue // existing upstream row updated — done for this model
		}

		// No upstream row exists for this (provider, model_id, input) tuple.
		// Insert a new row with estimated upstream_per_m = display_in / 7.27
		// (approximate CNY→USD at 7.27 rate) as a placeholder that ops can
		// correct later via the admin UI.
		estimatedUpstreamPerM := r.priceIn / 7.27
		if globals.SqliteEngine {
			_, err = globals.ExecDb(db, `
				INSERT OR IGNORE INTO gtk_provider_pricing
				  (provider, model_id, token_type, upstream_per_m,
				   display_in_cny_per_m, display_out_cny_per_m, display_credits_per_m,
				   display_name, vendor_label, context_size, cache_flag)
				VALUES (?, ?, 'input', ?, ?, ?, ?, ?, ?, ?, ?)
			`, r.provider, r.modelID, estimatedUpstreamPerM,
				r.priceIn, r.priceOut, r.creditsPerM,
				r.displayName, r.vendorLabel, r.contextSize, r.cacheFlag)
		} else {
			_, err = globals.ExecDb(db, `
				INSERT IGNORE INTO gtk_provider_pricing
				  (provider, model_id, token_type, upstream_per_m,
				   display_in_cny_per_m, display_out_cny_per_m, display_credits_per_m,
				   display_name, vendor_label, context_size, cache_flag)
				VALUES (?, ?, 'input', ?, ?, ?, ?, ?, ?, ?, ?)
			`, r.provider, r.modelID, estimatedUpstreamPerM,
				r.priceIn, r.priceOut, r.creditsPerM,
				r.displayName, r.vendorLabel, r.contextSize, r.cacheFlag)
		}
		if err != nil {
			return fmt.Errorf("insert display seed %s/%s: %w", r.provider, r.modelID, err)
		}
	}
	return nil
}
