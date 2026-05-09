package utils

import (
	"chat/globals"
	"testing"
)

// fakeCharge implements the Charge interface for tokenizer tests without
// pulling in channel.Charge (avoids an import cycle: channel imports utils).
// All rate fields default to 0 and the Get* methods either return them
// directly or fall back to GetInput so the test can probe both paths.
type fakeCharge struct {
	billingType  string
	input        float32
	output       float32
	cacheRead    float32
	cacheWrite5m float32
	cacheWrite1h float32
}

func (c *fakeCharge) GetType() string                  { return c.billingType }
func (c *fakeCharge) GetModels() []string              { return nil }
func (c *fakeCharge) GetInput() float32                { return c.input }
func (c *fakeCharge) GetOutput() float32               { return c.output }
func (c *fakeCharge) SupportAnonymous() bool           { return false }
func (c *fakeCharge) IsBilling() bool                  { return c.billingType != globals.NonBilling }
func (c *fakeCharge) IsBillingType(t string) bool      { return c.billingType == t }
func (c *fakeCharge) GetLimit() float32                { return 0 }
func (c *fakeCharge) GetCacheRead() float32 {
	if c.cacheRead <= 0 {
		return c.input
	}
	return c.cacheRead
}
func (c *fakeCharge) GetCacheWrite5m() float32 {
	if c.cacheWrite5m <= 0 {
		return c.input * 1.25
	}
	return c.cacheWrite5m
}
func (c *fakeCharge) GetCacheWrite1h() float32 {
	if c.cacheWrite1h <= 0 {
		return c.input * 2.0
	}
	return c.cacheWrite1h
}

// TestCountUpstreamQuota_NilSafe — nil usage / nil charge → 0, no panic.
func TestCountUpstreamQuota_NilSafe(t *testing.T) {
	if got := CountUpstreamQuota(nil, nil); got != 0 {
		t.Errorf("nil/nil: got %f want 0", got)
	}
	c := &fakeCharge{billingType: globals.TokenBilling, input: 0.04, output: 0.20}
	if got := CountUpstreamQuota(c, nil); got != 0 {
		t.Errorf("nil usage: got %f want 0", got)
	}
}

// TestCountUpstreamQuota_NonBilling — non-billing tier returns 0 regardless
// of usage (anonymous tier on a free model).
func TestCountUpstreamQuota_NonBilling(t *testing.T) {
	c := &fakeCharge{billingType: globals.NonBilling, input: 0.04}
	u := &globals.UpstreamUsage{InputTokens: 1000, OutputTokens: 500}
	if got := CountUpstreamQuota(c, u); got != 0 {
		t.Errorf("non-billing: got %f want 0", got)
	}
}

// TestCountUpstreamQuota_TimesBilling — flat per-call fee, ignores token
// counts entirely.
func TestCountUpstreamQuota_TimesBilling(t *testing.T) {
	c := &fakeCharge{billingType: globals.TimesBilling, output: 0.5}
	u := &globals.UpstreamUsage{InputTokens: 100000, OutputTokens: 100000}
	if got := CountUpstreamQuota(c, u); got != 0.5 {
		t.Errorf("times: got %f want 0.5", got)
	}
}

// TestCountUpstreamQuota_PureInputOutput — no cache fields, behaves like
// the legacy CountInputQuota+CountOutputToken sum but priced off the
// upstream's reported tokens (not tiktoken).
func TestCountUpstreamQuota_PureInputOutput(t *testing.T) {
	c := &fakeCharge{
		billingType: globals.TokenBilling,
		input:       0.04, // ¥/1k
		output:      0.20,
	}
	u := &globals.UpstreamUsage{InputTokens: 1500, OutputTokens: 800}
	want := float32(1.5*0.04 + 0.8*0.20) // 0.06 + 0.16 = 0.22
	got := CountUpstreamQuota(c, u)
	if !floatNear(got, want, 1e-5) {
		t.Errorf("pure: got %f want %f", got, want)
	}
}

// TestCountUpstreamQuota_CacheRead — cache_read priced via fall-through to
// input rate when CacheRead is unset (operator hasn't passed the discount
// to the customer yet). Result: customer pays full rate, we don't lose.
func TestCountUpstreamQuota_CacheReadFallsBackToInput(t *testing.T) {
	c := &fakeCharge{
		billingType: globals.TokenBilling,
		input:       0.04,
		output:      0.20,
		// cacheRead unset
	}
	u := &globals.UpstreamUsage{
		InputTokens:     500,
		OutputTokens:    200,
		CacheReadTokens: 10000,
	}
	// 0.5*0.04 + 0.2*0.20 + 10*0.04 (cache_read fall-through) = 0.02+0.04+0.40 = 0.46
	want := float32(0.5*0.04 + 0.2*0.20 + 10*0.04)
	got := CountUpstreamQuota(c, u)
	if !floatNear(got, want, 1e-5) {
		t.Errorf("cache_read fallback: got %f want %f", got, want)
	}
}

// TestCountUpstreamQuota_CacheReadExplicit — operator opts in to passing
// the cache discount on to the customer.
func TestCountUpstreamQuota_CacheReadExplicit(t *testing.T) {
	c := &fakeCharge{
		billingType: globals.TokenBilling,
		input:       0.04,
		output:      0.20,
		cacheRead:   0.005, // ~13% of input — passes Anthropic's 0.1× upstream + 1.3× markup
	}
	u := &globals.UpstreamUsage{
		InputTokens:     500,
		OutputTokens:    200,
		CacheReadTokens: 10000,
	}
	// 0.5*0.04 + 0.2*0.20 + 10*0.005 = 0.02+0.04+0.05 = 0.11
	want := float32(0.5*0.04 + 0.2*0.20 + 10*0.005)
	got := CountUpstreamQuota(c, u)
	if !floatNear(got, want, 1e-5) {
		t.Errorf("cache_read explicit: got %f want %f", got, want)
	}
}

