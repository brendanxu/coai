// HTTP routes for the greentokey Layer 3 service catalog.
//
// Two endpoints in v0.9 scaffolding:
//
//   GET  /api/gtk/v1/services            UNAUTHENTICATED. Returns the
//     active service catalog (PublicService DTOs). The marketing /
//     services page reads this to render the bundle cards.
//
//   POST /api/gtk/v1/service/order       AUTHENTICATED. Creates a
//     gtk_service_order row in status='pending_payment' and returns
//     the new order_no. Real checkout-URL generation is deferred to
//     v0.10 (will redirect to LemonSqueezy or generate hupijiao QR).
//     This stub exists so the frontend purchase flow can be wired
//     end-to-end against a real backend before payment integration
//     lands.

package service

import (
	"chat/auth"
	"chat/connection"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Register wires the gtk/v1 service routes onto the main API group.
// Called from main.go:registerApiRouter alongside payment.Register +
// newapi.Register.
func Register(app *gin.RouterGroup) {
	app.GET("/gtk/v1/services", CatalogAPI)
	app.POST("/gtk/v1/service/order", CreateOrderAPI)
}

// CatalogAPI returns the public service catalog. Public — no auth gate.
// Customers see this on the /services page when deciding what to buy.
func CatalogAPI(c *gin.Context) {
	items, err := LoadActiveServices(connection.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load catalog failed: " + err.Error(),
		})
		return
	}
	if items == nil {
		// Distinguish empty-active-set from nil. Frontend renders
		// "敬请期待" placeholder for empty.
		items = []PublicService{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"services": items,
			"total":    len(items),
		},
	})
}

// CreateOrderRequest is the JSON body shape for POST /service/order.
// payment_provider is required so the caller signals which payment flow
// they expect (overseas card → lemonsqueezy, mainland alipay → hupijiao,
// concierge / manual settlement → manual).
type CreateOrderRequest struct {
	ServiceSlug     string `json:"service_slug" binding:"required"`
	PaymentProvider string `json:"payment_provider" binding:"required,oneof=lemonsqueezy hupijiao manual"`
}

// CreateOrderAPI authenticates the user, validates the service slug,
// and inserts a pending order. Does NOT initiate payment — the response
// includes an order_no the frontend can poll, plus a checkout_url that
// is empty in v0.9 and will be filled in v0.10.
func CreateOrderAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	svc, err := LoadServiceBySlug(connection.DB, req.ServiceSlug)
	if errors.Is(err, ErrServiceNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "service not found or no longer available",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load service failed: " + err.Error(),
		})
		return
	}

	coaiUserID := user.GetID(connection.DB)
	orderNo, err := CreateOrder(connection.DB, coaiUserID, svc, req.PaymentProvider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "create order failed: " + err.Error(),
		})
		return
	}

	// v0.9 stub. v0.10 will fill checkout_url with:
	//   - LemonSqueezy: hosted checkout URL via newapi/checkout.go pattern
	//   - hupijiao: API call → returns code_url for QR rendering
	//   - manual: empty string (concierge handles offline)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"order_no":         orderNo,
			"checkout_url":     "",
			"checkout_pending": true,
			"price_cny_cents":  svc.PriceCNYCents,
			"price_display":    FormatPriceCNY(svc.PriceCNYCents),
			"included_credits": svc.IncludedCredits,
			"service_name":     svc.Name,
			"service_slug":     svc.Slug,
		},
	})
}
