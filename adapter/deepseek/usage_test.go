package deepseek

import (
	"chat/globals"
	"testing"
)

// TestNormaliseUsage_NilSafe — nil propagates so caller falls back to
// tiktoken.
func TestNormaliseUsage_NilSafe(t *testing.T) {
	if got := NormaliseUsage(nil); got != nil {
		t.Fatalf("nil input must map to nil; got %+v", got)
	}
}

// TestNormaliseUsage_NoCache — DeepSeek returns Usage with all hit tokens
// at 0 when KV cache misses (cold prompt).
func TestNormaliseUsage_NoCache(t *testing.T) {
	in := &Usage{
		PromptTokens:     1200,
		CompletionTokens: 500,
		TotalTokens:      1700,
	}
	want := &globals.UpstreamUsage{
		InputTokens:  1200,
		OutputTokens: 500,
	}
	got := NormaliseUsage(in)
	if got == nil || *got != *want {
		t.Errorf("no-cache: got %+v want %+v", got, want)
	}
}

// TestNormaliseUsage_HalfHit — typical KV cache scenario where ~50% of
// the prefix matches a previous request.
func TestNormaliseUsage_HalfHit(t *testing.T) {
	in := &Usage{
		PromptTokens:         2000,
		CompletionTokens:     800,
		PromptCacheHitTokens: 1000,
	}
	want := &globals.UpstreamUsage{
		InputTokens:     1000,
		OutputTokens:    800,
		CacheReadTokens: 1000,
	}
	got := NormaliseUsage(in)
	if got == nil || *got != *want {
		t.Errorf("half hit: got %+v want %+v", got, want)
	}
}

// TestNormaliseUsage_DefensiveOver — clamp negative un-cached to 0 if a
// proxy reports cache_hit > prompt.
func TestNormaliseUsage_DefensiveOver(t *testing.T) {
	in := &Usage{
		PromptTokens:         500,
		CompletionTokens:     100,
		PromptCacheHitTokens: 700,
	}
	got := NormaliseUsage(in)
	if got == nil {
		t.Fatal("nil")
	}
	if got.InputTokens != 0 {
		t.Errorf("defensive: un-cached must clamp to 0; got %d", got.InputTokens)
	}
}
