// Checkout-URL builders for Layer 3 service orders.
//
// Two providers in v0.10a:
//
//   LemonSqueezy (overseas card payments)
//     Hosted-checkout URL pattern, identical to payment/checkout.go
//     but per-service variant (Service.LSVariantID) instead of a global
//     LEMONSQUEEZY_VARIANT_ID. Embeds the order_no + user_id +
//     service_slug in custom_data so the webhook handler can match.
//
//   hupijiao 虎皮椒 (mainland alipay native QR)
//     POSTs to xunhupay.com/payment/do.html with HMAC-MD5 signed
//     parameters; response yields a code_url (alipay native deep-link)
//     and url_qrcode (PNG URL). We return both so the frontend can
//     render either depending on whether the customer is on mobile
//     (deep-link) or desktop (QR scan).
//
// For one_time + per_use service orders, both builders are
// fire-and-forget URL composers — the customer pays at the URL, the
// webhook flips order status, the agent runtime kicks in.
//
// For monthly service orders, ls_variant_id needs to be configured
// per-service as a *recurring* variant in LS dashboard, and the order
// row's subscription_id gets populated by the webhook handler when the
// subscription_created event lands. hupijiao monthly is not supported
// yet (hupijiao = one-shot alipay only).

package service

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// LSStoreSlugRe and LSVariantIDRe mirror payment/checkout.go validators
// (Codex P2 hardening). They reject operator misconfigurations like
// `evil.com/` or `foo@bar` that would silently route checkouts off-domain.
var (
	lsStoreSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	lsVariantIDRe = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
)

// ErrCheckoutNotConfigured is returned when the payment provider's
// required env / viper config is missing. Surfaces as 500 to the
// frontend with a clear message — operator must fix infrastructure.
var ErrCheckoutNotConfigured = errors.New("service: payment provider not configured")

// BuildLSServiceCheckoutURL composes a LemonSqueezy hosted-checkout URL
// for a single service order. The URL embeds order_no + user_id +
// service_slug as custom_data so the webhook handler can match the
// inbound order_created event to the gtk_service_order row.
//
// PKG-2 Wave 4 D3: also embeds greentokey_session_id when non-empty so
// the inbound webhook (Wave 3 C2 / dispatch_service.go) can call
// commerce.ClosePaymentSession by session_id (CR7 contract).
//
// Validation: per-service ls_variant_id (svc.LSVariantID) must be
// non-empty and pass the integer regex. svc.Status MUST be 'active'
// (caller's job to enforce — we just compose). Empty store_slug
// surfaces as ErrCheckoutNotConfigured because that's an operator
// misconfig, not a per-service issue.
func BuildLSServiceCheckoutURL(coaiUserID int64, orderNo string, svc *Service, sessionID string) (string, error) {
	if svc == nil {
		return "", errors.New("service: BuildLSServiceCheckoutURL requires non-nil service")
	}
	if orderNo == "" {
		return "", errors.New("service: BuildLSServiceCheckoutURL requires non-empty order_no")
	}

	slug := viper.GetString("lemonsqueezy.store_slug")
	if slug == "" {
		return "", fmt.Errorf("%w: lemonsqueezy.store_slug missing", ErrCheckoutNotConfigured)
	}
	if !lsStoreSlugRe.MatchString(slug) {
		return "", fmt.Errorf("service: lemonsqueezy.store_slug invalid (%q must be a DNS label)", slug)
	}
	if svc.LSVariantID == "" {
		return "", fmt.Errorf("service: %s has no ls_variant_id configured (set it in gtk_service.ls_variant_id before going active)", svc.Slug)
	}
	if !lsVariantIDRe.MatchString(svc.LSVariantID) {
		return "", fmt.Errorf("service: %s ls_variant_id %q invalid (must be a positive integer)", svc.Slug, svc.LSVariantID)
	}

	params := url.Values{}
	// Embed greentokey_* keys in custom_data. Webhook handler dispatches
	// on greentokey_order_no presence (payment/lemonsqueezy.go).
	params.Set("checkout[custom][greentokey_order_no]", orderNo)
	params.Set("checkout[custom][greentokey_user_id]", fmt.Sprintf("%d", coaiUserID))
	params.Set("checkout[custom][greentokey_service_slug]", svc.Slug)
	if sessionID != "" {
		// Wave 4 D3 wiring → Wave 3 C2 ClosePaymentSession.
		params.Set("checkout[custom][greentokey_session_id]", sessionID)
	}

	return fmt.Sprintf("https://%s.lemonsqueezy.com/buy/%s?%s",
		slug, svc.LSVariantID, params.Encode()), nil
}

// HupijiaoQR is the response shape from BuildHupijiaoQR.
//
// CodeURL is the alipay native deep-link (alipays://...) — useful for
// mobile customers who can tap-to-pay.
// QRPNGURL is a PNG image of the QR code rendered server-side by
// hupijiao — useful for desktop customers who scan with their phone.
// Both point at the same underlying alipay tx; frontend picks one
// based on UA detection.
type HupijiaoQR struct {
	CodeURL  string `json:"code_url"`
	QRPNGURL string `json:"qr_png_url"`
	TradeNo  string `json:"hupijiao_trade_no"`
}

// hupijiaoEndpoint is the hupijiao API base URL. Defaults to xunhupay.com
// per their docs; can be overridden via viper hupijiao.endpoint for
// staging or test endpoints.
const defaultHupijiaoEndpoint = "https://api.xunhupay.com/payment/do.html"

