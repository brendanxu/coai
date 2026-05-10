// Customer self-serve order endpoints (PKG-5).
//
// Two endpoints, both authenticated, both scoped to the calling user
// (no admin escalation; admin sees all orders via the existing
// /admin/refund + /admin/mark-paid surface):
//
//   GET  /api/gtk/v1/orders                List my service orders.
//                                           Optional ?status=<status> filter.
//   GET  /api/gtk/v1/orders/:order_no      One order's detail (mine only).
//
// Why these endpoints exist (and they didn't before):
//   PKG-2 Wave 4 wired all of the *write* sides of self-serve service
//   checkout (D2 LS session, D3 service order session, D7 admin
//   mark-paid). What was missing was a way for a logged-in customer to
//   *see* what they bought — which orders are pending payment, which
//   are running, which finished. PKG-5 closes that gap with read-only
//   customer-scoped endpoints + a frontend MyOrders page that consumes
//   them.
//
// Why scope-by-user is enforced at the SQL level (not in handler):
//   Every query has `AND coai_user_id = ?` baked into the WHERE clause.
//   The handler can't accidentally leak a row from a different user
//   even if it forgets to validate — the query simply won't return it.
//   Single-order detail returns 404 (not 403) for someone-else's order:
//   we don't want to confirm-or-deny the existence of order_nos the
//   caller doesn't own.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CustomerOrderSummary is the row shape returned by ListMyOrders. It's
// a denormalized convenience view: joins service.name in so the UI
// doesn't have to do a second lookup per row to render the card.
//
// Mirrors PublicService.PriceDisplayCNY by including a pre-formatted
// price string — frontend stays dumb.
type CustomerOrderSummary struct {
	OrderNo           string `json:"order_no"`
	ServiceSlug       string `json:"service_slug"`
	ServiceName       string `json:"service_name"`
	Status            string `json:"status"`
	PaymentProvider   string `json:"payment_provider"`
	PriceCNYCentsPaid int64  `json:"price_cny_cents_paid"`
	PriceDisplayCNY   string `json:"price_display_cny"`
	CreditsGranted    int    `json:"credits_granted"`
	HasRun            bool   `json:"has_run"` // agent_run_id IS NOT NULL
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	PaidAt            string `json:"paid_at,omitempty"`
	CompletedAt       string `json:"completed_at,omitempty"`
}

// CustomerOrderDetail extends the summary with refund/run metadata that
// only matters once you click into a specific order.
type CustomerOrderDetail struct {
	CustomerOrderSummary
	AgentRunID   string `json:"agent_run_id,omitempty"`
	RefundReason string `json:"refund_reason,omitempty"`
}

// validOrderStatuses is the closed set of values the status filter
// accepts. Anything else returns 400 — the customer can't probe for
// internal status enums by trying random strings. Mirrors the schema
// CHECK constraint in service/migration.go.
var validOrderStatuses = map[string]struct{}{
	"pending_payment":        {},
	"paid":                   {},
	"running":                {},
	"completed":              {},
	"refunded":               {},
	"refunded_post_delivery": {},
	"failed":                 {},
	"canceled_mid_flight":    {},
}

