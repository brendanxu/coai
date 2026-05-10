-- v0.18 gtk_payment_session — slim audit/visibility table (PKG-2 Wave 1, L23)
-- This file is documentation / manual-run only. Runtime migration applies
-- via commerce/session_migration.go:Migrate(). Mirror any change here
-- when DDL changes there.
--
-- L23 lock: PKG-2 needs an ephemeral state row between "user clicks Buy"
-- and "webhook acks payment" so:
--   - the inbound webhook can match by SessionID (CR7 fix; the LS
--     custom_data and hupijiao prepay payload carry the SessionID)
--   - ops can see "how many checkouts opened today; how many converted"
--   - a stuck-pending sweeper (cron) can reap abandoned sessions
-- See architecture doc:
--   docs/strategy/2026-05-09-token-product-service-product-architecture.md §8
--   docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md (D2 + CR7)
--
-- Idempotency: CREATE TABLE IF NOT EXISTS only. No ALTERs in this file
-- because gtk_payment_session is brand-new in v0.18 — no existing rows
-- to migrate. If a future revision changes the schema, add ALTER
-- statements here and pair them with addColumnIfMissing helpers in
-- commerce/session_migration.go.
--
-- IMPORTANT (Codex M1 in plan v2): FK is ONLY to auth(id). NO FK to
-- gtk_user_plan or gtk_service_order — adding either would make this
-- table commerce of record (which it isn't) and would force boot order:
-- payment-session before plans/service. gtk_payment_session is purely
-- audit/visibility; the actual commerce state lives in those two tables.

CREATE TABLE IF NOT EXISTS gtk_payment_session (
  id            BIGINT       AUTO_INCREMENT PRIMARY KEY,
  session_id    VARCHAR(64)  NOT NULL UNIQUE COMMENT 'UUID embedded in provider custom_data; webhook matches on this',
  order_no      VARCHAR(64)  NOT NULL COMMENT 'gtk_user_plan.order_id or gtk_service_order.order_no',
  product_type  ENUM('token','service') NOT NULL COMMENT 'dispatch branch for ClosePaymentSession + GrantEntitlement',
  provider      VARCHAR(32)  NOT NULL COMMENT 'lemonsqueezy | hupijiao | manual',
  amount_cents  BIGINT       NOT NULL COMMENT 'expected total; webhook payload mismatch is logged not rejected',
  status        ENUM('pending','paid','expired','failed') NOT NULL DEFAULT 'pending',
  coai_user_id  INT          NOT NULL,
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at    DATETIME     NOT NULL COMMENT '+30min for LS auto-redirect, +72h for manual',
  closed_at     DATETIME     NULL COMMENT 'stamped by ClosePaymentSession (paid) or ExpirePaymentSessions (expired)',
  KEY idx_session_order (order_no),
  KEY idx_session_user_status (coai_user_id, status),
  KEY idx_session_expires (status, expires_at),
  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
