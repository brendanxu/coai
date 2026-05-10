package claude

import (
	adaptercommon "chat/adapter/common"
	"chat/globals"
	"chat/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestE2E_AnthropicCacheFlow boots a mock Anthropic Messages API server
// and verifies the complete Path 1 chain end-to-end:
//
//	client cache_control marker → manager.MessageContent → claude adapter →
//	upstream HTTP body (with marker preserved) → upstream usage with
//	cache_creation/read fields → terminal Chunk{UpstreamUsage:} → caller
//
// Round 1 writes the cache (mock returns cache_creation_input_tokens > 0).
// Round 2 reads the cache (mock returns cache_read_input_tokens > 0).
// Verifies all 3 contracts the W1-W5 chain promised:
//
//	C1: cache_control marker in customer request reaches upstream verbatim
//	C2: upstream cache_creation / cache_read fields parsed into
//	    globals.UpstreamUsage with correct class assignment
//	C3: terminal callback delivers UpstreamUsage so Buffer.GetQuota
//	    can switch from tiktoken estimate to provider truth
func TestE2E_AnthropicCacheFlow(t *testing.T) {
	t.Run("round1_cache_write", func(t *testing.T) {
		runRound(t, "round1", 248, 0, 503)
	})
	t.Run("round2_cache_read", func(t *testing.T) {
		runRound(t, "round2", 0, 100000, 412)
	})
}

func runRound(t *testing.T, label string, cacheWriteTokens, cacheReadTokens, outputTokens int) {
	t.Helper()

	var capturedBody []byte
	var capturedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Anthropic Messages endpoint — match the suffix our adapter appends.
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			http.NotFound(w, r)
			return
		}
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("ResponseWriter is not a Flusher; httptest broken")
		}

		// message_start with input_tokens + cache_creation/read.
		writeJSON(w, fmt.Sprintf(
			`{"type":"message_start","message":{"id":"msg_%s","usage":{"input_tokens":50,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":1}}}`,
			label, cacheWriteTokens, cacheReadTokens,
		))
		flusher.Flush()

		// One content_block_delta with synthetic text.
		writeJSON(w, fmt.Sprintf(
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"reply from %s"}}`,
			label,
		))
		flusher.Flush()

		// message_delta with output_tokens (terminal usage info).
		writeJSON(w, fmt.Sprintf(
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":%d}}`,
			outputTokens,
		))
		flusher.Flush()

		// message_stop terminator.
		writeJSON(w, `{"type":"message_stop"}`)
		flusher.Flush()
	}))
	defer srv.Close()

	inst := NewChatInstance(srv.URL, "test-key-"+label)

	var capturedUsage *globals.UpstreamUsage
	var capturedText strings.Builder

	systemPrompt := strings.Repeat("Lorem ipsum dolor sit amet. ", 60) // ~1500 tokens of stand-in
	props := &adaptercommon.ChatProps{
		Model: "claude-sonnet-4-5",
		Message: []globals.Message{
			{
				Role: globals.System,
				Content: globals.MessageContent{
					Blocks: []globals.ContentBlock{{
						Type: "text",
						Text: systemPrompt,
						CacheControl: &globals.CacheControl{
							Type: "ephemeral",
							TTL:  "5m",
						},
					}},
				},
			},
			{
				Role:    globals.User,
				Content: globals.MessageContent{Plain: "hi from " + label},
			},
		},
	}

	err := inst.CreateStreamChatRequest(props, func(chunk *globals.Chunk) error {
		if chunk == nil {
			return nil
		}
		if chunk.UpstreamUsage != nil {
			capturedUsage = chunk.UpstreamUsage
		}
		capturedText.WriteString(chunk.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("CreateStreamChatRequest: %v", err)
	}

	// C1: cache_control marker preserved in upstream body.
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, `"cache_control"`) {
		t.Errorf("C1 broke — cache_control stripped from upstream body:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"ephemeral"`) {
		t.Errorf("C1 broke — ephemeral type missing:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"ttl":"5m"`) {
		t.Errorf("C1 broke — ttl=5m missing:\n%s", bodyStr)
	}

	// C1.1: system field serialised as typed array (not flat string) when
	// cache_control is present anywhere on system blocks.
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(capturedBody, &bodyMap); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if _, ok := bodyMap["system"].([]interface{}); !ok {
		t.Errorf("C1.1 broke — system field should be array when cache_control present, got %T",
			bodyMap["system"])
	}

	// C1.2: required Anthropic headers present.
	if got := capturedHeaders.Get("anthropic-version"); got == "" {
		t.Errorf("anthropic-version header missing: %v", capturedHeaders)
	}
	if got := capturedHeaders.Get("x-api-key"); got != "test-key-"+label {
		t.Errorf("x-api-key header wrong: got %q want %q", got, "test-key-"+label)
	}

	// C2: terminal UpstreamUsage emitted.
	if capturedUsage == nil {
		t.Fatal("C2 broke — no terminal UpstreamUsage chunk delivered")
	}

	// C2.1: cache_creation routed to CacheWriteTokens.
	if capturedUsage.CacheWriteTokens != cacheWriteTokens {
		t.Errorf("C2.1 broke — cache_write got %d want %d",
			capturedUsage.CacheWriteTokens, cacheWriteTokens)
	}

	// C2.2: cache_read routed to CacheReadTokens.
	if capturedUsage.CacheReadTokens != cacheReadTokens {
		t.Errorf("C2.2 broke — cache_read got %d want %d",
			capturedUsage.CacheReadTokens, cacheReadTokens)
	}

	// C2.3: output tokens preserved.
	if capturedUsage.OutputTokens != outputTokens {
		t.Errorf("C2.3 broke — output got %d want %d",
			capturedUsage.OutputTokens, outputTokens)
	}

	// C3: text content surfaced too (sanity that terminal chunk didn't
	// stomp the content stream).
	if !strings.Contains(capturedText.String(), "reply from "+label) {
		t.Errorf("C3 broke — content stream lost reply text: %q", capturedText.String())
	}

	// C4 (W3+W4 integration): drop a Buffer in front and verify GetQuota
	// flips to upstream-aware billing the moment we record the usage.
	chargeFake := &chargeStub{
		btype:        globals.TokenBilling,
		input:        0.04, // per 1k tokens, ¥
		output:       0.20,
		cacheRead:    0.005,  // operator-passed Anthropic 0.1× discount
		cacheWrite5m: 0.05,   // operator-passed Anthropic 1.25× upstream
		cacheWrite1h: 0.08,   // operator-passed Anthropic 2.0× upstream
	}
	buf := &utils.Buffer{Charge: chargeFake, PreferredCacheTTL: "5m"}
	buf.RecordUpstreamUsage(capturedUsage)

	if buf.Upstream == nil {
		t.Fatal("C4 broke — Buffer.RecordUpstreamUsage didn't store payload")
	}
	if buf.Upstream.CacheTTL != "5m" {
		t.Errorf("C4 broke — PreferredCacheTTL didn't stamp Upstream.CacheTTL; got %q", buf.Upstream.CacheTTL)
	}

	// CountUpstreamQuota math, hand-computed for traceability:
	//   un-cached input * 0.04/1k + output * 0.20/1k
	//     + cache_read * 0.005/1k + cache_write * 0.05/1k (5m TTL)
	expected := float32(50)/1000*0.04 +
		float32(outputTokens)/1000*0.20 +
		float32(cacheReadTokens)/1000*0.005 +
		float32(cacheWriteTokens)/1000*0.05
	got := buf.GetQuota()
	tol := float32(1e-5)
	if got-expected > tol || expected-got > tol {
		t.Errorf("C4 broke — GetQuota math drift; got %f want %f", got, expected)
	}
}

// writeJSON emits one SSE event line. The legacy scanner ignores
// `event:` prefixes, so we only emit `data:` lines + the blank
// terminator the scanner uses to split events.
func writeJSON(w io.Writer, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// chargeStub mirrors channel.Charge for test billing. Channel/types pulls in
// viper config; isolating with a stub keeps the test pure-Go and parallel-
// safe.
type chargeStub struct {
	btype                                  string
	input, output                          float32
	cacheRead, cacheWrite5m, cacheWrite1h  float32
}

var _ utils.Charge = (*chargeStub)(nil)

func (c *chargeStub) GetType() string             { return c.btype }
func (c *chargeStub) GetModels() []string         { return nil }
func (c *chargeStub) GetInput() float32           { return c.input }
func (c *chargeStub) GetOutput() float32          { return c.output }
func (c *chargeStub) GetCacheRead() float32 {
	if c.cacheRead <= 0 {
		return c.input
	}
	return c.cacheRead
}
func (c *chargeStub) GetCacheWrite5m() float32 {
	if c.cacheWrite5m <= 0 {
		return c.input * 1.25
	}
	return c.cacheWrite5m
}
func (c *chargeStub) GetCacheWrite1h() float32 {
	if c.cacheWrite1h <= 0 {
		return c.input * 2.0
	}
	return c.cacheWrite1h
}
func (c *chargeStub) SupportAnonymous() bool { return false }
func (c *chargeStub) IsBilling() bool        { return c.btype != globals.NonBilling }
func (c *chargeStub) IsBillingType(t string) bool { return c.btype == t }
func (c *chargeStub) GetLimit() float32      { return 0 }
