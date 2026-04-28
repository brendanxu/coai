package payment

import (
	"chat/globals"
	"chat/utils"

	"github.com/gin-gonic/gin"
)

// HealthAPI is a lightweight readiness probe for the payment subsystem.
// Used by Caddy / external monitors / ops dashboards to confirm the
// webhook surface is alive.
//
// GET /api/payment/health
//
// Response:
//
//	{ "healthy": true }
//	{ "healthy": false, "error": "database unreachable" }   // 503 on DB failure
//
// Intentionally minimal (Codex P2 fix, 2026-04-27): the previous version
// exposed processed_total and last_event_at, which leak business telemetry
// (subscriber count proxy, traffic floor). Public uptime monitors only need
// the boolean. Operators can run SQL directly against the DB for counts.
//
// Unauthenticated by design — external uptime monitors shouldn't need
// cookies. DB connectivity is verified implicitly via the SELECT 1 probe.
func HealthAPI(c *gin.Context) {
	db := utils.GetDBFromContext(c)

	// Cheap DB probe — confirms connectivity without exposing volume.
	var one int
	if err := globals.QueryRowDb(db, `SELECT 1`).Scan(&one); err != nil {
		c.JSON(503, gin.H{"healthy": false, "error": "database unreachable"})
		return
	}
	c.JSON(200, gin.H{"healthy": true})
}
