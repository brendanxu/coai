// dispatch_service.go — service-order webhook dispatch path (PKG-2 Wave 3 C2).
//
// Extracted from lemonsqueezy.go's monolithic dispatchServiceOrder() to:
//   1. Reduce blast radius (autoplan H2) — service-order changes no longer
//      sit next to token-plan code paths.
//   2. Wire the unified commerce backbone primitives:
//        - service.MarkOrderPaid     (existing — sets paid_at, ls_order_id)
//        - commerce.GrantEntitlement (Wave 2.5 B3 — unified post-payment hook)
//        - commerce.ClosePaymentSession (Wave 2 B1 — session lifecycle close)
//      All three are IDEMPOTENT, so a webhook retry running the chain twice
//      is safe.
//
// Why all three calls (and not just GrantEntitlement)?
//   service.MarkOrderPaid is the only call that records the LS external id
//   (ls_order_id) + paid_at timestamp on gtk_service_order. GrantEntitlement
//   only does the CAS pending_payment → paid. We keep MarkOrderPaid for the
//   audit fields and let GrantEntitlement no-op (CAS observes status='paid'
//   already, returns changed=false, nil).
//
//   ClosePaymentSession is the audit/visibility flip on gtk_payment_session.
//   We look up the session id from custom_data["greentokey_session_id"] —
//   Wave 4 D2 wires this at checkout. Pre-Wave-4 orders have no session_id
//   in custom_data, so we skip the close (debug-log) and let
//   GrantEntitlement do its thing.
//
// Architecture refs:
//   - docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md §C2
//   - commerce/entitlement.go (B3 GrantEntitlement contract)
//   - commerce/session.go (B1 ClosePaymentSession contract)

package payment

