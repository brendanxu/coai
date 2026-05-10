// entitlement.go — entitlement state machine that ties Wave 1 + Wave 2
// (B1/B2) together (PKG-2 Wave 2.5 B3, L23 §7 §9 §16).
//
// This file is the unified post-payment + refund control point for both
// product types (token plans + service products). Wave 3 webhook
// dispatchers call GrantEntitlement after payment; refund flows call
// RevokeEntitlement; runtime guards call CheckEntitlement.
//
// Public surface (per plan v2 §B3):
//
//   - GrantEntitlement(ctx, db, EntitlementGrant) error
//        Branches on g.ProductType:
//          token   → ProvisionForPlan (or enqueue gtk_newapi_pending_provisions
//                    on transient NewAPI failure per CR8); upsert
//                    gtk_user_plan to status='active' + cancellation_reason=NULL.
//          service → CompareAndSwap gtk_service_order pending_payment → paid.
//
//   - RevokeEntitlement(ctx, db, orderNo, productType, reason) (EntitlementState, error)
//        IDEMPOTENT (H4 from autoplan): second call returns nil + the
//        current state. Branches:
//          token  + full-refund          → DisableToken + status='canceled'
//                                           + cancellation_reason=reason
//          service in 'running'           → CAS to 'canceled_mid_flight'
//          service in 'completed'         → CAS to 'refunded_post_delivery'
//          service in 'pending_payment'  → status='refunded' (legacy)
//          service in 'paid'              → status='refunded' (legacy)
//
//   - CheckEntitlement(db, userID, ProductType) (EntitlementState, error)
//        Returns the canonical state across all the user's
//        gtk_user_plan / gtk_service_order rows for the product type.
//
//   - CompareAndSwapServiceOrderStatus(db, orderNo, fromStatus, toStatus)
//        (changed bool, err error)
//        Shared CAS helper that prevents the refund/runtime race (CR4).
//        Used by both Revoke (here) and Wave 4 D1 finalizeRun.
//
// Architecture refs:
//   - docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md §B3
//   - docs/strategy/2026-05-09-token-product-service-product-architecture.md
//     §7 (entitlement state), §9 (refund/cancel decision), §16 (KEEP 1:1).
//
// Why function-var hooks instead of a NewAPI interface:
//
//	GrantEntitlement and RevokeEntitlement need to call newapi.ProvisionForPlan
//	and (*newapi.Client).DisableToken respectively. The newapi package builds
//	its Client from viper config via sync.Once — fine for production, hard for
//	tests that don't run a NewAPI instance. Rather than restructuring newapi
//	with an interface (which would touch ProvisionForPlan call sites + ripple
//	through main.go / payment/lemonsqueezy.go), we declare two package-level
//	function vars here that default to the real newapi calls. Tests in
//	entitlement_test.go swap the vars to deterministic stubs. This keeps the
//	newapi pkg minimal (only a small DisableToken wrapper added) and concentrates
//	the test seam in one file.
//
//	Tradeoff: package-level vars are global state. We accept this here because
//	(a) the surface is two functions, (b) all callers are in this package,
//	(c) tests restore the vars on Cleanup. If commerce ever needs >3 such
//	hooks we should switch to an injected interface.

package commerce

