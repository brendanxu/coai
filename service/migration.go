// gtk_service / gtk_service_order / gtk_agent — Layer 3 service catalog
// of the greentokey 3-layer architecture (per
// docs/strategy/2026-04-30-business-model-v3.md).
//
// What lives here:
//
//   gtk_agent
//     The actual AI agents the platform offers. Each agent has a
//     system prompt, a preferred model, and a credit-tier minimum
//     (so a "premium" agent can't be served by a "light" channel).
//
//   gtk_service
//     A purchasable bundle. Each row binds an agent to a price and a
//     credit allotment. Three categories matching the v3 doc:
//       - diy_agent     ≈ ¥19/次
//       - content_pack  ≈ ¥299/组
//       - managed_ops   ≈ ¥2,800+/月
//     Customer-facing — non-tech buyers see service cards, never see
//     the credit count under the hood.
//
//   gtk_service_order
//     One row per purchase. Tracks payment provider (LemonSqueezy or
//     huppjao or manual), credits granted, agent run reference, and
//     fulfillment status.
//
// Why three tables vs one denormalized "service_purchases":
//   - agents are versioned independently of the bundles they appear in
//   - the same agent can power multiple service tiers (DIY single-shot
//     ¥19 vs monthly subscription ¥2,800)
//   - keeps the order audit trail intact even if catalog rows are
//     retired
//
// Idempotent migration; safe to call on every boot. Matches the
// payment / waitlist / plans / carbon / newapi pattern.

package service

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates gtk_agent / gtk_service / gtk_service_order. Idempotent.
//
// Order matters: gtk_service references gtk_agent; gtk_service_order
// references gtk_service. We CREATE TABLE IF NOT EXISTS in dependency
// order to keep the FK constraints clean on first boot.
func Migrate(db *sql.DB) error {
	if globals.SqliteEngine {
		return migrateSQLite(db)
	}
	return migrateMySQL(db)
}

