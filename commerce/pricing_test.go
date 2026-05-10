package commerce

import "testing"

// TestLookupCostCents_KnownModel verifies the integer math for a model
// the table has — deepseek-chat at a realistic million-token call.
//
// Math walkthrough:
//   in = 1_000_000, out = 500_000
//   µ¢ = (14 * 1_000_000)/1000 + (28 * 500_000)/1000
//      = 14_000 + 14_000
//      = 28_000 µ¢
//   cents = 28_000 / 1000 = 28
func TestLookupCostCents_KnownModel(t *testing.T) {
	got, known := LookupCostCents("deepseek-chat", 1_000_000, 500_000)
	if !known {
		t.Fatalf("expected known=true for deepseek-chat, got false")
	}
	const want int64 = 28
	if got != want {
		t.Errorf("LookupCostCents = %d, want %d", got, want)
	}
}

// TestLookupCostCents_KnownModel_GPT4o cross-checks the more expensive
// tier so a future edit to one entry doesn't silently break the other.
//
// gpt-4o at 100k in + 50k out:
//   µ¢ = (250 * 100_000)/1000 + (1000 * 50_000)/1000
//      = 25_000 + 50_000
//      = 75_000 µ¢
//   cents = 75_000 / 1000 = 75
func TestLookupCostCents_KnownModel_GPT4o(t *testing.T) {
	got, known := LookupCostCents("gpt-4o", 100_000, 50_000)
	if !known {
		t.Fatalf("expected known=true for gpt-4o, got false")
	}
	const want int64 = 75
	if got != want {
		t.Errorf("LookupCostCents = %d, want %d", got, want)
	}
}

// TestLookupCostCents_UnknownModel verifies the (0, false) sentinel for
// models not in the pricing table. Caller (Wave 2 B2 WriteUsageCost) uses
// `known=false` to log a warning + write a 0-cost row preserving call
// audit without billing.
func TestLookupCostCents_UnknownModel(t *testing.T) {
	got, known := LookupCostCents("nonexistent-model-xyz", 1_000_000, 1_000_000)
	if known {
		t.Errorf("expected known=false for unknown model, got true")
	}
	if got != 0 {
		t.Errorf("expected cost=0 for unknown model, got %d", got)
	}
}

// TestLookupCostCents_RoundingDown documents the sub-cent rounding
// behavior. A small call (1000 in + 500 out) on deepseek-chat costs
// 28 micro-cents, which rounds DOWN to 0 cents at storage time. This is
// intentional — a billing system that stored fractional cents would
// drift over millions of calls.
//
// Math:
//   µ¢ = (14*1000)/1000 + (28*500)/1000 = 14 + 14 = 28 µ¢
//   cents = 28 / 1000 = 0  (floor)
func TestLookupCostCents_RoundingDown(t *testing.T) {
	got, known := LookupCostCents("deepseek-chat", 1000, 500)
	if !known {
		t.Fatalf("expected known=true for deepseek-chat, got false")
	}
	if got != 0 {
		t.Errorf("expected sub-cent call to round to 0, got %d (this is the documented floor behavior)", got)
	}
}

// TestLookupProvider_KnownModel checks every provider tier in the table
// so a future edit that mistypes a provider tag is caught by the test
// matrix.
func TestLookupProvider_KnownModel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"deepseek-chat", "deepseek"},
		{"deepseek-reasoner", "deepseek"},
		{"gpt-4o", "openai"},
		{"gpt-4o-mini", "openai"},
		{"claude-3-5-sonnet-20241022", "anthropic"},
		{"claude-3-5-haiku-20241022", "anthropic"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := LookupProvider(tc.model); got != tc.want {
				t.Errorf("LookupProvider(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

// TestLookupProvider_UnknownModel returns empty string for unknown
// models so callers can write NULL into UsageCostEntry.Provider without
// a second branch.
func TestLookupProvider_UnknownModel(t *testing.T) {
	if got := LookupProvider("nonexistent-model-xyz"); got != "" {
		t.Errorf("expected empty provider for unknown model, got %q", got)
	}
}
