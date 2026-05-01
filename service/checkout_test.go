package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// ─────────────────────────────────────────────────────────────────────
// LemonSqueezy URL builder tests
// ─────────────────────────────────────────────────────────────────────

func TestBuildLSServiceCheckoutURL_HappyPath(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	t.Cleanup(func() { viper.Set("lemonsqueezy.store_slug", "") })

	svc := &Service{
		Slug:        "xhs-single-post",
		Name:        "单图小红书内容",
		LSVariantID: "1234567",
	}

	got, err := BuildLSServiceCheckoutURL(42, "SVC-AB12CD34", svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, "https://greentokey.lemonsqueezy.com/buy/1234567?") {
		t.Errorf("URL prefix wrong: %s", got)
	}

	// Verify all 3 custom_data keys round-trip via URL parse.
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	cases := map[string]string{
		"checkout[custom][greentokey_order_no]":     "SVC-AB12CD34",
		"checkout[custom][greentokey_user_id]":      "42",
		"checkout[custom][greentokey_service_slug]": "xhs-single-post",
	}
	for k, want := range cases {
		if got := q.Get(k); got != want {
			t.Errorf("custom_data[%s] = %q; want %q", k, got, want)
		}
	}
}

func TestBuildLSServiceCheckoutURL_RejectsNilService(t *testing.T) {
	if _, err := BuildLSServiceCheckoutURL(1, "ord", nil); err == nil {
		t.Error("expected error for nil service")
	}
}

func TestBuildLSServiceCheckoutURL_RejectsEmptyOrderNo(t *testing.T) {
	svc := &Service{LSVariantID: "1"}
	if _, err := BuildLSServiceCheckoutURL(1, "", svc); err == nil {
		t.Error("expected error for empty order_no")
	}
}

func TestBuildLSServiceCheckoutURL_DetectsMissingStoreSlug(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "")
	svc := &Service{LSVariantID: "1"}
	_, err := BuildLSServiceCheckoutURL(1, "ord", svc)
	if !errors.Is(err, ErrCheckoutNotConfigured) {
		t.Errorf("want ErrCheckoutNotConfigured, got %v", err)
	}
}

func TestBuildLSServiceCheckoutURL_DetectsBadStoreSlug(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "evil.com/")
	t.Cleanup(func() { viper.Set("lemonsqueezy.store_slug", "") })

	svc := &Service{LSVariantID: "1"}
	_, err := BuildLSServiceCheckoutURL(1, "ord", svc)
	if err == nil {
		t.Error("expected error for malformed store_slug")
	}
}

func TestBuildLSServiceCheckoutURL_DetectsMissingVariantID(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	t.Cleanup(func() { viper.Set("lemonsqueezy.store_slug", "") })

	svc := &Service{Slug: "test", LSVariantID: ""}
	_, err := BuildLSServiceCheckoutURL(1, "ord", svc)
	if err == nil {
		t.Error("expected error for empty variant_id (per-service unconfigured)")
	}
	if !strings.Contains(err.Error(), "ls_variant_id") {
		t.Errorf("error should mention ls_variant_id: %v", err)
	}
}