func migrateMySQL(db *sql.DB) error {
	// gtk_agent — registry of bundled agents.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_agent (
		  id                BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  slug              VARCHAR(64)  NOT NULL,
		  name              VARCHAR(128) NOT NULL,
		  description       TEXT,
		  system_prompt     TEXT         NOT NULL,
		  preferred_model   VARCHAR(64)  NOT NULL,
		  min_tier          VARCHAR(16)  NOT NULL DEFAULT 'standard',
		  inputs_schema     JSON,
		  status            VARCHAR(16)  NOT NULL DEFAULT 'draft',
		  version           INT          NOT NULL DEFAULT 1,
		  created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  UNIQUE KEY uniq_agent_slug (slug),
		  KEY idx_agent_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`); err != nil {
		return fmt.Errorf("create gtk_agent: %w", err)
	}

	// gtk_service — purchasable bundles. Each row pins an agent at a
	// specific price + credit allotment.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_service (
		  id                  BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  slug                VARCHAR(64)  NOT NULL,
		  name                VARCHAR(128) NOT NULL,
		  description         TEXT,
		  category            VARCHAR(32)  NOT NULL,
		  agent_slug          VARCHAR(64)  NOT NULL,
		  price_cny_cents     BIGINT       NOT NULL,
		  included_credits    INT          NOT NULL DEFAULT 0,
		  billing_type        VARCHAR(16)  NOT NULL DEFAULT 'one_time',
		  status              VARCHAR(16)  NOT NULL DEFAULT 'draft',
		  ls_variant_id       VARCHAR(64),
		  display_order       INT          NOT NULL DEFAULT 0,
		  created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  UNIQUE KEY uniq_service_slug (slug),
		  KEY idx_service_status (status, category),
		  KEY idx_service_agent (agent_slug)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`); err != nil {
		return fmt.Errorf("create gtk_service: %w", err)
	}

	// gtk_service_order — fulfillment + payment audit trail. Designed
	// idempotent against LemonSqueezy webhook replays via UNIQUE
	// (ls_order_id) where applicable.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_service_order (
		  id                    BIGINT       AUTO_INCREMENT PRIMARY KEY,
		  order_no              VARCHAR(64)  NOT NULL,
		  coai_user_id          INT          NOT NULL,
		  service_id            BIGINT       NOT NULL,
		  service_slug          VARCHAR(64)  NOT NULL,
		  price_cny_cents_paid  BIGINT       NOT NULL,
		  credits_granted       INT          NOT NULL DEFAULT 0,
		  payment_provider      VARCHAR(32)  NOT NULL,
		  ls_order_id           VARCHAR(64),
		  hupijiao_trade_no     VARCHAR(64),
		  subscription_id       INT          NULL,
		  status                VARCHAR(32)  NOT NULL DEFAULT 'pending_payment',
		  paid_at               DATETIME     NULL,
		  completed_at          DATETIME     NULL,
		  agent_run_id          VARCHAR(64),
		  refund_reason         TEXT,
		  created_at            DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at            DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  UNIQUE KEY uniq_order_no (order_no),
		  UNIQUE KEY uniq_ls_order (ls_order_id),
		  KEY idx_order_user (coai_user_id, created_at),
		  KEY idx_order_service (service_id),
		  KEY idx_order_subscription (subscription_id),
		  KEY idx_order_status (status),
		  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE,
		  FOREIGN KEY (service_id) REFERENCES gtk_service(id) ON DELETE RESTRICT,
		  FOREIGN KEY (subscription_id) REFERENCES gtk_ls_subscription(id) ON DELETE SET NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`); err != nil {
		return fmt.Errorf("create gtk_service_order: %w", err)
	}

	// Idempotent ALTER for upgrade-in-place: if gtk_service_order was
	// created by an earlier service.Migrate (pre-subscription_id),
	// add the column + FK now. INFORMATION_SCHEMA check avoids "duplicate
	// column" errors on a fresh table where CREATE TABLE above already
	// added the column.
	if err := addColumnIfMissing(db, "gtk_service_order", "subscription_id",
		"INT NULL AFTER hupijiao_trade_no"); err != nil {
		return fmt.Errorf("add subscription_id column: %w", err)
	}
	// v0.14: persist agent output so the /services/run page can render
	// the result on refresh / reopen, not just inside the synchronous
	// POST response. Idempotent — does nothing if column exists.
	if err := addColumnIfMissing(db, "gtk_service_order", "agent_output",
		"MEDIUMTEXT NULL AFTER agent_run_id"); err != nil {
		return fmt.Errorf("add agent_output column: %w", err)
	}
	if err := addColumnIfMissing(db, "gtk_service_order", "agent_inputs",
		"TEXT NULL AFTER agent_output"); err != nil {
		return fmt.Errorf("add agent_inputs column: %w", err)
	}
	return nil
}

// addColumnIfMissing runs ALTER TABLE ... ADD COLUMN only if the column
// doesn't already exist. Used for backwards-compatible migrations on
// MySQL where CREATE TABLE IF NOT EXISTS is a no-op for existing tables.
// SQLite engine skips this — fresh CREATE TABLE always fires there.
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

func migrateSQLite(db *sql.DB) error {
	// SQLite test-friendly variants. Same column semantics, simplified
	// types (no JSON, no ON UPDATE), CHECK constraints replace ENUM.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_agent (
		  id                INTEGER PRIMARY KEY AUTOINCREMENT,
		  slug              TEXT    NOT NULL UNIQUE,
		  name              TEXT    NOT NULL,
		  description       TEXT,
		  system_prompt     TEXT    NOT NULL,
		  preferred_model   TEXT    NOT NULL,
		  min_tier          TEXT    NOT NULL DEFAULT 'standard'
		                     CHECK (min_tier IN ('light','standard','premium')),
		  inputs_schema     TEXT,
		  status            TEXT    NOT NULL DEFAULT 'draft'
		                     CHECK (status IN ('active','draft','retired')),
		  version           INTEGER NOT NULL DEFAULT 1,
		  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("create gtk_agent (sqlite): %w", err)
	}
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_service (
		  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
		  slug                TEXT    NOT NULL UNIQUE,
		  name                TEXT    NOT NULL,
		  description         TEXT,
		  category            TEXT    NOT NULL
		                       CHECK (category IN ('diy_agent','content_pack','managed_ops')),
		  agent_slug          TEXT    NOT NULL,
		  price_cny_cents     INTEGER NOT NULL,
		  included_credits    INTEGER NOT NULL DEFAULT 0,
		  billing_type        TEXT    NOT NULL DEFAULT 'one_time'
		                       CHECK (billing_type IN ('one_time','monthly','per_use')),
		  status              TEXT    NOT NULL DEFAULT 'draft'
		                       CHECK (status IN ('active','draft','retired')),
		  ls_variant_id       TEXT,
		  display_order       INTEGER NOT NULL DEFAULT 0,
		  created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("create gtk_service (sqlite): %w", err)
	}
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_service_order (
		  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
		  order_no              TEXT    NOT NULL UNIQUE,
		  coai_user_id          INTEGER NOT NULL,
		  service_id            INTEGER NOT NULL,
		  service_slug          TEXT    NOT NULL,
		  price_cny_cents_paid  INTEGER NOT NULL,
		  credits_granted       INTEGER NOT NULL DEFAULT 0,
		  payment_provider      TEXT    NOT NULL
		                         CHECK (payment_provider IN ('lemonsqueezy','hupijiao','manual')),
		  ls_order_id           TEXT    UNIQUE,
		  hupijiao_trade_no     TEXT,
		  subscription_id       INTEGER,
		  status                TEXT    NOT NULL DEFAULT 'pending_payment'
		                         CHECK (status IN ('pending_payment','paid','running','completed','refunded','failed')),
		  paid_at               DATETIME,
		  completed_at          DATETIME,
		  agent_run_id          TEXT,
		  -- v0.14: agent output + customer inputs persisted so the
		  -- /services/run page can render results on refresh.
		  agent_output          TEXT,
		  agent_inputs          TEXT,
		  refund_reason         TEXT,
		  created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE,
		  FOREIGN KEY (service_id) REFERENCES gtk_service(id) ON DELETE RESTRICT
		);
	`); err != nil {
		return fmt.Errorf("create gtk_service_order (sqlite): %w", err)
	}
	return nil
}
