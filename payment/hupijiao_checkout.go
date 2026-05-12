// Hupijiao (虎皮椒) checkout endpoint for L2 token plans.
//
// Mainland customers pay in CNY via alipay/wechat; the LemonSqueezy path
// (checkout.go) handles overseas card payments in USD. Both end at
// auth.RedeemPlanForOrder via their respective webhook handlers
// (payment/lemonsqueezy.go for LS, service/webhook_handler.go::HupijiaoCallbackAPI
// for hupijiao — the L2 attach="plan:CODE:user:ID" parser was added by PKG-M1-1+2).
//
// Wire route: GET /api/payment/hupijiao/checkout?plan_code=token-99
// Auth: required (auth.GetUserByCtx)
// Response: 200 {"status": true, "code_url": "alipays://...", "qr_png_url": "...", "trade_no": "<order_no>"}
//          400 {"status": false, "error": "..."} on bad input
//          500 {"status": false, "error": "..."} on hupijiao-side or config failure
//
// Frontend pattern: fetch the endpoint → render qr_png_url for desktop
// or window.location.href = code_url for mobile (alipay deep-link).

package payment

import (
	"chat/auth"
	"chat/commerce"
	"chat/globals"
	"chat/plans"
	"chat/utils"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// HupijiaoCheckoutResult is the JSON envelope returned to the frontend.
//
// CodeURL  — alipay native deep-link (alipays://...) for mobile tap-to-pay
// QRPNGURL — PNG image URL for desktop QR scan
// TradeNo  — what we sent as trade_order_id; echoes back in webhook for traceability
type HupijiaoCheckoutResult struct {
	CodeURL  string `json:"code_url"`
	QRPNGURL string `json:"qr_png_url"`
	TradeNo  string `json:"trade_no"`
}

// CheckoutHupijiaoForPlanAPI builds a hupijiao alipay payment URL pre-bound
// to the authenticated user + the requested gtk_plan row. The webhook
// (HupijiaoCallbackAPI) parses attach="plan:CODE:user:ID" to call
// auth.RedeemPlanForOrder using `transaction_id` (or trade_order_id fallback)
// as the idempotency key.
func CheckoutHupijiaoForPlanAPI(c *gin.Context) {
	user := auth.GetUserByCtx(c)
	if user == nil {
		// auth.GetUserByCtx already wrote the response.
		return
	}

	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)

	planCode := strings.TrimSpace(c.Query("plan_code"))
	if planCode == "" {
		c.JSON(400, gin.H{"status": false,
			"error": "plan_code query parameter required"})
		return
	}

	plan, err := plans.LookupActivePlan(db, planCode)
	if err != nil {
		// ErrPlanNotFound + scan errors all surface as 400 to the user.
		// Operators check logs to disambiguate config vs corruption.
		c.JSON(400, gin.H{"status": false,
			"error": fmt.Sprintf("plan unavailable: %s", planCode)})
		return
	}
	if plan.PriceCents <= 0 {
		c.JSON(400, gin.H{"status": false,
			"error": fmt.Sprintf("plan %s has non-positive price", planCode)})
		return
	}

	// Synthetic order_no — gtk_user_plan row materializes inside
	// RedeemPlanForOrder when the webhook lands. The session row uses
	// this for audit; reconciliation happens by transaction_id (echoed
	// back in attach by hupijiao + parsed by webhook handler).
	orderNo := fmt.Sprintf("hupi_pending_%d_%d", userID, time.Now().Unix())

	session, sessErr := commerce.OpenPaymentSession(
		db, orderNo, commerce.ProductToken,
		"hupijiao", plan.PriceCents, userID,
	)
	if sessErr != nil {
		// Same tolerance as LS path — log + proceed without session_id.
		logf(globals.Warn, "hupi_checkout_session_open_failed",
			"user_id", userID, "plan_code", planCode, "error", sessErr)
	}
	var sessionID string
	if session != nil {
		sessionID = session.SessionID
	}

	result, err := buildHupijiaoCheckoutForPlan(userID, orderNo, plan, sessionID)
	if err != nil {
		if errors.Is(err, ErrHupijiaoNotConfigured) {
			c.JSON(500, gin.H{"status": false, "error": err.Error()})
			return
		}
		c.JSON(502, gin.H{"status": false,
			"error": fmt.Sprintf("hupijiao gateway: %v", err)})
		return
	}

	c.JSON(200, gin.H{
		"status":     true,
		"code_url":   result.CodeURL,
		"qr_png_url": result.QRPNGURL,
		"trade_no":   result.TradeNo,
	})
}