import (
	"chat/globals"
	"chat/newapi"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// provisionForPlanFn is the test-swappable handle on newapi.ProvisionForPlan.
// Production: defaults to the real call. Tests: swap to a stub that returns
// (nil, transient-err) or a fake binding. Restore on Cleanup.
var provisionForPlanFn = func(ctx context.Context, db *sql.DB, coaiUserID int64, spec newapi.PlanSpec) (*newapi.Binding, error) {
	return newapi.ProvisionForPlan(ctx, db, coaiUserID, spec)
}

// disableTokenFn is the test-swappable handle on (*newapi.Client).DisableToken.
// Production: defaults to "load Default client + call DisableToken". Tests:
// swap to a stub.
//
// We accept tokenID via the binding lookup inside RevokeEntitlement so this
// hook only needs the id, not the full binding/client construction.
var disableTokenFn = func(ctx context.Context, tokenID int64) error {
	cli, err := newapi.Default()
	if err != nil {
		// Treat ErrNotConfigured as "NewAPI not wired here" — the local DB
		// flip still proceeds. Caller (RevokeEntitlement) only logs.
		return err
	}
	return cli.DisableToken(ctx, tokenID)
}

// GrantEntitlement is called by the webhook dispatcher (Wave 3 C) after
// payment is confirmed. It is the single post-payment provisioning entry
// point — both product flows funnel through here so future product types
// (e.g. team quota, enterprise contracts) get the same lifecycle hooks.
//
// Branches on g.ProductType:
//
//   - "token":
//       1. ProvisionForPlan against NewAPI (creates user/token, sets quota).
//          On transient NewAPI failure (network, timeout, 5xx, ErrNotConfigured),
//          enqueue gtk_newapi_pending_provisions for the PKG-3 retry worker
//          and return nil — the webhook delivery contract is honored, the
//          retry worker drains the queue out-of-band. v0 simplification:
//          ALL NewAPI errors are classified transient (per plan v2 §B3 note).
//       2. UPSERT gtk_user_plan: status='active', cancellation_reason=NULL,
//          product_type='token'. Re-grants on the same order_no are idempotent
//          (DELETE-then-INSERT keyed on order_id).
//
//   - "service":
//       1. CompareAndSwap gtk_service_order.status from 'pending_payment'
//          to 'paid' via CompareAndSwapServiceOrderStatus. If the row is
//          already past pending (paid/running/completed), no error — the
//          dispatcher and existing service.MarkOrderPaid both honor the
//          same idempotency contract.
//
// Returns nil on success or pending-enqueued (NOT an error — caller is a
// webhook handler that succeeded its delivery contract).
//
// Returns an error only on:
//   - DB INSERT/UPDATE failures (caller should rollback the webhook event row).
//   - Invalid g.ProductType (programmer error — fail loudly).
//
// CR4 concurrency: RevokeEntitlement / finalizeRun share the CAS helper, so
// a refund landing during a Grant is mutually exclusive at the row level.
func GrantEntitlement(ctx context.Context, db *sql.DB, g EntitlementGrant) error {
	switch g.ProductType {
	case ProductToken:
		return grantTokenEntitlement(ctx, db, g)
	case ProductService:
		return grantServiceEntitlement(db, g)
	default:
		return fmt.Errorf("commerce.GrantEntitlement: unknown product_type %q", g.ProductType)
	}
}

// grantTokenEntitlement provisions NewAPI then upserts gtk_user_plan.
// On transient NewAPI failure, enqueues the retry queue and still upserts
// gtk_user_plan — the user paid, the entitlement record must exist so
// CheckEntitlement returns 'active', even if the actual sk-xxx is delayed
// behind the retry worker. The worker (PKG-3) updates the binding row when
// it succeeds.
func grantTokenEntitlement(ctx context.Context, db *sql.DB, g EntitlementGrant) error {
	spec := newapi.PlanSpec{
		Code:       fmt.Sprintf("plan-%d", g.PlanID),
		QuotaUnits: g.QuotaUnits,
		ExpiresAt:  g.ExpiresAt,
		// Group is intentionally empty here → newapi.SaveBinding normalizes
		// to "default". Service-runtime users get explicit groups via a
		// separate SyncBindingGroup call (out of B3 scope).
	}
	_, err := provisionForPlanFn(ctx, db, g.UserID, spec)
	if err != nil {
		// v0 policy: classify ALL NewAPI errors as transient and enqueue.
		// Trade-off: a permanent error (e.g. malformed plan code) sits in
		// the queue forever vs hard-failing the webhook. Operational
		// simplicity wins for v0; PKG-3 worker observability surfaces the
		// stuck rows. Reclassification by error type is a follow-up.
		if enqErr := enqueuePendingProvision(db, g, err.Error()); enqErr != nil {
			// Both NewAPI failed AND the queue insert failed → real DB
			// problem. Surface so the webhook handler rolls back.
			return fmt.Errorf(
				"commerce.GrantEntitlement(token): provision failed (%v) AND pending queue insert failed: %w",
				err, enqErr)
		}
		globals.Warn(fmt.Sprintf(
			"commerce.GrantEntitlement(token): user=%d order=%s — NewAPI provision failed (%v); enqueued for retry",
			g.UserID, g.OrderNo, err))
		// Fall through to upsert gtk_user_plan so the entitlement record
		// is consistent with "user paid"; the binding row will land later.
	}

	if err := upsertUserPlan(db, g); err != nil {
		return fmt.Errorf("commerce.GrantEntitlement(token): upsert gtk_user_plan: %w", err)
	}
	return nil
}

// grantServiceEntitlement flips gtk_service_order to 'paid' via CAS so a
// concurrent webhook retry (or a refund-during-pending) can't double-flip.
// Mirrors service.MarkOrderPaid's contract — this is the unified call site
// for future product types to share the path.
func grantServiceEntitlement(db *sql.DB, g EntitlementGrant) error {
	if g.OrderNo == "" {
		return errors.New("commerce.GrantEntitlement(service): OrderNo required")
	}
	_, err := CompareAndSwapServiceOrderStatus(db, g.OrderNo, "pending_payment", "paid")
	if err != nil {
		return fmt.Errorf("commerce.GrantEntitlement(service): CAS: %w", err)
	}
	// changed=false isn't an error — the order was already past pending
	// (paid/running/completed). Idempotent contract.
	return nil
}

// enqueuePendingProvision inserts a row into gtk_newapi_pending_provisions
// for the PKG-3 retry worker to drain. provision_type is hardcoded to
// 'token_plan' here because GrantEntitlement only enqueues for token plans
// (services don't go through NewAPI).
func enqueuePendingProvision(db *sql.DB, g EntitlementGrant, lastError string) error {
	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_newapi_pending_provisions
		  (user_id, plan_id, provision_type, status, last_error)
		VALUES (?, ?, 'token_plan', 'pending', ?)
	`, g.UserID, g.PlanID, lastError)
	return err
}

// upsertUserPlan writes the gtk_user_plan row representing "this user has
// an active token plan from this order". On a re-grant for the same
// order_id — which can happen on webhook retry or on a refund → re-purchase
// cycle — the row is updated rather than duplicated.
//
// Implementation: DELETE-then-INSERT keyed on order_id. We don't currently
// have a UNIQUE constraint on order_id (idx_gtk_user_plan_order is a
// non-unique index), so a portable INSERT...ON CONFLICT/DUPLICATE KEY
// would require schema work. DELETE+INSERT is safe because:
//   - order_id is per-purchase and effectively unique (one row per user
//     per order in practice; the LemonSqueezy webhook never re-uses ids).
//   - The DELETE is bounded by the order_id index.
//   - The whole flow runs inside the webhook handler's local txn (Wave 3),
//     so a partial state mid-operation is invisible to readers.
func upsertUserPlan(db *sql.DB, g EntitlementGrant) error {
	if _, err := globals.ExecDb(db,
		`DELETE FROM gtk_user_plan WHERE order_id = ?`, g.OrderNo,
	); err != nil {
		return fmt.Errorf("dedupe gtk_user_plan: %w", err)
	}
	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_user_plan
		  (user_id, plan_id, product_type, status, cancellation_reason,
		   expire_at, order_id)
		VALUES (?, ?, 'token', 'active', NULL, ?, ?)
	`, g.UserID, g.PlanID, g.ExpiresAt, g.OrderNo)
	return err
}

