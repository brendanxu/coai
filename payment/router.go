package payment

import "github.com/gin-gonic/gin"

// Register wires payment-related routes into the main API group.
// Called from main.go:registerApiRouter alongside auth/admin/etc.
//
// Auth model:
//   POST /webhook/lemonsqueezy   — UNAUTHENTICATED. LS does not carry a
//     Bearer token; authenticity is established via X-Signature HMAC
//     inside HandleWebhook.
//   GET  /payment/checkout       — AUTHENTICATED via CoAI's AuthMiddleware
//     (calls auth.GetUserByCtx); user ID gets baked into the LS URL.
//   GET  /payment/subscription   — AUTHENTICATED. Returns the user's LS-side
//     subscription state (status, cancelled_at, renews_at, test_mode) plus
//     CoAI's authoritative level + expired_at. Frontend uses this to render
//     CTA states: Upgrade / Active / Cancelled-still-active / Past-due / Trial.
//   GET  /payment/health         — UNAUTHENTICATED. Exposes only aggregate
//     counts (no user IDs, no payloads); safe for external uptime probes.
func Register(app *gin.RouterGroup) {
	app.POST("/webhook/lemonsqueezy", HandleWebhook)
	app.GET("/payment/checkout", CheckoutAPI)
	app.GET("/payment/subscription", SubscriptionAPI)
	app.GET("/payment/health", HealthAPI)
}
