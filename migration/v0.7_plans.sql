-- v0.7 plans — schema reference for greentokey 套餐 data model (T2.3)
-- This file is documentation / manual-run only. The actual runtime migration
-- is applied via plans/migration.go:Migrate() (MySQL) and the SQLite branch.
-- If you change the DDL there, mirror it here.
-- Idempotent via CREATE TABLE IF NOT EXISTS.
--
-- Tables created:
--   gtk_plan          — plan catalog (subscription tier or one-shot pack)
--   gtk_user_plan     — user → plan binding with status + remaining quota
--   gtk_app_usage_log — per-call usage record (user × plan × service × tokens × cost)
--
-- Naming: gtk_* prefix per PROJECT_BRIEF.md decision D2 — all greentokey-specific
-- tables namespace this way to keep CoAI rebases surgical.

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
