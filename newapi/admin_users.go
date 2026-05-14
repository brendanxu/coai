// admin_users.go — NewAPI user management handlers for the GTK admin panel.
//
// Surface:
//
//   GET  /gtk/v1/admin/users                   paginated user list from NewAPI
//   GET  /gtk/v1/admin/users/:user_id          single user detail
//   PUT  /gtk/v1/admin/users/:user_id/quota    adjust user quota
//   PUT  /gtk/v1/admin/users/:user_id/status   toggle user active/disabled
//
// All routes are gated by auth.RequireAdmin (401/403 before handler runs).
// actingUserID is always 0 (admin-level, no impersonation).

package newapi

import (
	"chat/auth"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// newAPIUserRaw mirrors the shape returned by NewAPI's user list endpoint.
// We decode into this and re-emit as-is so the frontend gets the full record.
type newAPIUserRaw struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        int    `json:"role"`       // 1=user, 10=admin, 100=root
	Status      int    `json:"status"`     // 1=active, 0=disabled
	Quota       int64  `json:"quota"`
	UsedQuota   int64  `json:"used_quota"`
	CreatedTime int64  `json:"created_time"` // unix seconds
}

// newAPIUserListEnvelope matches NewAPI's paginated user response shape.
// NewAPI /api/user/?p=&page_size= wraps the array in data.items.
type newAPIUserListEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    struct {
		Items []newAPIUserRaw `json:"items"`
		Total int64           `json:"total"`
	} `json:"data"`
}

// newAPIUserEnvelope matches NewAPI's single-user response shape.
type newAPIUserEnvelope struct {
	Success bool          `json:"success"`
	Message string        `json:"message,omitempty"`
	Data    newAPIUserRaw `json:"data"`
}

// ListUsersAPI handles GET /gtk/v1/admin/users
//
// Query params: page (default 1), page_size (default 20), keyword (optional).
// Proxies to NewAPI GET /api/user/?p=&page_size=&keyword=
func ListUsersAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	page := parseIntDefault(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := parseIntDefault(c.Query("page_size"), 20)
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	keyword := c.Query("keyword")

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}

	path := fmt.Sprintf("/api/user/?p=%d&page_size=%d&keyword=%s", page, pageSize, keyword)
	var env newAPIUserListEnvelope
	if err := cli.do(c.Request.Context(), "GET", path, nil, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "list users failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: list users: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"users": env.Data.Items,
			"total": env.Data.Total,
		},
	})
}

// GetUserAPI handles GET /gtk/v1/admin/users/:user_id
// Proxies to NewAPI GET /api/user/:id
func GetUserAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	userID, ok := parseUserIDParam(c)
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

	path := fmt.Sprintf("/api/user/%d", userID)
	var env newAPIUserEnvelope
	if err := cli.do(c.Request.Context(), "GET", path, nil, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "get user failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: get user: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    env.Data,
	})
}

// adminQuotaBody is the JSON body for PUT /admin/users/:user_id/quota.
// Named differently from the existing UpdateUserQuotaRequest in types.go
// (which carries ID+Group fields for internal use).
type adminQuotaBody struct {
	Quota int64 `json:"quota"`
}

// UpdateUserQuotaAPI handles PUT /gtk/v1/admin/users/:user_id/quota
// Body: {quota: 100000}
// Calls NewAPI PUT /api/user/ with {id, quota}.
func UpdateUserQuotaAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	userID, ok := parseUserIDParam(c)
	if !ok {
		return
	}

	var req adminQuotaBody
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

	if err := cli.UpdateUserQuota(c.Request.Context(), userID, req.Quota); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "update quota failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdateUserStatusRequest is the JSON body for PUT /admin/users/:user_id/status.
type UpdateUserStatusRequest struct {
	Status int `json:"status"` // 1=active, 0=disabled
}

// UpdateUserStatusAPI handles PUT /gtk/v1/admin/users/:user_id/status
// Body: {status: 1|0}
// Calls NewAPI PUT /api/user/ with {id, status}.
func UpdateUserStatusAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	userID, ok := parseUserIDParam(c)
	if !ok {
		return
	}

	var req UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	if req.Status != 0 && req.Status != 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "status must be 0 (disabled) or 1 (active)",
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

	// Send partial update — only id + status.
	body := struct {
		ID     int64 `json:"id"`
		Status int   `json:"status"`
	}{ID: userID, Status: req.Status}

	var env envelope[any]
	if err := cli.do(c.Request.Context(), "PUT", "/api/user/", body, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "update status failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: update status: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