// TestCountUpstreamQuota_CacheWrite5m — cache_write defaults to input ×
// 1.25 to mirror Anthropic's upstream surcharge.
func TestCountUpstreamQuota_CacheWrite5m(t *testing.T) {
	c := &fakeCharge{
		billingType: globals.TokenBilling,
		input:       0.04,
		output:      0.20,
	}
	u := &globals.UpstreamUsage{
		InputTokens:      500,
		OutputTokens:     200,
		CacheWriteTokens: 1000,
		CacheTTL:         "5m",
	}
	// 0.5*0.04 + 0.2*0.20 + 1*(0.04*1.25) = 0.02+0.04+0.05 = 0.11
	want := float32(0.5*0.04 + 0.2*0.20 + 1.0*0.05)
	got := CountUpstreamQuota(c, u)
	if !floatNear(got, want, 1e-5) {
		t.Errorf("cache_write_5m: got %f want %f", got, want)
	}
}

// TestCountUpstreamQuota_CacheWrite1h — 1h TTL routes to the 1h rate
// (default 2.0× input).
func TestCountUpstreamQuota_CacheWrite1h(t *testing.T) {
	c := &fakeCharge{
		billingType: globals.TokenBilling,
		input:       0.04,
		output:      0.20,
	}
	u := &globals.UpstreamUsage{
		InputTokens:      500,
		OutputTokens:     200,
		CacheWriteTokens: 1000,
		CacheTTL:         "1h",
	}
	// 0.5*0.04 + 0.2*0.20 + 1*(0.04*2.0) = 0.02+0.04+0.08 = 0.14
	want := float32(0.5*0.04 + 0.2*0.20 + 1.0*0.08)
	got := CountUpstreamQuota(c, u)
	if !floatNear(got, want, 1e-5) {
		t.Errorf("cache_write_1h: got %f want %f", got, want)
	}
}

// TestCountUpstreamQuota_NeverLoseMoney — invariant test. No matter how
// the customer splits the 4 token classes, customer-charge ≥ upstream-cost
// when every customer rate ≥ upstream rate × 1.0. Probes 5 mixes from
// "all output" to "all cache_read".
func TestCountUpstreamQuota_NeverLoseMoney(t *testing.T) {
	// Customer-side rates are upstream × markup 1.30.
	const upstreamInputUSD = 3.0     // Anthropic Sonnet 4.5
	const upstreamOutputUSD = 15.0
	const upstreamCacheReadUSD = 0.30 // 0.1× input
	const upstreamCacheWrite5mUSD = 3.75 // 1.25× input
	const markup = 1.30

	c := &fakeCharge{
		billingType:  globals.TokenBilling,
		input:        upstreamInputUSD * markup / 1000,    // per token, in micro-USD-ish units
		output:       upstreamOutputUSD * markup / 1000,
		cacheRead:    upstreamCacheReadUSD * markup / 1000,
		cacheWrite5m: upstreamCacheWrite5mUSD * markup / 1000,
	}

	cases := []struct {
		name  string
		usage globals.UpstreamUsage
	}{
		{"input-heavy", globals.UpstreamUsage{InputTokens: 10_000, OutputTokens: 1_000}},
		{"output-heavy", globals.UpstreamUsage{InputTokens: 1_000, OutputTokens: 10_000}},
		{"cache-read-heavy", globals.UpstreamUsage{InputTokens: 500, OutputTokens: 1_000, CacheReadTokens: 50_000}},
		{"cache-write-heavy", globals.UpstreamUsage{InputTokens: 500, OutputTokens: 1_000, CacheWriteTokens: 5_000, CacheTTL: "5m"}},
		{"mixed", globals.UpstreamUsage{InputTokens: 1_000, OutputTokens: 5_000, CacheReadTokens: 100_000, CacheWriteTokens: 500, CacheTTL: "5m"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			customerCharge := CountUpstreamQuota(c, &tc.usage)
			// Compute upstream cost on the same shape using the same
			// helper but with a baseline-rate Charge (multiplier = 1.0).
			baseline := &fakeCharge{
				billingType:  globals.TokenBilling,
				input:        upstreamInputUSD / 1000,
				output:       upstreamOutputUSD / 1000,
				cacheRead:    upstreamCacheReadUSD / 1000,
				cacheWrite5m: upstreamCacheWrite5mUSD / 1000,
			}
			upstreamCost := CountUpstreamQuota(baseline, &tc.usage)
			if customerCharge < upstreamCost {
				t.Errorf("we lose money: customer=%f upstream=%f (mix %s)",
					customerCharge, upstreamCost, tc.name)
			}
			// And we should be earning ~30% margin (within float tolerance).
			expectedRatio := float32(markup)
			actualRatio := customerCharge / upstreamCost
			if !floatNear(actualRatio, expectedRatio, 1e-4) {
				t.Errorf("margin drift: got %f want %f (mix %s)",
					actualRatio, expectedRatio, tc.name)
			}
		})
	}
}

func floatNear(a, b, tol float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}
