// NewAPI channel admin — listing + classification.
//
// "Pool" is the greentokey-facing concept: the union of all enabled NewAPI
// channels surfaces as a list of available models with provenance. The
// marketing landing's "14 款现货 · 4 款在路上" grid pulls from this.
//
// Channel "type" in NewAPI is an integer identifying the upstream provider.
// We map type → (provider, label, is_sub2api) via a stable table. New
// channel types added by NewAPI upstream will fall through to "Unknown
// provider" until we extend the table — that's intentional, prevents us
// silently surfacing a channel type we haven't vetted.
//
// sub2API classification: NewAPI ships some channel types that bridge a
// subscription account (Claude Pro, ChatGPT Plus) to an OpenAI-compatible
// endpoint by reverse-engineering the web/mobile UI. These are riskier
// (account bans, UI drift) and we want the dashboard to badge them
// "sub2API · 中转" or similar so users understand the trade-off.

package newapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ProviderInfo describes how greentokey labels a NewAPI channel type.
type ProviderInfo struct {
	// Type is NewAPI's internal channel type id.
	Type int
	// Provider is the upstream brand we surface to users (e.g. "OpenAI").
	Provider string
	// Label is the Chinese-formatted badge shown on the model grid card
	// (e.g. "OpenAI · 中转" or "DeepSeek · 官方").
	Label string
	// IsSub2API marks channel types that wrap a subscription/web reverse
	// engineering — these get a "sub2API" badge in the dashboard and may
	// be deprioritized in routing.
	IsSub2API bool
}

// providerCatalog is the canonical type → label map for NewAPI v0.13.x.
//
// Types not listed here render as "Unknown" — operationally that means
// either NewAPI added a new channel type we haven't vetted, or the
// founder added a custom channel type via NewAPI's "Custom" category.
// In both cases we surface the type number so it can be added safely.
//
// Extend this table when a new channel type ships in NewAPI upstream and
// we want it surfaced in greentokey marketing/dashboard.
var providerCatalog = map[int]ProviderInfo{
	// === 官方/中转 channels (low-risk, standard API) ===
	1:  {Type: 1, Provider: "OpenAI", Label: "OpenAI · 中转"},
	14: {Type: 14, Provider: "Anthropic", Label: "Anthropic · 中转"},
	17: {Type: 17, Provider: "Aliyun", Label: "阿里 · 通义"},
	24: {Type: 24, Provider: "Google", Label: "Google · 中转"},
	25: {Type: 25, Provider: "Gemini", Label: "Google · 中转"},
	28: {Type: 28, Provider: "Mistral", Label: "Mistral · 中转"},
	33: {Type: 33, Provider: "AI360", Label: "360 · 智脑"},
	36: {Type: 36, Provider: "DeepSeek", Label: "DeepSeek"},
	38: {Type: 38, Provider: "Moonshot", Label: "月之暗面 · Kimi"},
	41: {Type: 41, Provider: "ChatGLM", Label: "智谱"},

	// === 自定义 channels — could be either side; treat as 中转 by default ===
	8: {Type: 8, Provider: "Custom", Label: "自定义"},

	// === sub2API / web2api channels (subscription-account reverse) ===
	// NewAPI v0.13.x type IDs known to wrap web auth flows. These get
	// the IsSub2API flag — UI shows a "sub2API · 实验" badge so the
	// trust profile is honest.
	//
	// As we onboard more sub2API channel types (they're added periodically
	// in NewAPI upstream), extend this section.
	39: {Type: 39, Provider: "MoonshotWeb", Label: "Kimi · sub2API", IsSub2API: true},
	44: {Type: 44, Provider: "ClaudeWeb", Label: "Claude · sub2API", IsSub2API: true},
	45: {Type: 45, Provider: "OpenAIWeb", Label: "ChatGPT · sub2API", IsSub2API: true},
}

// LookupProvider returns the catalog entry for a NewAPI channel type, or
// a synthetic "Unknown" record so callers can always render *something*.
func LookupProvider(channelType int) ProviderInfo {
	if info, ok := providerCatalog[channelType]; ok {
		return info
	}
	return ProviderInfo{
		Type:     channelType,
		Provider: "Unknown",
		Label:    fmt.Sprintf("未知 · type=%d", channelType),
	}
}

// channelRaw mirrors NewAPI's /api/channel item shape (the bits we use).
// JSON field names match NewAPI's response — we only define what we need.
type channelRaw struct {
	ID           int64  `json:"id"`
	Type         int    `json:"type"`
	Name         string `json:"name"`
	Status       int    `json:"status"` // 1=enabled, 2=disabled
	Models       string `json:"models"` // comma-separated
	ResponseTime int    `json:"response_time"`
	Weight       int    `json:"weight"`
	Priority     int    `json:"priority"`
	Group        string `json:"group"`
	BaseURL      string `json:"base_url"`
}

// ChannelInfo is greentokey's view of a NewAPI channel with provider
// labels resolved + models split into a slice. This is the type the
// /api/gtk/v1/pool endpoint serializes.
type ChannelInfo struct {
	ID            int64    `json:"id"`
	Type          int      `json:"type"`
	Provider      string   `json:"provider"`
	ProviderLabel string   `json:"provider_label"`
	Name          string   `json:"name"`
	Status        int      `json:"status"`
	Models        []string `json:"models"`
	ResponseTime  int      `json:"response_time_ms"`
	Weight        int      `json:"weight"`
	Priority      int      `json:"priority"`
	Group         string   `json:"group"`
	IsSub2API     bool     `json:"is_sub2api"`
}

// ListChannels fetches all channels from NewAPI admin REST and returns
// greentokey-shaped records with provider labels resolved.
//
// Pagination: NewAPI's /api/channel paginates; for v0.9 we pull a
// generous first page (page_size=200) which fits easily under both
// our typical channel count (<50) and NewAPI's request budget. If we
// ever exceed that, extend with full pagination.
func (c *Client) ListChannels(ctx context.Context) ([]ChannelInfo, error) {
	var env listEnvelope[channelRaw]
	if err := c.do(ctx, "GET", "/api/channel/?p=0&page_size=200", nil, 0, &env); err != nil {
		return nil, fmt.Errorf("list newapi channels: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: list channels: %s", env.Message)
	}
	out := make([]ChannelInfo, 0, len(env.Data.Items))
	for _, raw := range env.Data.Items {
		info := LookupProvider(raw.Type)
		out = append(out, ChannelInfo{
			ID:            raw.ID,
			Type:          raw.Type,
			Provider:      info.Provider,
			ProviderLabel: info.Label,
			Name:          raw.Name,
			Status:        raw.Status,
			Models:        splitCSV(raw.Models),
			ResponseTime:  raw.ResponseTime,
			Weight:        raw.Weight,
			Priority:      raw.Priority,
			Group:         raw.Group,
			IsSub2API:     info.IsSub2API,
		})
	}
	// Stable order: by Type ASC, then Name ASC. Keeps the dashboard grid
	// from re-shuffling on every page load.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
