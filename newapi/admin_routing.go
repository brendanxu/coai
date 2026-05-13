// admin_routing.go — admin-only HTTP handlers for per-user channel routing
// (PKG-4, architecture §19, L23 Gap 7).
//
// Surface:
//
//   GET  /api/gtk/v1/admin/user-routing            paginated list of all
//                                                   gtk_newapi_binding rows
//                                                   joined with auth.username
//   GET  /api/gtk/v1/admin/user-routing/:user_id   single user detail
//   PUT  /api/gtk/v1/admin/user-routing/:user_id   change a user's
//                                                   newapi_group (writes
//                                                   to NewAPI then DB)
//   GET  /api/gtk/v1/admin/channels                NewAPI channel inventory
//                                                   with group whitelist
//   PUT  /api/gtk/v1/admin/channels/:channel_id    update a channel's group
//                                                   whitelist (v0 stub —
//                                                   logs intent + returns
//                                                   501 Not Implemented;
//                                                   founder edits via raw
//                                                   NewAPI dashboard until
//                                                   v0.20 builds the path)
//
// Why the stub for the channel-update endpoint: the read path
// (ListChannels) is already battle-tested by pool.go. The write path
// (PUT /api/channel/) requires a richer body shape than the simple
// {id, status} we use in DisableToken — model whitelist, model_mapping,
// priority, base URL etc. would all need to be preserved when patching
// just the group field. PKG-4's scope (per spec §19.6) is "view + flip
// user.group" — that ships in this PKG. Channel-side edits are listed
// as a follow-up so the founder can use the raw NewAPI admin UI in the
// interim.
//
// Auth: all routes are gated by auth.RequireAdmin which returns 401
// (no auth) or 403 (auth but not admin) before the handler runs.

package newapi

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// listChannelsFn is overridable in tests. Production wires it to
// Default().ListChannels via channelLister(). Tests swap this to return
// a canned channel list without making real HTTP to NewAPI.
var listChannelsFn = func(ctx context.Context) ([]ChannelInfo, error) {
	cli, err := Default()
	if err != nil {
		return nil, err
	}
	return cli.ListChannels(ctx)
}

// syncBindingGroupFn is overridable in tests. Production wires it to
// SyncBindingGroup which calls NewAPI then writes the DB. Tests swap
// this to skip the NewAPI roundtrip and exercise the handler-level
// branching only.
var syncBindingGroupFn = SyncBindingGroup

// RegisterAdminRoutes wires the admin routes onto the provided group.
// Called from Register so the existing main.go newapi.Register call
// picks them up without a separate hookup line.
func RegisterAdminRoutes(app *gin.RouterGroup) {
	app.GET("/gtk/v1/admin/user-routing", ListUserRoutingAPI)
	app.GET("/gtk/v1/admin/user-routing/:user_id", GetUserRoutingAPI)
	app.PUT("/gtk/v1/admin/user-routing/:user_id", UpdateUserRoutingAPI)
	app.GET("/gtk/v1/admin/channels", ListChannelsAPI)
	app.POST("/gtk/v1/admin/channels", CreateChannelAPI)
	app.GET("/gtk/v1/admin/channels/:channel_id", GetChannelAPI)
	app.PUT("/gtk/v1/admin/channels/:channel_id", UpdateChannelAPI)
	app.DELETE("/gtk/v1/admin/channels/:channel_id", DeleteChannelAPI)
	app.POST("/gtk/v1/admin/channels/:channel_id/test", TestChannelAPI)
}

// UserRoutingRow is the JSON shape returned by the list + detail endpoints.
type UserRoutingRow struct {
	CoaiUserID     int64  `json:"coai_user_id"`
	Username       string `json:"username"`
	NewapiUserID   int64  `json:"newapi_user_id"`
	NewapiGroup    string `json:"newapi_group"`
	LastKnownQuota int64  `json:"last_known_quota"`
}

