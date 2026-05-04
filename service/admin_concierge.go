// v0.16 — admin-side concierge order creation from /admin/mansu.
//
// Founder workflow this unblocks:
//   1. Lead submits via /contact form → /admin/mansu kanban shows it
//   2. Founder reaches out via WeChat, lead agrees, pays via Alipay/
//      cash/wechat-pay (anything outside the system)
//   3. Founder clicks "创建订单" on the lead card → picks a service
//      from the catalog → submits
//   4. Backend creates a manual+paid gtk_service_order linked to the
//      lead, mints an access_token, returns the shareable runner URL
//   5. Founder pastes runner URL into WeChat conversation; lead clicks
//      it (no login), uploads photos + theme, gets generated content
//
// payment_provider="manual" is the existing escape hatch from v0.10
// for offline settlement. v0.16 just exposes it through an admin UI
// that's faster than SQL.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AdminCreateConciergeOrderRequest is the JSON body for the new
// admin endpoint. Service slug picks from the active catalog; the
// price + included_credits are snapshotted from gtk_service at order
// creation time, same as self-serve orders.
type AdminCreateConciergeOrderRequest struct {
	ServiceSlug string `json:"service_slug" binding:"required"`
	// Notes get appended to the lead's notes column for audit. Optional
	// but recommended ("微信付款 ¥1980 / 已签约 4 周").
	Notes string `json:"notes,omitempty"`
}

// AdminCreateConciergeOrderAPI is wired by service.RegisterAdmin onto
// POST /api/gtk/v1/admin/leads/:id/create-order. Auth: admin only.
func AdminCreateConciergeOrderAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return
	}
	adminID := admin.GetID(connection.DB)

	leadIDParam := c.Param("id")
	leadID, err := strconv.ParseInt(leadIDParam, 10, 64)
	if err != nil || leadID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid lead id",
		})
		return
	}

	// Validate the lead exists. We don't UPDATE it here — the kanban
	// status PATCH endpoint already lets the founder move it to
	// 'signed' separately. Decoupled so a creating-order doesn't
	// implicitly change pipeline state (founder might want to create
	// a draft order before lead is officially "signed").
	if !leadExists(connection.DB, leadID) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "lead not found",
		})
		return
	}

	var req AdminCreateConciergeOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid body: " + err.Error(),
		})
		return
	}

	svc, err := LoadServiceBySlug(connection.DB, req.ServiceSlug)
	if errors.Is(err, ErrServiceNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "service not found or not active",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load service: " + err.Error(),
		})
		return
	}

	// Concierge mode: order goes directly to status='paid' (money
	// taken outside the system) and is owned by the admin user, since
	// we don't auto-create CoAI accounts for leads yet. Customer
	// accesses via the access_token URL instead.
	orderNo, accessToken, err := CreateConciergeOrder(connection.DB, adminID, leadID, svc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "create order: " + err.Error(),
		})
		return
	}

	// Append notes to the lead row if provided. Idempotent: just
	// concatenates with a separator so we don't lose history.
	if req.Notes != "" {
		appendLeadNote(connection.DB, leadID, fmt.Sprintf("[v0.16 order %s] %s", orderNo, req.Notes))
	}

	globals.Info(fmt.Sprintf("service: concierge order %s created (lead=%d service=%s admin=%d)",
		orderNo, leadID, svc.Slug, adminID))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"order_no":         orderNo,
			"access_token":     accessToken,
			"runner_url":       fmt.Sprintf("/services/run/%s?token=%s", orderNo, accessToken),
			"price_cny_cents":  svc.PriceCNYCents,
			"price_display":    FormatPriceCNY(svc.PriceCNYCents),
			"included_credits": svc.IncludedCredits,
			"service_name":     svc.Name,
			"service_slug":     svc.Slug,
		},
	})
}

// leadExists is a fast existence check. Returns false on any error so
// the caller fails closed (404 rather than 500 on a DB hiccup).
func leadExists(db *sql.DB, leadID int64) bool {
	var n int
	row := globals.QueryRowDb(db, `SELECT 1 FROM gtk_lead WHERE id = ? LIMIT 1`, leadID)
	if err := row.Scan(&n); err != nil {
		return false
	}
	return n == 1
}

// appendLeadNote concatenates a note onto the lead's existing notes
// column with a separator. Best-effort — failures don't abort the
// order creation (the order itself is the durable artifact).
func appendLeadNote(db *sql.DB, leadID int64, note string) {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_lead
		SET notes = TRIM(BOTH '\n' FROM CONCAT(COALESCE(notes, ''), '\n', ?))
		WHERE id = ?
	`, note, leadID)
	if err != nil {
		globals.Warn(fmt.Sprintf("service: append lead note failed (lead=%d): %v", leadID, err))
	}
}
