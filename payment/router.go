package payment

import "github.com/gin-gonic/gin"

// Register wires payment-related routes into the main API group.
// Called from main.go:registerApiRouter alongside auth/admin/etc.
//
// Auth model:
//   POST /webhook/lemonsqueezy        — UNAUTHENTICATED. LS does not carry a
//     Bearer token; authenticity is established via X-Signature HMAC
//     inside HandleWebhook.
//   GET  /payment/checkout            — AUTHENTICATED via CoAI's AuthMiddleware
//     (calls auth.GetUserByCtx); user ID gets baked into the LS URL.
//     Optional ?plan_code=<code> embeds custom_data.plan_code so the LS
//     webhook routes the paid event to auth.RedeemPlanForOrder (L23 token
//     product path).
//   GET  /payment/hupijiao/checkout   — AUTHENTICATED. Mainland-pay sibling
//     of /payment/checkout. Required ?plan_code=<code>. Returns alipay
//     deep-link + QR PNG URL; webhook (service/webhook_handler.go::
//     HupijiaoCallbackAPI) parses attach="plan:CODE:user:ID" to redeem.
//   GET  /payment/subscription        — AUTHENTICATED. Returns the user's LS-side
//     subscription state (status, cancelled_at, renews_at, test_mode) plus
//     CoAI's authoritative level + expired_at. Frontend uses this to render
//     CTA states: Upgrade / Active / Cancelled-still-active / Past-due / Trial.
//   GET  /payment/health              — UNAUTHENTICATED. Exposes only aggregate
//     counts (no user IDs, no payloads); safe for external uptime probes.
func Register(app *gin.RouterGroup) {
	app.POST("/webhook/lemonsqueezy", HandleWebhook)
	app.GET("/payment/checkout", CheckoutAPI)
	app.GET("/payment/hupijiao/checkout", CheckoutHupijiaoForPlanAPI)
	app.GET("/payment/subscription", SubscriptionAPI)
	app.GET("/payment/health", HealthAPI)
}
