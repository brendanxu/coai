package openai

import "chat/globals"

type ImageUrl struct {
	Url    string  `json:"url"`
	Detail *string `json:"detail,omitempty"`
}

type MessageContent struct {
	Type     string    `json:"type"`
	Text     *string   `json:"text,omitempty"`
	ImageUrl *ImageUrl `json:"image_url,omitempty"`
}

type MessageContents []MessageContent

type Message struct {
	Role             string                `json:"role"`
	Content          MessageContents       `json:"content"`
	Name             *string               `json:"name,omitempty"`
	FunctionCall     *globals.FunctionCall `json:"function_call,omitempty"` // only `function` role
	ToolCallId       *string               `json:"tool_call_id,omitempty"`  // only `tool` role
	ToolCalls        *globals.ToolCalls    `json:"tool_calls,omitempty"`    // only `assistant` role
	ReasoningContent *string               `json:"reasoning,omitempty"`     // only for claude reasoning models
}

// ChatRequest is the request body for openai
type ChatRequest struct {
	Model               string                 `json:"model"`
	Messages            interface{}            `json:"messages"`
	MaxToken            *int                   `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                   `json:"max_completion_tokens,omitempty"`
	Stream              bool                   `json:"stream"`
	PresencePenalty     *float32               `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float32               `json:"frequency_penalty,omitempty"`
	Temperature         *float32               `json:"temperature,omitempty"`
	TopP                *float32               `json:"top_p,omitempty"`
	Tools               *globals.FunctionTools `json:"tools,omitempty"`
	ToolChoice          *interface{}           `json:"tool_choice,omitempty"` // string or object
}

// CompletionRequest is the request body for openai completion
type CompletionRequest struct {
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	MaxToken *int   `json:"max_tokens,omitempty"`
	Stream   bool   `json:"stream"`
}

// PromptTokensDetails is OpenAI's nested cache info on usage. Schema:
// https://platform.openai.com/docs/guides/prompt-caching — `cached_tokens`
// is the count of input tokens served from the auto-cache (≥1024-token
// prefix match required, 50% pricing on hits as of 2026-05-10).
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// CompletionTokensDetails carries reasoning + audio breakdowns we don't
// charge differently for today; included so json round-trips faithfully
// for upstream-fidelity reconcile audits.
type CompletionTokensDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

// Usage mirrors OpenAI's response usage block. PromptTokens INCLUDES the
// CachedTokens (Anthropic style is the opposite — cache_read is on top of
// input_tokens). Adapter normalisation (chat.go::emitUpstreamUsage) splits
// them so globals.UpstreamUsage carries the un-cached input portion only.
type Usage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// ChatResponse is the native http request body for openai
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
	Usage *Usage `json:"usage,omitempty"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatStreamResponse is the stream response body for openai. Usage is only
// populated on the final chunk when the request sets
// stream_options.include_usage=true (OpenAI default for new clients);
// older models / proxies omit it and we fall back to tiktoken.
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

// NormaliseUsage converts an OpenAI Usage payload into the canonical
// 4-class shape used by globals.UpstreamUsage. Returns nil if u is nil
// (caller falls back to tiktoken).
//
//	cached_tokens = read of the auto-cache hit
//	un-cached input = prompt_tokens - cached_tokens
//	output = completion_tokens
//	cache_write = 0 (OpenAI's auto-cache has no separate write tier)
func NormaliseUsage(u *Usage) *globals.UpstreamUsage {
	if u == nil {
		return nil
	}
	cachedRead := 0
	if u.PromptTokensDetails != nil {
		cachedRead = u.PromptTokensDetails.CachedTokens
	}
	uncachedInput := u.PromptTokens - cachedRead
	if uncachedInput < 0 {
		uncachedInput = 0
	}
	return &globals.UpstreamUsage{
		InputTokens:     uncachedInput,
		OutputTokens:    u.CompletionTokens,
		CacheReadTokens: cachedRead,
	}
}

// CompletionResponse is the native http request body / stream response body for openai completion
type CompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Text  string `json:"text"`
		Index int    `json:"index"`
	} `json:"choices"`
}

type ChatStreamErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type ImageSize string

// ImageRequest is the request body for openai dalle image generation
type ImageRequest struct {
	Model  string    `json:"model"`
	Prompt string    `json:"prompt"`
	Size   ImageSize `json:"size"`
	N      int       `json:"n"`
}

type ImageResponse struct {
	Data []struct {
		Url     string `json:"url,omitempty"`
		B64Json string `json:"b64_json,omitempty"`
	} `json:"data"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

var (
	ImageSize256  ImageSize = "256x256"
	ImageSize512  ImageSize = "512x512"
	ImageSize1024 ImageSize = "1024x1024"
)
