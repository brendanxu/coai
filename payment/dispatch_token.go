// dispatch_token.go — token-plan webhook dispatch path (PKG-2 Wave 3 C1).
//
// Extracted from lemonsqueezy.go's monolithic dispatch() to:
//   1. Reduce blast radius — token-plan changes no longer touch service code
//      paths, and vice-versa. (Codex H2 from autoplan.)
//   2. Make the file's purpose readable from the filename alone.
//   3. Open room for follow-up work (variant_id → plan mapping, multi-tier
//      pricing) without growing lemonsqueezy.go past comprehension.
//
// Public-ish surface (lowercase package-internal):
//
//   - handleTokenPlanEvent(db, p) — top-level entry for token-plan webhook
//                                    events. Branches on EventName.
//   - RefundTokenPlan(ctx, db, lsSubscriptionID, reason) — refund entry
//                                    point that calls
//                                    commerce.RevokeEntitlement (B3).
//                                    Public so admin/refund handlers wire
//                                    to it.
//
// Hard-won invariants preserved (autoplan I3 + I4 — both must survive the
// refactor; they are NOT cosmetic):
//
//   I3 (monotonic renews_at guard, line ~507-519 of original lemonsqueezy.go):
//       upsertLsMapping's UPDATE keeps the
//       AND (renews_at IS NULL OR renews_at <= ?)
//       clause. Two concurrent webhooks for the same subscription can
//       arrive out-of-order at the DB; this clause makes older events
//       idempotent no-ops instead of stale-state regressions.
//
//   I4 (monotonic expired_at guard, line ~564-579 of original lemonsqueezy.go):
//       activateExternalSubscription's UPDATE keeps the
//       AND (expired_at IS NULL OR expired_at <= ?)
//       clause. Same race protection but on CoAI's authoritative
//       subscription table.
//
// Both clauses were Codex P1 fixes from 2026-04-27. Removing either would
// re-introduce the race where a delayed renewal webhook clobbers a fresher
// expiry.
//
// Refund flow shape:
//
//   webhook (refund event) → RefundTokenPlan(ctx, db, ls_subscription_id, reason)
//   RefundTokenPlan → commerce.RevokeEntitlement(token)
//
// For token plans we use the LS subscription id itself as the order_no
// (Wave 4 D2 will lock this in when wiring OpenPaymentSession at checkout
// — until then the test path uses the ls_subscription_id directly).

package payment

