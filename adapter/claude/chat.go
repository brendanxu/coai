package claude

import (
	adaptercommon "chat/adapter/common"
	"chat/globals"
	"chat/utils"
	"errors"
	"fmt"
)

const defaultTokens = 2500

func (c *ChatInstance) GetChatEndpoint() string {
	return fmt.Sprintf("%s/v1/messages", c.GetEndpoint())
}

func (c *ChatInstance) GetChatHeaders() map[string]string {
	return map[string]string{
		"content-type":      "application/json",
		"anthropic-version": "2023-06-01",
		"x-api-key":         c.GetApiKey(),
	}
}

// ConvertCompletionMessage converts the completion message to anthropic complete format (deprecated)
func (c *ChatInstance) ConvertCompletionMessage(message []globals.Message) string {
	mapper := map[string]string{
		globals.System:    "Assistant",
		globals.User:      "Human",
		globals.Assistant: "Assistant",
	}

	var result string
	for i, item := range message {
		if item.Role == globals.Tool {
			continue
		}
		if i == 0 && item.Role == globals.Assistant {
			// skip first assistant message
			continue
		}

		result += fmt.Sprintf("\n\n%s: %s", mapper[item.Role], item.Content)
	}
	return fmt.Sprintf("%s\n\nAssistant:", result)
}

func (c *ChatInstance) GetTokens(props *adaptercommon.ChatProps) int {
	if props.MaxTokens == nil || *props.MaxTokens <= 0 {
		return defaultTokens
	}

	return *props.MaxTokens
}

func (c *ChatInstance) ConvertMessages(props *adaptercommon.ChatProps) []globals.Message {
	// anthropic api: top message must be user message, only `user` and `assistant` role messages are allowd
	start := false

	result := make([]globals.Message, 0)

	for _, message := range props.Message {
		if message.Role == globals.System {
			continue
		}

		// if is first message, set it to user message
		if !start {
			start = true
			result = append(result, globals.Message{
				Role:    globals.User,
				Content: message.Content,
			})
			continue
		}

		// anthropic api does not allow multi-same role messages
		if len(result) > 0 && result[len(result)-1].Role == message.Role {
			result[len(result)-1].Content += "\n" + message.Content
			continue
		}

		result = append(result, message)
	}

	return result
}

func (c *ChatInstance) GetMessages(props *adaptercommon.ChatProps) []Message {
	converted := c.ConvertMessages(props)
	return utils.Each(converted, func(message globals.Message) Message {
		if !globals.IsVisionModel(props.Model) || message.Role != globals.User {
			return Message{
				Role:    message.Role,
				Content: message.Content,
			}
		}

		content, urls := utils.ExtractImages(message.Content, true)
		images := utils.EachNotNil(urls, func(url string) *MessageContent {
			obj, err := utils.NewImage(url)
			props.Buffer.AddImage(obj)
			if err != nil {
				globals.Info(fmt.Sprintf("cannot process image: %s (source: %s)", err.Error(), utils.Extract(url, 24, "...")))
			}

			i := utils.NewImageContent(url)
			return &MessageContent{
				Type: "image",
				Source: &MessageImage{
					Type:      "base64",
					MediaType: i.GetType(),
					Data:      i.ToRawBase64(),
				},
			}
		})

		return Message{
			Role: message.Role,
			Content: utils.Prepend(images, MessageContent{
				Type: "text",
				Text: &content,
			}),
		}
	})
}

func (c *ChatInstance) GetSystemPrompt(props *adaptercommon.ChatProps) (prompt string) {
	for _, message := range props.Message {
		if message.Role == globals.System {
			prompt += message.Content
		}
	}
	return
}

func (c *ChatInstance) GetChatBody(props *adaptercommon.ChatProps, stream bool) *ChatBody {
	messages := c.GetMessages(props)
	return &ChatBody{
		Messages:    messages,
		MaxTokens:   c.GetTokens(props),
		Model:       props.Model,
		System:      c.GetSystemPrompt(props),
		Stream:      stream,
		Temperature: props.Temperature,
		TopP:        props.TopP,
		TopK:        props.TopK,
	}
}

