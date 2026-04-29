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
			  order_id     TEXT    NOT NULL,
			  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
			  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_user_status ON gtk_user_plan(user_id, status);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_user_plan_order ON gtk_user_plan(order_id);`)
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
