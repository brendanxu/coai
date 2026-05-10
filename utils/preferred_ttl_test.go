package utils

import (
	"chat/globals"
	"testing"
)

func TestPreferredCacheTTL_DetectsMarker(t *testing.T) {
	ttl := preferredCacheTTL([]globals.Message{
		{Role: globals.User, Content: globals.MessageContent{Blocks: []globals.ContentBlock{{
			Type:         "text",
			Text:         "ctx",
			CacheControl: &globals.CacheControl{Type: "ephemeral", TTL: "1h"},
		}}}},
	})

	if ttl != "1h" {
		t.Errorf("ttl=%q want 1h", ttl)
	}
}

func TestPreferredCacheTTL_DefaultsEmptyTTLTo5m(t *testing.T) {
	ttl := preferredCacheTTL([]globals.Message{
		{Role: globals.System, Content: globals.MessageContent{Blocks: []globals.ContentBlock{{
			Type:         "text",
			Text:         "ctx",
			CacheControl: &globals.CacheControl{Type: "ephemeral"},
		}}}},
	})

	if ttl != "5m" {
		t.Errorf("ttl=%q want 5m", ttl)
	}
}

func TestRecordUpstreamUsage_StampsTTL(t *testing.T) {
	b := &Buffer{PreferredCacheTTL: "1h"}
	b.RecordUpstreamUsage(&globals.UpstreamUsage{InputTokens: 100, CacheWriteTokens: 50})
	if b.Upstream.CacheTTL != "1h" {
		t.Errorf("ttl=%q want 1h", b.Upstream.CacheTTL)
	}
}

func TestRecordUpstreamUsage_DoesntOverwrite(t *testing.T) {
	b := &Buffer{PreferredCacheTTL: "5m"}
	b.RecordUpstreamUsage(&globals.UpstreamUsage{CacheTTL: "1h", InputTokens: 100})
	if b.Upstream.CacheTTL != "1h" {
		t.Errorf("buffer overrode adapter-supplied ttl: got %q", b.Upstream.CacheTTL)
	}
}