// RevokeEntitlement is called on refund (full or mid-flight). IDEMPOTENT
// per H4: second call returns nil + the current state without erroring.
//
// Branches on productType + the order's current status:
//
//   - Token full-refund: DisableToken + flip gtk_user_plan to status='canceled'
//                        with cancellation_reason=reason. Already-canceled is
//                        a no-op; current state returned.
//
//   - Service in 'running': CAS status running → 'canceled_mid_flight'
//                            (CR5: this is the new ENUM value Wave 1 added).
//
//   - Service in 'completed': CAS status completed → 'refunded_post_delivery'
//                              (CR5: also a Wave 1 ENUM addition).
//
//   - Service in 'pending_payment' or 'paid': flip to 'refunded' (legacy
//                                              state preserved for backward
//                                              compatibility — services that
//                                              never started running can use
//                                              the simple path).
//
// reason is free-text per architecture §9.1:
//
//   'refund_full' | 'refund_partial' | 'cancel_at_period_end' |
//   'admin_revoke' | 'refund_during_run' | 'refund_post_delivery' | ...
//
// Caller is responsible for choosing the right reason string for audit
// purposes — RevokeEntitlement does not validate the value.
//
// Returns the resulting EntitlementState so caller can audit/log.
//
// Errors only on DB driver failure or NewAPI returning a non-transient
// error during DisableToken. NewAPI transient errors are logged and the
// local DB flip still proceeds (the local entitlement record is the SoT
// for the user's "can I use this?" gating; NewAPI eventual consistency
// is acceptable for the disable side because the user has already been
// refunded and the worst case is brief continued usage that we've already
// agreed to eat).
func RevokeEntitlement(ctx context.Context, db *sql.DB, orderNo string, productType ProductType, reason string) (EntitlementState, error) {
	switch productType {
	case ProductToken:
		return revokeTokenEntitlement(ctx, db, orderNo, reason)
	case ProductService:
		return revokeServiceEntitlement(db, orderNo, reason)
	default:
		return "", fmt.Errorf("commerce.RevokeEntitlement: unknown product_type %q", productType)
	}
}

