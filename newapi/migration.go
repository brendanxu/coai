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
	"context"
	"database/sql"
	"fmt"
)

// Migrate creates gtk_newapi_binding + gtk_newapi_pending_provisions. Idempotent.
//
// gtk_newapi_pending_provisions (PKG-1, v0.17, L23) is the durable retry
// queue for NewAPI provisioning calls that fail transiently. The
// LemonSqueezy webhook handler (and future hupijiao callback) enqueues
// here when a NewAPI top-up / token-issue call fails or times out; a
// background worker drains pending → retrying → succeeded|failed with
// exponential backoff (worker added in PKG-3 PKG-TOKEN-PRODUCT-RENTAL).
//
// L23 §16 KEEP 1:1: gtk_newapi_binding PRIMARY KEY (coai_user_id) is
// preserved as-is — no multi-token-per-user yet. Defer to PKG-TEAM-QUOTA
// when explicit demand surfaces.
func Migrate(db *sql.DB) error {
	if err := migrateBinding(db); err != nil {
		return err
	}
	if err := migratePendingProvisions(db); err != nil {
		return err
	}
	return nil
}

func migrateBinding(db *sql.DB) error {
	if globals.SqliteEngine {
		// PKG-2 Wave 1: newapi_group inline (addColumnIfMissing skips
		// SQLite). Default 'default' matches NewAPI's own column default.
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_newapi_binding (
			  coai_user_id      INTEGER PRIMARY KEY,
			  newapi_user_id    INTEGER NOT NULL UNIQUE,
			  newapi_token_id   INTEGER NOT NULL UNIQUE,
			  newapi_token_key  TEXT    NOT NULL,
			  newapi_group      TEXT    NOT NULL DEFAULT 'default',
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
	if _, err := globals.ExecDb(db, `
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
	`); err != nil {
		return fmt.Errorf("create gtk_newapi_binding: %w", err)
	}
	// PKG-2 Wave 1 (Q2 / CR8 / architecture §16 §19): per-user routing
	// group. Idempotent ALTER for upgrade-in-place; SQLite branch above
	// includes it inline. Default 'default' is NewAPI's own group default.
	if err := addColumnIfMissing(db, "gtk_newapi_binding", "newapi_group",
		"VARCHAR(64) NOT NULL DEFAULT 'default' AFTER newapi_token_key"); err != nil {
		return fmt.Errorf("add newapi_group: %w", err)
	}
	return nil
}

func migratePendingProvisions(db *sql.DB) error {
	if globals.SqliteEngine {
		// SQLite test schema. ENUMs become CHECK constraints; AUTO_INCREMENT
		// → AUTOINCREMENT; ON UPDATE CURRENT_TIMESTAMP omitted (SQLite
		// doesn't support it — tests don't depend on auto-update).
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_newapi_pending_provisions (
			  id              INTEGER PRIMARY KEY AUTOINCREMENT,
			  user_id         INTEGER NOT NULL,
			  plan_id         INTEGER NOT NULL,
			  provision_type  TEXT    NOT NULL
			                   CHECK (provision_type IN ('token_plan','service_workflow_rights')),
			  status          TEXT    NOT NULL DEFAULT 'pending'
			                   CHECK (status IN ('pending','retrying','succeeded','failed')),
			  retry_count     INTEGER NOT NULL DEFAULT 0,
			  last_attempt_at DATETIME,
			  last_error      TEXT,
			  succeeded_at    DATETIME,
			  failed_at       DATETIME,
			  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
			  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
			);
		`); err != nil {
			return fmt.Errorf("create gtk_newapi_pending_provisions (sqlite): %w", err)
		}
		if _, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_pending_status ON gtk_newapi_pending_provisions(status, provision_type);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_pending_user ON gtk_newapi_pending_provisions(user_id, status);`)
		return err
	}
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_newapi_pending_provisions (
		  id              BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  user_id         INT          NOT NULL,
		  plan_id         INT          NOT NULL,
		  provision_type  ENUM('token_plan','service_workflow_rights') NOT NULL,
		  status          ENUM('pending','retrying','succeeded','failed') NOT NULL DEFAULT 'pending',
		  retry_count     INT          NOT NULL DEFAULT 0,
		  last_attempt_at DATETIME     NULL,
		  last_error      TEXT         NULL,
		  succeeded_at    DATETIME     NULL,
		  failed_at       DATETIME     NULL,
		  created_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  KEY idx_gtk_pending_status (status, provision_type),
		  KEY idx_gtk_pending_user (user_id, status),
		  FOREIGN KEY (user_id) REFERENCES auth(id) ON DELETE CASCADE,
		  FOREIGN KEY (plan_id) REFERENCES gtk_plan(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return fmt.Errorf("create gtk_newapi_pending_provisions: %w", err)
	}
	return nil
}

// PendingProvision mirrors a row in gtk_newapi_pending_provisions. Used by
// the retry worker (PKG-3) to drain the queue. LastAttemptAt / LastError /
// SucceededAt / FailedAt are nullable since they're populated only after
// the first attempt / a final state transition.
type PendingProvision struct {
	ID            int64
	UserID        int64
	PlanID        int64
	ProvisionType string // 'token_plan' | 'service_workflow_rights'
	Status        string // 'pending' | 'retrying' | 'succeeded' | 'failed'
	RetryCount    int
	LastAttemptAt sql.NullTime
	LastError     sql.NullString
	SucceededAt   sql.NullTime
	FailedAt      sql.NullTime
}

// Binding is the local in-Go view of a gtk_newapi_binding row.
//
// PKG-2 Wave 1 (Q2 / CR8 / architecture §16 §19): Group is the NewAPI
// per-user routing group. Empty string is normalized to "default" by
// SaveBinding so the DB never holds NULL/empty here even if a caller
// forgets to set it.
type Binding struct {
	CoaiUserID     int64
	NewapiUserID   int64
	NewapiTokenID  int64
	NewapiTokenKey string
	Group          string
	LastKnownQuota int64
}

// LoadBinding fetches the binding for a greentokey user. Returns
// (nil, sql.ErrNoRows) when the user has never been provisioned.
func LoadBinding(db *sql.DB, coaiUserID int64) (*Binding, error) {
	var b Binding
	err := db.QueryRow(`
		SELECT coai_user_id, newapi_user_id, newapi_token_id,
		       newapi_token_key, newapi_group, last_known_quota
		FROM gtk_newapi_binding WHERE coai_user_id = ?
	`, coaiUserID).Scan(
		&b.CoaiUserID, &b.NewapiUserID, &b.NewapiTokenID,
		&b.NewapiTokenKey, &b.Group, &b.LastKnownQuota,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveBinding upserts a binding row. Used both on first-time provision
// and on top-ups (where last_known_quota changes).
//
// Group normalization: empty Group → "default" before write, so the DB
// never holds an empty string here even if older callers (pre-CR8)
// forget to set it.
func SaveBinding(db *sql.DB, b *Binding) error {
	group := b.Group
	if group == "" {
		group = "default"
	}
	if globals.SqliteEngine {
		_, err := globals.ExecDb(db, `
			INSERT INTO gtk_newapi_binding
			    (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(coai_user_id) DO UPDATE SET
			    newapi_user_id   = excluded.newapi_user_id,
			    newapi_token_id  = excluded.newapi_token_id,
			    newapi_token_key = excluded.newapi_token_key,
			    newapi_group     = excluded.newapi_group,
			    last_known_quota = excluded.last_known_quota,
			    updated_at       = CURRENT_TIMESTAMP
		`, b.CoaiUserID, b.NewapiUserID, b.NewapiTokenID, b.NewapiTokenKey, group, b.LastKnownQuota)
		return err
	}
	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_newapi_binding
		    (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
		VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		    newapi_user_id   = VALUES(newapi_user_id),
		    newapi_token_id  = VALUES(newapi_token_id),
		    newapi_token_key = VALUES(newapi_token_key),
		    newapi_group     = VALUES(newapi_group),
		    last_known_quota = VALUES(last_known_quota),
		    updated_at       = CURRENT_TIMESTAMP
	`, b.CoaiUserID, b.NewapiUserID, b.NewapiTokenID, b.NewapiTokenKey, group, b.LastKnownQuota)
	return err
}

// addColumnIfMissing runs ALTER TABLE ... ADD COLUMN only if the column
// doesn't already exist. Mirrors plans/migration.go and service/migration.go
// addColumnIfMissing — duplicated here per the package-internal-helper
// convention that keeps each schema package self-contained for clean
// upstream rebases. SQLite engine skips this; SQLite branch in the
// CREATE TABLE above already includes every column.
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

// SyncBindingGroup updates an existing user's NewAPI group remotely + locally.
// Algorithm (no partial state):
//   1. LoadBinding(coaiUserID) — must exist (returns sql.ErrNoRows otherwise)
//   2. Call NewAPI PUT /api/user/ with {id: newapi_user_id, group: newGroup}
//   3. On NewAPI success, UPDATE gtk_newapi_binding SET newapi_group, updated_at
//   4. Returns error on either step's failure; DB is only mutated after NewAPI ack
//
// Caller (PKG-4 admin UI / PKG-3 provisioning worker) is responsible for
// passing a group name that exists in NewAPI's group config — we don't
// validate. Empty newGroup is rejected to keep the "default" semantic
// explicit (caller passes "default" if that's what they mean).
//
// PKG-2 Wave 1 ships this with a no-op-style integration: callers exist
// in PKG-3+. Test coverage validates the DB layer; live NewAPI roundtrip
// gated behind newapi.IsConfigured() at call time.
func SyncBindingGroup(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error {
	if newGroup == "" {
		return fmt.Errorf("newapi: SyncBindingGroup: newGroup must be non-empty (pass 'default' explicitly)")
	}
	bind, err := LoadBinding(db, coaiUserID)
	if err != nil {
		return fmt.Errorf("load binding: %w", err)
	}
	cli, err := Default()
	if err != nil {
		return fmt.Errorf("newapi client: %w", err)
	}
	// Reuse UpdateUserQuotaRequest because PKG-2 extended it with Group;
	// passing Quota=0 with omitempty omits the field, so we patch group only.
	req := UpdateUserQuotaRequest{ID: bind.NewapiUserID, Group: newGroup}
	var env envelope[any]
	if err := cli.do(ctx, "PUT", "/api/user/", req, 0, &env); err != nil {
		return fmt.Errorf("newapi update group: %w", err)
	}
	if !env.Success {
		return fmt.Errorf("newapi update group: %s", env.Message)
	}
	// Only after NewAPI ack do we mutate local state.
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_newapi_binding
		SET newapi_group = ?, updated_at = CURRENT_TIMESTAMP
		WHERE coai_user_id = ?
	`, newGroup, coaiUserID); err != nil {
		return fmt.Errorf("update gtk_newapi_binding.newapi_group: %w", err)
	}
	return nil
}
