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
		// TODO v0.11: create new gtk_service_order row for this month of
		// the existing subscription, flip to paid.
		logf(globals.Info, "service_order_monthly_renewal_deferred",
			"order_no", orderNo, "ls_subscription_id", p.Data.ID,
			"todo", "v0.11 cron creates new monthly order row")
		return nil

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
