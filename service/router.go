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
	"chat/commerce"
	"chat/connection"
	"chat/globals"
	"chat/newapi"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Register wires the gtk/v1 service routes onto the main API group.
// Called from main.go:registerApiRouter alongside payment.Register +
// newapi.Register.
func Register(app *gin.RouterGroup) {
	app.GET("/gtk/v1/services", CatalogAPI)
	app.POST("/gtk/v1/service/order", CreateOrderAPI)
	// Authenticated — execute the agent for a paid order. v0.10 ④.
	app.POST("/gtk/v1/service/run/:order_no", RunOrderAPI)
	// Admin-only — flip an order to refunded status (no actual money
	// moved; founder handles the LS / hupijiao dashboard refund
	// separately). v0.10 ② per recommendation 11.Q5.
	app.POST("/gtk/v1/admin/refund", RefundAPI)
	// Admin-only — mark a manual / concierge order as paid (PKG-2 Wave 4
	// D7, Q5 GO). Wraps commerce.MarkPaid which calls GrantEntitlement.
	app.POST("/gtk/v1/admin/mark-paid", MarkPaidAPI)
	// Admin-only — read-only ops visibility into the
	// gtk_newapi_pending_provisions retry queue (PKG-3). Counts by
	// status; lets dashboards / Grafana panel surface drain health
	// without granting raw DB access. Pairs with the
	// bin/check-pending-provisioning.sh cron alert.
	app.GET("/gtk/v1/admin/pending-provisions", PendingProvisionsAPI)
	// Public — hupijiao webhook target. Auth is HMAC-MD5 against
	// hupijiao.merchant_secret (verified inside the handler).
	app.POST("/gtk/v1/service/hupijiao-callback", HupijiaoCallbackAPI)
	// Authenticated, customer-scoped. PKG-5 self-serve order tracking.
	// Both endpoints filter by coai_user_id at the SQL level — see
	// customer_orders.go header for security rationale.
	app.GET("/gtk/v1/orders", ListMyOrdersAPI)
	app.GET("/gtk/v1/orders/:order_no", MyOrderDetailAPI)
	// PKG-N2 — customer-side write actions on the order detail page.
	// Both filter by coai_user_id at the SQL level (same security
	// rationale as customer_orders.go).
	app.POST("/gtk/v1/orders/:order_no/refund-request", CustomerRefundRequestAPI)
	app.POST("/gtk/v1/orders/:order_no/reorder", CustomerReorderAPI)
}

// PendingProvisionsAPI returns the current state of the NewAPI
// provisioning retry queue (PKG-3). Admin-only; mirrors the auth +
// envelope pattern of MarkPaidAPI / RefundAPI.
//
// Response shape:
//
//	{
//	  "success": true,
//	  "data": {
//	    "pending":   <int64>,  // never-tried rows from this boot
//	    "retrying":  <int64>,  // attempted at least once, still in retry window
//	    "succeeded": <int64>,  // historical successes (cumulative)
//	    "failed":    <int64>,  // gave up after maxRetries — needs human review
//	    "total":     <int64>   // sum of the four buckets
//	  }
//	}
//
// 'failed' > 0 is the row-count to alert on; 'pending' / 'retrying'
// fluctuate normally during transient NewAPI outages.
func PendingProvisionsAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return // RequireAdmin already wrote the 401/403 envelope.
	}

	pending, retrying, succeeded, failed, err := newapi.PendingProvisionStats(connection.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "pending-provisions stats failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"pending":   pending,
			"retrying":  retrying,
			"succeeded": succeeded,
			"failed":    failed,
			"total":     pending + retrying + succeeded + failed,
		},
	})
}

// MarkPaidRequest is the JSON body for POST /admin/mark-paid.
//
// product_type is required and (v0) must be "service". Token plans go
// through the LS dashboard, not this endpoint.
type MarkPaidRequest struct {
	OrderNo     string `json:"order_no" binding:"required"`
	ProductType string `json:"product_type" binding:"required,oneof=service"`
}