// ListUserRoutingAPI handles GET /api/gtk/v1/admin/user-routing.
//
// Query params:
//
//	limit   default 50, max 500 — clamped, never errors
//	offset  default 0
//	group   optional; when set, restricts to rows with newapi_group = ?
//
// Response shape:
//
//	{
//	  "success": true,
//	  "data": {
//	    "users": [{coai_user_id, username, newapi_user_id, newapi_group,
//	               last_known_quota}, ...],
//	    "total": 17,
//	    "limit": 50,
//	    "offset": 0
//	  }
//	}
func ListUserRoutingAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	limit := parseIntDefault(c.Query("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := parseIntDefault(c.Query("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	group := strings.TrimSpace(c.Query("group"))

	rows, total, err := listUserRouting(connection.DB, group, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "list user-routing failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"users":  rows,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetUserRoutingAPI handles GET /api/gtk/v1/admin/user-routing/:user_id.
// Returns 404 when the user has no binding row yet.
func GetUserRoutingAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	userID, ok := parseUserIDParam(c)
	if !ok {
		return
	}
	row, err := loadUserRoutingRow(connection.DB, userID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "no binding for user_id " + strconv.FormatInt(userID, 10),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load user-routing failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    row,
	})
}

// UpdateUserRoutingRequest is the JSON body for PUT /admin/user-routing/:user_id.
type UpdateUserRoutingRequest struct {
	NewapiGroup string `json:"newapi_group" binding:"required"`
}

// UpdateUserRoutingAPI handles PUT /api/gtk/v1/admin/user-routing/:user_id.
//
// Validates that newapi_group is non-empty / non-whitespace (caller passes
// "default" explicitly if that's what they mean — see SyncBindingGroup).
// On success returns the updated row.
func UpdateUserRoutingAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return
	}
	userID, ok := parseUserIDParam(c)
	if !ok {
		return
	}

	var req UpdateUserRoutingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	newGroup := strings.TrimSpace(req.NewapiGroup)
	if newGroup == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "newapi_group must be non-empty (use 'default' explicitly)",
		})
		return
	}

	if err := syncBindingGroupFn(c.Request.Context(), connection.DB, userID, newGroup); err != nil {
		// SyncBindingGroup wraps both load + remote + DB write errors;
		// distinguish "no binding" so the admin UI can show a helpful
		// message vs a generic 500.
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "load binding") {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "no binding for user_id " + strconv.FormatInt(userID, 10),
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "sync group failed: " + err.Error(),
		})
		return
	}

	adminID := admin.GetID(connection.DB)
	globals.Info(fmt.Sprintf(
		"newapi: admin=%d flipped user_id=%d newapi_group → %s",
		adminID, userID, newGroup))

	// Read back the updated row so the client gets the fresh state.
	row, err := loadUserRoutingRow(connection.DB, userID)
	if err != nil {
		// SyncBindingGroup already ack'd, so the write succeeded; surface
		// a degraded payload rather than 500.
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": UserRoutingRow{
				CoaiUserID:  userID,
				NewapiGroup: newGroup,
			},
			"warning": "row updated but read-back failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    row,
	})
}

// ListChannelsAPI handles GET /api/gtk/v1/admin/channels.
// Returns the NewAPI channel inventory with group + status + models.
//
// 503 + degraded payload when NewAPI is unconfigured (mirrors PoolAPI).
func ListChannelsAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	channels, err := listChannelsFn(c.Request.Context())
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":  false,
			"message":  "newapi: admin token not configured — channels unavailable",
			"degraded": true,
			"data": gin.H{
				"channels": []ChannelInfo{},
				"total":    0,
			},
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "list channels failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"channels": channels,
			"total":    len(channels),
		},
	})
}

// parseChannelIDParam pulls :channel_id and writes a 400 on failure. Returns
// (id, true) on success and (0, false) when the handler should bail.
func parseChannelIDParam(c *gin.Context) (int64, bool) {
	raw := c.Param("channel_id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid channel_id: " + raw,
		})
		return 0, false
	}
	return id, true
}

// CreateChannelAPI handles POST /api/gtk/v1/admin/channels.
// Creates a new channel in NewAPI and returns the created ChannelInfo (201).
func CreateChannelAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	var req ChannelWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}
	ch, err := cli.CreateChannel(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "create channel failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    ch,
	})
}

