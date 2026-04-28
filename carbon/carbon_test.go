package carbon

import (
	"math"
	"testing"
)

// ----- TierFor -------------------------------------------------------------

func TestTierFor(t *testing.T) {
	cases := []struct {
		name string
		g    float64
		want Tier
	}{
		{"zero is low", 0, TierLow},
		{"just below mid is low", 0.499, TierLow},
		{"exactly 0.5 is mid", 0.5, TierMid},
		{"middle of mid is mid", 1.0, TierMid},
		{"just below high is mid", 1.999, TierMid},
		{"exactly 2.0 is high", 2.0, TierHigh},
		{"large is high", 100.0, TierHigh},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TierFor(c.g)
			if got != c.want {
				t.Errorf("TierFor(%v) = %v, want %v", c.g, got, c.want)
			}
		})
	}
}

// ----- LookupCoefficient ---------------------------------------------------

func TestLookupCoefficient_Hit(t *testing.T) {
	g, ver, ok := LookupCoefficient("gpt-4o-mini", "default")
	if !ok {
		t.Fatalf("expected hit for gpt-4o-mini@default, got ok=false")
	}
	if g <= 0 || g > 1 {
		t.Errorf("gpt-4o-mini coefficient looks wrong: %v (expected small positive number)", g)
	}
	if ver == "" {
		t.Errorf("expected non-empty version, got empty")
	}
}

func TestLookupCoefficient_Miss(t *testing.T) {
	_, _, ok := LookupCoefficient("nonexistent-model-xyz-99", "default")
	if ok {
		t.Errorf("expected miss for unknown model, got ok=true")
	}
}

func TestLookupCoefficient_EmptyRegionUsesDefault(t *testing.T) {
	g1, _, ok1 := LookupCoefficient("gpt-4o", "")
	g2, _, ok2 := LookupCoefficient("gpt-4o", "default")
	if !ok1 || !ok2 {
		t.Fatalf("expected both lookups to hit, got ok1=%v ok2=%v", ok1, ok2)
	}
	if g1 != g2 {
		t.Errorf("empty region should fall back to default; g1=%v g2=%v", g1, g2)
	}
}

func TestLookupCoefficient_UnknownRegionFallsBackToDefault(t *testing.T) {
	g1, _, ok1 := LookupCoefficient("gpt-4o", "made-up-region")
	g2, _, ok2 := LookupCoefficient("gpt-4o", "default")
	if !ok1 || !ok2 {
		t.Fatalf("expected both lookups to hit, got ok1=%v ok2=%v", ok1, ok2)
	}
	if g1 != g2 {
		t.Errorf("unknown region should fall back to default; g1=%v g2=%v", g1, g2)
	}
}

// ----- EstimateCO2 ---------------------------------------------------------

func TestEstimateCO2_HappyPath(t *testing.T) {
	tokens := 1000
	g, ver, ok := EstimateCO2(tokens, "gpt-4o-mini", "default")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	coef, _, _ := LookupCoefficient("gpt-4o-mini", "default")
	expected := float64(tokens) / 1000.0 * coef
	if math.Abs(g-expected) > 1e-9 {
		t.Errorf("got %v, want %v", g, expected)
	}
	if ver == "" {
		t.Errorf("expected version, got empty")
	}
}

func TestEstimateCO2_FractionalTokens(t *testing.T) {
	// 500 tokens = exactly half the per-1k coefficient
	tokens := 500
	g, _, ok := EstimateCO2(tokens, "gpt-4o-mini", "default")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	coef, _, _ := LookupCoefficient("gpt-4o-mini", "default")
	expected := coef / 2.0
	if math.Abs(g-expected) > 1e-9 {
		t.Errorf("500-token estimate: got %v, want %v", g, expected)
	}
}

func TestEstimateCO2_CoefficientGap(t *testing.T) {
	_, _, ok := EstimateCO2(1000, "nonexistent-model-xyz-99", "default")
	if ok {
		t.Errorf("expected ok=false for unknown model")
	}
}

func TestEstimateCO2_ZeroTokens(t *testing.T) {
	g, _, ok := EstimateCO2(0, "gpt-4o-mini", "default")
	if !ok {
		t.Fatalf("expected ok=true even at 0 tokens")
	}
	if g != 0 {
		t.Errorf("0 tokens should be 0g, got %v", g)
	}
}

// ----- MaybeRouteEco -------------------------------------------------------

func TestMaybeRouteEco_GPT4ToMini(t *testing.T) {
	r := MaybeRouteEco("gpt-4")
	if !r.DidRoute {
		t.Errorf("expected DidRoute=true for gpt-4")
	}
	if r.NewModel != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %q", r.NewModel)
	}
	if r.OriginalModel != "gpt-4" {
		t.Errorf("expected OriginalModel=gpt-4, got %q", r.OriginalModel)
	}
	if r.ExpectedSavingsPct <= 0 {
		t.Errorf("expected positive savings %%, got %v", r.ExpectedSavingsPct)
	}
}

