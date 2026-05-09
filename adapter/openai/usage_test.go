package openai

import (
	"chat/globals"
	"testing"
)

// TestNormaliseUsage_NilSafe — nil → nil so callers can fall through to
// tiktoken without branching.
func TestNormaliseUsage_NilSafe(t *testing.T) {
	if got := NormaliseUsage(nil); got != nil {
		t.Fatalf("nil input must map to nil; got %+v", got)
	}
}

// TestNormaliseUsage_NoCache — no cache hit, no PromptTokensDetails. Matches
// older models / proxies that omit the details block.
func TestNormaliseUsage_NoCache(t *testing.T) {
	in := &Usage{
		PromptTokens:     1500,
		CompletionTokens: 800,
		TotalTokens:      2300,
	}
	want := &globals.UpstreamUsage{
		InputTokens:  1500,
		OutputTokens: 800,
	}
	got := NormaliseUsage(in)
	if got == nil || *got != *want {
		t.Errorf("no-cache: got %+v want %+v", got, want)
	}
}

// TestNormaliseUsage_AutoCacheHit — cached_tokens > 0; un-cached input is
// the prompt-tokens minus the cache-read portion.
func TestNormaliseUsage_AutoCacheHit(t *testing.T) {
	in := &Usage{
		PromptTokens:        2048,
		CompletionTokens:    400,
		PromptTokensDetails: &PromptTokensDetails{CachedTokens: 1024},
	}
	want := &globals.UpstreamUsage{
		InputTokens:     1024, // 2048 - 1024
		OutputTokens:    400,
		CacheReadTokens: 1024,
	}
	got := NormaliseUsage(in)
	if got == nil || *got != *want {
		t.Errorf("auto cache hit: got %+v want %+v", got, want)
	}
}

// TestNormaliseUsage_FullHit — every input token came from cache.
func TestNormaliseUsage_FullHit(t *testing.T) {
	in := &Usage{
		PromptTokens:        1024,
		CompletionTokens:    100,
		PromptTokensDetails: &PromptTokensDetails{CachedTokens: 1024},
	}
	got := NormaliseUsage(in)
	if got == nil {
		t.Fatal("nil")
	}
	if got.InputTokens != 0 {
		t.Errorf("full hit: un-cached input must be 0, got %d", got.InputTokens)
	}
	if got.CacheReadTokens != 1024 {
		t.Errorf("full hit: cache read must be 1024, got %d", got.CacheReadTokens)
	}
}

// TestNormaliseUsage_DefensiveBadInput — if a misbehaving proxy reports
// cached > prompt (which would imply negative un-cached), clamp to 0
// rather than letting the billing layer see a negative count.
func TestNormaliseUsage_DefensiveBadInput(t *testing.T) {
	in := &Usage{
		PromptTokens:        500,
		CompletionTokens:    100,
		PromptTokensDetails: &PromptTokensDetails{CachedTokens: 800}, // bogus
	}
	got := NormaliseUsage(in)
	if got == nil {
		t.Fatal("nil")
	}
	if got.InputTokens != 0 {
		t.Errorf("defensive: un-cached must clamp to 0; got %d", got.InputTokens)
	}
}