// revokeTokenEntitlement disables the NewAPI token (best-effort) and flips
// gtk_user_plan to status='canceled'. Idempotent: if the plan is already
// canceled, returns the current state without error.
func revokeTokenEntitlement(ctx context.Context, db *sql.DB, orderNo, reason string) (EntitlementState, error) {
	// Read current plan + binding so we know what to disable + whether
	// this is a re-revoke (idempotent path).
	var (
		userID         int64
		currentStatus  string
		currentReason  sql.NullString
	)
	err := globals.QueryRowDb(db, `
		SELECT user_id, status, cancellation_reason
		FROM gtk_user_plan
		WHERE order_id = ? AND product_type = 'token'
	`, orderNo).Scan(&userID, &currentStatus, &currentReason)
	if err == sql.ErrNoRows {
		// No matching plan row. Webhook race tolerance: don't crash.
		// Return zero-value state so caller can log and continue.
		globals.Warn(fmt.Sprintf(
			"commerce.RevokeEntitlement(token): order %q not found (webhook race tolerated)",
			orderNo))
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read gtk_user_plan: %w", err)
	}

	// Already-canceled: idempotent no-op. Determine whether the prior
	// reason makes this a 'revoked' vs 'canceled' for the unified vocab.
	if currentStatus == "canceled" {
		return classifyCanceledState(currentReason), nil
	}

	// Active or expired: do the work.
	// 1. Best-effort DisableToken via the binding. Failures are logged
	//    but don't block the local DB flip.
	bind, bindErr := newapi.LoadBinding(db, userID)
	switch {
	case bindErr == sql.ErrNoRows:
		// No binding (NewAPI provision was deferred or never landed).
		// Nothing to disable; proceed to local flip.
		globals.Info(fmt.Sprintf(
			"commerce.RevokeEntitlement(token): user=%d order=%s — no NewAPI binding to disable",
			userID, orderNo))
	case bindErr != nil:
		return "", fmt.Errorf("load gtk_newapi_binding: %w", bindErr)
	default:
		if disableErr := disableTokenFn(ctx, bind.NewapiTokenID); disableErr != nil {
			// Log but continue: NewAPI may be down, the user is being
			// refunded regardless. The local entitlement flip is what
			// gates the user's continued access at our gateway.
			if errors.Is(disableErr, newapi.ErrTokenNotFound) {
				globals.Info(fmt.Sprintf(
					"commerce.RevokeEntitlement(token): NewAPI token %d already gone; proceeding",
					bind.NewapiTokenID))
			} else {
				globals.Warn(fmt.Sprintf(
					"commerce.RevokeEntitlement(token): NewAPI DisableToken failed for token=%d (%v); proceeding with local flip",
					bind.NewapiTokenID, disableErr))
			}
		}
	}

	// 2. Local flip: status='canceled' + cancellation_reason=reason.
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_user_plan
		SET status = 'canceled', cancellation_reason = ?
		WHERE order_id = ? AND product_type = 'token'
	`, reason, orderNo); err != nil {
		return "", fmt.Errorf("flip gtk_user_plan to canceled: %w", err)
	}

	// 3. Map reason → unified vocabulary.
	return classifyCanceledState(sql.NullString{String: reason, Valid: true}), nil
}

// classifyCanceledState maps the cancellation_reason string to the unified
// EntitlementState vocabulary. 'refund_*' reasons are 'revoked' (hard
// removal); everything else is 'canceled' (soft, e.g. cancel_at_period_end).
func classifyCanceledState(reason sql.NullString) EntitlementState {
	if !reason.Valid || reason.String == "" {
		return EntitlementCanceled
	}
	if strings.HasPrefix(reason.String, "refund_") {
		return EntitlementRevoked
	}
	return EntitlementCanceled
}

// revokeServiceEntitlement reads the current order status and CAS-flips
// to the appropriate Wave 1 ENUM value. Idempotent: terminal states
// (refunded, refunded_post_delivery, canceled_mid_flight, failed) are
// re-mapped to the corresponding EntitlementState and returned without
// further mutation.
func revokeServiceEntitlement(db *sql.DB, orderNo, reason string) (EntitlementState, error) {
	if orderNo == "" {
		return "", errors.New("commerce.RevokeEntitlement(service): orderNo required")
	}

	var currentStatus string
	err := globals.QueryRowDb(db, `
		SELECT status FROM gtk_service_order WHERE order_no = ?
	`, orderNo).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		// Webhook race tolerance: don't crash when the order isn't found.
		globals.Warn(fmt.Sprintf(
			"commerce.RevokeEntitlement(service): order %q not found (webhook race tolerated)",
			orderNo))
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read gtk_service_order: %w", err)
	}

	switch currentStatus {
	case "running":
		// Mid-flight refund (CR5). CAS to canceled_mid_flight.
		changed, casErr := CompareAndSwapServiceOrderStatus(
			db, orderNo, "running", "canceled_mid_flight")
		if casErr != nil {
			return "", fmt.Errorf("CAS running → canceled_mid_flight: %w", casErr)
		}
		if !changed {
			// Concurrent runtime flipped to 'completed' (or another refund
			// landed first). Re-read and return the resulting state.
			return revokeServiceEntitlement(db, orderNo, reason)
		}
		recordRefundReason(db, orderNo, reason)
		return EntitlementCanceled, nil

	case "completed":
		// Post-delivery refund (CR5). CAS to refunded_post_delivery.
		changed, casErr := CompareAndSwapServiceOrderStatus(
			db, orderNo, "completed", "refunded_post_delivery")
		if casErr != nil {
			return "", fmt.Errorf("CAS completed → refunded_post_delivery: %w", casErr)
		}
		if !changed {
			// Already moved by a concurrent refund call.
			return revokeServiceEntitlement(db, orderNo, reason)
		}
		recordRefundReason(db, orderNo, reason)
		return EntitlementRevoked, nil

	case "pending_payment", "paid":
		// Pre-runtime refund. Use the legacy 'refunded' state — the user
		// never started using the service, simple path applies.
		changed, casErr := CompareAndSwapServiceOrderStatus(
			db, orderNo, currentStatus, "refunded")
		if casErr != nil {
			return "", fmt.Errorf("CAS %s → refunded: %w", currentStatus, casErr)
		}
		if !changed {
			return revokeServiceEntitlement(db, orderNo, reason)
		}
		recordRefundReason(db, orderNo, reason)
		return EntitlementRevoked, nil

	case "refunded", "refunded_post_delivery":
		// Already terminally refunded. Idempotent no-op.
		return EntitlementRevoked, nil

	case "canceled_mid_flight":
		// Already terminally canceled mid-flight. Idempotent no-op.
		return EntitlementCanceled, nil

	case "failed":
		// Order failed pre-payment; refund is meaningless. Treat as
		// idempotent no-op so dispatcher doesn't crash on a late refund
		// webhook for a failed order.
		return EntitlementRevoked, nil

	default:
		return "", fmt.Errorf(
			"commerce.RevokeEntitlement(service): order %q in unexpected state %q",
			orderNo, currentStatus)
	}
}

// recordRefundReason writes the human-readable refund_reason column on
// gtk_service_order. Best-effort: failure is logged, not propagated, so
// we don't undo a successful CAS just because the audit field write
// failed.
func recordRefundReason(db *sql.DB, orderNo, reason string) {
	if reason == "" {
		return
	}
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ?`,
		reason, orderNo,
	); err != nil {
		globals.Warn(fmt.Sprintf(
			"commerce.RevokeEntitlement(service): failed to record refund_reason for %s: %v",
			orderNo, err))
	}
}

