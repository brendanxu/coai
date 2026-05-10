package utils

import (
	"chat/globals"
	"fmt"
	"strings"
	"time"
)

type Charge interface {
	GetType() string
	GetModels() []string
	GetInput() float32
	GetOutput() float32
	// GetCacheRead / GetCacheWrite5m / GetCacheWrite1h are per-1k-token
	// rates the billing layer applies when the adapter delivers a real
	// UpstreamUsage from the provider (W2). Unset implementations fall
	// back to GetInput so legacy adapters keep working unchanged.
	GetCacheRead() float32
	GetCacheWrite5m() float32
	GetCacheWrite1h() float32
	SupportAnonymous() bool
	IsBilling() bool
	IsBillingType(t string) bool
	GetLimit() float32
}

type Buffer struct {
	Model           string                `json:"model"`
	Quota           float32               `json:"quota"`
	Data            string                `json:"data"`
	Latest          string                `json:"latest"`
	Cursor          int                   `json:"cursor"`
	Times           int                   `json:"times"`
	InputTokens     int                   `json:"input_tokens"`
	Images          Images                `json:"images"`
	ToolCalls       *globals.ToolCalls    `json:"tool_calls"`
	ToolCallsCursor int                   `json:"tool_calls_cursor"`
	FunctionCall    *globals.FunctionCall `json:"function_call"`
	StartTime       *time.Time            `json:"-"`
	Prompts         string                `json:"prompts"`
	TokenName       string                `json:"-"`
	Charge          Charge                `json:"-"`
	VisionRecall    bool                  `json:"-"`

	// Upstream is the provider-truth usage block emitted by the adapter
	// at end-of-stream (W2). Nil while the stream is in flight or the
	// upstream / proxy didn't supply usage — in that case GetQuota falls
	// back to the legacy tiktoken-based estimate. When non-nil the
	// billing layer prefers it because it accounts for cache_creation /
	// cache_read tokens that tiktoken can't see.
	Upstream *globals.UpstreamUsage `json:"upstream,omitempty"`

	// PreferredCacheTTL is the customer-declared cache_control TTL detected
	// from the request body. The adapter supplies token counts from upstream;
	// this request-side marker tells W4 billing whether cache writes should
	// use the 5m or 1h rate when Anthropic omits TTL from usage.
	PreferredCacheTTL string `json:"-"`
}

func initInputToken(model string, history []globals.Message) int {
	if globals.IsVisionModel(model) {
		for _, message := range history {
			if message.Role == globals.User {
				content, _ := ExtractImages(message.Content.String(), true)
				message.Content = globals.MessageContent{Plain: content}
			}
		}

		history = Each(history, func(message globals.Message) globals.Message {
			if message.Role == globals.User {
				raw, _ := ExtractImages(message.Content.String(), true)
				return globals.Message{
					Role:         message.Role,
					Content:      globals.MessageContent{Plain: raw},
					Name:         message.Name,
					FunctionCall: message.FunctionCall,
					ToolCalls:    message.ToolCalls,
					ToolCallId:   message.ToolCallId,
				}
			}

			return message
		})
	}

	return NumTokensFromMessages(history, model, false)
}

func preferredCacheTTL(history []globals.Message) string {
	for _, message := range history {
		if t, ok := message.Content.HasCacheControl(); ok {
			return t
		}
	}
	return ""
}

func NewBuffer(model string, history []globals.Message, charge Charge) *Buffer {
	token := initInputToken(model, history)
	ttl := preferredCacheTTL(history)

	return &Buffer{
		Model:             model,
		Quota:             CountInputQuota(charge, token),
		InputTokens:       token,
		Charge:            charge,
		FunctionCall:      nil,
		ToolCalls:         nil,
		ToolCallsCursor:   0,
		StartTime:         ToPtr(time.Now()),
		PreferredCacheTTL: ttl,
	}
}

func (b *Buffer) GetCursor() int {
	return b.Cursor
}

