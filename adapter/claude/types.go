package claude

import "chat/globals"

// ChatBody is the request body for anthropic claude

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type MessageImage struct {
	Type      string      `json:"type"`
	MediaType interface{} `json:"media_type"`
	Data      interface{} `json:"data"`
}

type MessageContent struct {
	Type         string                `json:"type"`
	Text         *string               `json:"text,omitempty"`
	Source       *MessageImage         `json:"source,omitempty"`
	CacheControl *globals.CacheControl `json:"cache_control,omitempty"`
}

type SystemBlock struct {
	Type         string                `json:"type"`
	Text         string                `json:"text"`
	CacheControl *globals.CacheControl `json:"cache_control,omitempty"`
}

type ChatBody struct {
	Messages    []Message   `json:"messages"`
	MaxTokens   int         `json:"max_tokens"`
	Model       string      `json:"model"`
	System      interface{} `json:"system,omitempty"`
	Stream      bool        `json:"stream"`
	Temperature *float32    `json:"temperature,omitempty"`
	TopP        *float32    `json:"top_p,omitempty"`
	TopK        *int        `json:"top_k,omitempty"`
}

type ChatStreamResponse struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	// Message is populated on message_start events. The ChatInstance reads
	// Message.Usage to capture initial input + cache token counts so the
	// billing layer can replace tiktoken estimates with provider truth.
	Message *struct {
		Usage Usage `json:"usage"`
	} `json:"message,omitempty"`
	// Usage is populated on message_delta events. Anthropic streams
	// output_tokens here at the end of the response.
	Usage *Usage `json:"usage,omitempty"`
}

// Usage mirrors the Anthropic Messages API usage block. CacheCreation /
// CacheRead are only non-zero when the request carried cache_control
// markers and the upstream actually wrote / hit the cache. Schema:
// https://docs.claude.com/en/api/messages#response-usage
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ChatNonStreamResponse is the body returned by /v1/messages when stream=false.
// Today CoAI only ever calls Claude with stream=true (CreateStreamChatRequest),
// but billing reconcile + future non-stream paths benefit from a typed shape.
type ChatNonStreamResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage Usage  `json:"usage"`
}

type ChatErrorResponse struct {
	Error struct {
		Type    string `json:"type" binding:"required"`
		Message string `json:"message"`
	} `json:"error"`
}
