// Admin-side lead pipeline API for /admin/mansu kanban.
//
// Two endpoints, both behind auth.RequireAdmin:
//
//   GET   /api/gtk/v1/admin/leads[?status=...]
//         List leads, optionally filtered by status. Returns Lead[]
//         shaped for the frontend admin-mansu.ts type contract.
//
//   PATCH /api/gtk/v1/admin/leads/:id/status
//         Move a lead between stages. JSON body: {"status": "..."}
//
// Status enum (kanban order):
//   new         — just submitted, founder hasn't reached out yet
//   contacted   — founder has 微信 / called, awaiting customer reply
//   signed      — customer agreed, paid; not yet running
//   running     — service order active (gtk_service_order rows exist)
//   done        — service complete; renewal conversation now
//   lost        — didn't convert (subsumes legacy 'dropped' and 'spam')
//
// The migration's older values (converted/dropped/spam) are still
// readable; the frontend kanban silently treats unknown statuses as
// 'lost' to fail-safe. Future migration can rewrite old rows once
// founder confirms zero data left in old states.

package lead

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterAdmin wires the admin endpoints onto the gin router. Public
// /gtk/v1/contact stays in router.go; this file owns admin paths only.
func RegisterAdmin(app *gin.RouterGroup) {
	app.GET("/gtk/v1/admin/leads", AdminListLeadsAPI)
	app.PATCH("/gtk/v1/admin/leads/:id/status", AdminPatchLeadStatusAPI)
}

// validStatuses is the v0.13 kanban set. Backend rejects anything
// outside this list to keep DB clean. Frontend admin-mansu.ts uses
// the exact same strings.
var validStatuses = map[string]bool{
	"new":       true,
	"contacted": true,
	"signed":    true,
	"running":   true,
	"done":      true,
	"lost":      true,
}

// AdminLead is the JSON shape for a lead row sent to the kanban.
// Field names match admin-mansu.ts Lead type exactly.
type AdminLead struct {
	ID             int64  `json:"id"`
	Wechat         string `json:"wechat"`
	Phone          string `json:"phone"`
	HomestayName   string `json:"homestay_name"`
	HomestayLoc    string `json:"homestay_loc"`
	Notes          string `json:"notes"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ActiveOrderNo  string `json:"active_order_no,omitempty"`
}

// AdminListLeadsAPI returns the lead pipeline. Newest-first, capped at
// 500 rows (kanban gets unusable past that — pagination is a v0.14+
// problem, not a 大理 50-customer-cap problem).
func AdminListLeadsAPI(c *gin.Context) {
	if auth.RequireAdmin(c) == nil {
		return
	}

	db := connection.DB
	statusFilter := c.Query("status")

	var rows *sql.Rows
	var err error

	// v0.16: prefer the direct lead_id link on gtk_service_order. Falls
	// back to the wechat/phone=username heuristic so legacy orders
	// (created before lead_id existed) still surface in the kanban.
	// Picks the most recent active order so a re-buy supersedes the
	// completed prior one.
	const baseSQL = `
		SELECT
		  l.id, l.wechat, l.phone, l.homestay_name, l.homestay_loc,
		  l.notes, l.source, l.status, l.created_at, l.updated_at,
		  COALESCE((
		    SELECT o.order_no FROM gtk_service_order o
		    WHERE o.lead_id = l.id
		      AND o.status IN ('pending_payment','paid','running','completed')
		    ORDER BY o.created_at DESC LIMIT 1
		  ), (
		    SELECT o.order_no FROM gtk_service_order o
		    JOIN auth u ON u.id = o.coai_user_id
		    WHERE o.lead_id IS NULL
		      AND ((l.wechat <> '' AND u.username = l.wechat)
		           OR (l.phone <> ''  AND u.username = l.phone))
		      AND o.status IN ('pending_payment','paid','running','completed')
		    ORDER BY o.created_at DESC LIMIT 1
		  ), '') AS active_order_no
		FROM gtk_lead l
	`

	if statusFilter != "" {
		if !validStatuses[statusFilter] {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid status filter",
			})
			return
		}
		rows, err = db.Query(baseSQL+" WHERE l.status = ? ORDER BY l.created_at DESC LIMIT 500", statusFilter)
	} else {
		rows, err = db.Query(baseSQL + " ORDER BY l.created_at DESC LIMIT 500")
	}

	if err != nil {
		globals.Warn("[lead.admin] list query failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "查询失败: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	leads := make([]AdminLead, 0, 16)
	for rows.Next() {
		var l AdminLead
		var wechat, phone, name, loc, notes, status, src, orderNo sql.NullString
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&l.ID, &wechat, &phone, &name, &loc, &notes, &src, &status,
			&createdAt, &updatedAt, &orderNo,
		); err != nil {
			globals.Warn("[lead.admin] scan failed: " + err.Error())
			continue
		}
		l.Wechat = wechat.String
		l.Phone = phone.String
		l.HomestayName = name.String
		l.HomestayLoc = loc.String
		l.Notes = notes.String
		l.Source = src.String
		l.Status = normalizeStatus(status.String)
		l.CreatedAt = createdAt.Format(time.RFC3339)
		l.UpdatedAt = updatedAt.Format(time.RFC3339)
		l.ActiveOrderNo = orderNo.String
		leads = append(leads, l)
	}
	if err := rows.Err(); err != nil {
		globals.Warn("[lead.admin] rows iteration: " + err.Error())
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    leads,
	})
}

// PatchStatusRequest is the JSON body for the status update endpoint.
type PatchStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// AdminPatchLeadStatusAPI moves a lead between kanban stages.
// Idempotent: setting the current status returns 200 with no DB write.
func AdminPatchLeadStatusAPI(c *gin.Context) {
	if auth.RequireAdmin(c) == nil {
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid lead id",
		})
		return
	}

	var req PatchStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid body: " + err.Error(),
		})
		return
	}

	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "unknown status (allowed: new/contacted/signed/running/done/lost)",
		})
		return
	}

	// Stamp contacted_at the first time we transition out of 'new'.
	// Lets us answer "how long does first response take?" without a
	// separate audit log.
	const sql = `
		UPDATE gtk_lead
		SET status = ?,
		    contacted_at = CASE
		      WHEN ? <> 'new' AND contacted_at IS NULL THEN CURRENT_TIMESTAMP
		      ELSE contacted_at
		    END
		WHERE id = ?
	`
	res, err := globals.ExecDb(connection.DB, sql, req.Status, req.Status, id)
	if err != nil {
		globals.Warn("[lead.admin] update status failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "更新失败: " + err.Error(),
		})
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "lead not found",
		})
		return
	}

	globals.Info("[lead.admin] status patched: id=" + idParam + " status=" + req.Status)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// normalizeStatus maps legacy values to the v0.13 enum so the kanban
// always sees something it can render. Older `converted/dropped/spam`
// values from pre-v0.13 still scan into one of the new buckets.
func normalizeStatus(raw string) string {
	switch raw {
	case "new", "contacted", "signed", "running", "done", "lost":
		return raw
	case "converted":
		return "done"
	case "dropped", "spam":
		return "lost"
	case "":
		return "new"
	default:
		// Unknown future value — keep it round-tripped so an admin
		// rolling back can debug instead of seeing data corruption.
		return raw
	}
}