// BuildHupijiaoQR sends a payment request to hupijiao and returns the
// alipay-native code URL + QR-image URL. Uses HMAC-MD5 signing per
// hupijiao spec (sort params alphabetically, concat as k=v&k=v, append
// merchant_secret, MD5).
//
// Preconditions:
//   - hupijiao.merchant_id and hupijiao.merchant_secret in viper config
//   - svc.Status='active' (caller's responsibility)
//   - svc.PriceCNYCents > 0 (hupijiao rejects zero-amount payments)
//
// PKG-2 Wave 4 D3: when sessionID is non-empty, it's appended to the
// `plugins` field (hupijiao's free-form custom-data carrier) so the
// inbound callback can match by session_id. hupijiao docs only document
// `plugins` as a string passed back via webhook attribute — multiple
// key:value pairs separated by commas is the established convention.
//
// Returns HupijiaoQR with TradeNo so the caller can persist it onto
// gtk_service_order.hupijiao_trade_no for webhook reconciliation.
func BuildHupijiaoQR(coaiUserID int64, orderNo string, svc *Service, sessionID string) (*HupijiaoQR, error) {
	if svc == nil {
		return nil, errors.New("service: BuildHupijiaoQR requires non-nil service")
	}
	if orderNo == "" {
		return nil, errors.New("service: BuildHupijiaoQR requires non-empty order_no")
	}
	if svc.PriceCNYCents <= 0 {
		return nil, fmt.Errorf("service: %s price must be > 0 for hupijiao (got %d cents)", svc.Slug, svc.PriceCNYCents)
	}

	merchantID := viper.GetString("hupijiao.merchant_id")
	merchantSecret := viper.GetString("hupijiao.merchant_secret")
	if merchantID == "" || merchantSecret == "" {
		return nil, fmt.Errorf("%w: hupijiao.merchant_id or merchant_secret missing", ErrCheckoutNotConfigured)
	}
	endpoint := viper.GetString("hupijiao.endpoint")
	if endpoint == "" {
		endpoint = defaultHupijiaoEndpoint
	}

	// hupijiao expects yuan as a decimal string ("19.00"), not cents.
	yuan := fmt.Sprintf("%d.%02d", svc.PriceCNYCents/100, svc.PriceCNYCents%100)

	// trade_no is what we send to hupijiao; they echo it back in the
	// webhook. We use the order_no for traceability (same value lives
	// on both sides).
	tradeNo := orderNo

	notifyURL := viper.GetString("hupijiao.callback_url")
	if notifyURL == "" {
		notifyURL = "https://api.greentokey.com/api/gtk/v1/service/hupijiao-callback"
	}

	// `plugins` carries our custom data through hupijiao's webhook envelope.
	// Append greentokey_session_id when supplied (Wave 4 D3 wiring).
	plugins := fmt.Sprintf("greentokey_user_id:%d", coaiUserID)
	if sessionID != "" {
		plugins += fmt.Sprintf(",greentokey_session_id:%s", sessionID)
	}

	params := map[string]string{
		"version":        "1.1",
		"appid":          merchantID,
		"trade_order_id": tradeNo,
		"total_fee":      yuan,
		"title":          svc.Name,
		"time":           fmt.Sprintf("%d", time.Now().Unix()),
		"notify_url":     notifyURL,
		"return_url":     "https://api.greentokey.com/order/" + orderNo,
		"nonce_str":      randomNonce(),
		"type":           "WAP", // alipay native — works for QR + deep-link
		"plugins":        plugins,
	}
	params["hash"] = hupijiaoSign(params, merchantSecret)

	resp, err := postForm(endpoint, params)
	if err != nil {
		return nil, fmt.Errorf("service: hupijiao POST failed: %w", err)
	}

	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("service: hupijiao rejected payment (code=%d, msg=%s)", resp.ErrCode, resp.ErrMsg)
	}

	return &HupijiaoQR{
		CodeURL:  resp.URL,
		QRPNGURL: resp.URLQRCode,
		TradeNo:  tradeNo,
	}, nil
}

// hupijiaoResponse is the shape we expect from xunhupay.com/payment/do.html.
// Fields outside what we use are intentionally elided.
type hupijiaoResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	URL       string `json:"url"`        // alipay native deep-link
	URLQRCode string `json:"url_qrcode"` // PNG image URL
}

// hupijiaoSign computes HMAC-MD5 over alphabetically-sorted k=v pairs
// joined by &, with merchant_secret appended, per hupijiao docs.
//
// Why MD5: hupijiao spec mandates it. Yes, MD5 is broken for
// collision resistance — but for HMAC-style signed payloads with a
// shared secret, the attack surface is preimage resistance which MD5
// still survives. We don't get to pick the algo here, hupijiao does.
func hupijiaoSign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "hash" {
			continue // never sign over the hash field itself
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	b.WriteString(secret)

	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// randomNonce returns 16 hex chars, derived from time.Now().UnixNano()
// and a fresh sum. Cryptographic strength isn't required — hupijiao
// only needs a short-lived per-request token.
func randomNonce() string {
	b := time.Now().UnixNano()
	sum := md5.Sum([]byte(fmt.Sprintf("%d", b)))
	return hex.EncodeToString(sum[:8])
}

// postForm POSTs application/x-www-form-urlencoded to the given URL and
// decodes the JSON response into hupijiaoResponse.
//
// httpClient is package-private + has a 10s timeout — sane default,
// but tests can swap it out via the testHTTPClient hook.
var httpClient = &http.Client{Timeout: 10 * time.Second}

func postForm(endpoint string, params map[string]string) (*hupijiaoResponse, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	resp, err := httpClient.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var r hupijiaoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse response: %w (raw: %s)", err, truncate(body, 200))
	}
	return &r, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