// GetQuota returns the quota the customer is currently on the hook for.
//
// Upstream-first billing: when the adapter has handed us a real
// UpstreamUsage block (W2), use the 4-class calculator
// (CountUpstreamQuota) which prices input / output / cache_read /
// cache_write independently per channel.Charge config. This is the
// "we never lose money" path because the rates are pinned to actual
// upstream multipliers.
//
// Legacy fallback: if Upstream is nil (older adapters, proxies that
// strip usage, or providers we haven't wired yet), fall back to the
// CoAI baseline that uses tiktoken estimates × Charge.Input/Output.
func (b *Buffer) GetQuota() float32 {
	if b.Upstream != nil {
		return CountUpstreamQuota(b.Charge, b.Upstream)
	}
	return b.Quota + CountOutputToken(b.Charge, b.CountOutputToken(true))
}

func (b *Buffer) GetRecordQuota() float32 {
	if b.Upstream != nil {
		return CountUpstreamQuota(b.Charge, b.Upstream)
	}
	// end of the buffer, the output token is counted using the times
	return b.Quota + CountOutputToken(b.Charge, b.CountOutputToken(false))
}

// RecordUpstreamUsage stores the provider-truth usage block coming off
// the adapter (one terminal emit per stream). Subsequent GetQuota /
// GetRecordQuota calls return cache-aware totals that override the
// tiktoken estimate accumulated during streaming.
//
// Idempotent: calling twice with the same payload is a no-op (the second
// call simply overwrites with identical data).
func (b *Buffer) RecordUpstreamUsage(u *globals.UpstreamUsage) {
	if u == nil {
		return
	}
	if u.CacheTTL == "" && b.PreferredCacheTTL != "" {
		u.CacheTTL = b.PreferredCacheTTL
	}
	b.Upstream = u
	if u.InputTokens > 0 {
		b.InputTokens = u.InputTokens
	}
}

func (b *Buffer) Write(data string) string {
	b.Data += data
	b.Cursor += len(data)
	b.Times++
	b.Latest = data
	return data
}

func (b *Buffer) WriteChunk(data *globals.Chunk) string {
	if data == nil {
		return ""
	}

	// Adapter end-of-stream emits a content-empty Chunk with UpstreamUsage
	// set. Capture it so the billing layer can switch from tiktoken
	// estimate to provider truth. Done before Write so the Times counter
	// only reflects content events for downstream stats.
	if data.UpstreamUsage != nil {
		b.RecordUpstreamUsage(data.UpstreamUsage)
		// Empty-content terminal chunk: skip the Write to avoid
		// inflating Times and to keep Latest reflecting the last visible
		// text. AddToolCalls / SetFunctionCall are already nil-safe.
		if data.Content == "" {
			b.AddToolCalls(data.ToolCall)
			b.SetFunctionCall(data.FunctionCall)
			return ""
		}
	}

	b.Write(data.Content)
	b.AddToolCalls(data.ToolCall)
	b.SetFunctionCall(data.FunctionCall)

	return data.Content
}

func (b *Buffer) GetChunk() string {
	return b.Latest
}

func (b *Buffer) InitVisionRecall() {
	// set the vision recall flag to true to prevent the buffer from adding more images of retrying the channel
	b.VisionRecall = true
}

func (b *Buffer) AddImage(image *Image) {
	if image == nil || b.VisionRecall {
		return
	}

	b.Images = append(b.Images, *image)

	tokens := image.CountTokens(b.Model)
	b.InputTokens += tokens

	if b.Charge.IsBillingType(globals.TokenBilling) {
		b.Quota += float32(tokens) / 1000 * b.Charge.GetInput()
	}
}

func (b *Buffer) GetImages() Images {
	return b.Images
}

func (b *Buffer) SetToolCalls(toolCalls *globals.ToolCalls) {
	b.ToolCalls = toolCalls
}

func hitTool(tool globals.ToolCall, tools globals.ToolCalls) (int, *globals.ToolCall) {
	for i, t := range tools {
		if t.Id == tool.Id {
			return i, &t
		}
	}

	if len(tool.Type) == 0 && len(tool.Id) == 0 {
		length := len(tools)

		if length > 0 {
			// if the tool is empty, return the last tool as the hit
			return length - 1, &tools[length-1]
		}
	}

	return 0, nil
}

func appendTool(tool globals.ToolCall, chunk globals.ToolCall) string {
	from := ToString(tool.Function.Arguments)
	to := ToString(chunk.Function.Arguments)

	return from + to
}