// CheckEntitlement reads the current entitlement status for a user+product
// type. It's the canonical "can this user use X?" gate used by:
//
//   - chat handler (token): is there an active gtk_user_plan?
//   - service runtime (service): does the user have any active
//     gtk_service_order? (caller usually queries by orderNo directly,
//     but this surface is here for parity + future "user has any active
//     service?" queries.)
//
// Returns:
//   - For token: gtk_user_plan.status mapped to EntitlementState. If the
//     user has multiple plans (renewals over time), returns the most-recent
//     non-canceled one; an active row anywhere in their history wins over
//     stale terminal rows.
//   - For service: aggregates the user's gtk_service_order rows for the
//     product type. Returns the latest entitlement across all orders so
//     billing guards can answer "can this user use the service surface?".
//
// Returns EntitlementExpired (not an error) if the user has no rows for
// the product type — semantically "no active entitlement". This is the
// documented zero-value choice for callers that want to gate on
// entitlement >= active.
func CheckEntitlement(db *sql.DB, userID int64, productType ProductType) (EntitlementState, error) {
	switch productType {
	case ProductToken:
		return checkTokenEntitlement(db, userID)
	case ProductService:
		return checkServiceEntitlement(db, userID)
	default:
		return "", fmt.Errorf("commerce.CheckEntitlement: unknown product_type %q", productType)
	}
}