import (
	"chat/auth"
	"chat/commerce"
	"chat/globals"
	"chat/newapi"
	"chat/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// handleTokenPlanEvent dispatches the LS subscription-* event family for
// token-plan webhooks (custom_data does NOT carry greentokey_order_no —
// that branch belongs to dispatch_service.go).
//
// Returns nil on success or a no-op (unknown event names ack silently).
// Returns err only on actual DB failure or invalid payload — caller
// (HandleWebhook) rolls back gtk_webhook_event on err so LS retries.
func handleTokenPlanEvent(db *sql.DB, p *webhookPayload) error {
	switch p.Meta.EventName {
	case eventCreated, eventUpdated, eventResumed:
		return upsertSubscription(db, p)
	case eventCancelled:
		return markCancelled(db, p)
	case eventPaymentFailed:
		// Log only; LS handles dunning. User keeps access until subscription_cancelled fires.
		logf(globals.Warn, "payment_failed", "ls_subscription_id", p.Data.ID)
		return nil
	case eventSubPayment:
		// PKG-M1-①: subscription_payment_success fires on every renewal cycle
		// (month 2, month 3, …). Create a new gtk_user_plan row so the user's
		// credit quota is topped up for the new period.
		return handleTokenRenewal(db, p)
	default:
		// Unknown event_name. Ack with 200 (don't 4xx — LS would mark endpoint broken).
		logf(globals.Info, "unknown_event", "event_type", p.Meta.EventName)
		return nil
	}
}

// upsertSubscription handles created/updated/resumed events. The semantics:
//   * created: first time this user subscribes — INSERT subscription row + INSERT mapping
//   * updated: renewal or plan change — UPDATE subscription.expired_at to new renews_at
//   * resumed: user un-cancelled before period ended — clear cancelled_at, refresh status
//
// All three converge on "ensure subscription.expired_at == LS renews_at" and
// "upsert mapping row with current LS state". The DB primitives are the same.
func upsertSubscription(db *sql.DB, p *webhookPayload) error {
	userID, err := userIDFromCustomData(p.Meta.CustomData)
	if err != nil {
		return err
	}
	renewsAt, err := time.Parse(time.RFC3339, p.Data.Attributes.RenewsAt)
	if err != nil {
		return fmt.Errorf("parse renews_at %q: %w", p.Data.Attributes.RenewsAt, err)
	}

	if err := activateExternalSubscription(db, userID, levelStarter, renewsAt); err != nil {
		return fmt.Errorf("activate: %w", err)
	}

	// v0.9: also provision (or top-up) NewAPI user + token so the user gets
	// an api-key (sk-xxx) the moment payment succeeds. Failure here is
	// LOGGED-NOT-FATAL: the user has already paid + CoAI subscription is
	// active; a follow-up retry queue (TODO gtk_newapi_pending_provisions)
	// re-tries provisioning. Returning an error here would 500 the webhook
	// → LemonSqueezy retries → potential double-activate. Better to ack +
	// retry async.
	if newapi.IsConfigured() {
		spec := newapi.PlanSpec{
			Code:       "starter",
			QuotaUnits: quotaUnitsForLevel(levelStarter),
			// 1-day grace past LS renews_at — protects users from instant
			// access loss if the next-month webhook is briefly delayed.
			ExpiresAt: renewsAt.Add(24 * time.Hour),
		}
		if _, err := newapi.ProvisionForPlan(context.Background(), db, userID, spec); err != nil {
			logf(globals.Warn, "newapi_provision_failed",
				"user_id", userID, "ls_subscription_id", p.Data.ID, "err", err)
		}
	}

	// Upsert the audit/mapping row. Existing cancelled_at gets cleared on resumed
	// (resumed = un-cancellation), preserved on update (re-billing of an active sub).
	clearCancelled := p.Meta.EventName == eventResumed
	return upsertLsMapping(db, userID, p, renewsAt, clearCancelled)
}

// upsertLsMapping inserts or updates the gtk_ls_subscription audit/mapping
// row.
//
// PRESERVES INVARIANT I3 (monotonic renews_at guard, autoplan): the UPDATE
// statement keeps `AND (renews_at IS NULL OR renews_at <= ?)` so a delayed
// older webhook can't clobber a fresher renews_at. Removing this clause
// re-opens the race documented in Codex P1 (2026-04-27).
func upsertLsMapping(db *sql.DB, userID int64, p *webhookPayload, renewsAt time.Time, clearCancelled bool) error {
	// Engine-agnostic upsert: try INSERT, swallow duplicate-key, then UPDATE
	// fields that can change. CoAI's globals.PreflightSql only translates
	// MySQL-specific ON DUPLICATE KEY UPDATE for the `quota` table, so we
	// can't rely on that syntax for new tables.
	variantID := fmt.Sprintf("%d", p.Data.Attributes.VariantID)
	renewsAtStr := utils.ConvertSqlTime(renewsAt)

	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_ls_subscription
			(user_id, ls_subscription_id, variant_id, status, renews_at, test_mode)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		userID, p.Data.ID, variantID,
		p.Data.Attributes.Status, renewsAtStr, p.Data.Attributes.TestMode)
	if err != nil && !isDupErr(err) {
		return err
	}

	// I3: Monotonic renews_at guard (Codex P1, 2026-04-27). DO NOT remove the
	// `AND (renews_at IS NULL OR renews_at <= ?)` clause — two concurrent
	// webhooks for the same subscription can land out-of-order; older events
	// arriving last must become no-ops instead of stale-state regressions.
	_, err = globals.ExecDb(db, `
		UPDATE gtk_ls_subscription
		SET variant_id = ?, status = ?, renews_at = ?, test_mode = ?
		WHERE ls_subscription_id = ?
		  AND (renews_at IS NULL OR renews_at <= ?)
	`,
		variantID, p.Data.Attributes.Status, renewsAtStr,
		p.Data.Attributes.TestMode, p.Data.ID, renewsAtStr)
	if err != nil {
		return err
	}

	if clearCancelled {
		_, err = globals.ExecDb(db,
			`UPDATE gtk_ls_subscription SET cancelled_at = NULL WHERE ls_subscription_id = ?`,
			p.Data.ID)
	}
	return err
}