// ErrHupijiaoNotConfigured surfaces operator misconfig (missing
// hupijiao.merchant_id or hupijiao.merchant_secret in viper).
var ErrHupijiaoNotConfigured = errors.New("payment: hupijiao not configured")

// buildHupijiaoCheckoutForPlan composes the hupijiao request, signs it,
// POSTs to xunhupay.com/payment/do.html, and returns the URLs on success.
//
// attach="plan:<code>:user:<userID>" is the discriminator the webhook
// handler parses (PKG-M1-1+2 done report + service/webhook_handler.go:238).
// Format must match exactly — splitter expects strings.Split(":") len=4
// with parts[0]=="plan" and parts[2]=="user".
func buildHupijiaoCheckoutForPlan(
	userID int64, orderNo string, plan *plans.Plan, sessionID string,
) (*HupijiaoCheckoutResult, error) {
	if plan == nil {
		return nil, errors.New("payment: buildHupijiaoCheckoutForPlan requires non-nil plan")
	}
	if plan.PriceCents <= 0 {
		return nil, fmt.Errorf("payment: plan %s price must be > 0 (got %d cents)",
			plan.Code, plan.PriceCents)
	}

	merchantID := viper.GetString("hupijiao.merchant_id")
	merchantSecret := viper.GetString("hupijiao.merchant_secret")
	if merchantID == "" || merchantSecret == "" {
		return nil, fmt.Errorf("%w: merchant_id or merchant_secret missing",
			ErrHupijiaoNotConfigured)
	}
	endpoint := viper.GetString("hupijiao.endpoint")
	if endpoint == "" {
		endpoint = defaultHupijiaoEndpoint
	}
	notifyURL := viper.GetString("hupijiao.callback_url")
	if notifyURL == "" {
		// Service-side callback handler also services L2 plan redemption
		// (per PKG-M1-1+2 done report) — same URL.
		notifyURL = "https://api.greentokey.com/api/gtk/v1/service/hupijiao-callback"
	}
	returnURL := viper.GetString("hupijiao.return_url")
	if returnURL == "" {
		returnURL = "https://api.greentokey.com/account"
	}

	// Yuan as decimal string per hupijiao spec.
	yuan := fmt.Sprintf("%d.%02d", plan.PriceCents/100, plan.PriceCents%100)

	// attach format MUST match service/webhook_handler.go:238 parser:
	//   strings.HasPrefix(attach, "plan:") &&
	//   parts := strings.Split(":") with len=4 and parts[2]=="user"
	attach := fmt.Sprintf("plan:%s:user:%d", plan.Code, userID)

	// session_id rides as a sidecar field. Webhook doesn't currently use
	// it for plan path (RedeemPlanForOrder is keyed on transaction_id),
	// but include it for audit + future ClosePaymentSession support.
	if sessionID != "" {
		attach += fmt.Sprintf(":session:%s", sessionID)
	}

	params := map[string]string{
		"version":        "1.1",
		"appid":          merchantID,
		"trade_order_id": orderNo,
		"total_fee":      yuan,
		"title":          plan.Name,
		"time":           fmt.Sprintf("%d", time.Now().Unix()),
		"notify_url":     notifyURL,
		"return_url":     returnURL,
		"nonce_str":      randomNonce(),
		"type":           "WAP", // alipay native — works for QR + deep-link
		"attach":         attach,
	}
	params["hash"] = hupijiaoSign(params, merchantSecret)

	resp, err := postForm(endpoint, params)
	if err != nil {
		return nil, fmt.Errorf("hupijiao request: %w", err)
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("hupijiao rejected (code=%d msg=%s)",
			resp.ErrCode, resp.ErrMsg)
	}

	return &HupijiaoCheckoutResult{
		CodeURL:  resp.URL,
		QRPNGURL: resp.URLQRCode,
		TradeNo:  orderNo,
	}, nil
}
