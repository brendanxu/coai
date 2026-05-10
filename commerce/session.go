// session.go — payment session lifecycle (PKG-2 Wave 2 B1, L23 §8).
//
// gtk_payment_session is the slim audit/visibility table linking a "user
// clicked Buy" event to the eventual webhook ack. Codex M1 reaffirmed it's
// NOT commerce of record — that lives in gtk_user_plan + gtk_service_order.
//
// This file is the thin lifecycle layer over the table:
//
//   - OpenPaymentSession  — checkout-time row insert, returns SessionID
//                           the caller embeds in the provider's payload
//                           (LS custom_data["greentokey_session_id"] /
//                            hupijiao prepay metadata).
//   - ClosePaymentSession — webhook-time status flip to 'paid'.
//                           IDEMPOTENT (CR7 + H4): matches by session_id
//                           (NOT order_no), double-close returns success,
//                           not-found returns success-with-warning so a
//                           webhook race can't crash the dispatcher.
//   - ExpirePaymentSessions — cron sweep that flips pending sessions past
//                             ExpiresAt to 'expired'. Returns count for
//                             observability.
//
// Provider TTL policy (per plan v2 §B1 + §D3):
//
//   - lemonsqueezy: 24h — LS hosted-checkout sessions stay open ~24h
//                         before LS itself expires them; we mirror.
//   - hupijiao:     24h — same shape as LS; ample for Alipay redirect.
//   - manual:       72h — manual-bank-transfer orders need 3 business
//                         days to settle (per Founder, 2026-05-09).
//   - default:      24h — unknown providers get the conservative LS TTL
//                         + a warning log (typo-defense).
//
// Why match by SessionID and not order_no (CR7):
//
//	The order_no isn't yet stable at checkout time for token plans (LS
//	subscription_id is only assigned post-payment). Matching by session_id
//	— the UUID we mint at OpenPaymentSession and embed in custom_data —
//	is the only key both ends control.
//
// Why idempotency on close + not-found (H4):
//
//	Webhooks are at-least-once; the same provider event can fire twice.
//	A naive "UPDATE ... WHERE status='pending'" returning an error on the
//	second call would crash the dispatcher and trigger a webhook retry
//	loop. We instead return nil + log appropriately so the dispatcher can
//	continue to the GrantEntitlement step (which has its own idempotency).

package commerce

