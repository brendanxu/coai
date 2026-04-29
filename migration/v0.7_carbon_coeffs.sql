-- v0.7 carbon coefficients DB layer — schema reference (T2.6 Path B)
-- This file is documentation / manual-run only. The actual runtime migration
-- is applied via carbon/migration.go:Migrate() (MySQL) and the SQLite branch.
-- If you change the DDL there, mirror it here.
-- Idempotent via CREATE TABLE IF NOT EXISTS + count-based seed gate.
--
-- Replaces JSON-as-source-of-truth at runtime; carbon/data/carbon_factors.json
-- becomes the seed source on first boot + the runtime fallback when DB
-- returns no row. Per-row source_url solves the per-citation gap that
-- docs/branding-esg.md §2 anti-greenwashing rule wants. valid_to enables
-- coefficient retirement-without-deletion (audit-friendly).

CREATE TABLE IF NOT EXISTS gtk_carbon_coeffs (
  id                    INT           PRIMARY KEY AUTO_INCREMENT,
  model                 VARCHAR(100)  NOT NULL,
  region                VARCHAR(50)   NOT NULL,
  version               VARCHAR(20)   NOT NULL,
  gco2e_per_1k_tokens   DECIMAL(12,6) NOT NULL,
  source_url            VARCHAR(500)  NULL,
  valid_from            DATETIME      NOT NULL,
  valid_to              DATETIME      NULL,
  notes                 VARCHAR(500)  NULL,
  created_at            DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_model_region_validfrom (model, region, valid_from),
  INDEX idx_active_lookup (model, region, valid_from, valid_to)
);
