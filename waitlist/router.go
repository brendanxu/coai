package waitlist

import "github.com/gin-gonic/gin"

// Register wires waitlist routes into the main API group.
// Called from main.go:registerApiRouter alongside auth/admin/payment/carbon.
//
// Auth model:
//
//	POST /waitlist — UNAUTHENTICATED. Visitors on the marketing landing
//	  page are pre-account; the endpoint relies on per-IP rate limiting
//	  and a unique (email, service) constraint to bound abuse.
func Register(app *gin.RouterGroup) {
	app.POST("/waitlist", HandleJoin)
}
