package claude

import (
	"bytes"
	adaptercommon "chat/adapter/common"
	"chat/globals"
	"encoding/json"
	"testing"
)

func TestGetChatBody_PassesCacheControl(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: "claude-sonnet-4-5",
		Message: []globals.Message{
			{Role: "system", Content: globals.MessageContent{
				Blocks: []globals.ContentBlock{{
					Type: "text", Text: "long ctx",
					CacheControl: &globals.CacheControl{Type: "ephemeral", TTL: "5m"},
				}},
			}},
			{Role: "user", Content: globals.MessageContent{Plain: "hi"}},
		},
	}
	c := &ChatInstance{}
	body := c.GetChatBody(props, true)
	out, _ := json.Marshal(body)
	if !bytes.Contains(out, []byte(`"cache_control"`)) {
		t.Errorf("marker stripped: %s", out)
	}
	if !bytes.Contains(out, []byte(`"ephemeral"`)) {
		t.Errorf("ephemeral type missing: %s", out)
	}
}

func TestGetMessages_PassesContentBlockCacheControl(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: "claude-sonnet-4-5",
		Message: []globals.Message{
			{Role: "user", Content: globals.MessageContent{
				Blocks: []globals.ContentBlock{{
					Type: "text", Text: "long ctx",
					CacheControl: &globals.CacheControl{Type: "ephemeral", TTL: "1h"},
				}},
			}},
		},
	}
	c := &ChatInstance{}
	out, _ := json.Marshal(c.GetMessages(props))
	if !bytes.Contains(out, []byte(`"cache_control"`)) {
		t.Errorf("marker stripped: %s", out)
	}
	if !bytes.Contains(out, []byte(`"ttl":"1h"`)) {
		t.Errorf("ttl missing: %s", out)
	}
}

func TestGetSystemPrompt_PlainWithoutCacheControl(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: "claude-sonnet-4-5",
		Message: []globals.Message{
			{Role: "system", Content: globals.MessageContent{Plain: "plain ctx"}},
		},
	}
	c := &ChatInstance{}
	if got, ok := c.GetSystemPrompt(props).(string); !ok || got != "plain ctx" {
		t.Fatalf("system=%#v ok=%v", got, ok)
	}
}
