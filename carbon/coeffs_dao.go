// T2.6 — gtk_carbon_coeffs read-side DAO.
//
// Two functions used by factors.go to do DB-first lookup with JSON fallback:
//
//   getActiveCoeffFromDB(db, model, region) — single-row lookup
//   listAllActiveCoeffsFromDB(db)           — all currently-active rows
//
// "Active" = valid_from <= now AND (valid_to IS NULL OR valid_to > now).
// Lookup is case-insensitive on model + region (matches JSON behavior).
// On any DB error these helpers return ok=false (or empty slice) — the
// caller in factors.go falls back to the embedded JSON map.
//
// Uses CURRENT_TIMESTAMP rather than NOW() so the same SQL runs on both
// MySQL and SQLite without engine-dispatch.

package carbon

import (
	"database/sql"
)

// getActiveCoeffFromDB returns the most-recently-activated coefficient row
// matching (model, region). Case-insensitive on both. Returns ok=false on
// no-row, sql.ErrNoRows, or any other DB error — the caller falls back to
// the JSON map.
func getActiveCoeffFromDB(db *sql.DB, model, region string) (FactorEntry, bool) {
	var entry FactorEntry
	var notes sql.NullString
	err := db.QueryRow(`
		SELECT model, region, gco2e_per_1k_tokens, version, COALESCE(notes, '')
		FROM gtk_carbon_coeffs
		WHERE LOWER(model) = LOWER(?) AND LOWER(region) = LOWER(?)
		  AND valid_from <= CURRENT_TIMESTAMP
		  AND (valid_to IS NULL OR valid_to > CURRENT_TIMESTAMP)
		ORDER BY valid_from DESC
		LIMIT 1
	`, model, region).Scan(&entry.Model, &entry.Region, &entry.GCO2ePer1KTokens, &entry.Version, &notes)
	if err != nil {
		return FactorEntry{}, false
	}
	entry.Notes = notes.String
	// ValidFrom is not surfaced from the DB into FactorEntry; consumers don't
	// need it for the lookup hot path.
	return entry, true
}

// listAllActiveCoeffsFromDB returns every currently-active row, sorted by
// (model, region) ascending. Used by GetFactorsTable() to compose the
// /api/carbon/factors response from DB state when the table is non-empty.
// Returns an empty slice on error or empty table.
func listAllActiveCoeffsFromDB(db *sql.DB) []FactorEntry {
	rows, err := db.Query(`
		SELECT model, region, gco2e_per_1k_tokens, version, COALESCE(notes, '')
		FROM gtk_carbon_coeffs
		WHERE valid_from <= CURRENT_TIMESTAMP
		  AND (valid_to IS NULL OR valid_to > CURRENT_TIMESTAMP)
		ORDER BY model ASC, region ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []FactorEntry
	for rows.Next() {
		var e FactorEntry
		var notes sql.NullString
		if err := rows.Scan(&e.Model, &e.Region, &e.GCO2ePer1KTokens, &e.Version, &notes); err != nil {
			return nil
		}
		e.Notes = notes.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}
