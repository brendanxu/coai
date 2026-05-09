package deepseek

import (
	"chat/globals"
)

// DeepSeek API is similar to OpenAI API with additional reasoning content

type ChatRequest struct {
	Model            string            `json:"model"`
	Messages         []globals.Message `json:"messages"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	Stream           bool              `json:"stream"`
	Temperature      *float32          `json:"temperature,omitempty"`
	TopP             *float32          `json:"top_p,omitempty"`
	PresencePenalty  *float32          `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float32          `json:"frequency_penalty,omitempty"`
}

// Usage mirrors DeepSeek's response usage block. PromptCacheHitTokens is
// DeepSeek-specific (their KV cache returns a count of input tokens that
// were served from disk cache, charged at ~0.1× input). Schema:
// https://api-docs.deepseek.com/quick_start/pricing — context-caching
// section, 2026-05-10.
//
// Note: PromptTokens INCLUDES PromptCacheHitTokens (same convention as
// OpenAI; opposite of Anthropic). NormaliseUsage splits them out.
type Usage struct {
	PromptTokens         int `json:"prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	TotalTokens          int `json:"total_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
}

// ChatResponse is the native http request body for deepseek
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int             `json:"index"`
		Message      globals.Message `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// ChatStreamResponse is the stream response body for deepseek. DeepSeek
// includes a `usage` block on the final chunk by default (no opt-in
// required, unlike OpenAI's stream_options).
type ChatStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Delta        globals.Message `json:"delta"`
		Index        int             `json:"index"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

// NormaliseUsage splits DeepSeek's PromptTokens into the un-cached input
// portion and the cache-read portion that the billing layer needs.
// Returns nil for a nil input so the caller falls back to tiktoken.
func NormaliseUsage(u *Usage) *globals.UpstreamUsage {
	if u == nil {
		return nil
	}
	uncached := u.PromptTokens - u.PromptCacheHitTokens
	if uncached < 0 {
		uncached = 0
	}
	return &globals.UpstreamUsage{
		InputTokens:     uncached,
		OutputTokens:    u.CompletionTokens,
		CacheReadTokens: u.PromptCacheHitTokens,
	}
}

type ChatStreamErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}