// ListMyOrders returns all orders owned by coaiUserID, newest first.
// statusFilter == "" means "any status".
//
// Hard cap of 100 rows — a paying customer is unlikely to have more
// than that, and an unbounded list lets a curious account enumerate
// all their historical purchases in one shot. If we ever hit the cap
// in production we'll add cursor pagination; for v0 the simpler bound
// is fine.
func ListMyOrders(db *sql.DB, coaiUserID int64, statusFilter string) ([]CustomerOrderSummary, error) {
	const baseSelect = `
		SELECT o.order_no, o.service_slug, COALESCE(s.name, o.service_slug),
		       o.status, o.payment_provider, o.price_cny_cents_paid,
		       o.credits_granted,
		       CASE WHEN o.agent_run_id IS NULL OR o.agent_run_id = '' THEN 0 ELSE 1 END,
		       o.created_at, o.updated_at,
		       COALESCE(o.paid_at, ''), COALESCE(o.completed_at, '')
		FROM gtk_service_order o
		LEFT JOIN gtk_service s ON s.id = o.service_id
		WHERE o.coai_user_id = ?
	`
	const orderClause = ` ORDER BY o.id DESC LIMIT 100`

	var (
		rows *sql.Rows
		err  error
	)
	if statusFilter == "" {
		rows, err = globals.QueryDb(db, baseSelect+orderClause, coaiUserID)
	} else {
		rows, err = globals.QueryDb(db,
			baseSelect+` AND o.status = ?`+orderClause,
			coaiUserID, statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("service: list my orders: %w", err)
	}
	defer rows.Close()

	var out []CustomerOrderSummary
	for rows.Next() {
		var s CustomerOrderSummary
		var hasRunInt int
		if err := rows.Scan(
			&s.OrderNo, &s.ServiceSlug, &s.ServiceName,
			&s.Status, &s.PaymentProvider, &s.PriceCNYCentsPaid,
			&s.CreditsGranted, &hasRunInt,
			&s.CreatedAt, &s.UpdatedAt,
			&s.PaidAt, &s.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("service: scan my order row: %w", err)
		}
		s.HasRun = hasRunInt == 1
		s.PriceDisplayCNY = FormatPriceCNY(s.PriceCNYCentsPaid)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: iterate my orders: %w", err)
	}
	return out, nil
}

// LoadMyOrderDetail returns one order's detail iff coaiUserID owns it.
// Returns sql.ErrNoRows when either (a) the order_no doesn't exist or
// (b) it exists but belongs to someone else. Both cases collapse to
// 404 on the wire — see comment at top of file for why.
func LoadMyOrderDetail(db *sql.DB, coaiUserID int64, orderNo string) (*CustomerOrderDetail, error) {
	row := globals.QueryRowDb(db, `
		SELECT o.order_no, o.service_slug, COALESCE(s.name, o.service_slug),
		       o.status, o.payment_provider, o.price_cny_cents_paid,
		       o.credits_granted,
		       CASE WHEN o.agent_run_id IS NULL OR o.agent_run_id = '' THEN 0 ELSE 1 END,
		       o.created_at, o.updated_at,
		       COALESCE(o.paid_at, ''), COALESCE(o.completed_at, ''),
		       COALESCE(o.agent_run_id, ''), COALESCE(o.refund_reason, '')
		FROM gtk_service_order o
		LEFT JOIN gtk_service s ON s.id = o.service_id
		WHERE o.order_no = ? AND o.coai_user_id = ?
	`, orderNo, coaiUserID)

	var d CustomerOrderDetail
	var hasRunInt int
	if err := row.Scan(
		&d.OrderNo, &d.ServiceSlug, &d.ServiceName,
		&d.Status, &d.PaymentProvider, &d.PriceCNYCentsPaid,
		&d.CreditsGranted, &hasRunInt,
		&d.CreatedAt, &d.UpdatedAt,
		&d.PaidAt, &d.CompletedAt,
		&d.AgentRunID, &d.RefundReason,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("service: load my order detail: %w", err)
	}
	d.HasRun = hasRunInt == 1
	d.PriceDisplayCNY = FormatPriceCNY(d.PriceCNYCentsPaid)
	return &d, nil
}

// ─────────────────────────────────────────────────────────────────────
// HTTP handlers
// ─────────────────────────────────────────────────────────────────────

// ListMyOrdersAPI handles GET /api/gtk/v1/orders. Requires auth.
//
// Optional query string: ?status=<value>. Unknown status values return
// 400 (vs silently returning [] which would mask client typos).
func ListMyOrdersAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := user.GetID(connection.DB)

	statusFilter := c.Query("status")
	if statusFilter != "" {
		if _, ok := validOrderStatuses[statusFilter]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("unknown status filter: %q", statusFilter),
			})
			return
		}
	}

	items, err := ListMyOrders(connection.DB, userID, statusFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "list orders failed: " + err.Error(),
		})
		return
	}
	if items == nil {
		items = []CustomerOrderSummary{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"orders": items,
			"total":  len(items),
		},
	})
}

// MyOrderDetailAPI handles GET /api/gtk/v1/orders/:order_no. Requires
// auth. Returns 404 if the order doesn't exist OR belongs to someone
// else (intentional — see file header).
func MyOrderDetailAPI(c *gin.Context) {
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

	detail, err := LoadMyOrderDetail(connection.DB, userID, orderNo)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "order not found",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load order failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    detail,
	})
}
