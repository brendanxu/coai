// Package waitlist captures pre-launch interest in services that aren't live
// yet (greentokey v0.6.1+). The marketing landing page surfaces "Coming Soon"
// service cards; clicking one opens a modal that POSTs the visitor's email +
// service slug to /api/waitlist. This package owns that endpoint and its
// storage.
//
// All greentokey-specific tables are prefixed `gtk_*` so this package owns its
// schema cleanly and rebases against upstream CoAI stay surgical.
package waitlist

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates the waitlist table. Idempotent: safe to call on every boot.
//
//	gtk_waitlist — pre-launch email capture, one row per (email, service)
func Migrate(db *sql.DB) error {
	if err := createWaitlistTable(db); err != nil {
		return fmt.Errorf("create gtk_waitlist: %w", err)
	}
	return nil
}

func createWaitlistTable(db *sql.DB) error {
	// UNIQUE on (email, service) is the idempotency contract: HandleJoin
	// re-issues INSERTs blindly and treats a unique-violation as "already
	// in waitlist → success". A plain index would let duplicates leak in
	// under racing requests.
	//
	// `consent_ts` doubles as audit (when did this visitor consent?) and
	// future GDPR-erasure key (find rows older than N days, etc.). It is
	// the row's creation time; we never UPDATE it.
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_waitlist (
		  id INT PRIMARY KEY AUTO_INCREMENT,
		  email VARCHAR(254) NOT NULL,
		  service VARCHAR(64) NOT NULL,
		  source VARCHAR(64),
		  consent_ts DATETIME DEFAULT CURRENT_TIMESTAMP,
		  UNIQUE (email, service)
		);
	`)
	return err
}
