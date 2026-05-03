// gtk_lead — marketing-side contact / demo-request capture.
//
// Why a separate table from gtk_user / gtk_waitlist:
//   - Leads aren't authenticated CoAI users yet; many will never
//     sign up. Storing them on auth.users would be wrong.
//   - gtk_waitlist is for "notify me when X service launches" (a
//     single email + service-slug pair). gtk_lead captures rich
//     demo-request info (微信号 + 手机号 + 民宿名 + 地址 + 备注).
//
// Privacy: 民宿主's contact info. We store the minimum needed for
// founder follow-up + auto-purge after follow-up completes. Status
// transitions: new → contacted → converted | dropped.
//
// Idempotency: UNIQUE on (wechat, phone) pair so accidental
// double-submit doesn't create dup leads. Re-submit updates the
// `notes` field (latest wins).

package lead

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

func Migrate(db *sql.DB) error {
	if globals.SqliteEngine {
		return migrateSQLite(db)
	}
	return migrateMySQL(db)
}

func migrateMySQL(db *sql.DB) error {
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_lead (
		  id              BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  wechat          VARCHAR(64),
		  phone           VARCHAR(32),
		  homestay_name   VARCHAR(128),
		  homestay_loc    VARCHAR(128),
		  notes           TEXT,
		  source          VARCHAR(64)  NOT NULL DEFAULT 'home',
		  status          VARCHAR(32)  NOT NULL DEFAULT 'new',
		  created_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  contacted_at    DATETIME     NULL,
		  KEY idx_lead_status (status, created_at),
		  KEY idx_lead_phone (phone),
		  KEY idx_lead_wechat (wechat)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return fmt.Errorf("create gtk_lead: %w", err)
	}
	return nil
}

func migrateSQLite(db *sql.DB) error {
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_lead (
		  id              INTEGER PRIMARY KEY AUTOINCREMENT,
		  wechat          TEXT,
		  phone           TEXT,
		  homestay_name   TEXT,
		  homestay_loc    TEXT,
		  notes           TEXT,
		  source          TEXT    NOT NULL DEFAULT 'home',
		  -- v0.13: removed SQLite CHECK constraint; v0.13 expanded the
		  -- kanban enum (new/contacted/signed/running/done/lost) and the
		  -- inline CHECK list would block fresh dev DBs from accepting
		  -- the new statuses. App-layer validation in lead/admin.go is
		  -- the single source of truth (validStatuses map).
		  status          TEXT    NOT NULL DEFAULT 'new',
		  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  contacted_at    DATETIME
		);
	`)
	return err
}
