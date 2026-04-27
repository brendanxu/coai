package connection

import (
	"chat/globals"
	"database/sql"
	"strings"
)

func validSqlError(err error) bool {
	if err == nil {
		return false
	}

	content := err.Error()

	// Error 1060: Duplicate column name
	// Error 1050: Table already exists

	return !(strings.Contains(content, "Error 1060") || strings.Contains(content, "Error 1050"))
}

func checkSqlError(_ sql.Result, err error) error {
	if validSqlError(err) {
		return err
	}

	return nil
}

func execSql(db *sql.DB, sql string, args ...interface{}) error {
	return checkSqlError(globals.ExecDb(db, sql, args...))
}

func doMigration(db *sql.DB) error {
	if globals.SqliteEngine {
		return doSqliteMigration(db)
	}

	// v3.10 migration

	// update `quota`, `used` field in `quota` table
	// migrate `DECIMAL(16, 4)` to `DECIMAL(24, 6)`

	if err := execSql(db, `
		ALTER TABLE quota
		MODIFY COLUMN quota DECIMAL(24, 6),
		MODIFY COLUMN used DECIMAL(24, 6);
	`); err != nil {
		return err
	}

	// add new field `is_banned` in `auth` table
	if err := execSql(db, `
		ALTER TABLE auth
		ADD COLUMN is_banned BOOLEAN DEFAULT FALSE;
	`); err != nil {
		return err
	}

	// add new field `task_id` in `conversation` table to store task id (e.g., video job id)
	if err := execSql(db, `
		ALTER TABLE conversation
		ADD COLUMN task_id VARCHAR(255) NULL;
	`); err != nil {
		return err
	}

	// v0.6 carbon — usage_carbon: per-completion carbon footprint log
	if err := execSql(db, `
		CREATE TABLE IF NOT EXISTS usage_carbon (
			id                  BIGINT       PRIMARY KEY AUTO_INCREMENT,
			user_id             INT          NOT NULL,
			model               VARCHAR(100) NOT NULL,
			region              VARCHAR(50)  NULL,
			tokens              INT          NOT NULL,
			co2g_estimate       DECIMAL(12,4) NULL,
			coefficient_version VARCHAR(20)  NULL,
			eco_routed_from     VARCHAR(100) NULL,
			notes               VARCHAR(50)  NULL,
			created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user_created (user_id, created_at),
			INDEX idx_coefficient_gap (notes, model)
		);
	`); err != nil {
		return err
	}

	// v0.6 carbon — user_carbon_prefs: per-user Eco Mode + first-feedback flag
	if err := execSql(db, `
		CREATE TABLE IF NOT EXISTS user_carbon_prefs (
			user_id                       INT       PRIMARY KEY,
			eco_mode                      BOOLEAN   NOT NULL DEFAULT FALSE,
			eco_mode_first_feedback_seen  BOOLEAN   NOT NULL DEFAULT FALSE,
			updated_at                    DATETIME  NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}

	return nil
}

func doSqliteMigration(db *sql.DB) error {
	// v3.10 added sqlite support, no migration needed before this version

	// v4 migration
	// add new field `task_id` in `conversation` table to store task id (e.g., video job id)
	if err := execSql(db, `
		ALTER TABLE conversation
		ADD COLUMN task_id VARCHAR(255) NULL;
	`); err != nil {
		return err
	}

	// v0.6 carbon — usage_carbon (sqlite variant: AUTOINCREMENT spelled differently, indexes are separate statements)
	if err := execSql(db, `
		CREATE TABLE IF NOT EXISTS usage_carbon (
			id                  INTEGER      PRIMARY KEY AUTOINCREMENT,
			user_id             INTEGER      NOT NULL,
			model               TEXT         NOT NULL,
			region              TEXT,
			tokens              INTEGER      NOT NULL,
			co2g_estimate       REAL,
			coefficient_version TEXT,
			eco_routed_from     TEXT,
			notes               TEXT,
			created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}
	if err := execSql(db, `CREATE INDEX IF NOT EXISTS idx_usage_carbon_user_created ON usage_carbon(user_id, created_at);`); err != nil {
		return err
	}
	if err := execSql(db, `CREATE INDEX IF NOT EXISTS idx_usage_carbon_gap ON usage_carbon(notes, model);`); err != nil {
		return err
	}

	// v0.6 carbon — user_carbon_prefs (sqlite has no ON UPDATE; updated_at is set by application code)
	if err := execSql(db, `
		CREATE TABLE IF NOT EXISTS user_carbon_prefs (
			user_id                       INTEGER  PRIMARY KEY,
			eco_mode                      INTEGER  NOT NULL DEFAULT 0,
			eco_mode_first_feedback_seen  INTEGER  NOT NULL DEFAULT 0,
			updated_at                    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}

	return nil
}
