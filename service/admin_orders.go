// Admin variant of the customer order list (PKG-N1).
//
// What this is:
//   GET /api/gtk/v1/admin/orders        Admin-only — list ALL service orders
//                                        across all users, with the username
//                                        joined in for ops visibility.
//
// What this is NOT:
//   - This is read-only. Mutating actions (mark-paid / refund) live in the
//     existing /api/gtk/v1/admin/mark-paid + /api/gtk/v1/admin/refund
//     endpoints; this endpoint just feeds the table the UI iterates over
//     when deciding which order to mark / refund next.
//
// Why a separate file from customer_orders.go:
//   The customer-scoped endpoint has a security-critical "WHERE
//   coai_user_id = ?" clause baked into every query. The admin variant
//   intentionally drops that filter — keeping the two flows in separate
//   files makes it obvious at a glance which is which, and prevents a
//   future refactor from accidentally collapsing them into a single
//   handler with a "scope" boolean (which is the kind of thing that ends
//   up leaking rows). See the customer_orders.go header for the
//   complementary security rationale.
//
// LEFT JOIN auth a ON a.id = o.coai_user_id mirrors the PKG-4 pattern
// (newapi/admin_routing.go::listUserRouting): the username is a
// convenience for the operator, not load-bearing for correctness, so a
// missing auth row (orphan FK) surfaces as empty-string rather than
// dropping the order from the result.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminOrderRow is the admin-table row shape. Superset of
// CustomerOrderSummary: adds the customer's coai_user_id + username so
// the table can be filtered/searched by user, and refund_reason so a
// refunded row shows why without an extra detail click.
type AdminOrderRow struct {
	OrderNo           string `json:"order_no"`
	CoaiUserID        int64  `json:"coai_user_id"`
	Username          string `json:"username"` // "" if auth row is missing
	ServiceSlug       string `json:"service_slug"`
	ServiceName       string `json:"service_name"`
	Status            string `json:"status"`
	PaymentProvider   string `json:"payment_provider"`
	PriceCNYCentsPaid int64  `json:"price_cny_cents_paid"`
	PriceDisplayCNY   string `json:"price_display_cny"`
	CreditsGranted    int    `json:"credits_granted"`
	HasRun            bool   `json:"has_run"`
	RefundReason      string `json:"refund_reason,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	PaidAt            string `json:"paid_at,omitempty"`
	CompletedAt       string `json:"completed_at,omitempty"`
}

// AdminOrderListFilters captures the query-string filters the admin UI
// can apply. All fields are optional; any unset/empty filter means
// "don't filter on this dimension".
type AdminOrderListFilters struct {
	Status string // status filter; "" = all
	UserID int64  // 0 = no user filter
	Search string // case-sensitive LIKE on order_no OR username
	Limit  int
	Offset int
}

// adminListLimitDefault / adminListLimitMax bracket what the UI is
// allowed to ask for. Default is comfortable for a single-screen table
// at 50; max prevents accidentally pulling 50K rows on a misconfigured
// client. If the founder ever needs to bulk-export, that's a separate
// CSV endpoint (out of scope for v0).
const (
	adminListLimitDefault = 50
	adminListLimitMax     = 500
)

// ListAdminOrders runs the admin-scoped JOIN over gtk_service_order ×
// auth × gtk_service. Returns (rows, total) where total is the count
// for the SAME filters (so the frontend's pagination math is correct).
//
// ORDER BY o.id DESC: newest first. id is monotonic-by-INSERT, so this
// is stable even when created_at clocks skew.
//
// Search semantics: matches if order_no OR username starts with the
// search term (LIKE 'foo%'). The leading anchor keeps the index usable
// on order_no; username has no index in v0 but the user table is small
// enough that a sequential scan is fine for now.
func ListAdminOrders(db *sql.DB, f AdminOrderListFilters) ([]AdminOrderRow, int64, error) {
	if f.Limit <= 0 {
		f.Limit = adminListLimitDefault
	}
	if f.Limit > adminListLimitMax {
		f.Limit = adminListLimitMax
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var (
		whereParts []string
		args       []any
	)
	if f.Status != "" {
		if _, ok := validOrderStatuses[f.Status]; !ok {
			return nil, 0, fmt.Errorf("unknown status filter: %q", f.Status)
		}
		whereParts = append(whereParts, "o.status = ?")
		args = append(args, f.Status)
	}
	if f.UserID > 0 {
		whereParts = append(whereParts, "o.coai_user_id = ?")
		args = append(args, f.UserID)
	}
	if f.Search != "" {
		// Match either an order_no prefix or a username prefix. Both LIKE
		// arms share the same parameter to keep arg ordering stable.
		whereParts = append(whereParts, "(o.order_no LIKE ? OR a.username LIKE ?)")
		needle := f.Search + "%"
		args = append(args, needle, needle)
	}

	where := ""
	if len(whereParts) > 0 {
		where = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// Count first so the UI can paginate accurately.
	var total int64
	countSQL := `
		SELECT COUNT(*)
		FROM gtk_service_order o
		LEFT JOIN auth a ON a.id = o.coai_user_id
		` + where
	if err := globals.QueryRowDb(db, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("service: count admin orders: %w", err)
	}

	listSQL := `
		SELECT o.order_no, o.coai_user_id, COALESCE(a.username, ''),
		       o.service_slug, COALESCE(s.name, o.service_slug),
		       o.status, o.payment_provider, o.price_cny_cents_paid,
		       o.credits_granted,
		       CASE WHEN o.agent_run_id IS NULL OR o.agent_run_id = '' THEN 0 ELSE 1 END,
		       COALESCE(o.refund_reason, ''),
		       o.created_at, o.updated_at,
		       COALESCE(o.paid_at, ''), COALESCE(o.completed_at, '')
		FROM gtk_service_order o
		LEFT JOIN auth a ON a.id = o.coai_user_id
		LEFT JOIN gtk_service s ON s.id = o.service_id
		` + where + `
		ORDER BY o.id DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := globals.QueryDb(db, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("service: list admin orders: %w", err)
	}
	defer rows.Close()

	out := make([]AdminOrderRow, 0, f.Limit)
	for rows.Next() {
		var r AdminOrderRow
		var hasRunInt int
		if err := rows.Scan(
			&r.OrderNo, &r.CoaiUserID, &r.Username,
			&r.ServiceSlug, &r.ServiceName,
			&r.Status, &r.PaymentProvider, &r.PriceCNYCentsPaid,
			&r.CreditsGranted, &hasRunInt, &r.RefundReason,
			&r.CreatedAt, &r.UpdatedAt,
			&r.PaidAt, &r.CompletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("service: scan admin order row: %w", err)
		}
		r.HasRun = hasRunInt == 1
		r.PriceDisplayCNY = FormatPriceCNY(r.PriceCNYCentsPaid)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("service: iterate admin orders: %w", err)
	}
	return out, total, nil
}

