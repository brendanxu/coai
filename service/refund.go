// Admin refund endpoint for Layer 3 service orders.
//
// What this does:
//   POST /api/gtk/v1/admin/refund (admin auth required)
//   Body: { order_no, refund_reason, refund_amount_cents? }
//   Action: flip gtk_service_order.status to 'refunded', record reason.
//
// What this does NOT do:
//   - Does NOT trigger an actual refund in the payment provider
//     (LemonSqueezy or hupijiao). Operator handles that externally
//     via the LS / hupijiao dashboard. This endpoint just updates
//     OUR record.
//   - Does NOT auto-debit credits or revoke agent runs. If the order
//     already executed (status=completed), the credits were spent;
//     they don't come back. Refund here = financial concession only.
//
// Why founder-review only (no auto-refund):
// Per recommendation 11.Q5 — with <50 paid orders the dispute rate
// and root causes are unknown. Auto-refund opens a feedback loop we
// can't yet measure. Manual review every refund for v0.10 → learn
// patterns → automate the obvious cases in v0.11+.

package service

import (
	"chat/auth"
	"chat/commerce"
	"chat/connection"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"chat/globals"
	"github.com/gin-gonic/gin"
)

// RefundRequest is the JSON body for POST /admin/refund.
//
// refund_amount_cents is optional. If 0/missing, refund is conceptually
// "full". The schema's `refund_reason` field carries the human note;
// the actual cents-refunded number is recorded for partial-refund cases
// even though the status field is binary.
type RefundRequest struct {
	OrderNo            string `json:"order_no"            binding:"required"`
	RefundReason       string `json:"refund_reason"       binding:"required"`
	RefundAmountCents  int64  `json:"refund_amount_cents,omitempty"`
}

// RefundAPI flips an order to status='refunded'. Idempotent: refunding
// an already-refunded order returns 200 with the existing record, not
// an error. (We treat double-clicks as harmless.)
func RefundAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return // RequireAdmin already wrote the 401/403 envelope
	}

	var req RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	result, err := refundOrder(connection.DB, req.OrderNo, req.RefundReason, req.RefundAmountCents)
	if errors.Is(err, ErrOrderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "order_no not found",
		})
		return
	}
	if errors.Is(err, ErrOrderNotRefundable) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "order is in a state that cannot be refunded (likely 'failed' — already terminal)",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "refund failed: " + err.Error(),
		})
		return
	}

	globals.Info(fmt.Sprintf("service: order %s refunded by admin %d (reason: %s, idempotent_no_op=%v)",
		req.OrderNo, admin.GetID(connection.DB), req.RefundReason, result.AlreadyRefunded))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"order_no":         req.OrderNo,
			"status":           "refunded",
			"refund_reason":    result.Reason,
			"already_refunded": result.AlreadyRefunded,
		},
	})
}

// ErrOrderNotFound: order_no doesn't match any row.
// ErrOrderNotRefundable: order is in a terminal state we don't refund
//   from (e.g. 'failed'). 'completed' IS refundable — the customer
//   got their output but is still entitled to a refund per founder's
//   judgment. 'refunded' is also OK (idempotent).
var (
	ErrOrderNotFound      = errors.New("service: order not found")
	ErrOrderNotRefundable = errors.New("service: order in non-refundable state")
)

// refundResult captures whether the call did real work or was idempotent.
type refundResult struct {
	Reason          string
	AlreadyRefunded bool
}

// refundOrder is the storage-layer implementation. Idempotent on
// double-call: a second refund with the same reason is a no-op; with
// a different reason, the latest reason wins (operator updating notes).
func refundOrder(db *sql.DB, orderNo, reason string, amountCents int64) (*refundResult, error) {
	row := globals.QueryRowDb(db,
		`SELECT status, COALESCE(refund_reason, '') FROM gtk_service_order WHERE order_no = ?`, orderNo)
	var (
		currentStatus string
		currentReason string
	)
	if err := row.Scan(&currentStatus, &currentReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("read order: %w", err)
	}

	switch currentStatus {
	case "refunded":
		// Idempotent — but allow reason update. If new reason supplied
		// and differs, update it; if same, no-op.
		if reason != currentReason {
			_, err := globals.ExecDb(db,
				`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ?`,
				reason, orderNo)
			if err != nil {
				return nil, fmt.Errorf("update reason on already-refunded: %w", err)
			}
			return &refundResult{Reason: reason, AlreadyRefunded: true}, nil
		}
		return &refundResult{Reason: currentReason, AlreadyRefunded: true}, nil

	case "failed":
		// 'failed' is terminal — payment never went through. Nothing
		// to refund. (Treat as data-quality issue, not a refund.)
		return nil, ErrOrderNotRefundable

	case "pending_payment", "paid", "running", "completed":
		// All these are refundable. Flip to 'refunded'.
		_, err := globals.ExecDb(db, `
			UPDATE gtk_service_order
			SET status = 'refunded', refund_reason = ?
			WHERE order_no = ?
		`, reason, orderNo)
		if err != nil {
			return nil, fmt.Errorf("flip to refunded: %w", err)
		}
		_ = amountCents // currently informational only; v0.11 may persist partial-refund amount in a separate column

		// PKG-2 Wave 4 D4: also call commerce.RevokeEntitlement so the
		// unified entitlement layer is in sync. RevokeEntitlement is
		// IDEMPOTENT (H4); for status 'refunded' (which we just set) it
		// hits the "refunded → terminal idempotent no-op" branch and
		// returns EntitlementRevoked + nil. We surface a state-divergence
		// warning when the returned state isn't EntitlementRevoked, since
		// that means the refund-state-machine and our local flip got out
		// of sync (the typical cause is a Wave-1 ENUM mismatch — useful
		// diagnostic to log).
		//
		// NOT a double-call hazard with the LS webhook path: that path
		// (payment/dispatch_service.go::RefundServiceOrder) calls
		// RevokeEntitlement DIRECTLY without going through this function,
		// so there's no overlap. This is for the admin-UI refund handler.
		state, revErr := commerce.RevokeEntitlement(
			context.Background(), db, orderNo, commerce.ProductService, reason)
		if revErr != nil {
			// Don't fail the refund — local DB state is already 'refunded'
			// (authoritative). Log loud so ops can investigate the
			// commerce-layer divergence.
			globals.Warn(fmt.Sprintf(
				"service: refund of %s flipped local status to 'refunded' "+
					"but commerce.RevokeEntitlement failed: %v "+
					"(local state is authoritative; manual reconciliation may be needed)",
				orderNo, revErr))
		} else if state != commerce.EntitlementRevoked {
			// Both succeeded but they disagree on the resulting unified
			// state — diagnostic only.
			globals.Warn(fmt.Sprintf(
				"service: refund of %s — local status='refunded' but "+
					"commerce.RevokeEntitlement returned state=%q (expected 'revoked'); "+
					"may indicate Wave-1 ENUM mismatch or stale local row",
				orderNo, state))
		}

		return &refundResult{Reason: reason, AlreadyRefunded: false}, nil

	default:
		return nil, fmt.Errorf("unexpected status %q", currentStatus)
	}
}
