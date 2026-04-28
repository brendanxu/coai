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

	got, err := buildCheckoutURL(42)
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

	if _, err := buildCheckoutURL(42); err == nil {
		t.Fatal("missing store_slug should error")
	}
}

func TestBuildCheckoutURL_MissingVariant(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	viper.Set("lemonsqueezy.variant_id", "")
	t.Cleanup(func() {
		viper.Set("lemonsqueezy.store_slug", "")
	})

	if _, err := buildCheckoutURL(42); err == nil {
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

	url1, _ := buildCheckoutURL(1)
	url2, _ := buildCheckoutURL(2)
	if url1 == url2 {
		t.Fatal("URLs for different users must differ on user_id")
	}
}