// markCancelled handles subscription_cancelled. Per LS docs, the user
// retains access until period end — so we DO NOT touch CoAI's subscription
// table. We only set cancelled_at + status on the mapping row. CoAI's
// existing IsSubscribe() returns false naturally once expired_at passes.
func markCancelled(db *sql.DB, p *webhookPayload) error {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_ls_subscription
		SET cancelled_at = ?, status = ?
		WHERE ls_subscription_id = ?
	`, utils.ConvertSqlTime(time.Now()), p.Data.Attributes.Status, p.Data.ID)
	return err
}

// activateExternalSubscription is the bridge between LS payment events and
// CoAI's authoritative `subscription` table. It mirrors auth.User.AddSubscription
// but takes an explicit expiredAt (from LS renews_at) and bypasses user.Pay()
// — money flowed through LS, not CoAI's wallet.
//
// total_month=1 because LS bills monthly; we record one increment per webhook.
// On renewal (subscription_updated), the same DB row is UPDATEd with a new
// expired_at; total_month is NOT incremented here (LS payload doesn't tell us
// "this is renewal #N"; treating each event as +1 month would double-count).
//
// PRESERVES INVARIANT I4 (monotonic expired_at guard, autoplan): the UPDATE
// statement keeps `AND (expired_at IS NULL OR expired_at <= ?)` so a delayed
// older webhook can't roll back a fresher expiry. Removing this clause
// re-opens the race documented in Codex P1 (2026-04-27).
func activateExternalSubscription(db *sql.DB, userID int64, level int, expiredAt time.Time) error {
	if level < 1 {
		return fmt.Errorf("invalid plan level: %d", level)
	}
	date := utils.ConvertSqlTime(expiredAt)

	// Engine-agnostic upsert: INSERT (ignore dup-key) then UPDATE. We can't
	// use MySQL-specific ON DUPLICATE KEY UPDATE because CoAI's PreflightSql
	// doesn't translate it for non-quota tables.
	_, err := globals.ExecDb(db,
		`INSERT INTO subscription (user_id, expired_at, total_month, level) VALUES (?, ?, 1, ?)`,
		userID, date, level)
	if err != nil && !isDupErr(err) {
		return err
	}

	// I4: Monotonic expired_at guard (Codex P1, 2026-04-27). DO NOT remove the
	// `AND (expired_at IS NULL OR expired_at <= ?)` clause — only advance
	// expired_at, never roll it back. total_month is NOT touched here; LS
	// payload has no signal of "this is renewal #N".
	_, err = globals.ExecDb(db,
		`UPDATE subscription SET expired_at = ?, level = ?
		 WHERE user_id = ? AND (expired_at IS NULL OR expired_at <= ?)`,
		date, level, userID, date)
	return err
}

// handleTokenRenewal handles subscription_payment_success for the legacy
// levelStarter subscription path (no plan_code in custom_data). It creates a
// new gtk_user_plan row per renewal cycle so the user's quota is topped up.
//
// First-month-vs-renewal detection: LS fires subscription_payment_success
// alongside order_created + subscription_created on the very first purchase
// (BL-01 multi-fire pattern). To avoid a double-grant on month 1, we look up
// the most recent gtk_user_plan for this subscription; if it was purchased
// within the last 24h, the initial order_created handler already covered it
// and we skip. Month 2+ rows are older than 24h → create new renewal row.
//
// Dedup key for the new gtk_user_plan row:
//   "renew-" + ls_subscription_id + "-" + renews_at_date (YYYY-MM-DD)
// This is stable across LS retries of the same webhook (same renews_at) and
// unique per billing cycle (renews_at advances monthly). The outer
// gtk_webhook_event SHA256 provides a second dedup layer.
//
// Back-fill: existing gtk_user_plan rows with NULL subscription_id but
// matching (user_id, plan_code='token-99') get back-filled on first renewal.
// This covers purchases made before PKG-M1-① deployed.
func handleTokenRenewal(db *sql.DB, p *webhookPayload) error {
	userID, err := userIDFromCustomData(p.Meta.CustomData)
	if err != nil {
		return fmt.Errorf("token_renewal: user_id: %w", err)
	}
	lsSubID := p.Data.ID // LS subscription id (string, e.g. "123456")

	// Look up the most recent gtk_user_plan row for this subscription.
	// NULL subscription_id rows may exist for pre-PKG-M1 purchases — we join
	// on user_id as a fallback so back-fill can happen.
	var latestPurchasedAt sql.NullString
	var latestID int64
	err = db.QueryRow(`
		SELECT id, purchased_at FROM gtk_user_plan
		WHERE subscription_id = ? AND user_id = ?
		ORDER BY id DESC LIMIT 1
	`, lsSubID, userID).Scan(&latestID, &latestPurchasedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("token_renewal: lookup latest row: %w", err)
	}

	if latestPurchasedAt.Valid {
		// Parse the purchased_at timestamp. Both SQLite ("2006-01-02 15:04:05")
		// and MySQL ("2006-01-02T15:04:05Z") formats are tried.
		var parsed time.Time
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if t, e := time.Parse(layout, latestPurchasedAt.String); e == nil {
				parsed = t
				break
			}
		}
		if !parsed.IsZero() && time.Since(parsed) < 24*time.Hour {
			// Most recent row is <24h old → initial purchase already handled.
			logf(globals.Info, "token_renewal_skipped_first_month",
				"ls_subscription_id", lsSubID, "user_id", userID,
				"latest_purchased_at", latestPurchasedAt.String)
			return nil
		}
	}

	// Build a stable, cycle-unique order_id for the new gtk_user_plan row.
	renewsAt := p.Data.Attributes.RenewsAt
	renewsDate := renewsAt
	if len(renewsAt) >= 10 {
		renewsDate = renewsAt[:10] // YYYY-MM-DD
	}
	renewalOrderID := fmt.Sprintf("renew-%s-%s", lsSubID, renewsDate)

	// auth.RedeemPlanForOrder inserts gtk_user_plan + tops up quota.
	// It is idempotent on order_id: a second call with the same renewalOrderID
	// is a no-op, which protects against LS retry storms.
	if err := auth.RedeemPlanForOrder(db, userID, "token-99", renewalOrderID); err != nil {
		return fmt.Errorf("token_renewal: redeem: %w", err)
	}

	// Stamp subscription_id on the freshly created row.
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_user_plan SET subscription_id = ?
		WHERE order_id = ? AND subscription_id IS NULL
	`, lsSubID, renewalOrderID); err != nil {
		// Non-fatal: the row was created; subscription_id is for query
		// efficiency, not correctness. Log and continue.
		logf(globals.Warn, "token_renewal_sub_id_stamp_failed",
			"order_id", renewalOrderID, "err", err)
	}

	// Back-fill subscription_id on legacy NULL rows for this user so future
	// renewals can find them via the subscription_id index.
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_user_plan SET subscription_id = ?
		WHERE user_id = ? AND subscription_id IS NULL AND product_type = 'token'
	`, lsSubID, userID); err != nil {
		logf(globals.Warn, "token_renewal_backfill_failed",
			"ls_subscription_id", lsSubID, "user_id", userID, "err", err)
	}

	logf(globals.Info, "token_renewal_created",
		"ls_subscription_id", lsSubID, "user_id", userID,
		"renewal_order_id", renewalOrderID)
	return nil
}

// RefundTokenPlan is the public refund entry point for token plans.
// Resolves the LS subscription_id back to the greentokey order_no (we use
// the LS subscription_id as the order_no for token plans pre-Wave 4) and
// hands off to commerce.RevokeEntitlement which is IDEMPOTENT (H4).
//
// reason is one of the strings architecture §9.1 defines:
//   'refund_full' | 'admin_revoke' | 'cancel_at_period_end' | ...
//
// Returns the resulting EntitlementState (caller can audit-log) and any
// error from the DB / NewAPI flip. On already-canceled plans, returns
// (currentState, nil) so callers can ack the webhook idempotently.
func RefundTokenPlan(ctx context.Context, db *sql.DB, lsSubscriptionID, reason string) (commerce.EntitlementState, error) {
	if lsSubscriptionID == "" {
		return "", errors.New("payment.RefundTokenPlan: lsSubscriptionID required")
	}
	// For token plans, the order_no IS the LS subscription id (until Wave 4
	// D2 unifies via OpenPaymentSession). Pass through directly.
	return commerce.RevokeEntitlement(ctx, db, lsSubscriptionID, commerce.ProductToken, reason)
}
