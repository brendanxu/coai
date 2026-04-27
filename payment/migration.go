// Package payment implements greentokey's external payment provider integration
// (LemonSqueezy v0.6+). It bridges LS subscription webhooks to CoAI's existing
// `subscription` table without going through CoAI's internal wallet (`user.Pay`).
//
// All greentokey-specific tables are prefixed `gtk_*` so this package owns its
// schema cleanly and rebases against upstream CoAI stay surgical.
package payment

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// Migrate creates greentokey's payment-bridge tables. Idempotent: safe to call
// on every boot. Schema:
//
//   gtk_ls_subscription   — maps LS subscription_id → user_id (audit + reverse lookup)
//   gtk_webhook_event     — event_id PK for webhook idempotency
//
// The authoritative subscription state lives in CoAI's existing `subscription`
// table (level + expired_at). gtk_ls_subscription is a mapping/audit layer.
func Migrate(db *sql.DB) error {
	if err := createLsSubscriptionTable(db); err != nil {
		return fmt.Errorf("create gtk_ls_subscription: %w", err)
	}
	if err := createWebhookEventTable(db); err != nil {
		return fmt.Errorf("create gtk_webhook_event: %w", err)
	}
	return nil
}

func createLsSubscriptionTable(db *sql.DB) error {
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_ls_subscription (
		  id INT PRIMARY KEY AUTO_INCREMENT,
		  user_id INT NOT NULL,
		  ls_subscription_id VARCHAR(64) NOT NULL UNIQUE,
		  variant_id VARCHAR(64),
		  status VARCHAR(32),
		  cancelled_at DATETIME NULL,
		  renews_at DATETIME NULL,
		  test_mode BOOLEAN DEFAULT FALSE,
		  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY (user_id) REFERENCES auth(id)
		);
	`)
	return err
}

func createWebhookEventTable(db *sql.DB) error {
	// event_id is LS's UUID — PRIMARY KEY makes idempotent INSERT trivial:
	// a duplicate webhook returns ER_DUP_ENTRY (1062) which the caller
	// interprets as "already processed → return 200 OK without re-acting".
	_, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_webhook_event (
		  event_id VARCHAR(128) PRIMARY KEY,
		  event_type VARCHAR(64) NOT NULL,
		  received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  processed_at DATETIME NULL
		);
	`)
	return err
}
