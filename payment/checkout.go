package payment

import (
	"chat/auth"
	"chat/utils"
	"errors"
	"fmt"
	"net/url"
	"regexp"

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
// authenticated user via custom_data[user_id]. The frontend can either
// open this URL directly (redirect) or pass it to lemon.js for an overlay.
//
// GET /api/payment/checkout
//
// Response shapes (matches CoAI convention: status flag + payload):
//
//	200 {"status": true,  "url": "https://<slug>.lemonsqueezy.com/buy/<variant>?...&checkout%5Bcustom%5D%5Buser_id%5D=42"}
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

	checkoutURL, err := buildCheckoutURL(userID)
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
func buildCheckoutURL(userID int64) (string, error) {
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

	return fmt.Sprintf("https://%s.lemonsqueezy.com/buy/%s?%s",
		slug, variantID, params.Encode()), nil
}
