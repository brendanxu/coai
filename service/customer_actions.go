// Customer-side write actions for service orders (PKG-N2).
//
// PKG-5 added the read-only customer endpoints (GET /orders + detail).
// PKG-N2 adds the two write actions a customer needs from the order
// detail page so the support load drops:
//
//   POST /api/gtk/v1/orders/:order_no/refund-request
//     Body: { reason }
//     Auth: must own the order.
//     Effect: appends a "[CUSTOMER REQUEST]" prefixed note to
//             gtk_service_order.refund_reason WITHOUT flipping status.
//             Founder/admin still has to confirm via the existing
//             admin/refund endpoint — this is just a structured
//             customer request channel that beats "email support".
//     Why no schema change: refund_reason is a TEXT column we already
//             have (and currently only an admin sets it). Stuffing the
//             customer's ask in there with a clear marker keeps PKG-N2
//             scope-tight; a future PKG can promote this to a proper
//             gtk_refund_request table once we see the volume.
//     Returns: 202 Accepted with { acknowledged: true } on success.
//
//   POST /api/gtk/v1/orders/:order_no/reorder
//     Auth: must own the order.
//     Effect: stub — returns 200 with redirect_url=/pricing pointing
//             the customer at the catalog page. Implementing real
//             one-click reorder requires re-running CreateOrder with
//             the same service_slug + new payment_provider, which we
//             defer (the customer needs to re-pick payment provider
//             anyway, so the savings vs catalog re-flow are small).
//     Returns: 200 with { redirect_url, original_service_slug, is_stub }.
//
// Why these endpoints exist on the order_no path (not the catalog
// path): the customer's mental model on the order detail page is
// "this specific order — give me X". Endpoint locality matters for
// frontend wiring + audit log scoping.
//
// Idempotency: refund-request appending is intentionally append-only.
// Multiple sends from a customer (frantic clicking) all show up in the
// note for the founder to reconcile. Better than silently dropping or
// pretending only the latest matters.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CustomerRefundRequestBody is the JSON body for
// POST /orders/:order_no/refund-request.
//
// reason is required: a refund request without a reason is a noisy
// support ticket, and we'd rather force the customer to articulate
// why before the founder sees it. Capped to 500 chars on the wire.
type CustomerRefundRequestBody struct {
	Reason string `json:"reason" binding:"required,min=4,max=500"`
}

// statesAcceptingCustomerRefund is the closed set of order statuses
// where a customer-side refund request makes sense. Excludes:
//   - pending_payment: nothing to refund yet (just don't pay).
//   - failed: payment never happened.
//   - refunded / refunded_post_delivery / canceled_mid_flight: terminal.
var statesAcceptingCustomerRefund = map[string]struct{}{
	"paid":      {},
	"running":   {},
	"completed": {},
}

// CustomerRefundRequestAPI handles
// POST /api/gtk/v1/orders/:order_no/refund-request.
//
// Auth: caller must own the order. Returns 404 (not 403) for
// cross-user calls — same convention as MyOrderDetailAPI to avoid
// confirming order_no existence.
func CustomerRefundRequestAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := user.GetID(connection.DB)

	orderNo := c.Param("order_no")
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing order_no path parameter",
		})
		return
	}

	var body CustomerRefundRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	// Verify ownership AND fetch current status in one query.
	row := globals.QueryRowDb(connection.DB, `
		SELECT status, COALESCE(refund_reason, '')
		FROM gtk_service_order
		WHERE order_no = ? AND coai_user_id = ?
	`, orderNo, userID)
	var (
		status        string
		currentReason string
	)
	if err := row.Scan(&status, &currentReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "order not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "lookup failed: " + err.Error(),
		})
		return
	}

	if _, ok := statesAcceptingCustomerRefund[status]; !ok {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": fmt.Sprintf("order status %q does not accept refund requests", status),
			"status":  status,
		})
		return
	}

	// Append the customer note. Pre-existing reason (typically empty)
	// is preserved so multiple requests stack.
	note := fmt.Sprintf("[CUSTOMER REQUEST %s] %s",
		time.Now().UTC().Format(time.RFC3339),
		strings.TrimSpace(body.Reason))
	var newReason string
	if currentReason == "" {
		newReason = note
	} else {
		newReason = currentReason + "\n" + note
	}

	if _, err := globals.ExecDb(connection.DB,
		`UPDATE gtk_service_order SET refund_reason = ? WHERE order_no = ? AND coai_user_id = ?`,
		newReason, orderNo, userID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "save refund request failed: " + err.Error(),
		})
		return
	}

	// Loud log so founder sees the request without polling the DB.
	// Eventually this should be a Slack / wechat ping.
	globals.Info(fmt.Sprintf(
		"service: customer %d requested refund on order %s (status=%s, reason=%q)",
		userID, orderNo, status, body.Reason))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"data": gin.H{
			"acknowledged": true,
			"order_no":     orderNo,
			"status":       status, // unchanged — admin still needs to confirm
		},
	})
}

// CustomerReorderAPI handles POST /api/gtk/v1/orders/:order_no/reorder.
//
// v0 implementation: stub. Returns the catalog redirect URL + the
// service_slug the customer originally bought, so the frontend can
// pre-select on the catalog page once that exists. We don't auto-
// create a new order here because:
//  1. Customer needs to re-pick payment provider (LS vs hupijiao).
//  2. Service price may have changed since original purchase.
//  3. Auto-charging without confirmation is a UX trap.
func CustomerReorderAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := user.GetID(connection.DB)

	orderNo := c.Param("order_no")
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing order_no path parameter",
		})
		return
	}

	// Ownership check + fetch service_slug in one query.
	row := globals.QueryRowDb(connection.DB, `
		SELECT service_slug
		FROM gtk_service_order
		WHERE order_no = ? AND coai_user_id = ?
	`, orderNo, userID)
	var serviceSlug string
	if err := row.Scan(&serviceSlug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "order not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "lookup failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"redirect_url":          "/pricing",
			"original_service_slug": serviceSlug,
			// signal to the frontend that this is a redirect-stub, not
			// a real new order. When v0 promotes to real reorder this
			// flips to false and we ship `new_order_no`.
			"is_stub": true,
		},
	})
}