func TestMaybeRouteEco_OpusToHaiku(t *testing.T) {
	r := MaybeRouteEco("claude-opus-4-7")
	if !r.DidRoute {
		t.Errorf("expected DidRoute=true for claude-opus-4-7")
	}
	if r.NewModel != "claude-haiku-4-5" {
		t.Errorf("expected claude-haiku-4-5, got %q", r.NewModel)
	}
}

func TestMaybeRouteEco_AlreadyEfficient(t *testing.T) {
	r := MaybeRouteEco("gpt-4o-mini")
	if r.DidRoute {
		t.Errorf("expected DidRoute=false for already-small model")
	}
	if !r.AlreadyEfficient {
		t.Errorf("expected AlreadyEfficient=true for gpt-4o-mini")
	}
	if r.NewModel != "gpt-4o-mini" {
		t.Errorf("expected NewModel unchanged, got %q", r.NewModel)
	}
}

func TestMaybeRouteEco_UnmappedModel(t *testing.T) {
	r := MaybeRouteEco("some-future-model-not-yet-mapped")
	if r.DidRoute {
		t.Errorf("expected DidRoute=false for unmapped model")
	}
	if r.AlreadyEfficient {
		t.Errorf("expected AlreadyEfficient=false for unmapped model")
	}
	if r.NewModel != "some-future-model-not-yet-mapped" {
		t.Errorf("expected NewModel unchanged when no mapping, got %q", r.NewModel)
	}
}

func TestMaybeRouteEco_CaseInsensitive(t *testing.T) {
	// Frontend may normalize differently than backend; lookup must be lower()
	r := MaybeRouteEco("GPT-4")
	if !r.DidRoute {
		t.Errorf("expected DidRoute=true for GPT-4 (case insensitive)")
	}
}

// ----- Sanity checks on data integrity -------------------------------------

func TestFactorsTable_HasMinimumModels(t *testing.T) {
	tbl := GetFactorsTable()
	if tbl.Version == "" {
		t.Errorf("expected non-empty Version")
	}
	// Honest margins for closed-model inference are 50-200% per Codex
	// review of public LLM-carbon literature. Anything ≤25% is suspicious
	// (false precision); anything >300% is meaningless.
	if tbl.ErrorMarginPct < 25 || tbl.ErrorMarginPct > 300 {
		t.Errorf("ErrorMarginPct should be in [25, 300]; got %v (false precision or meaningless?)", tbl.ErrorMarginPct)
	}
	if len(tbl.Sources) < 2 {
		t.Errorf("expected ≥2 sources cited, got %d", len(tbl.Sources))
	}
	if len(tbl.WhatWeDontMeasure) < 3 {
		t.Errorf("expected ≥3 honesty bullets in WhatWeDontMeasure, got %d", len(tbl.WhatWeDontMeasure))
	}

	// Every model in eco mappings must have a coefficient OR be in skip list
	ecoTbl := GetEcoRoutingTable()
	for _, m := range ecoTbl.Mappings {
		if _, _, ok := LookupCoefficient(m.From, ""); !ok {
			t.Errorf("eco mapping FROM %q has no coefficient — methodology page would render '~?g'", m.From)
		}
		if _, _, ok := LookupCoefficient(m.To, ""); !ok {
			t.Errorf("eco mapping TO %q has no coefficient — savings %% claim would be unverifiable", m.To)
		}
	}
}

func TestEcoRouting_SavingsMatchFactors(t *testing.T) {
	// Verify expected_savings_pct in eco_routing.json matches the integer-rounded
	// coefficient delta from carbon_factors.json. Tightened from ±5pp to ±0.5pp
	// per Codex review — looser slack rubber-stamps drift; ±0.5pp catches any
	// hand-edit that doesn't recompute. Note this is INTERNAL consistency only:
	// it does NOT validate the underlying coefficients are scientifically sound.
	ecoTbl := GetEcoRoutingTable()
	for _, m := range ecoTbl.Mappings {
		fromG, _, ok1 := LookupCoefficient(m.From, "")
		toG, _, ok2 := LookupCoefficient(m.To, "")
		if !ok1 || !ok2 {
			continue // already flagged in the previous test
		}
		actual := (fromG - toG) / fromG * 100
		diff := math.Abs(actual - float64(m.ExpectedSavingsPct))
		if diff > 0.5 {
			t.Errorf("mapping %s→%s: claimed savings=%d%%, computed=%.2f%% (diff %.2f > 0.5)",
				m.From, m.To, m.ExpectedSavingsPct, actual, diff)
		}
	}
}
