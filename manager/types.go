package manager

import (
	"chat/globals"
	"chat/utils"
)

type Message struct {
	Role         string                `json:"role,omitempty"`
	Content      interface{}           `json:"content"`
	Name         *string               `json:"name,omitempty"`
	FunctionCall *globals.FunctionCall `json:"function_call,omitempty"` // only `function` role
	ToolCallId   *string               `json:"tool_call_id,omitempty"`  // only `tool` role
	ToolCalls    *globals.ToolCalls    `json:"tool_calls,omitempty"`    // only `assistant` role
}

type ImageUrl struct {
	Url    string  `json:"url"`
	Detail *string `json:"detail,omitempty"`
}

type MessageContent struct {
	Type         string                `json:"type"`
	Text         *string               `json:"text,omitempty"`
	ImageUrl     *ImageUrl             `json:"image_url,omitempty"`
	CacheControl *globals.CacheControl `json:"cache_control,omitempty"`
}

type MessageContents []MessageContent

type RelayForm struct {
	Model             string    `json:"model" binding:"required"`
	Messages          []Message `json:"messages" binding:"required"`
	Stream            bool      `json:"stream"`
	MaxTokens         *int      `json:"max_tokens"`
	PresencePenalty   *float32  `json:"presence_penalty"`
	FrequencyPenalty  *float32  `json:"frequency_penalty"`
	RepetitionPenalty *float32  `json:"repetition_penalty"`
	Temperature       *float32  `json:"temperature"`
	TopP              *float32  `json:"top_p"`
	TopK              *int      `json:"top_k"`
	Tools             *globals.FunctionTools
	ToolChoice        *interface{}
	Official          bool `json:"official"`
}

type Choice struct {
	Index        int             `json:"index"`
	Message      globals.Message `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type StreamMessage struct {
	Role         *string               `json:"role"`
	Content      string                `json:"content"`
	Name         *string               `json:"name,omitempty"`
	FunctionCall *globals.FunctionCall `json:"function_call,omitempty"` // only `function` role
	ToolCallId   *string               `json:"tool_call_id,omitempty"`  // only `tool` role
	ToolCalls    *globals.ToolCalls    `json:"tool_calls,omitempty"`    // only `assistant` role
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type RelayResponse struct {
	Id      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Quota   *float32 `json:"quota,omitempty"`
}

type ChoiceDelta struct {
	Index        int         `json:"index"`
	Delta        Message     `json:"delta"`
	FinishReason interface{} `json:"finish_reason"`
}

type RelayStreamResponse struct {
	Id      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChoiceDelta `json:"choices"`
	Usage   Usage         `json:"usage"`
	Quota   *float32      `json:"quota,omitempty"`
	Error   error         `json:"error,omitempty"`
}

type RelayErrorResponse struct {
	Error TranshipmentError `json:"error"`
}

type TranshipmentError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type RelayImageForm struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      *int   `json:"n,omitempty"`
}

type RelayImageData struct {
	Url     string `json:"url,omitempty"`
	B64Json string `json:"b64_json,omitempty"`
}

type RelayImageResponse struct {
	Created int64            `json:"created"`
	Data    []RelayImageData `json:"data"`
}

type RelayVideoForm struct {
	Model          string  `json:"model"`
	Prompt         string  `json:"prompt" binding:"required"`
	Seconds        *string `json:"seconds,omitempty"`
	Size           *string `json:"size,omitempty"`
	InputReference *string `json:"input_reference,omitempty"`
}

type RelayVideoError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RelayVideoJob struct {
	CompletedAt        *int64           `json:"completed_at,omitempty"`
	CreatedAt          int64            `json:"created_at"`
	Error              *RelayVideoError `json:"error,omitempty"`
	ExpiresAt          *int64           `json:"expires_at,omitempty"`
	Id                 string           `json:"id"`
	Model              string           `json:"model"`
	Object             string           `json:"object"`
	Progress           *int             `json:"progress,omitempty"`
	Prompt             string           `json:"prompt"`
	RemixedFromVideoId *string          `json:"remixed_from_video_id,omitempty"`
	Seconds            string           `json:"seconds"`
	Size               string           `json:"size"`
	Status             string           `json:"status"`
}

func transformContent(content interface{}) globals.MessageContent {
	switch v := content.(type) {
	case string:
		return globals.MessageContent{Plain: v}
	default:
		blocks := utils.MapToStruct[[]globals.ContentBlock](v)
		if blocks != nil {
			return globals.MessageContent{Blocks: *blocks}
		}

		data := utils.MapToStruct[MessageContents](v)
		if data == nil || len(*data) == 0 {
			return globals.MessageContent{}
		}

		converted := make([]globals.ContentBlock, 0, len(*data))
		for _, v := range *data {
			block := globals.ContentBlock{
				Type:         v.Type,
				CacheControl: v.CacheControl,
			}
			if v.Text != nil {
				block.Text = *v.Text
			}
			if v.ImageUrl != nil {
				block.ImageURL = &globals.ImageURL{
					URL:    v.ImageUrl.Url,
					Detail: v.ImageUrl.Detail,
				}
			}
			converted = append(converted, block)
		}
		return globals.MessageContent{Blocks: converted}
	}
}

func transform(m []Message) []globals.Message {
	var messages []globals.Message
	for _, v := range m {
		messages = append(messages, globals.Message{
			Role:         v.Role,
			Content:      transformContent(v.Content),
			Name:         v.Name,
			FunctionCall: v.FunctionCall,
			ToolCallId:   v.ToolCallId,
			ToolCalls:    v.ToolCalls,
		})
	}
	return messages
}