import (
	"chat/commerce"
	"chat/globals"
	"chat/service"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// handleServiceEvent dispatches the LS event family for service-order
// webhooks (custom_data carries greentokey_order_no — that branch belongs
// to dispatch_service.go, not dispatch_token.go).
//
// v0.10 first-cut handling:
//   - order_created             → MarkOrderPaid + GrantEntitlement + Close
//   - subscription_created      → same (covers first-month subscriptions)
//   - subscription_cancelled    → log only (order itself is paid)
//   - subscription_payment_*    → DEFERRED (v0.11 cron creates new monthly row)
//   - other                     → log + ack
//
// Returns nil on success or no-op (unknown event names ack silently).
// Returns err only on real DB failure — caller (HandleWebhook) rolls back
// gtk_webhook_event on err so LS retries.
func handleServiceEvent(db *sql.DB, p *webhookPayload, orderNo string) error {
	switch p.Meta.EventName {
	case eventOrderCreated, eventCreated:
		return handleServiceOrderPaid(db, p, orderNo)

	case eventCancelled:
		logf(globals.Info, "service_order_subscription_cancelled",
			"order_no", orderNo, "ls_subscription_id", p.Data.ID,
			"note", "order itself is already paid; refund via /admin/refund if needed")
		return nil

	case eventSubPayment:
		// PKG-M1-①: subscription_payment_success on a service subscription.
		// Create a new gtk_service_order row for the next billing cycle.
		return handleServiceRenewal(db, p, orderNo)

	default:
		logf(globals.Info, "service_order_event_acked",
			"order_no", orderNo, "event", p.Meta.EventName)
		return nil
	}
}

// handleServiceOrderPaid runs the unified post-payment chain for a
// service-order webhook event:
//
//  1. service.MarkOrderPaid — flips status='paid', records ls_order_id +
//                              paid_at. Idempotent on second call.
//  2. commerce.GrantEntitlement — unified post-payment hook (Wave 2.5 B3).
//                                  CAS is no-op on already-paid; safe.
//  3. commerce.ClosePaymentSession — session lifecycle close (Wave 2 B1).
//                                     Idempotent on all 3 failure modes
//                                     (already-paid / terminal / not-found),
//                                     all return nil.
//
// If MarkOrderPaid fails, return the error so the webhook retries (the
// transaction model is "all three or none" at the LS retry layer; dropping
// the webhook + retrying is the durable failure signal).
//
// If GrantEntitlement or ClosePaymentSession fail, log + return nil — the
// authoritative state (gtk_service_order.status='paid') is already set by
// step 1, and the auxiliary table updates are safe to retry on a
// subsequent webhook delivery (LS will resend on our 5xx).
//
// session_id in custom_data["greentokey_session_id"] is OPTIONAL pre-Wave-4
// (Wave 4 D2 wires it at checkout). Absent → skip ClosePaymentSession with
// a debug log. Present → attempt close; failures are non-fatal.
func handleServiceOrderPaid(db *sql.DB, p *webhookPayload, orderNo string) error {
	// 1. Authoritative state flip on gtk_service_order.
	if err := service.MarkOrderPaid(db, orderNo, p.Data.ID, "lemonsqueezy"); err != nil {
		return fmt.Errorf("service.MarkOrderPaid: %w", err)
	}

	// 2. Unified entitlement hook (B3). Service grant is a CAS; if
	//    MarkOrderPaid already flipped to 'paid', the CAS observes
	//    status mismatch and returns nil (idempotent contract).
	grant := commerce.EntitlementGrant{
		ProductType: commerce.ProductService,
		OrderNo:     orderNo,
	}
	if err := commerce.GrantEntitlement(context.Background(), db, grant); err != nil {
		// Non-fatal: MarkOrderPaid already set the authoritative state.
		// Log so ops can investigate; let LS retry on a future event if
		// they want a re-attempt at the auxiliary path.
		logf(globals.Warn, "service_order_grant_entitlement_failed",
			"order_no", orderNo, "ls_id", p.Data.ID, "err", err)
	}

	// 3. Close gtk_payment_session if we have a session_id from checkout.
	//    Pre-Wave-4 orders won't have one (skip with debug log). All three
	//    failure modes of ClosePaymentSession (already-paid / terminal /
	//    not-found) return nil — see commerce/session.go header comment.
	sessionID := sessionIDFromCustomData(p.Meta.CustomData)
	if sessionID == "" {
		logf(globals.Info, "service_order_no_session_id",
			"order_no", orderNo, "note",
			"pre-Wave-4 checkout; ClosePaymentSession skipped")
	} else if err := commerce.ClosePaymentSession(db, sessionID); err != nil {
		// All ClosePaymentSession's documented "expected" outcomes return
		// nil; non-nil here is a genuine DB error. Log non-fatal.
		logf(globals.Warn, "service_order_close_session_failed",
			"order_no", orderNo, "session_id", sessionID, "err", err)
	}

	return nil
}

// sessionIDFromCustomData extracts greentokey_session_id from LS
// custom_data. Returns empty string if absent (pre-Wave-4 checkout flows
// don't embed it). Mirrors serviceOrderNoFromCustomData's shape +
// permissive type-coercion so checkout.go can write either string or
// number without breaking us.
func sessionIDFromCustomData(custom map[string]interface{}) string {
	raw, ok := custom["greentokey_session_id"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// handleServiceRenewal handles subscription_payment_success for service-order
// subscriptions. It creates a new gtk_service_order row per billing cycle so
// each renewal period has its own audit row.
//
// First-month-vs-renewal detection: looks up the most recent gtk_service_order
// for this subscription_id. If created_at < 24h ago → initial order_created
// already covered it → skip. Month 2+ rows are older → create new order.
//
// The new order copies service_id, service_slug, and price_cny_cents_paid from
// the most-recent order for this subscription (stable; ops edits the catalog
// row, not historical orders). status is set directly to 'paid' because LS has
// already collected the money before firing this event.
//
// Dedup: ls_order_id on gtk_service_order is UNIQUE. We use
// "renew-" + ls_subscription_id + "-" + renews_at_date (YYYY-MM-DD) as the
// stable-per-cycle ls_order_id. order_no is freshly generated (SVC-XXXXXXXX).
// The outer gtk_webhook_event SHA256 provides a second dedup layer.
func handleServiceRenewal(db *sql.DB, p *webhookPayload, origOrderNo string) error {
	lsSubID := p.Data.ID // LS subscription id string

	// Look up subscription_id from the original order (populated at first
	// payment by handleServiceOrderPaid or set by the subscription FK).
	// Fall back to lsSubID string comparison if no numeric id is stored yet.
	var (
		latestCreatedAt  sql.NullString
		latestServiceID  int64
		latestSlug       string
		latestPriceCents int64
		latestSubID      sql.NullString
	)
	err := db.QueryRow(`
		SELECT created_at, service_id, service_slug, price_cny_cents_paid,
		       CAST(subscription_id AS TEXT)
		FROM gtk_service_order
		WHERE subscription_id = ? OR order_no = ?
		ORDER BY id DESC LIMIT 1
	`, lsSubID, origOrderNo).Scan(
		&latestCreatedAt, &latestServiceID, &latestSlug,
		&latestPriceCents, &latestSubID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logf(globals.Warn, "service_renewal_no_prior_order",
				"ls_subscription_id", lsSubID, "orig_order_no", origOrderNo)
			return nil // nothing to renew against
		}
		return fmt.Errorf("service_renewal: lookup latest order: %w", err)
	}

	// First-month detection: if the latest order was created within 24h, skip.
	if latestCreatedAt.Valid {
		var parsed time.Time
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if t, e := time.Parse(layout, latestCreatedAt.String); e == nil {
				parsed = t
				break
			}
		}
		if !parsed.IsZero() && time.Since(parsed) < 24*time.Hour {
			logf(globals.Info, "service_renewal_skipped_first_month",
				"ls_subscription_id", lsSubID, "orig_order_no", origOrderNo,
				"latest_created_at", latestCreatedAt.String)
			return nil
		}
	}

	// Build a stable, cycle-unique ls_order_id for the new service order row.
	renewsAt := p.Data.Attributes.RenewsAt
	renewsDate := renewsAt
	if len(renewsAt) >= 10 {
		renewsDate = renewsAt[:10]
	}
	renewalLsOrderID := fmt.Sprintf("renew-%s-%s", lsSubID, renewsDate)
	newOrderNo := service.NewOrderNo()

	// Insert the new renewal order row. Status='paid' because LS already
	// collected the money. Idempotent via UNIQUE(ls_order_id): a second call
	// with the same renewalLsOrderID is caught by isDupErr → log + no-op.
	_, err = globals.ExecDb(db, `
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, subscription_id,
		   ls_order_id, status, paid_at)
		SELECT ?, coai_user_id, ?, ?, ?,
		       'lemonsqueezy', ?,
		       ?, 'paid', CURRENT_TIMESTAMP
		FROM gtk_service_order WHERE order_no = ? LIMIT 1
	`, newOrderNo, latestServiceID, latestSlug, latestPriceCents,
		lsSubID, renewalLsOrderID, origOrderNo)
	if err != nil {
		if isDupErr(err) {
			logf(globals.Info, "service_renewal_idempotent",
				"ls_order_id", renewalLsOrderID, "orig_order_no", origOrderNo)
			return nil
		}
		return fmt.Errorf("service_renewal: insert order: %w", err)
	}

	// Back-fill subscription_id on the original order row if it was NULL
	// (pre-PKG-M1 orders don't have it set).
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_service_order SET subscription_id = ?
		WHERE order_no = ? AND subscription_id IS NULL
	`, lsSubID, origOrderNo); err != nil {
		logf(globals.Warn, "service_renewal_backfill_failed",
			"orig_order_no", origOrderNo, "err", err)
	}

	logf(globals.Info, "service_renewal_created",
		"ls_subscription_id", lsSubID, "new_order_no", newOrderNo,
		"orig_order_no", origOrderNo, "ls_order_id", renewalLsOrderID)
	return nil
}

// RefundServiceOrder is the public refund entry point for service-order
// products. Wraps commerce.RevokeEntitlement (B3, IDEMPOTENT per H4)
// with payment-package conventions for symmetry with RefundTokenPlan.
//
// reason follows architecture §9.1:
//   'refund_full' | 'refund_during_run' | 'refund_post_delivery' |
//   'admin_revoke' | ...
//
// commerce.RevokeEntitlement(service) handles the state-machine branching
// (running → canceled_mid_flight, completed → refunded_post_delivery,
//  pending_payment/paid → refunded). Returns (state, nil) on already-
// terminal orders so callers can ack idempotently.
func RefundServiceOrder(ctx context.Context, db *sql.DB, orderNo, reason string) (commerce.EntitlementState, error) {
	if orderNo == "" {
		return "", errors.New("payment.RefundServiceOrder: orderNo required")
	}
	return commerce.RevokeEntitlement(ctx, db, orderNo, commerce.ProductService, reason)
}