// processStreamEvent normalises one Anthropic SSE event into a Chunk plus an
// optional usage delta. Anthropic streams usage in two events:
//
//   - message_start: carries initial input_tokens + cache_creation/read
//   - message_delta: carries output_tokens at the end
//
// Callers (CreateStreamChatRequest) accumulate the partials in a closure and
// emit one final Chunk{UpstreamUsage:...} on message_stop so the billing
// layer sees provider truth instead of tiktoken estimates.
func processStreamEvent(data string) (*globals.Chunk, *Usage) {
	form := processChatResponse(data)
	if form == nil {
		return nil, nil
	}
	var usage *Usage
	switch form.Type {
	case "message_start":
		if form.Message != nil {
			u := form.Message.Usage
			usage = &u
		}
	case "message_delta":
		if form.Usage != nil {
			usage = form.Usage
		}
	}
	return &globals.Chunk{Content: form.Delta.Text}, usage
}

// ProcessLine is retained for callers that don't need usage (e.g. simpler
// tests). Production stream loop uses processStreamEvent directly.
func (c *ChatInstance) ProcessLine(data string) (*globals.Chunk, error) {
	chunk, _ := processStreamEvent(data)
	if chunk != nil {
		return chunk, nil
	}

	if form := processChatErrorResponse(data); form != nil {
		return &globals.Chunk{Content: ""}, fmt.Errorf("anthropic error: %s (type: %s)", form.Error.Message, form.Error.Type)
	}

	return &globals.Chunk{Content: ""}, nil
}

func processChatErrorResponse(data string) *ChatErrorResponse {
	if form := utils.UnmarshalForm[ChatErrorResponse](data); form != nil {
		return form
	}
	return nil
}

func processChatResponse(data string) *ChatStreamResponse {
	if form := utils.UnmarshalForm[ChatStreamResponse](data); form != nil {
		return form
	}
	return nil
}

// CreateStreamChatRequest is the stream request for anthropic claude.
//
// Stream lifecycle (Anthropic 2026-05-10):
//
//	message_start         → initial input_tokens + cache_creation/read
//	content_block_start   → block opens
//	content_block_delta×N → text deltas (forwarded to hook as Content)
//	content_block_stop    → block closes
//	message_delta         → final output_tokens
//	message_stop          → terminator (we emit aggregated UpstreamUsage here)
//
// The closure-scoped `usage` accumulates the two partials so we can hand the
// billing layer a single, complete UpstreamUsage at end-of-stream. Falling
// back gracefully: if the upstream skips message_start/delta (older models
// or proxies), the final emit is skipped and tiktoken estimates take over
// in utils/buffer.go.
func (c *ChatInstance) CreateStreamChatRequest(props *adaptercommon.ChatProps, hook globals.Hook) error {
	var usage Usage
	var sawUsage bool

	err := utils.EventScanner(&utils.EventScannerProps{
		Method:  "POST",
		Uri:     c.GetChatEndpoint(),
		Headers: c.GetChatHeaders(),
		Body:    c.GetChatBody(props, true),
		Callback: func(data string) error {
			chunk, partial := processStreamEvent(data)
			if partial != nil {
				sawUsage = true
				if partial.InputTokens > 0 {
					usage.InputTokens = partial.InputTokens
				}
				if partial.OutputTokens > 0 {
					usage.OutputTokens = partial.OutputTokens
				}
				if partial.CacheCreationInputTokens > 0 {
					usage.CacheCreationInputTokens = partial.CacheCreationInputTokens
				}
				if partial.CacheReadInputTokens > 0 {
					usage.CacheReadInputTokens = partial.CacheReadInputTokens
				}
			}
			if chunk == nil {
				if errForm := processChatErrorResponse(data); errForm != nil {
					return fmt.Errorf("anthropic error: %s (type: %s)",
						errForm.Error.Message, errForm.Error.Type)
				}
				return nil
			}
			return hook(chunk)
		},
	},
		props.Proxy,
	)

	// Emit usage once the stream finishes cleanly. CacheTTL is empty here
	// because the request body owns the marker; W5 will plumb the
	// "5m"/"1h" hint through props so we can record it on the usage row.
	if err == nil && sawUsage {
		_ = hook(&globals.Chunk{
			UpstreamUsage: &globals.UpstreamUsage{
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheWriteTokens: usage.CacheCreationInputTokens,
				CacheReadTokens:  usage.CacheReadInputTokens,
			},
		})
	}

	if err != nil {
		if form := processChatErrorResponse(err.Body); form != nil {
			if form.Error.Type == "" && form.Error.Message == "" {
				return errors.New(utils.ToMarkdownCode("json", err.Body))
			}

			return errors.New(fmt.Sprintf("%s (type: %s)", form.Error.Message, form.Error.Type))
		}
		return fmt.Errorf("%s\n%s", err.Error, errors.New(utils.ToMarkdownCode("json", err.Body)))
	}

	return nil
}
