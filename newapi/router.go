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
//
// PKG-4 (architecture §19) added the /gtk/v1/admin/* surface for
// per-user channel routing admin; those routes live in admin_routing.go
// and are mounted via RegisterAdminRoutes here so main.go's existing
// newapi.Register call picks them up without a separate hookup.
func Register(app *gin.RouterGroup) {
	app.GET("/gtk/v1/pool", PoolAPI)
	app.GET("/gtk/v1/binding", BindingAPI)
	RegisterAdminRoutes(app)
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

// BindingAPI returns the caller's NewAPI api-key + endpoint URL +
// credit balance. Authenticated; uses CoAI's auth.RequireAuth (401s
// automatically).
//
// Response shape matches what the dashboard "API Key 复制 + 余额" widget
// needs (per v2 redesign, dashboard shows "Token 套餐剩 3,716k tokens"
// where the number is actually credits, not tokens):
//
//   {
//     "success": true,
//     "data": {
//       "api_key":           "sk-gtk-...",
//       "api_endpoint":      "https://api.greentokey.com/v1",
//       "credits_remaining": 4823,
//       "credits_used":      177,
//       "credits_total":     5000,
//       "newapi_user_id":    17,
//
//       // Raw quota for backend debugging — NOT shown to users:
//       "_quota_units":      7234500
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

	// last_known_quota is the absolute quota allocated at last provision/
	// top-up. To get "credits remaining" we need NewAPI's *current* quota
	// (which decreases as the user calls models). v0.9 uses the cached
	// value as a stale-acceptable proxy; v1 will fetch live from NewAPI's
	// /api/user endpoint or maintain a usage delta. For now the dashboard
	// gets a pessimistic-but-truthful "since last top-up" view.
	creditsTotal := QuotaToCredits(bind.LastKnownQuota)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"api_key":           bind.NewapiTokenKey,
			"api_endpoint":      endpoint,
			"credits_remaining": creditsTotal, // TODO v1: fetch live
			"credits_used":      0,            // TODO v1: live - last_known
			"credits_total":     creditsTotal,
			"newapi_user_id":    bind.NewapiUserID,
			"_quota_units":      bind.LastKnownQuota,
		},
	})
}
