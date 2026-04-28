-- v0.6 carbon footprint v1 — schema reference
-- This file is documentation / manual-run only. The actual runtime migration
-- is applied via connection/db_migration.go:doMigration() (MySQL) and
-- doSqliteMigration() (SQLite). If you change the DDL there, mirror it here.
-- Idempotent via existing validSqlError handler (Error 1050 / 1060).

-- Per-completion carbon usage log
CREATE TABLE IF NOT EXISTS usage_carbon (
    id                  BIGINT       PRIMARY KEY AUTO_INCREMENT,
    user_id             INT          NOT NULL,
    model               VARCHAR(100) NOT NULL,
    region              VARCHAR(50)  NULL,             -- nullable: provider-determined, may be unknown
    tokens              INT          NOT NULL,
    co2g_estimate       DECIMAL(12,4) NULL,            -- NULL when coefficient missing or stream errored
    coefficient_version VARCHAR(20)  NULL,
    eco_routed_from     VARCHAR(100) NULL,             -- if eco-routed, original requested model
    notes               VARCHAR(50)  NULL,             -- 'stream_error' | 'coefficient_gap' | NULL
    created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_user_created (user_id, created_at),
    INDEX idx_coefficient_gap (notes, model)
);

-- Per-user carbon prefs (lazy created on first toggle of Eco Mode)
CREATE TABLE IF NOT EXISTS user_carbon_prefs (
    user_id                       INT       PRIMARY KEY,
    eco_mode                      BOOLEAN   NOT NULL DEFAULT FALSE,
    eco_mode_first_feedback_seen  BOOLEAN   NOT NULL DEFAULT FALSE,
    updated_at                    DATETIME  NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
