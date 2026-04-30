// HTTP routes for the greentokey newapi feature surface.
//
// Two endpoints in v0.9:
//
//   GET /api/gtk/v1/pool       UNAUTHENTICATED. Returns the public PoolSnapshot
//     (model list with provider labels, channel count, avg latency). The
//     marketing landing's "14 款现货" grid + "+3 在路上" coming-soon strip read
//     from this. Cached 30s server-side.
//
//   GET /api/gtk/v1/binding    AUTHENTICATED. Returns the calling user's
//     NewAPI api-key (sk-xxx), endpoint URL, and last-known quota. The
//     dashboard's "复制我的 API Key" widget reads from this. If the user
//     has not yet been provisioned (no row in gtk_newapi_binding), 404.

package newapi

import (
	"chat/auth"
	"chat/connection"
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Register wires the gtk/v1 newapi routes onto the main API router group.
// Called from main.go:registerApiRouter alongside payment.Register etc.
func Register(app *gin.RouterGroup) {
	app.GET("/gtk/v1/pool", PoolAPI)
	app.GET("/gtk/v1/binding", BindingAPI)
}

// PoolAPI returns the current model pool snapshot. Public — no auth gate.
// Returns 503 + degraded payload when NewAPI is unconfigured (dev/preview)
// so the frontend can render a placeholder instead of 500-ing.
func PoolAPI(c *gin.Context) {
	snap, err := GetPoolSnapshot(c.Request.Context())
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success":  false,
			"message":  "newapi: admin token not configured — pool unavailable",
			"degraded": true,
			"data": &PoolSnapshot{
				// empty zeros so frontend renders "0 款现货" gracefully
				Channels: []ChannelInfo{},
				Models:   []ModelEntry{},
			},
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "pool snapshot failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    snap,
	})
}

// BindingAPI returns the caller's NewAPI api-key + endpoint URL.
// Authenticated; uses CoAI's auth.RequireAuth (401s automatically).
//
// Response shape matches what the dashboard "API Key 复制" widget expects:
//
//   {
//     "success": true,
//     "data": {
//       "api_key": "sk-gtk-...",
//       "api_endpoint": "https://api.greentokey.com/v1",
//       "last_known_quota": 7500000,
//       "newapi_user_id": 17
//     }
//   }
//
// 404 when the user hasn't been provisioned yet (e.g. they signed up
// but haven't paid, or webhook fire failed).
func BindingAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return // RequireAuth already wrote 401
	}

	bind, err := LoadBinding(connection.DB, int64(user.GetID(connection.DB)))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "no api-key provisioned yet — purchase a 套餐 to get one",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load binding failed: " + err.Error(),
		})
		return
	}

	endpoint := viper.GetString("newapi.public_endpoint")
	if endpoint == "" {
		endpoint = "https://api.greentokey.com/v1"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"api_key":          bind.NewapiTokenKey,
			"api_endpoint":     endpoint,
			"last_known_quota": bind.LastKnownQuota,
			"newapi_user_id":   bind.NewapiUserID,
		},
	})
}