// ─────────────────────────────────────────────────────────────────────
// HTTP handler
// ─────────────────────────────────────────────────────────────────────

// ListAdminOrdersAPI handles GET /api/gtk/v1/admin/orders. Admin-only.
//
// Query params (all optional):
//
//	status     One of validOrderStatuses; unknown values return 400.
//	user_id    Numeric coai_user_id; non-numeric returns 400.
//	q          Free-text search; LIKE-prefix on order_no OR username.
//	limit      Max rows (default 50, hard cap 500).
//	offset     Pagination offset (default 0).
//
// Response envelope mirrors the customer endpoint:
//
//	{
//	  "success": true,
//	  "data": {
//	    "orders": [...],
//	    "total":  <int64>,
//	    "limit":  <int>,
//	    "offset": <int>
//	  }
//	}
func ListAdminOrdersAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	f := AdminOrderListFilters{
		Status: c.Query("status"),
		Search: strings.TrimSpace(c.Query("q")),
	}

	if raw := c.Query("user_id"); raw != "" {
		uid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || uid <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("invalid user_id: %q", raw),
			})
			return
		}
		f.UserID = uid
	}

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("invalid limit: %q", raw),
			})
			return
		}
		f.Limit = n
	}
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("invalid offset: %q", raw),
			})
			return
		}
		f.Offset = n
	}

	rows, total, err := ListAdminOrders(connection.DB, f)
	if err != nil {
		// ListAdminOrders surfaces the "unknown status filter" branch as
		// an error string — translate that to 400 rather than 500.
		if strings.HasPrefix(err.Error(), "unknown status filter") {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "list admin orders failed: " + err.Error(),
		})
		return
	}
	if rows == nil {
		rows = []AdminOrderRow{}
	}

	// Effective limit/offset (post-clamping) so the UI knows what was
	// actually applied.
	limit := f.Limit
	if limit <= 0 {
		limit = adminListLimitDefault
	}
	if limit > adminListLimitMax {
		limit = adminListLimitMax
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"orders": rows,
			"total":  total,
			"limit":  limit,
			"offset": f.Offset,
		},
	})
}
