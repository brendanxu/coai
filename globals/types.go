package globals

type Hook func(data *Chunk) error

type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	Name             *string       `json:"name,omitempty"`
	FunctionCall     *FunctionCall `json:"function_call,omitempty"`     // only `function` role
	ToolCallId       *string       `json:"tool_call_id,omitempty"`      // only `tool` role
	ToolCalls        *ToolCalls    `json:"tool_calls,omitempty"`        // only `assistant` role
	ReasoningContent *string       `json:"reasoning_content,omitempty"` // only for deepseek reasoner models
}

type Chunk struct {
	Content      string        `json:"content"`
	ToolCall     *ToolCalls    `json:"tool_call,omitempty"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"`

	// UpstreamUsage carries per-call token + cache counts when the adapter
	// can scrape them off the provider's response. Nil for chunks that
	// only deliver content (most stream events). When the upstream
	// response actually arrives at the terminal point, the adapter
	// emits one final Chunk with this field set so the billing layer
	// (Buffer.RecordUpstreamUsage in utils/buffer.go) can replace its
	// tiktoken estimate with the ground-truth numbers.
	//
	// Adapters that don't yet parse usage (CoAI baseline) leave this nil
	// and the billing layer falls back to the legacy tiktoken path.
	UpstreamUsage *UpstreamUsage `json:"upstream_usage,omitempty"`
}

// UpstreamUsage is the canonical 4-class token shape that greentokey's
// "we never lose money" billing rule (see
// docs/research/token-cache-AUDIT-and-billing-design.md) reads to bill
// each call against gtk_provider_pricing × markup_multiplier.
//
// Provider-shape mapping (validated 2026-05-10):
//
//	Anthropic Messages API
//	  InputTokens       = usage.input_tokens          (the un-cached tail)
//	  OutputTokens      = usage.output_tokens
//	  CacheWriteTokens  = usage.cache_creation_input_tokens
//	  CacheReadTokens   = usage.cache_read_input_tokens
//	  CacheTTL          = "5m" or "1h" (callers infer from the request marker)
//
//	OpenAI Chat Completions
//	  InputTokens       = usage.prompt_tokens - cached_tokens
//	  OutputTokens      = usage.completion_tokens
//	  CacheWriteTokens  = 0 (OpenAI auto-caches; no explicit write tier)
//	  CacheReadTokens   = usage.prompt_tokens_details.cached_tokens
//	  CacheTTL          = "" (auto, opaque)
//
//	DeepSeek Chat Completions
//	  InputTokens       = usage.prompt_tokens - prompt_cache_hit_tokens
//	  OutputTokens      = usage.completion_tokens
//	  CacheWriteTokens  = 0 (KV cache, implicit)
//	  CacheReadTokens   = usage.prompt_cache_hit_tokens
//	  CacheTTL          = "" (auto, opaque)
//
// Adapters do the math above and emit a normalised UpstreamUsage so the
// billing layer never branches on provider.
type UpstreamUsage struct {
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheTTL         string `json:"cache_ttl,omitempty"`
}

type ChatSegmentResponse struct {
	Conversation int64   `json:"conversation"`
	Quota        float32 `json:"quota"`
	Keyword      string  `json:"keyword"`
	Message      string  `json:"message"`
	End          bool    `json:"end"`
	Plan         bool    `json:"plan"`
}

type GenerationSegmentResponse struct {
	Quota   float32 `json:"quota"`
	Message string  `json:"message"`
	Hash    string  `json:"hash"`
	End     bool    `json:"end"`
	Error   string  `json:"error"`
}

type ListModels struct {
	Object string           `json:"object"`
	Data   []ListModelsItem `json:"data"`
}

type ListModelsItem struct {
	Id      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ProxyConfig struct {
	ProxyType int    `json:"proxy_type" mapstructure:"proxytype"`
	Proxy     string `json:"proxy" mapstructure:"proxy"`
	Username  string `json:"username" mapstructure:"username"`
	Password  string `json:"password" mapstructure:"password"`
}
