// gtk_payment_session — slim audit/visibility table linking the
// "user clicked Buy" event to the eventual webhook ack. Owned by the
// commerce backbone (PKG-2, v0.18).
//
// Codex M1 in plan v2: this is NOT commerce of record. The actual
// commerce state lives in gtk_user_plan (token plans) and
// gtk_service_order (service products). gtk_payment_session is purely an
// audit + visibility layer:
//
//   - ops can see "how many checkouts opened today; how many converted"
//   - the inbound webhook can match by SessionID (CR7 fix; the LS
//     custom_data and hupijiao prepay payload carry the SessionID)
//   - a stuck-pending sweeper (cron) can flip abandoned sessions to
//     'expired' without touching the order tables
//
// FK is ONLY to auth(id). NO FK to gtk_user_plan or gtk_service_order —
// adding either would make this table commerce of record (which it isn't)
// and would force boot order: payment-session before plans/service. It's
// safer + more honest to leave it un-FK'd to the order tables.

package commerce

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates gtk_payment_session. Idempotent: safe to call on every
// boot. Dispatches MySQL vs SQLite DDL based on globals.SqliteEngine
// because SQLite lacks ENUM and ON UPDATE CURRENT_TIMESTAMP.
//
// Mirrors the payment / waitlist / plans / service / newapi / carbon
// pattern: one Migrate(db) entry point, idempotent, dual-engine.
func Migrate(db *sql.DB) error {
	if err := createPaymentSessionTable(db); err != nil {
		return fmt.Errorf("create gtk_payment_session: %w", err)
	}
	return nil
}

func createPaymentSessionTable(db *sql.DB) error {
	if globals.SqliteEngine {
		// SQLite test schema: ENUMs become CHECK constraints, BIGINT
		// AUTO_INCREMENT becomes INTEGER PRIMARY KEY AUTOINCREMENT,
		// indexes are inline-named via separate CREATE INDEX since
		// SQLite doesn't support inline KEY clauses.
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_payment_session (
			  id            INTEGER PRIMARY KEY AUTOINCREMENT,
			  session_id    TEXT    NOT NULL UNIQUE,
			  order_no      TEXT    NOT NULL,
			  product_type  TEXT    NOT NULL
			                 CHECK (product_type IN ('token','service')),
			  provider      TEXT    NOT NULL,
			  amount_cents  INTEGER NOT NULL,
			  status        TEXT    NOT NULL DEFAULT 'pending'
			                 CHECK (status IN ('pending','paid','expired','failed')),
			  coai_user_id  INTEGER NOT NULL,
			  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  expires_at    DATETIME NOT NULL,
			  closed_at     DATETIME,
			  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db,
			`CREATE INDEX IF NOT EXISTS idx_session_order ON gtk_payment_session(order_no);`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db,
			`CREATE INDEX IF NOT EXISTS idx_session_user_status ON gtk_payment_session(coai_user_id, status);`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db,
			`CREATE INDEX IF NOT EXISTS idx_session_expires ON gtk_payment_session(status, expires_at);`); err != nil {
			return err
		}
		return nil
	}

	// MySQL production schema. ENUMs are native; updated_at column is
	// intentionally absent because gtk_payment_session is write-once-
	// then-close-once — there's no general "updated" notion. closed_at
	// captures the only mutation point.
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_payment_session (
		  id            BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  session_id    VARCHAR(64)  NOT NULL UNIQUE,
		  order_no      VARCHAR(64)  NOT NULL,
		  product_type  ENUM('token','service') NOT NULL,
		  provider      VARCHAR(32)  NOT NULL,
		  amount_cents  BIGINT       NOT NULL,
		  status        ENUM('pending','paid','expired','failed') NOT NULL DEFAULT 'pending',
		  coai_user_id  INT          NOT NULL,
		  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  expires_at    DATETIME     NOT NULL,
		  closed_at     DATETIME     NULL,
		  KEY idx_session_order (order_no),
		  KEY idx_session_user_status (coai_user_id, status),
		  KEY idx_session_expires (status, expires_at),
		  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return err
	}
	return nil
}