// GetChannelAPI handles GET /api/gtk/v1/admin/channels/:channel_id.
// Returns the ChannelInfo for a single channel.
func GetChannelAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parseChannelIDParam(c)
	if !ok {
		return
	}
	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}
	ch, err := cli.GetChannel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "get channel failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    ch,
	})
}

// UpdateChannelAPI handles PUT /api/gtk/v1/admin/channels/:channel_id.
// Replaces the v0 stub (UpdateChannelGroupsAPI). Accepts a full
// ChannelWriteRequest body and forwards it to NewAPI.
func UpdateChannelAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parseChannelIDParam(c)
	if !ok {
		return
	}
	var req ChannelWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	req.ID = id
	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}
	ch, err := cli.UpdateChannel(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "update channel failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    ch,
	})
}

// DeleteChannelAPI handles DELETE /api/gtk/v1/admin/channels/:channel_id.
// Deletes the channel from NewAPI and returns {"success":true}.
func DeleteChannelAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parseChannelIDParam(c)
	if !ok {
		return
	}
	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}
	if err := cli.DeleteChannel(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "delete channel failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TestChannelAPI handles POST /api/gtk/v1/admin/channels/:channel_id/test.
// Triggers a test ping on the channel via NewAPI and returns the result.
func TestChannelAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parseChannelIDParam(c)
	if !ok {
		return
	}
	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}
	result, err := cli.TestChannel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "test channel failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// listUserRouting runs the JOIN over gtk_newapi_binding × auth and returns
// (rows, total) where total is the count *for the same filter* (so the
// frontend can paginate correctly).
//
// The query uses LEFT JOIN auth so a binding for a deleted user (orphan
// FK if ON DELETE somehow misfired) still surfaces with empty username
// rather than vanishing.
func listUserRouting(db *sql.DB, group string, limit, offset int) ([]UserRoutingRow, int64, error) {
	var args []any
	where := ""
	if group != "" {
		where = "WHERE b.newapi_group = ?"
		args = append(args, group)
	}

	// Total first (same WHERE so pagination math is consistent).
	var total int64
	totalSQL := "SELECT COUNT(*) FROM gtk_newapi_binding b " + where
	if err := globals.QueryRowDb(db, totalSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count gtk_newapi_binding: %w", err)
	}

	listSQL := `
		SELECT b.coai_user_id, COALESCE(a.username, ''), b.newapi_user_id,
		       b.newapi_group, b.last_known_quota
		FROM gtk_newapi_binding b
		LEFT JOIN auth a ON a.id = b.coai_user_id
		` + where + `
		ORDER BY b.coai_user_id ASC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]any{}, args...), limit, offset)
	rows, err := globals.QueryDb(db, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list gtk_newapi_binding: %w", err)
	}
	defer rows.Close()

	out := make([]UserRoutingRow, 0, limit)
	for rows.Next() {
		var r UserRoutingRow
		if err := rows.Scan(&r.CoaiUserID, &r.Username, &r.NewapiUserID,
			&r.NewapiGroup, &r.LastKnownQuota); err != nil {
			return nil, 0, fmt.Errorf("scan binding row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iter binding rows: %w", err)
	}
	return out, total, nil
}

// loadUserRoutingRow returns one row by coai_user_id. Returns sql.ErrNoRows
// when the user has no binding (caller maps to 404).
func loadUserRoutingRow(db *sql.DB, coaiUserID int64) (*UserRoutingRow, error) {
	var r UserRoutingRow
	err := globals.QueryRowDb(db, `
		SELECT b.coai_user_id, COALESCE(a.username, ''), b.newapi_user_id,
		       b.newapi_group, b.last_known_quota
		FROM gtk_newapi_binding b
		LEFT JOIN auth a ON a.id = b.coai_user_id
		WHERE b.coai_user_id = ?
	`, coaiUserID).Scan(&r.CoaiUserID, &r.Username, &r.NewapiUserID,
		&r.NewapiGroup, &r.LastKnownQuota)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// parseUserIDParam pulls :user_id and writes a 400 on failure. Returns
// (id, true) on success and (0, false) when the handler should bail.
func parseUserIDParam(c *gin.Context) (int64, bool) {
	raw := c.Param("user_id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid user_id: " + raw,
		})
		return 0, false
	}
	return id, true
}
