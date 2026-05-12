// Tests for hupijiao_helpers.go (sign + nonce) and the pure-Go portion of
// hupijiao_checkout.go (config validation + attach format). The end-to-end
// HTTP path that actually POSTs to xunhupay.com is left for staging — too
// much surface to mock cleanly without a vendored response fixture, and
// the wire format is locked in by service/checkout_test.go's tests for
// the sibling BuildHupijiaoQR.

package payment

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// hupijiaoSign is deterministic over a given (params, secret) pair —
// same input must always produce the same MD5 hex output. Wire-compatible
// with service/checkout.go::hupijiaoSign (verified by manually comparing
// against the same fixture).
func TestHupijiaoSign_Deterministic(t *testing.T) {
	params := map[string]string{
		"appid":          "test-merchant",
		"trade_order_id": "test-order-123",
		"total_fee":      "99.00",
		"title":          "Token 套餐",
		"nonce_str":      "abcdef0123456789",
	}
	secret := "test-secret"

	first := hupijiaoSign(params, secret)
	second := hupijiaoSign(params, secret)
	if first != second {
		t.Fatalf("hupijiaoSign not deterministic: %q vs %q", first, second)
	}
	if len(first) != 32 {
		t.Errorf("hupijiaoSign should be 32-char MD5 hex, got %d chars: %q",
			len(first), first)
	}
}

// `hash` field excluded from signing per hupijiao spec — including it would
// create a chicken-and-egg problem (sign over the hash to compute the hash).
func TestHupijiaoSign_ExcludesHashField(t *testing.T) {
	base := map[string]string{
		"appid":     "x",
		"nonce_str": "y",
	}
	withHash := map[string]string{
		"appid":     "x",
		"nonce_str": "y",
		"hash":      "this-should-be-ignored",
	}
	if hupijiaoSign(base, "s") != hupijiaoSign(withHash, "s") {
		t.Fatal("hash field must be excluded from signing")
	}
}

// Different secrets must produce different signatures — otherwise the secret
// has zero contribution and the gateway would accept any signed payload.
func TestHupijiaoSign_SecretChangesHash(t *testing.T) {
	params := map[string]string{"appid": "x"}
	if hupijiaoSign(params, "s1") == hupijiaoSign(params, "s2") {
		t.Fatal("different secrets must produce different signatures")
	}
}

// randomNonce should produce hex output of expected length and (with
// extremely high probability) different output across calls.
func TestRandomNonce_Format(t *testing.T) {
	a := randomNonce()
	b := randomNonce()
	if len(a) != 16 {
		t.Errorf("randomNonce length got %d want 16: %q", len(a), a)
	}
	if a == b {
		t.Errorf("two consecutive nonces collided (statistically near-impossible): %q", a)
	}
	for _, c := range a {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("randomNonce contains non-hex char %q in %q", c, a)
		}
	}
}

// buildHupijiaoCheckoutForPlan with no merchant_id/secret in viper must
// surface ErrHupijiaoNotConfigured so the API handler can return 500 with
// a clear operator-facing message instead of trying to POST to xunhupay.
func TestBuildHupijiaoCheckoutForPlan_MissingConfig(t *testing.T) {
	viper.Set("hupijiao.merchant_id", "")
	viper.Set("hupijiao.merchant_secret", "")
	t.Cleanup(func() {
		viper.Set("hupijiao.merchant_id", "")
		viper.Set("hupijiao.merchant_secret", "")
	})

	plan := &plans.Plan{
		Code:       "token-99",
		Name:       "Token 套餐",
		PriceCents: 9900,
	}
	_, err := buildHupijiaoCheckoutForPlan(42, "test-order", plan, "sess-x")
	if err == nil {
		t.Fatal("missing merchant_id/secret should surface ErrHupijiaoNotConfigured")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error should mention 'not configured', got: %v", err)
	}
}

// Nil plan must not crash — return error instead.
func TestBuildHupijiaoCheckoutForPlan_NilPlan(t *testing.T) {
	_, err := buildHupijiaoCheckoutForPlan(42, "test-order", nil, "sess-x")
	if err == nil {
		t.Fatal("nil plan should error, not crash")
	}
}

// Zero or negative price must be rejected — hupijiao gateway rejects
// zero-amount payments with an opaque code; better to fail fast.
func TestBuildHupijiaoCheckoutForPlan_ZeroPrice(t *testing.T) {
	viper.Set("hupijiao.merchant_id", "x")
	viper.Set("hupijiao.merchant_secret", "y")
	t.Cleanup(func() {
		viper.Set("hupijiao.merchant_id", "")
		viper.Set("hupijiao.merchant_secret", "")
	})

	plan := &plans.Plan{Code: "free", Name: "Free", PriceCents: 0}
	_, err := buildHupijiaoCheckoutForPlan(42, "test-order", plan, "")
	if err == nil {
		t.Fatal("zero-price plan should error before hupijiao POST")
	}
	if !strings.Contains(err.Error(), "must be > 0") {
		t.Errorf("error should mention price > 0, got: %v", err)
	}
}

// LookupActivePlan integration: a manually-inserted token-99 row must be
// findable by code, with all checkout-relevant fields populated. Note
// that seedTokenPlans skips the SQLite engine (collision-avoidance for
// other packages' tests that hardcode id=1) so the test inserts its own
// row matching the seed's shape. Production exercises seedTokenPlans
// during MySQL boot and via the docker-mysql migration drill.
func TestLookupActivePlan_TokenPlanSeeded(t *testing.T) {
	db := newPlanCheckoutTestDB(t)

	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan (id, code, name, type, product_type, billing_mode,
		                      price_cents, duration_days, quota_grant, quota_config, is_active)
		VALUES (5099, 'token-99', 'Token 套餐 ¥99/月', 'subscription', 'token',
		        'subscription', 9900, 30, 5000, '{"quota":5000,"reset":"monthly"}', 1)
	`); err != nil {
		t.Fatalf("insert token-99 fixture: %v", err)
	}

	plan, err := plans.LookupActivePlan(db, "token-99")
	if err != nil {
		t.Fatalf("LookupActivePlan(token-99) failed: %v", err)
	}
	if plan.Code != "token-99" {
		t.Errorf("plan.Code got %q want token-99", plan.Code)
	}
	if plan.PriceCents != 9900 {
		t.Errorf("plan.PriceCents got %d want 9900", plan.PriceCents)
	}
	if plan.ProductType != "token" {
		t.Errorf("plan.ProductType got %q want token", plan.ProductType)
	}
	if !plan.IsActive {
		t.Error("token-99 fixture should be is_active=true")
	}
}

// Unknown plan code surfaces ErrPlanNotFound so the API handler can
// return 400 with a clean message instead of leaking the raw SQL error.
func TestLookupActivePlan_UnknownCode(t *testing.T) {
	db := newPlanCheckoutTestDB(t)

	_, err := plans.LookupActivePlan(db, "does-not-exist")
	if err == nil {
		t.Fatal("unknown plan code should error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// newPlanCheckoutTestDB creates a fresh SQLite-backed engine with the
// plans migration applied. Note: seedTokenPlans is gated to MySQL only
// (see plans/migration.go), so tests that need a token-99 row must
// insert it themselves.
func newPlanCheckoutTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTokenTestEngine(t) // runs plans.Migrate (seedTokenPlans skipped on SQLite)
	return db
}
