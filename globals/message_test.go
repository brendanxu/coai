package globals

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMessageContent_StringRoundTrip(t *testing.T) {
	in := []byte(`{"role":"user","content":"hello"}`)
	var m Message
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatal(err)
	}
	if m.Content.String() != "hello" {
		t.Errorf("got %q", m.Content.String())
	}
	out, _ := json.Marshal(m)
	if !bytes.Contains(out, []byte(`"content":"hello"`)) {
		t.Errorf("round-trip lost shape: %s", out)
	}
}

func TestMessageContent_ArrayRoundTrip(t *testing.T) {
	in := []byte(`{"role":"system","content":[{"type":"text","text":"ctx","cache_control":{"type":"ephemeral","ttl":"5m"}}]}`)
	var m Message
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Content.Blocks) != 1 {
		t.Fatalf("blocks=%d", len(m.Content.Blocks))
	}
	if m.Content.Blocks[0].CacheControl == nil {
		t.Fatal("cache_control lost")
	}
	if ttl, ok := m.Content.HasCacheControl(); !ok || ttl != "5m" {
		t.Errorf("ttl=%q ok=%v", ttl, ok)
	}
}

func TestMessageContent_DefaultCacheTTL(t *testing.T) {
	in := []byte(`{"role":"system","content":[{"type":"text","text":"ctx","cache_control":{"type":"ephemeral"}}]}`)
	var m Message
	if err := json.Unmarshal(in, &m); err != nil {
		t.Fatal(err)
	}
	if ttl, ok := m.Content.HasCacheControl(); !ok || ttl != "5m" {
		t.Errorf("ttl=%q ok=%v", ttl, ok)
	}
}

func TestMessageContent_RejectsInvalidShape(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":{"text":"bad"}}`), &m); err == nil {
		t.Fatal("expected invalid shape error")
	}
}
