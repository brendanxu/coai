// Package plans implements greentokey's 套餐 (subscription / pack) data model.
//
// It owns three tables that drive the user-facing pricing layer:
//
//	gtk_plan          — plan catalog (subscription tier or one-shot pack)
//	gtk_user_plan     — user → plan binding with status + remaining quota
//	gtk_app_usage_log — per-call usage record (user × plan × service × tokens × cost)
//
// The package owns ONLY the schema + types in this dispatch (T2.3). Business
// logic — buying flows, quota deduction, admin CRUD — belongs to later sprints
// (Sprint 4 payment integration, Sprint 3 BYOK / cost cap).
//
// All greentokey-specific tables are prefixed `gtk_*` (decision D2 in
// PROJECT_BRIEF.md §"主线 locked decisions") so this package can rebase
// against upstream CoAI without table-name collisions.
package plans

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates the three plans tables. Idempotent: safe to call on every
// boot. Dispatches MySQL vs SQLite DDL based on globals.SqliteEngine because:
//
//   - gtk_plan uses ENUM + JSON which SQLite doesn't have — translate to
//     TEXT + CHECK and TEXT.
//   - INDEX clauses inside CREATE TABLE work in MySQL but cause syntax errors
//     in SQLite, which requires separate CREATE INDEX IF NOT EXISTS.
//
// Each helper wraps its DDL with fmt.Errorf so a failure clearly identifies
// which table failed (matches the payment + waitlist pattern).
//
// PKG-1 (v0.17, L23 lock 2026-05-09): after CREATE TABLE, runs ALTER passes
// to add product_type / billing_mode / quota_grant / service_id discriminators
// to gtk_plan, plus product_type / cancellation_reason to gtk_user_plan, plus
// source / order_id / provider attribution columns to gtk_app_usage_log. All
// ALTERs go through addColumnIfMissing so re-boot is a no-op on a migrated
// DB. See docs/strategy/2026-05-09-token-product-service-product-architecture.md.
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
	if err := alterPlanForProductType(db); err != nil {
		return fmt.Errorf("alter gtk_plan for product_type: %w", err)
	}
	if err := alterUserPlanForProductType(db); err != nil {
		return fmt.Errorf("alter gtk_user_plan for product_type: %w", err)
	}
	if err := alterAppUsageLogForAttribution(db); err != nil {
		return fmt.Errorf("alter gtk_app_usage_log for attribution: %w", err)
	}
	return nil
}

// alterPlanForProductType adds the four PKG-1 discriminator columns to
// gtk_plan plus the product_type lookup index plus the FK to gtk_service.
// MySQL only — SQLite branch in createPlanTable already includes the
// columns inline (extended below). The FK to gtk_service uses
// addForeignKeyIfMissing so re-boot is a no-op once the constraint is in place.
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

func alterAppUsageLogForAttribution(db *sql.DB) error {
	if err := addColumnIfMissing(db, "gtk_app_usage_log", "source",
		"ENUM('chat','api','service_order','admin_test') NOT NULL DEFAULT 'chat' AFTER service"); err != nil {
		return fmt.Errorf("add source: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_app_usage_log", "order_id",
		"VARCHAR(100) NULL AFTER source"); err != nil {
		return fmt.Errorf("add order_id: %w", err)
	}
	// provider = upstream label. Free-text VARCHAR per plan §6 (canonicalize
	// via lookup table later if too many distinct values surface).
	if err := addColumnIfMissing(db, "gtk_app_usage_log", "provider",
		"VARCHAR(64) NULL AFTER order_id"); err != nil {
		return fmt.Errorf("add provider: %w", err)
	}
	if err := addIndexIfMissing(db, "gtk_app_usage_log",
		"idx_gtk_usage_source_order", "(source, order_id)"); err != nil {
		return fmt.Errorf("add idx_gtk_usage_source_order: %w", err)
	}
	return nil
}

// addColumnIfMissing runs ALTER TABLE ... ADD COLUMN only if the column
// doesn't already exist. Mirrors service/migration.go addColumnIfMissing —
// duplicated here to keep the package internal-helper boundary intact.
// SQLite engine skips this — fresh CREATE TABLE in the SQLite branch
// already includes every column.
func addColumnIfMissing(db *sql.DB, table, column, columnDef string) error {
	if globals.SqliteEngine {
		return nil
	}
	var count int
	row := globals.QueryRowDb(db, `
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, table, column)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil
	}
	_, err := globals.ExecDb(db, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s %s", table, column, columnDef))
	return err
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
		// inline to the SQLite branch since addColumnIfMissing skips SQLite.
		// service_id has no FK in SQLite test schema (gtk_service may not be
		// created in isolated unit tests; service.Migrate seeds it in
		// integration-style tests).
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
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_user_plan (
			  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id             INTEGER NOT NULL,
			  plan_id             INTEGER NOT NULL,
			  product_type        TEXT    NOT NULL DEFAULT 'token'
			                       CHECK (product_type IN ('token','service')),
			  status              TEXT    NOT NULL DEFAULT 'active'
			                       CHECK (status IN ('active','expired','canceled')),
			  cancellation_reason TEXT,
			  expire_at           DATETIME,
			  remaining           TEXT,
			  purchased_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  order_id            TEXT    NOT NULL,
			  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
			  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_user_status ON gtk_user_plan(user_id, status);`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_order ON gtk_user_plan(order_id);`); err != nil {
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
		  INDEX idx_order (order_id),
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
	if globals.SqliteEngine {
		// PKG-1: source / order_id / provider added inline.
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
			  id          INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id     INTEGER NOT NULL,
			  plan_id     INTEGER,
			  service     TEXT    NOT NULL,
			  source      TEXT    NOT NULL DEFAULT 'chat'
			               CHECK (source IN ('chat','api','service_order','admin_test')),
			  order_id    TEXT,
			  provider    TEXT,
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