func TestBuildLSServiceCheckoutURL_DetectsBadVariantID(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	t.Cleanup(func() { viper.Set("lemonsqueezy.store_slug", "") })

	for _, bad := range []string{"abc", "0", "-1", "1.5", "foo bar"} {
		svc := &Service{Slug: "test", LSVariantID: bad}
		if _, err := BuildLSServiceCheckoutURL(1, "ord", svc); err == nil {
			t.Errorf("variant_id %q should be rejected", bad)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// hupijiao tests
// ─────────────────────────────────────────────────────────────────────

func TestHupijiaoSign_DeterministicAndExcludesHash(t *testing.T) {
	params := map[string]string{
		"appid":          "merchant1",
		"trade_order_id": "SVC-ABCD",
		"total_fee":      "19.00",
		"hash":           "should-be-excluded",
	}
	sig1 := hupijiaoSign(params, "secret")
	sig2 := hupijiaoSign(params, "secret")
	if sig1 != sig2 {
		t.Error("sign must be deterministic")
	}
	// Mutating hash must NOT change signature (we exclude it).
	params["hash"] = "different"
	sig3 := hupijiaoSign(params, "secret")
	if sig3 != sig1 {
		t.Error("hash field must be excluded from signing")
	}
	// Mutating any other field MUST change signature.
	params["total_fee"] = "299.00"
	sig4 := hupijiaoSign(params, "secret")
	if sig4 == sig1 {
		t.Error("non-hash field changes must affect signature")
	}
}

func TestBuildHupijiaoQR_RejectsNilService(t *testing.T) {
	if _, err := BuildHupijiaoQR(1, "ord", nil); err == nil {
		t.Error("expected error for nil service")
	}
}

func TestBuildHupijiaoQR_RejectsZeroPrice(t *testing.T) {
	svc := &Service{Slug: "free-thing", PriceCNYCents: 0}
	_, err := BuildHupijiaoQR(1, "ord", svc)
	if err == nil {
		t.Error("hupijiao requires price > 0")
	}
}

func TestBuildHupijiaoQR_DetectsMissingMerchantConfig(t *testing.T) {
	viper.Set("hupijiao.merchant_id", "")
	viper.Set("hupijiao.merchant_secret", "")
	svc := &Service{Slug: "test", Name: "Test", PriceCNYCents: 1900}
	_, err := BuildHupijiaoQR(1, "ord", svc)
	if !errors.Is(err, ErrCheckoutNotConfigured) {
		t.Errorf("want ErrCheckoutNotConfigured, got %v", err)
	}
}

func TestBuildHupijiaoQR_HappyPath_AgainstFakeServer(t *testing.T) {
	// Stand up a fake hupijiao endpoint that returns canned success.
	var capturedPayload url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedPayload = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errcode":0,"errmsg":"OK","url":"alipays://platformapi/startapp?orderId=12345","url_qrcode":"https://hupijiao/qr/12345.png"}`))
	}))
	defer server.Close()

	viper.Set("hupijiao.merchant_id", "merchant42")
	viper.Set("hupijiao.merchant_secret", "topsecret")
	viper.Set("hupijiao.endpoint", server.URL)
	t.Cleanup(func() {
		viper.Set("hupijiao.merchant_id", "")
		viper.Set("hupijiao.merchant_secret", "")
		viper.Set("hupijiao.endpoint", "")
	})

	// Tighter client timeout for tests so we don't sit 10s on a hang.
	prev := httpClient
	httpClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { httpClient = prev })

	svc := &Service{
		Slug:          "xhs-single-post",
		Name:          "单图小红书内容",
		PriceCNYCents: 1900, // ¥19
	}
	got, err := BuildHupijiaoQR(42, "SVC-AB12CD34", svc)
	if err != nil {
		t.Fatalf("BuildHupijiaoQR: %v", err)
	}
	if got.CodeURL != "alipays://platformapi/startapp?orderId=12345" {
		t.Errorf("CodeURL: %s", got.CodeURL)
	}
	if got.QRPNGURL != "https://hupijiao/qr/12345.png" {
		t.Errorf("QRPNGURL: %s", got.QRPNGURL)
	}
	if got.TradeNo != "SVC-AB12CD34" {
		t.Errorf("TradeNo should echo order_no: %s", got.TradeNo)
	}

	// Verify the payload sent to hupijiao.
	if capturedPayload.Get("appid") != "merchant42" {
		t.Errorf("appid: %s", capturedPayload.Get("appid"))
	}
	if capturedPayload.Get("total_fee") != "19.00" {
		t.Errorf("total_fee: %s (want 19.00 — cents → yuan with 2-decimal)", capturedPayload.Get("total_fee"))
	}
	if capturedPayload.Get("trade_order_id") != "SVC-AB12CD34" {
		t.Errorf("trade_order_id: %s", capturedPayload.Get("trade_order_id"))
	}
	if !strings.Contains(capturedPayload.Get("plugins"), "greentokey_user_id:42") {
		t.Errorf("plugins should embed user_id: %s", capturedPayload.Get("plugins"))
	}
	if capturedPayload.Get("hash") == "" {
		t.Error("hash field should be present")
	}
}

func TestBuildHupijiaoQR_HandlesUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errcode":501,"errmsg":"insufficient balance"}`))
	}))
	defer server.Close()

	viper.Set("hupijiao.merchant_id", "merchant42")
	viper.Set("hupijiao.merchant_secret", "topsecret")
	viper.Set("hupijiao.endpoint", server.URL)
	t.Cleanup(func() {
		viper.Set("hupijiao.merchant_id", "")
		viper.Set("hupijiao.merchant_secret", "")
		viper.Set("hupijiao.endpoint", "")
	})

	prev := httpClient
	httpClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { httpClient = prev })

	svc := &Service{Slug: "test", Name: "Test", PriceCNYCents: 100}
	_, err := BuildHupijiaoQR(1, "ord", svc)
	if err == nil {
		t.Fatal("expected error from upstream errcode != 0")
	}
	if !strings.Contains(err.Error(), "501") || !strings.Contains(err.Error(), "insufficient") {
		t.Errorf("error should surface upstream details: %v", err)
	}
}

func TestRandomNonce_NonEmpty(t *testing.T) {
	if got := randomNonce(); len(got) == 0 {
		t.Error("nonce empty")
	}
}
