package payment

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestBuildCheckoutURL_Valid(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
		viper.Set("lemonsqueezy.variant_id", "")
	})

	got, err := buildCheckoutURL(42, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Spot-check the structure rather than the exact string —
	// url.Values.Encode() may key-order differently across Go versions.
	wantHostPath := "https://greentokey.lemonsqueezy.com/buy/999999"
	if !strings.HasPrefix(got, wantHostPath+"?") {
		t.Fatalf("URL host/path wrong: got=%q want prefix=%q", got, wantHostPath+"?")
	}
	// Ensure user_id custom data is present and URL-encoded.
	if !strings.Contains(got, "checkout%5Bcustom%5D%5Buser_id%5D=42") {
		t.Fatalf("user_id custom_data missing from URL: %q", got)
	}
}

func TestBuildCheckoutURL_MissingSlug(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "")
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.variant_id", "")
	})

	if _, err := buildCheckoutURL(42, ""); err == nil {
		t.Fatal("missing store_slug should error")
	}
}

func TestBuildCheckoutURL_MissingVariant(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
	})

	if _, err := buildCheckoutURL(42, ""); err == nil {
		t.Fatal("missing variant_id should error")
	}
}

func TestBuildCheckoutURL_DifferentUsersGetDifferentURLs(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
		viper.Set("lemonsqueezy.variant_id", "")
	})

	url1, _ := buildCheckoutURL(1, "")
	url2, _ := buildCheckoutURL(2, "")
	if url1 == url2 {
		t.Fatal("URLs for different users must differ on user_id")
	}
}

// PKG-2 Wave 4 D2: when a session_id is supplied, it must round-trip into
// LS custom_data so the inbound webhook (Wave 3 C1 / dispatch_token) can
// match by session_id and call commerce.ClosePaymentSession.
func TestBuildCheckoutURL_EmbedsSessionID(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
		viper.Set("lemonsqueezy.variant_id", "")
	})

	got, err := buildCheckoutURL(42, "sess-abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// custom_data key URL-encodes to checkout%5Bcustom%5D%5Bgreentokey_session_id%5D=
	if !strings.Contains(got, "checkout%5Bcustom%5D%5Bgreentokey_session_id%5D=sess-abc-123") {
		t.Errorf("session_id custom_data missing or wrong: %q", got)
	}
}

// Empty sessionID (Wave 3 fallback path) must NOT add the
// greentokey_session_id key — the dispatcher's "no session_id" branch
// (logf info + skip) relies on absence.
func TestBuildCheckoutURL_EmptySessionIDOmitsKey(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
		viper.Set("lemonsqueezy.variant_id", "")
	})

	got, _ := buildCheckoutURL(42, "")
	if strings.Contains(got, "greentokey_session_id") {
		t.Errorf("empty sessionID must omit the key entirely: %q", got)
	}
}