func mixTools(source *globals.ToolCalls, target *globals.ToolCalls) *globals.ToolCalls {
	if source == nil {
		return target
	}

	tools := make(globals.ToolCalls, 0)
	arr := Collect[globals.ToolCall](*source, *target)

	for _, tool := range arr {
		idx, hit := hitTool(tool, tools)

		if hit != nil {
			tools[idx].Function.Arguments = appendTool(tools[idx], tool)
		} else {
			tools = append(tools, tool)
		}
	}

	return &tools
}

func (b *Buffer) AddToolCalls(toolCalls *globals.ToolCalls) {
	if toolCalls == nil {
		return
	}

	b.ToolCalls = mixTools(b.ToolCalls, toolCalls)
}

func (b *Buffer) SetFunctionCall(functionCall *globals.FunctionCall) {
	if functionCall == nil {
		return
	}

	b.FunctionCall = functionCall
}

func (b *Buffer) GetToolCalls() *globals.ToolCalls {
	calls := b.ToolCalls
	b.ToolCalls = nil

	return calls
}

func (b *Buffer) GetFunctionCall() *globals.FunctionCall {
	return b.FunctionCall
}

func (b *Buffer) IsFunctionCalling() bool {
	return b.FunctionCall != nil || b.ToolCalls != nil
}

func (b *Buffer) IsEmpty() bool {
	return b.Cursor == 0 && !b.IsFunctionCalling()
}

func (b *Buffer) GetModel() string {
	return b.Model
}

func (b *Buffer) GetCharge() Charge {
	return b.Charge
}

func (b *Buffer) ToChargeInfo() string {
	switch b.Charge.GetType() {
	case globals.TokenBilling:
		return fmt.Sprintf(
			"input tokens: %0.4f quota / 1k tokens\n"+
				"output tokens: %0.4f quota / 1k tokens\n",
			b.Charge.GetInput(), b.Charge.GetOutput(),
		)
	case globals.TimesBilling:
		return fmt.Sprintf("%f quota per request\n", b.Charge.GetLimit())
	case globals.NonBilling:
		return "no cost"
	}

	return ""
}

func (b *Buffer) SetPrompts(prompts interface{}) {
	b.Prompts = ToString(prompts)
}

func (b *Buffer) Read() string {
	return b.Data
}

func (b *Buffer) ReadBytes() []byte {
	return []byte(b.Data)
}

func (b *Buffer) ReadWithDefault(_default string) string {
	if b.IsEmpty() || (len(strings.TrimSpace(b.Data)) == 0 && !b.IsFunctionCalling()) {
		return _default
	}
	return b.Data
}

func (b *Buffer) ReadTimes() int {
	return b.Times
}

func (b *Buffer) SetInputTokens(tokens int) {
	b.InputTokens = tokens
}

func (b *Buffer) CountInputToken() int {
	return b.InputTokens
}

func (b *Buffer) CountOutputToken(running bool) int {
	if running {
		// performance optimization:
		// if the buffer is still running, the output token counted using the times instead
		return b.Times
	}

	return NumTokensFromResponse(b.Read(), b.Model)
}

func (b *Buffer) CountToken() int {
	return b.CountInputToken() + b.CountOutputToken(true)
}

func (b *Buffer) GetDuration() float32 {
	if b.StartTime == nil {
		return 0
	}

	return float32(time.Since(*b.StartTime).Seconds())
}

func (b *Buffer) GetStartTime() *time.Time {
	return b.StartTime
}

func (b *Buffer) GetPrompts() string {
	return b.Prompts
}

func (b *Buffer) GetTokenName() string {
	if len(b.TokenName) == 0 {
		return globals.WebTokenType
	}

	return b.TokenName
}

func (b *Buffer) SetTokenName(tokenName string) {
	b.TokenName = tokenName
}

func (b *Buffer) GetRecordPrompts() string {
	if !globals.AcceptPromptStore {
		return ""
	}

	return b.GetPrompts()
}

func (b *Buffer) GetRecordResponsePrompts() string {
	if !globals.AcceptPromptStore {
		return ""
	}

	return b.Read()
}