// MarkPaidAPI is the admin-only HTTP handler for marking a manual order
// as paid. Mirrors RefundAPI's auth + envelope pattern.
//
// Use cases (per Q5 GO):
//   - Concierge order paid via offline channel (wechat / bank transfer).
//   - LS / hupijiao webhook lost; admin re-fires the entitlement grant.
//
// Status semantics: idempotent. If the order is already past
// 'pending_payment', commerce.GrantEntitlement's CAS no-ops without
// erroring.
func MarkPaidAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return // RequireAdmin already wrote the 401/403 envelope.
	}

	var req MarkPaidRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	adminID := admin.GetID(connection.DB)
	err := commerce.MarkPaid(
		c.Request.Context(),
		connection.DB,
		req.OrderNo,
		commerce.ProductType(req.ProductType),
		adminID,
	)
	if err != nil {
		// commerce.MarkPaid wraps GrantEntitlement errors; surface as 500.
		// The "unsupported product_type" branch shouldn't trigger because
		// of the binding validator, but if a future caller bypasses
		// validation we still want a clear error.
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":  false,
			"message":  "mark-paid failed: " + err.Error(),
			"order_no": req.OrderNo,
		})
		return
	}

	globals.Info(fmt.Sprintf(
		"service: order %s marked paid by admin %d (product_type=%s)",
		req.OrderNo, adminID, req.ProductType))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"order_no":     req.OrderNo,
			"product_type": req.ProductType,
			"status":       "paid",
		},
	})
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

	// PKG-2 Wave 4 D3: open a payment session for the order. Session is
	// the bridge between checkout-time and webhook-time
	// (commerce.ClosePaymentSession matches by session_id, CR7). All three
	// providers benefit:
	//   - lemonsqueezy → embed session_id in custom_data
	//   - hupijiao     → embed in prepay metadata (best-effort; hupijiao's
	//                    `plugins` field already carries greentokey_user_id
	//                    so we tack on greentokey_session_id alongside)
	//   - manual       → no provider payload to embed in; admin reconciles
	//                    via the gtk_payment_session row (visibility win)
	//
	// Failure to open the session is logged + non-fatal: the order row is
	// already authoritative, and webhook-side ClosePaymentSession tolerates
	// missing session_id (Wave 3 C2: pre-Wave-4 fallback path).
	session, sessErr := commerce.OpenPaymentSession(
		connection.DB, orderNo, commerce.ProductService,
		req.PaymentProvider, svc.PriceCNYCents, coaiUserID,
	)
	if sessErr != nil {
		globals.Warn(fmt.Sprintf(
			"service: OpenPaymentSession failed for order %s (provider=%s, user=%d): %v — proceeding without session_id",
			orderNo, req.PaymentProvider, coaiUserID, sessErr))
	}
	var sessionID string
	if session != nil {
		sessionID = session.SessionID
	}

	// Fill checkout payload by provider. The order row is already
	// inserted at this point — if checkout building fails, the order
	// stays in pending_payment for manual cleanup. Better than
	// leaving the customer without an order_no to reference.
	resp := gin.H{
		"order_no":         orderNo,
		"price_cny_cents":  svc.PriceCNYCents,
		"price_display":    FormatPriceCNY(svc.PriceCNYCents),
		"included_credits": svc.IncludedCredits,
		"service_name":     svc.Name,
		"service_slug":     svc.Slug,
	}

	switch req.PaymentProvider {
	case "lemonsqueezy":
		checkoutURL, err := BuildLSServiceCheckoutURL(coaiUserID, orderNo, svc, sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":  false,
				"message":  "build LS checkout URL failed: " + err.Error(),
				"order_no": orderNo, // surface so customer can reach support
			})
			return
		}
		resp["checkout_url"] = checkoutURL

	case "hupijiao":
		qr, err := BuildHupijiaoQR(coaiUserID, orderNo, svc, sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":  false,
				"message":  "build hupijiao QR failed: " + err.Error(),
				"order_no": orderNo,
			})
			return
		}
		// Frontend picks code_url (mobile, deep-link tap) vs qr_png_url
		// (desktop, scan with phone) based on UA detection.
		resp["alipay_code_url"] = qr.CodeURL
		resp["alipay_qr_png_url"] = qr.QRPNGURL
		resp["hupijiao_trade_no"] = qr.TradeNo

	case "manual":
		// Concierge / offline settlement. tana or founder calls the
		// customer, takes payment via wechat / bank transfer / cash,
		// then PATCHes the order to status='paid' via the admin
		// endpoint (D7 admin/mark-paid).
		// session_id is captured in the gtk_payment_session row so admin
		// UI / D7 reconcile workflow has visibility — no provider-side
		// payload to embed.
		resp["concierge"] = true
		resp["concierge_message"] = "我们将与您联系完成付款"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}
