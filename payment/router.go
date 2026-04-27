package payment

import "github.com/gin-gonic/gin"

// Register wires payment-related routes into the main API group.
// Called from main.go:registerApiRouter alongside auth/admin/etc.
//
// The webhook endpoint is INTENTIONALLY unauthenticated — LS does not
// carry a Bearer token; authenticity is established via X-Signature HMAC
// inside HandleWebhook. CoAI's AuthMiddleware does not gate this path
// because it doesn't gate /register, /login etc. either.
//
// CheckoutAPI requires auth (calls auth.GetUserByCtx); CoAI's middleware
// chain handles the JWT extraction and exposes the username via context.
func Register(app *gin.RouterGroup) {
	app.POST("/webhook/lemonsqueezy", HandleWebhook)
	app.GET("/payment/checkout", CheckoutAPI)
}
