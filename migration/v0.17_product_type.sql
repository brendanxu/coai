-- v0.17 product_type discriminator + provisioning retry queue (PKG-1, L23)
-- This file is documentation / manual-run only. Runtime migration applies
-- via plans/migration.go:Migrate() (gtk_plan / gtk_user_plan / gtk_app_usage_log
-- ALTERs) and newapi/migration.go:Migrate() (gtk_newapi_pending_provisions
-- CREATE). Mirror any change here when DDL changes there.
--
-- L23 lock: gtk_plan + gtk_user_plan need to discriminate Token Product vs
-- Service Product so the shared commerce backbone (PKG-2) can dispatch the
-- right provisioning + entitlement state machine on each product type
-- without a second backbone or a forked webhook. See architecture doc:
--   docs/strategy/2026-05-09-token-product-service-product-architecture.md
--
-- Idempotency:
--   - All ALTERs use INFORMATION_SCHEMA.COLUMNS lookup before adding (per
--     existing service/migration.go addColumnIfMissing helper pattern).
--   - All indexes use INFORMATION_SCHEMA.STATISTICS lookup (addIndexIfMissing).
--   - CREATE TABLE IF NOT EXISTS for the new table.
--
-- Backfill: production has 0 rows in gtk_plan / gtk_user_plan / gtk_app_usage_log
-- as of 2026-05-09 (no Token product live yet); defaults catch all rows.
-- KEPT (per L23 §16): gtk_newapi_binding 1:1 PRIMARY KEY (coai_user_id) — no
-- multi-token-per-user yet; defer to PKG-TEAM-QUOTA when explicit demand surfaces.
-- DEFERRED (plan §6 open issue): gtk_app_usage_log.cost_cents stays INT for
-- PKG-1; widening to DECIMAL(10,4) is a separate forward decision.

-- gtk_plan: discriminate by product_type so a single catalog table serves
-- both Token Plan rentals and Service Product rentals. billing_mode is
-- finer-grained than the legacy `type ENUM('subscription','pack')` — kept
-- both columns so existing reads of `type` continue working.
ALTER TABLE gtk_plan
  ADD COLUMN product_type ENUM('token','service') NOT NULL DEFAULT 'token' AFTER type,
  ADD COLUMN billing_mode ENUM('subscription','one_time','top_up','manual') NOT NULL DEFAULT 'subscription' AFTER product_type,
  ADD COLUMN quota_grant BIGINT NULL COMMENT 'credits/tokens granted per period for token plans' AFTER duration_days,
  ADD COLUMN service_id BIGINT NULL COMMENT 'FK to gtk_service for service plans (nullable for token plans)' AFTER quota_grant,
  ADD INDEX idx_gtk_plan_product_type (product_type),
  ADD CONSTRAINT fk_gtk_plan_service_id FOREIGN KEY (service_id) REFERENCES gtk_service(id) ON DELETE SET NULL;

-- gtk_user_plan: inherit product_type from plan at write time. Lets future
-- queries filter "all token plans this user holds" without a join.
-- cancellation_reason: free-text reason like 'refund_full', 'cancel_at_period_end'
-- so refund + cancel flows have an audit trail when status flips off active.
ALTER TABLE gtk_user_plan
  ADD COLUMN product_type ENUM('token','service') NOT NULL DEFAULT 'token' AFTER plan_id,
  ADD COLUMN cancellation_reason VARCHAR(64) NULL COMMENT 'refund_full | refund_partial | cancel_at_period_end | admin_revoke' AFTER status,
  ADD INDEX idx_gtk_user_plan_user_product (user_id, product_type, status);

-- gtk_app_usage_log: source classifies the call so revenue attribution can
-- split chat-driven token usage from service-order-driven token usage.
-- order_id (nullable) links a usage row back to gtk_service_order for service
-- runs — avoids a join when computing per-order cost. provider records the
-- upstream so per-provider margin math is one GROUP BY away.
-- NOT widened: cost_cents stays INT (open issue: sub-cent precision needs
-- DECIMAL widening — separate decision, plan §6 deferred to a follow-up PKG).
ALTER TABLE gtk_app_usage_log
  ADD COLUMN source ENUM('chat','api','service_order','admin_test') NOT NULL DEFAULT 'chat' AFTER service,
  ADD COLUMN order_id VARCHAR(100) NULL COMMENT 'nullable link to gtk_service_order.order_no for service runs' AFTER source,
  ADD COLUMN provider VARCHAR(64) NULL COMMENT 'upstream provider: openai/anthropic/deepseek/sub2api etc.' AFTER order_id,
  ADD INDEX idx_gtk_usage_source_order (source, order_id);

-- gtk_newapi_pending_provisions: durable retry queue for NewAPI provisioning
-- calls that fail transiently (network blip, quota exceeded, NewAPI 5xx).
-- Worker drains pending → retrying → succeeded|failed; gives the
-- LemonSqueezy webhook handler a "fire-and-forget with audit" path instead
-- of "succeed only if NewAPI is up at the exact moment of webhook delivery".
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
