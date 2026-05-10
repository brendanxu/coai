package openai

import (
	"bytes"
	adaptercommon "chat/adapter/common"
	"chat/globals"
	"encoding/json"
	"testing"
)

func TestFormatMessages_PassesCacheControl(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: globals.GPT3Turbo,
		Message: []globals.Message{
			{Role: "user", Content: globals.MessageContent{
				Blocks: []globals.ContentBlock{{
					Type: "text", Text: "long ctx",
					CacheControl: &globals.CacheControl{Type: "ephemeral", TTL: "5m"},
				}},
			}},
		},
	}
	out, _ := json.Marshal(formatMessages(props))
	if !bytes.Contains(out, []byte(`"cache_control"`)) {
		t.Errorf("marker stripped: %s", out)
	}
	if !bytes.Contains(out, []byte(`"ephemeral"`)) {
		t.Errorf("ephemeral type missing: %s", out)
	}
}

func TestFormatMessages_KeepsPlainStringPath(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: globals.GPT3Turbo,
		Message: []globals.Message{
			{Role: "user", Content: globals.MessageContent{Plain: "hello"}},
		},
	}
	out, _ := json.Marshal(formatMessages(props))
	if !bytes.Contains(out, []byte(`"content":"hello"`)) {
		t.Errorf("plain content shape changed: %s", out)
	}
}

func TestFormatMessages_PreservesImageURLBlock(t *testing.T) {
	props := &adaptercommon.ChatProps{
		Model: globals.GPT3Turbo,
		Message: []globals.Message{
			{Role: "user", Content: globals.MessageContent{
				Blocks: []globals.ContentBlock{{
					Type:     "image_url",
					ImageURL: &globals.ImageURL{URL: "https://example.com/a.png"},
				}},
			}},
		},
	}
	out, _ := json.Marshal(formatMessages(props))
	if !bytes.Contains(out, []byte(`"image_url"`)) {
		t.Errorf("image_url block lost: %s", out)
	}
	if !bytes.Contains(out, []byte(`"https://example.com/a.png"`)) {
		t.Errorf("image url lost: %s", out)
	}
}
