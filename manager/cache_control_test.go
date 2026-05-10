package manager

import (
	"encoding/json"
	"testing"
)

func TestTransformContent_PreservesCacheControl(t *testing.T) {
	var raw interface{}
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"ctx","cache_control":{"type":"ephemeral","ttl":"1h"}}]`), &raw); err != nil {
		t.Fatal(err)
	}

	content := transformContent(raw)
	if len(content.Blocks) != 1 {
		t.Fatalf("blocks=%d", len(content.Blocks))
	}
	if content.Blocks[0].CacheControl == nil {
		t.Fatal("cache_control lost")
	}
	if ttl, ok := content.HasCacheControl(); !ok || ttl != "1h" {
		t.Errorf("ttl=%q ok=%v", ttl, ok)
	}
}

func TestTransformContent_PreservesPlainString(t *testing.T) {
	content := transformContent("hello")
	if content.Plain != "hello" || len(content.Blocks) != 0 {
		t.Errorf("content=%+v", content)
	}
}
