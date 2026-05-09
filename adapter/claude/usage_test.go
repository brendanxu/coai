package claude

import (
	"testing"
)

// TestProcessStreamEvent_MessageStart — initial usage block is on
// message_start. Captures input + cache_creation/read so the closure in
// CreateStreamChatRequest can persist it for the final UpstreamUsage emit.
func TestProcessStreamEvent_MessageStart(t *testing.T) {
	data := `{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":50,"cache_creation_input_tokens":248,"cache_read_input_tokens":100000,"output_tokens":1}}}`
	chunk, usage := processStreamEvent(data)
	if chunk == nil {
		t.Fatal("expected non-nil chunk for message_start")
	}
	if usage == nil {
		t.Fatal("expected usage on message_start")
	}
	if usage.InputTokens != 50 {
		t.Errorf("input: got %d want 50", usage.InputTokens)
	}
	if usage.CacheCreationInputTokens != 248 {
		t.Errorf("cache_creation: got %d want 248", usage.CacheCreationInputTokens)
	}
	if usage.CacheReadInputTokens != 100000 {
		t.Errorf("cache_read: got %d want 100000", usage.CacheReadInputTokens)
	}
}

// TestProcessStreamEvent_ContentBlockDelta — text deltas must NOT carry
// usage (would cause double-count in the closure).
func TestProcessStreamEvent_ContentBlockDelta(t *testing.T) {
	data := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`
	chunk, usage := processStreamEvent(data)
	if chunk == nil || chunk.Content != "hello" {
		t.Errorf("expected content 'hello', got %+v", chunk)
	}
	if usage != nil {
		t.Errorf("content_block_delta must not carry usage; got %+v", usage)
	}
}

// TestProcessStreamEvent_MessageDelta — output_tokens lands here at end of
// stream. CacheCreation/Read remain 0 (those came in message_start).
func TestProcessStreamEvent_MessageDelta(t *testing.T) {
	data := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":503}}`
	_, usage := processStreamEvent(data)
	if usage == nil {
		t.Fatal("expected usage on message_delta")
	}
	if usage.OutputTokens != 503 {
		t.Errorf("output: got %d want 503", usage.OutputTokens)
	}
}

// TestProcessStreamEvent_MessageStop — terminator carries no payload, no
// usage. Returns chunk so the loop can keep iterating cleanly.
func TestProcessStreamEvent_MessageStop(t *testing.T) {
	data := `{"type":"message_stop"}`
	chunk, usage := processStreamEvent(data)
	if chunk == nil {
		t.Error("expected non-nil chunk on message_stop")
	}
	if usage != nil {
		t.Errorf("message_stop must not carry usage; got %+v", usage)
	}
}

// TestProcessStreamEvent_NonJSON — robustness against half-rendered SSE
// chunks. Returns nil/nil so the caller can fall through to error parser.
func TestProcessStreamEvent_NonJSON(t *testing.T) {
	chunk, usage := processStreamEvent("not json")
	if chunk != nil || usage != nil {
		t.Errorf("non-JSON: expected nil/nil, got chunk=%+v usage=%+v", chunk, usage)
	}
}
