package payment

import (
	"chat/globals"
	"chat/utils"
	"database/sql"

	"github.com/gin-gonic/gin"
)

// HealthAPI is a lightweight readiness probe for the payment subsystem.
// Used by Caddy / external monitors / ops dashboards to confirm the
// webhook surface is alive and processing events.
//
// GET /api/payment/health
//
// Response:
//
//	{
//	  "healthy": true,
//	  "processed_total": 1247,
//	  "last_event_at": "2026-04-27 22:47:11"
//	}
//
// "healthy" is always true if the handler responds at all — DB connectivity
// is implicit via the COUNT query. A real monitor probes the HTTP 200 +
// JSON shape, not the boolean (which is mostly self-descriptive).
//
// Intentionally unauthenticated. The exposed counts are not sensitive
// (no user IDs, no event payloads) and keeping it auth-free lets external
// uptime monitors hit it without cookie wrangling.
func HealthAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	var processed int64
	var lastEventAt sql.NullString

	if err := globals.QueryRowDb(db,
		`SELECT COUNT(*) FROM gtk_webhook_event WHERE processed_at IS NOT NULL`,
	).Scan(&processed); err != nil {
		// DB error — surface as unhealthy without leaking detail.
		c.JSON(503, gin.H{"healthy": false, "error": "database unreachable"})
		return
	}

	// Best-effort: last_event_at is informational. Empty string if no events yet.
	_ = globals.QueryRowDb(db,
		`SELECT MAX(processed_at) FROM gtk_webhook_event`,
	).Scan(&lastEventAt)

	c.JSON(200, gin.H{
		"healthy":         true,
		"processed_total": processed,
		"last_event_at":   lastEventAt.String,
	})
}
