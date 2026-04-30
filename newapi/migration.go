// gtk_newapi_binding — maps greentokey user_id (auth.id) to NewAPI's
// internal user_id + token_id. Idempotent migration; safe to call on
// every boot (matches the payment / waitlist / plans / carbon pattern).
//
// Why a separate table (vs storing on auth.User): keeps the binding +
// last-known-quota state local to greentokey for fast reads (dashboards,
// "remaining tokens" widgets) without round-tripping to NewAPI on every
// page load. Authoritative quota still lives in NewAPI; this table is
// a denormalized cache + audit log.
//
// Schema:
//   coai_user_id      — PK, FK to auth(id) ON DELETE CASCADE
//   newapi_user_id    — UNIQUE, the user.id we provisioned in NewAPI
//   newapi_token_id   — UNIQUE, the primary token's id (for top-up calls)
//   newapi_token_key  — sk-xxx api-key (TODO encrypt at rest in v2; for
//                        v0 prototype stored plain — these are OUR tokens
//                        not external customer keys, blast radius is
//                        gateway access, not third-party providers)
//   last_known_quota  — denormalized cache, refreshed on each top-up
//   created_at / updated_at — audit timestamps

package newapi

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates gtk_newapi_binding. Idempotent.
func Migrate(db *sql.DB) error {
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_newapi_binding (
			  coai_user_id      INTEGER PRIMARY KEY,
			  newapi_user_id    INTEGER NOT NULL UNIQUE,
			  newapi_token_id   INTEGER NOT NULL UNIQUE,
			  newapi_token_key  TEXT    NOT NULL,
			  last_known_quota  INTEGER NOT NULL DEFAULT 0,
			  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
			);
		`); err != nil {
			return fmt.Errorf("create gtk_newapi_binding: %w", err)
		}
		return nil
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_newapi_binding (
		  coai_user_id      INT          PRIMARY KEY,
		  newapi_user_id    INT          NOT NULL,
		  newapi_token_id   INT          NOT NULL,
		  newapi_token_key  VARCHAR(64)  NOT NULL,
		  last_known_quota  BIGINT       NOT NULL DEFAULT 0,
		  created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  UNIQUE KEY uniq_newapi_user (newapi_user_id),
		  UNIQUE KEY uniq_newapi_token (newapi_token_id),
		  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return fmt.Errorf("create gtk_newapi_binding: %w", err)
	}
	return nil
}

// Binding is the local in-Go view of a gtk_newapi_binding row.
type Binding struct {
	CoaiUserID     int64
	NewapiUserID   int64
	NewapiTokenID  int64
	NewapiTokenKey string
	LastKnownQuota int64
}

// LoadBinding fetches the binding for a greentokey user. Returns
// (nil, sql.ErrNoRows) when the user has never been provisioned.
func LoadBinding(db *sql.DB, coaiUserID int64) (*Binding, error) {
	var b Binding
	err := db.QueryRow(`
		SELECT coai_user_id, newapi_user_id, newapi_token_id,
		       newapi_token_key, last_known_quota
		FROM gtk_newapi_binding WHERE coai_user_id = ?
	`, coaiUserID).Scan(
		&b.CoaiUserID, &b.NewapiUserID, &b.NewapiTokenID,
		&b.NewapiTokenKey, &b.LastKnownQuota,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveBinding upserts a binding row. Used both on first-time provision
// and on top-ups (where last_known_quota changes).
func SaveBinding(db *sql.DB, b *Binding) error {
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			INSERT INTO gtk_newapi_binding
			    (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, last_known_quota)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(coai_user_id) DO UPDATE SET
			    newapi_user_id   = excluded.newapi_user_id,
			    newapi_token_id  = excluded.newapi_token_id,
			    newapi_token_key = excluded.newapi_token_key,
			    last_known_quota = excluded.last_known_quota,
			    updated_at       = CURRENT_TIMESTAMP
		`, b.CoaiUserID, b.NewapiUserID, b.NewapiTokenID, b.NewapiTokenKey, b.LastKnownQuota)
		return err
	}
	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_newapi_binding
		    (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, last_known_quota)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		    newapi_user_id   = VALUES(newapi_user_id),
		    newapi_token_id  = VALUES(newapi_token_id),
		    newapi_token_key = VALUES(newapi_token_key),
		    last_known_quota = VALUES(last_known_quota),
		    updated_at       = CURRENT_TIMESTAMP
	`, b.CoaiUserID, b.NewapiUserID, b.NewapiTokenID, b.NewapiTokenKey, b.LastKnownQuota)
	return err
}