import (
	"chat/globals"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ttlByProvider returns the session expiry duration for a payment
// provider tag. Centralized so callers don't open-code provider-specific
// TTLs and so future provider additions need only one edit here.
//
// Unknown providers fall back to 24h (the conservative LS TTL) and log
// a warning — protects against typos at the call site that would
// otherwise silently get e.g. "0 duration" semantics.
func ttlByProvider(provider string) time.Duration {
	switch provider {
	case "lemonsqueezy", "hupijiao":
		return 24 * time.Hour
	case "manual":
		return 72 * time.Hour
	default:
		globals.Warn(fmt.Sprintf(
			"commerce.session: unknown provider %q, defaulting TTL to 24h",
			provider))
		return 24 * time.Hour
	}
}

// OpenPaymentSession inserts a fresh row into gtk_payment_session and
// returns the populated PaymentSession. The caller MUST embed the
// returned SessionID into the provider's checkout payload so the inbound
// webhook can match back via ClosePaymentSession(sessionID).
//
// Wave 4 D2/D3 wiring:
//
//	LemonSqueezy: custom_data["greentokey_session_id"] = session.SessionID
//	hupijiao:     attach attribute on the prepay request
//
// SessionID is a UUID v4 from github.com/google/uuid (already a project
// dep; same generator used by service.runtime + adapter.hunyuan).
//
// On DB error returns (nil, err). On the (extremely unlikely) UUID
// collision with an existing UNIQUE session_id, the underlying ExecDb
// returns a constraint-violation error which surfaces unchanged — caller
// should retry once if it really matters, but with v4 entropy
// (~5.3×10^36 values) this is theoretically impossible at our scale.
func OpenPaymentSession(
	db *sql.DB,
	orderNo string,
	productType ProductType,
	provider string,
	amountCents int64,
	userID int64,
) (*PaymentSession, error) {
	sessionID := uuid.NewString()
	now := time.Now().UTC()
	expiresAt := now.Add(ttlByProvider(provider))

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_payment_session
		  (session_id, order_no, product_type, provider,
		   amount_cents, status, coai_user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?)
	`,
		sessionID, orderNo, string(productType), provider,
		amountCents, userID, now, expiresAt,
	); err != nil {
		return nil, fmt.Errorf("insert gtk_payment_session: %w", err)
	}

	return &PaymentSession{
		SessionID:   sessionID,
		OrderNo:     orderNo,
		ProductType: productType,
		Provider:    provider,
		AmountCents: amountCents,
		Status:      "pending",
		CoaiUserID:  userID,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		ClosedAt:    sql.NullTime{}, // NULL while pending
	}, nil
}

// ClosePaymentSession flips a session to status='paid' and stamps
// closed_at. IDEMPOTENT by design (CR7 + H4):
//
//   - Already 'paid': returns nil (no-op success).
//   - Already 'expired' or 'failed': returns nil + logs a warning
//     (terminal state mismatch — the webhook arrived after the cron
//     reaped the session; rare but expected at the boundary).
//   - Not found: returns nil + logs a warning. A webhook can theoretically
//     arrive before the session row is committed (e.g. a flaky checkout
//     that double-fires). Failing here would crash the dispatcher and
//     trigger an at-least-once webhook retry loop. The grant-entitlement
//     step downstream has its own idempotency and will fill the gap.
//
// Match key is session_id (UNIQUE column), per CR7. order_no would be
// ambiguous because the same physical order_no can briefly exist across
// retries before the order tables stabilize.
//
// Implementation uses compare-and-swap-by-status (UPDATE ... WHERE
// status='pending') so a concurrent expire-cron can't race us into a
// "paid then immediately expired" double mutation. RowsAffected == 0
// triggers a follow-up SELECT to disambiguate already-paid vs terminal-
// state vs not-found and log accordingly.
func ClosePaymentSession(db *sql.DB, sessionID string) error {
	res, err := globals.ExecDb(db, `
		UPDATE gtk_payment_session
		   SET status     = 'paid',
		       closed_at  = ?
		 WHERE session_id = ?
		   AND status     = 'pending'
	`, time.Now().UTC(), sessionID)
	if err != nil {
		return fmt.Errorf("update gtk_payment_session: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		// Some drivers don't support RowsAffected; treat as success
		// (we did issue the UPDATE) but log so we know.
		globals.Warn(fmt.Sprintf(
			"commerce.session: close session_id=%s — RowsAffected unavailable: %v",
			sessionID, err))
		return nil
	}
	if rows >= 1 {
		// Happy path: pending → paid.
		return nil
	}

	// rows == 0: either session is already in a non-pending state, or
	// session_id doesn't exist. Disambiguate so the warning is honest.
	var existingStatus string
	err = globals.QueryRowDb(db, `
		SELECT status FROM gtk_payment_session WHERE session_id = ?
	`, sessionID).Scan(&existingStatus)

	switch {
	case err == sql.ErrNoRows:
		// Webhook race: session row not (yet) committed. Don't crash
		// the dispatcher; downstream grant-entitlement is idempotent.
		globals.Warn(fmt.Sprintf(
			"commerce.session: close session_id=%s — not found "+
				"(webhook race tolerated; entitlement layer will reconcile)",
			sessionID))
		return nil
	case err != nil:
		// Real DB error on the disambiguation read — surface it.
		return fmt.Errorf("disambiguate session_id=%s: %w", sessionID, err)
	case existingStatus == "paid":
		// Double-close: previous webhook already settled. No-op.
		return nil
	default:
		// Terminal mismatch: 'expired' or 'failed' meeting a late
		// 'paid' webhook. Don't fail — log for ops investigation.
		globals.Warn(fmt.Sprintf(
			"commerce.session: close session_id=%s — terminal state %q "+
				"received late paid webhook (manual reconciliation may be needed)",
			sessionID, existingStatus))
		return nil
	}
}

// ExpirePaymentSessions sweeps any pending sessions past expires_at and
// flips them to status='expired' with closed_at=now. Returns the count
// of rows affected for observability (cron logs / metrics).
//
// Cron-callable: idempotent by construction (a pending session past TTL
// is flipped exactly once; running this every minute is safe). The
// expected cadence is hourly — TTLs are 24h/72h, so urgency is low.
//
// Compare-and-swap on status='pending' protects against racing with a
// late ClosePaymentSession — if the webhook lands first, the session is
// already 'paid' and our WHERE clause skips it.
func ExpirePaymentSessions(db *sql.DB) (int64, error) {
	now := time.Now().UTC()
	res, err := globals.ExecDb(db, `
		UPDATE gtk_payment_session
		   SET status    = 'expired',
		       closed_at = ?
		 WHERE status     = 'pending'
		   AND expires_at < ?
	`, now, now)
	if err != nil {
		return 0, fmt.Errorf("expire pending sessions: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		// Driver doesn't report it — treat the sweep as completed but
		// surface the count gap.
		return 0, fmt.Errorf("expire pending sessions: rows affected: %w", err)
	}
	return count, nil
}
