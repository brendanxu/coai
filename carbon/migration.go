// T2.6 — gtk_carbon_coeffs DB layer.
//
// This file extends the carbon package with a persistent coefficient table
// that:
//
//   1. Replaces JSON-as-source-of-truth at runtime (DB now authoritative)
//   2. Keeps embedded JSON as the seed source on first boot + the runtime
//      fallback if DB returns no row
//   3. Adds per-row source_url (was global in JSON) — anti-greenwashing
//      attribution per docs/branding-esg.md §2
//   4. Adds valid_to versioning column — schema supports retiring a
//      coefficient without DELETE; logic to write valid_to is Sprint 3+
//
// Design notes:
//
//   - Idempotent: Migrate runs on every boot. Table creation uses
//     CREATE TABLE IF NOT EXISTS; seeding uses a count > 0 gate so an
//     admin who deletes a row by hand doesn't get it auto-resurrected
//     on next boot.
//   - Engine dispatch matches the plans/migration.go T2.3 pattern:
//     each helper internally branches on globals.SqliteEngine.
//   - This file does NOT touch carbon/factors.go — DB-first lookup
//     wiring is in coeffs_dao.go + factors.go (separate concern,
//     separate commit).
//
// Decided 2026-04-30 per docs/codex-dispatch/T2.6-decision-record.md
// (Path B). Dispatch: T2.6-overnight-carbon-coeffs-db.md.

package carbon

import (
	"chat/globals"
	"database/sql"
	"fmt"
	"time"
)

// Migrate creates gtk_carbon_coeffs and seeds it from the embedded
// carbon_factors.json on first boot. Safe to call on every boot.
func Migrate(db *sql.DB) error {
	if err := createCoeffsTable(db); err != nil {
		return fmt.Errorf("create gtk_carbon_coeffs: %w", err)
	}
	if err := seedCoeffsIfEmpty(db); err != nil {
		return fmt.Errorf("seed gtk_carbon_coeffs: %w", err)
	}
	return nil
}

func createCoeffsTable(db *sql.DB) error {
	if globals.SqliteEngine {
		if _, err := globals.ExecDb(db, `
			CREATE TABLE IF NOT EXISTS gtk_carbon_coeffs (
			  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			  model               TEXT    NOT NULL,
			  region              TEXT    NOT NULL,
			  version             TEXT    NOT NULL,
			  gco2e_per_1k_tokens REAL    NOT NULL,
			  source_url          TEXT,
			  valid_from          DATETIME NOT NULL,
			  valid_to            DATETIME,
			  notes               TEXT,
			  created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`); err != nil {
			return err
		}
		if _, err := globals.ExecDb(db, `CREATE UNIQUE INDEX IF NOT EXISTS uniq_gtk_coeffs_model_region_validfrom ON gtk_carbon_coeffs(model, region, valid_from);`); err != nil {
			return err
		}
		_, err := globals.ExecDb(db, `CREATE INDEX IF NOT EXISTS idx_gtk_coeffs_active_lookup ON gtk_carbon_coeffs(model, region, valid_from, valid_to);`)
		return err
	}
	_, err := globals.ExecDb(db, `
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
	`)
	return err
}

// seedCoeffsIfEmpty reads the embedded carbon_factors.json and inserts each
// factor as a row when the table is empty. count > 0 gates idempotency:
// an admin who has deleted rows by hand doesn't see them auto-resurrected.
//
// Per dispatch §3.3:
//   - source_url seeded NULL (JSON has only global Sources[]; founder/admin
//     backfills per-row URLs later)
//   - valid_to seeded NULL (= currently active)
//   - valid_from parsed from JSON's ValidFrom string ("YYYY-MM-DD")
//   - on parse failure, fall back to time.Now() rather than skip the row
func seedCoeffsIfEmpty(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM gtk_carbon_coeffs").Scan(&count); err != nil {
		return fmt.Errorf("count gtk_carbon_coeffs: %w", err)
	}
	if count > 0 {
		return nil
	}
	table := GetFactorsTable() // loads embedded JSON via factorsOnce.Do()
	if table == nil || len(table.Factors) == 0 {
		return fmt.Errorf("seed: embedded factors empty — JSON parse failure?")
	}
	for _, f := range table.Factors {
		validFrom, _ := time.Parse("2006-01-02", f.ValidFrom)
		if validFrom.IsZero() {
			validFrom = time.Now() // defensive fallback if JSON has bad date
		}
		var notesArg interface{}
		if f.Notes != "" {
			notesArg = f.Notes
		} else {
			notesArg = nil
		}
		_, err := db.Exec(`
			INSERT INTO gtk_carbon_coeffs
			    (model, region, version, gco2e_per_1k_tokens, source_url, valid_from, valid_to, notes)
			VALUES
			    (?, ?, ?, ?, NULL, ?, NULL, ?)
		`, f.Model, f.Region, f.Version, f.GCO2ePer1KTokens, validFrom, notesArg)
		if err != nil {
			return fmt.Errorf("seed insert (%s/%s): %w", f.Model, f.Region, err)
		}
	}
	return nil
}
