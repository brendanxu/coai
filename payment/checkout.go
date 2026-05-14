package payment

import (
	"chat/auth"
	"chat/commerce"
	"chat/globals"
	"chat/plans"
	"chat/utils"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// LS store slugs are DNS labels: lowercase alphanumerics + hyphens, 1-63 chars,
// not starting or ending with a hyphen. LS variant IDs are positive integers.
//
// Validating these defends against (Codex P2 fix, 2026-04-27): operator
// misconfiguration like `evil.com/`, `..`, `foo@evil.com`, or `#` that would
// reshape the URL into something away from `*.lemonsqueezy.com` — and against
// silent failures where the URL builds successfully but routes to nowhere.
var (
	storeSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	variantIDRe = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
)

// CheckoutAPI returns a LemonSqueezy Checkout URL pre-bound to the
// authenticated user via custom_data[user_id]. Optionally accepts
// ?plan_code=<code> to also embed custom_data[type]=plan +
// custom_data[plan_code]=<code> so the LS webhook (lemonsqueezy.go
// planCodeFromCustomData) routes the paid event to
// auth.RedeemPlanForOrder per the L23 token-product path.
//
// GET /api/payment/checkout                      → legacy starter (level subscribe)
// GET /api/payment/checkout?plan_code=token-99   → L2 plan redeem path
//
// Response shapes (matches CoAI convention: status flag + payload):
//
//	200 {"status": true,  "url": "https://<slug>.lemonsqueezy.com/buy/<variant>?...&checkout%5Bcustom%5D%5Buser_id%5D=42&checkout%5Bcustom%5D%5Bplan_code%5D=token-99"}
//	400 {"status": false, "error": "plan_code unknown: <code>"}
//	500 {"status": false, "error": "LEMONSQUEEZY_STORE_SLUG not configured"}
//	(unauthenticated path is handled by auth.GetUserByCtx, which writes a 200+error envelope)
//
// Test-mode toggle is implicit: the variant_id you configure determines
// whether the checkout runs against LS Test Mode or Live Mode (LS uses
// separate variants per mode).
func CheckoutAPI(c *gin.Context) {
	user := auth.GetUserByCtx(c)
	if user == nil {
		// auth.GetUserByCtx already wrote the response.
		return
	}

	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)

	planCode := strings.TrimSpace(c.Query("plan_code"))

	// When plan_code is supplied, look up gtk_plan to derive the audit
	// amount for gtk_payment_session. The LS webhook is still authoritative
	// on actual paid amount; this is just the row's expected_amount column.
	amountCents := int64(1500) // legacy ¥99 ≈ $15 placeholder
	if planCode != "" {
		plan, err := plans.LookupActivePlan(db, planCode)
		if err != nil {
			c.JSON(400, gin.H{"status": false,
				"error": fmt.Sprintf("plan_code unknown: %s", planCode)})
			return
		}
		if plan.PriceCents > 0 {
			amountCents = plan.PriceCents
		}
	}

	// PKG-2 Wave 4 D2: open a payment session BEFORE building the URL so
	// we can embed session_id in LS custom_data. Webhook dispatch
	// (dispatch_token.go via Wave 3 C1 + commerce.ClosePaymentSession in
	// Wave 2 B1) matches inbound by session_id (CR7) — this is the only
	// key both ends control at checkout time, since the LS subscription_id
	// only exists post-payment.
	syntheticOrderNo := fmt.Sprintf("ls_pending_%d_%d", userID, time.Now().Unix())

	session, sessErr := commerce.OpenPaymentSession(
		db, syntheticOrderNo, commerce.ProductToken,
		"lemonsqueezy", amountCents, userID,
	)
	if sessErr != nil {
		// Don't break the user's checkout flow on a session-row insert
		// failure. Log + proceed without session_id; the webhook layer
		// already tolerates absent session_id (Wave 3 C1: "pre-Wave-4
		// checkout; ClosePaymentSession skipped").
		logf(globals.Warn, "checkout_session_open_failed",
			"user_id", userID, "error", sessErr)
	}

	var sessionID string
	if session != nil {
		sessionID = session.SessionID
	}

	checkoutURL, err := buildCheckoutURL(userID, sessionID, planCode)
	if err != nil {
		c.JSON(500, gin.H{"status": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": true, "url": checkoutURL})
}

// buildCheckoutURL composes the LS Checkout URL.
//
// LS URL shape: https://<store-slug>.lemonsqueezy.com/buy/<variant-id>?<query>
//
// We embed user_id via the documented `checkout[custom][user_id]` parameter
// so it survives all the way through to the webhook payload's
// `meta.custom_data.user_id`. That's how the webhook handler links a payment
// back to the greentokey user.
//
// PKG-2 Wave 4 D2: also embeds greentokey_session_id (when non-empty) so
// the inbound webhook can call commerce.ClosePaymentSession. Empty
// sessionID is tolerated — the webhook layer logs + skips the close
// (pre-Wave-4 contract).
//
// PKG-M1 (v0.22, 2026-05-12): when planCode is non-empty, also embeds
// `checkout[custom][type]=plan` + `checkout[custom][plan_code]=<code>`.
// LS webhook lemonsqueezy.go::planCodeFromCustomData detects the
// plan_code presence and routes to auth.RedeemPlanForOrder (atomic
// gtk_user_plan + quota recharge per L23 token-product path).
func buildCheckoutURL(userID int64, sessionID, planCode string) (string, error) {
	slug := viper.GetString("lemonsqueezy.store_slug")
	variantID := viper.GetString("lemonsqueezy.variant_id")
	if slug == "" {
		return "", errors.New("LEMONSQUEEZY_STORE_SLUG not configured")
	}
	if variantID == "" {
		return "", errors.New("LEMONSQUEEZY_VARIANT_ID not configured")
	}
	if !storeSlugRe.MatchString(slug) {
		return "", fmt.Errorf("LEMONSQUEEZY_STORE_SLUG invalid: %q must be a DNS label (a-z, 0-9, hyphen)", slug)
	}
	if !variantIDRe.MatchString(variantID) {
		return "", fmt.Errorf("LEMONSQUEEZY_VARIANT_ID invalid: %q must be a positive integer", variantID)
	}

	params := url.Values{}
	params.Set("checkout[custom][user_id]", fmt.Sprintf("%d", userID))
	if sessionID != "" {
		params.Set("checkout[custom][greentokey_session_id]", sessionID)
	}
	if planCode != "" {
		params.Set("checkout[custom][type]", "plan")
		params.Set("checkout[custom][plan_code]", planCode)
	}

	// Redirect back to our success page after payment. LS appends
	// ?subscription_id=<id> which PaymentSuccess.tsx uses as order_no
	// (token plans use LS subscription_id as their local order_no).
	successBase := viper.GetString("payment.success_url")
	if successBase == "" {
		successBase = "https://api.greentokey.com/payment/success"
	}
	params.Set("checkout[redirect_url]", successBase+"?provider=ls")

	return fmt.Sprintf("https://%s.lemonsqueezy.com/buy/%s?%s",
		slug, variantID, params.Encode()), nil
}