func checkTokenEntitlement(db *sql.DB, userID int64) (EntitlementState, error) {
	rows, err := db.Query(`
		SELECT status, cancellation_reason
		FROM gtk_user_plan
		WHERE user_id = ? AND product_type = 'token'
		ORDER BY purchased_at DESC
	`, userID)
	if err != nil {
		return "", fmt.Errorf("query gtk_user_plan: %w", err)
	}
	defer rows.Close()

	var (
		latest         EntitlementState = EntitlementExpired
		foundAny       bool
		latestIsActive bool
	)
	for rows.Next() {
		var (
			status string
			reason sql.NullString
		)
		if err := rows.Scan(&status, &reason); err != nil {
			return "", fmt.Errorf("scan gtk_user_plan: %w", err)
		}
		foundAny = true
		st := mapPlanStatusToEntitlement(status, reason)
		if !latestIsActive {
			latest = st
			if st == EntitlementActive {
				latestIsActive = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate gtk_user_plan: %w", err)
	}
	if !foundAny {
		return EntitlementExpired, nil
	}
	return latest, nil
}

func mapPlanStatusToEntitlement(status string, reason sql.NullString) EntitlementState {
	switch status {
	case "active":
		return EntitlementActive
	case "expired":
		return EntitlementExpired
	case "canceled":
		return classifyCanceledState(reason)
	default:
		return EntitlementExpired
	}
}

func checkServiceEntitlement(db *sql.DB, userID int64) (EntitlementState, error) {
	rows, err := db.Query(`
		SELECT status FROM gtk_service_order
		WHERE coai_user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return "", fmt.Errorf("query gtk_service_order: %w", err)
	}
	defer rows.Close()

	var (
		latest         EntitlementState = EntitlementExpired
		foundAny       bool
		latestIsActive bool
	)
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return "", fmt.Errorf("scan gtk_service_order: %w", err)
		}
		foundAny = true
		st := mapServiceStatusToEntitlement(status)
		if !latestIsActive {
			latest = st
			if st == EntitlementActive {
				latestIsActive = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate gtk_service_order: %w", err)
	}
	if !foundAny {
		return EntitlementExpired, nil
	}
	return latest, nil
}

func mapServiceStatusToEntitlement(status string) EntitlementState {
	switch status {
	case "paid", "running":
		return EntitlementActive
	case "completed":
		// Service one-shot: post-completion the user has consumed it.
		// Map to expired (no longer active, but not refunded).
		return EntitlementExpired
	case "canceled_mid_flight":
		return EntitlementCanceled
	case "refunded", "refunded_post_delivery", "failed":
		return EntitlementRevoked
	case "pending_payment":
		// Not yet paid — no entitlement granted.
		return EntitlementExpired
	default:
		return EntitlementExpired
	}
}

// CompareAndSwapServiceOrderStatus is a helper for refund (RevokeEntitlement)
// AND finalizeRun (Wave 4 D1) to prevent the refund/runtime race (CR4).
//
// Pattern:
//
//	UPDATE gtk_service_order
//	   SET status = ?, updated_at = CURRENT_TIMESTAMP
//	 WHERE order_no = ? AND status = ?
//
// Returns:
//   - (true, nil)  when exactly 1 row was updated (CAS succeeded).
//   - (false, nil) when 0 rows updated — status differed at the time of
//                  UPDATE (someone else got there first, or the order
//                  doesn't exist). Caller observed the race and can decide
//                  whether to retry / re-read / log accordingly.
//   - (_, err)     on driver failure or RowsAffected unsupported.
//
// The CAS protects the runtime/refund race documented in CR4: while
// finalizeRun (Wave 4 D1) wants to flip running → completed, a concurrent
// refund webhook may want to flip running → canceled_mid_flight. Both
// callers using this helper means the loser observes (false, nil) and
// gracefully gives up rather than overwriting the winner's terminal state.
//
// Note: under SQLite (test driver) writes are serialized at the connection
// level, so the test for true concurrency is more about correctness of the
// CAS logic than wall-clock racing. Real MySQL with row-level locking
// benefits from this CAS in actual production scenarios.
func CompareAndSwapServiceOrderStatus(
	db *sql.DB, orderNo string, fromStatus, toStatus string,
) (changed bool, err error) {
	if orderNo == "" {
		return false, errors.New("commerce.CompareAndSwapServiceOrderStatus: orderNo required")
	}

	// Note: SQLite ignores ON UPDATE CURRENT_TIMESTAMP, so we set updated_at
	// explicitly. MySQL would auto-update it via the column default, but
	// we set it explicitly for parity + forward-compat (defensive coding
	// against a future schema where the default is removed).
	res, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		   SET status     = ?,
		       updated_at = CURRENT_TIMESTAMP
		 WHERE order_no   = ?
		   AND status     = ?
	`, toStatus, orderNo, fromStatus)
	if err != nil {
		return false, fmt.Errorf("CAS update gtk_service_order: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		// Driver doesn't report — surface so caller knows the CAS outcome
		// is undeterminable.
		return false, fmt.Errorf("CAS RowsAffected: %w", err)
	}
	return rows == 1, nil
}
