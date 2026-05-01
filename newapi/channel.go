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

// providerCatalog is the canonical type → label map.
//
// SOURCE OF TRUTH: NewAPI v0.13.1 `constant/channel.go` from upstream
// QuantumNous/new-api repo, fetched 2026-04-30. Every entry below is
// cross-referenced to that file. Types omitted from the catalog are
// types we don't expect in greentokey's marketing surface (deeply
// niche providers, image/audio/video-only providers like SunoAPI/Jina
// — they still render via the Unknown fallback if a channel of that
// type is created).
//
// PRIOR BUG (fixed 2026-04-30): the initial v0.9 catalog had several
// type IDs guessed wrong (e.g. type 36 was tagged DeepSeek when it's
// actually SunoAPI; types 39/44/45 were tagged sub2API when they're
// Cloudflare/MokaAI/VolcEngine respectively). This rewrite corrects
// all of them to match NewAPI source. See docs/strategy/newapi-
// reference.md for the full v0.13.1 type table.
var providerCatalog = map[int]ProviderInfo{
	// === Standard API channels (LLM chat-completion-grade) ===
	// "中转" = third-party relay aggregator pricing; "官方" = direct provider.
	1:  {Type: 1, Provider: "OpenAI", Label: "OpenAI · 中转"},
	3:  {Type: 3, Provider: "Azure", Label: "Azure OpenAI"},
	4:  {Type: 4, Provider: "Ollama", Label: "Ollama · 本地"},
	8:  {Type: 8, Provider: "Custom", Label: "自定义"}, // ← also THE sub2API path; see name-based override below
	14: {Type: 14, Provider: "Anthropic", Label: "Anthropic · 中转"},
	15: {Type: 15, Provider: "Baidu", Label: "百度 · 千帆 v1"},
	16: {Type: 16, Provider: "Zhipu", Label: "智谱 v1"},
	17: {Type: 17, Provider: "Ali", Label: "阿里 · 通义"},
	18: {Type: 18, Provider: "Xunfei", Label: "讯飞 · 星火"},
	19: {Type: 19, Provider: "AI360", Label: "360 · 智脑"},
	20: {Type: 20, Provider: "OpenRouter", Label: "OpenRouter · 聚合"},
	23: {Type: 23, Provider: "Tencent", Label: "腾讯 · 混元"},
	24: {Type: 24, Provider: "Gemini", Label: "Google · Gemini"},
	25: {Type: 25, Provider: "Moonshot", Label: "月之暗面 · Kimi"},
	26: {Type: 26, Provider: "ZhipuV4", Label: "智谱 v4"},
	27: {Type: 27, Provider: "Perplexity", Label: "Perplexity"},
	31: {Type: 31, Provider: "LingYiWanWu", Label: "零一万物"},
	33: {Type: 33, Provider: "AWS", Label: "AWS · Bedrock"},
	34: {Type: 34, Provider: "Cohere", Label: "Cohere"},
	35: {Type: 35, Provider: "MiniMax", Label: "MiniMax"},
	37: {Type: 37, Provider: "Dify", Label: "Dify"},
	39: {Type: 39, Provider: "Cloudflare", Label: "Cloudflare · Workers AI"},
	40: {Type: 40, Provider: "SiliconFlow", Label: "SiliconFlow · 硅基流动"},
	41: {Type: 41, Provider: "VertexAi", Label: "Google · Vertex AI"},
	42: {Type: 42, Provider: "Mistral", Label: "Mistral"},
	43: {Type: 43, Provider: "DeepSeek", Label: "DeepSeek · 官方"}, // ← 43, NOT 36!
	45: {Type: 45, Provider: "VolcEngine", Label: "火山 · 豆包"},
	46: {Type: 46, Provider: "BaiduV2", Label: "百度 · 千帆 v2"},
	47: {Type: 47, Provider: "Xinference", Label: "Xinference · 自部署"},
	48: {Type: 48, Provider: "Xai", Label: "xAI · Grok"},
	49: {Type: 49, Provider: "Coze", Label: "Coze"},

	// === sub2API channels — known/suspected ===
	// Type 57 (Codex) added in v0.13.1, base_url defaults to "chatgpt.com"
	// (NOT api.openai.com). Strong signal that this is the canonical
	// ChatGPT-Plus → API reverse channel. Marking IsSub2API; if it turns
	// out to be a different concept on hands-on test, drop the flag.
	57: {Type: 57, Provider: "Codex", Label: "ChatGPT · Codex (sub2API)", IsSub2API: true},

	// === Image / video / audio gen channels (NOT in our 3-tier credit pricing) ===
	// Listed for label completeness so PoolSnapshot doesn't render
	// "Unknown · type=N" for a creative-media channel — but greentokey
	// won't expose these via the standard chat 套餐 grid.
	2:  {Type: 2, Provider: "Midjourney", Label: "Midjourney"},
	5:  {Type: 5, Provider: "MidjourneyPlus", Label: "Midjourney Plus"},
	36: {Type: 36, Provider: "SunoAPI", Label: "Suno · 音乐"},
	38: {Type: 38, Provider: "Jina", Label: "Jina · embeddings"},
	50: {Type: 50, Provider: "Kling", Label: "可灵 · 视频"},
	51: {Type: 51, Provider: "Jimeng", Label: "即梦 · 视频"},
	52: {Type: 52, Provider: "Vidu", Label: "Vidu · 视频"},
	54: {Type: 54, Provider: "DoubaoVideo", Label: "豆包 · 视频"},
	55: {Type: 55, Provider: "Sora", Label: "OpenAI · Sora"},
	56: {Type: 56, Provider: "Replicate", Label: "Replicate"},
}

// sub2APINamePatterns marks a channel as sub2API based on its name —
// independent of channel type. This catches the most common sub2API
// implementation (channel type 8 Custom + naming convention) since
// we cannot tell from type=8 alone whether the channel is wrapping a
// PandoraNext / chatgpt-mirror / claude-web microservice or a plain
// custom OpenAI-compatible endpoint.
//
// CONVENTION: when adding a sub2API channel via type=8 (Custom), name
// it with one of these markers and the dashboard will badge it
// correctly:
//
//	 sub2api / sub2-...               (most explicit)
//	 pandora / chatgpt-mirror         (well-known reverse projects)
//	 chatgpt-web / claude-web /
//	 gpt-web / claude-pro             (web-account reverse hints)
var sub2APINamePatterns = []string{
	"sub2api",
	"sub2-",
	"pandora",
	"chatgpt-mirror",
	"chatgpt-web",
	"claude-web",
	"gpt-web",
	"claude-pro",
}

// LookupProvider returns the catalog entry for a NewAPI channel type.
// For unknown types, returns a synthetic "Unknown" record so callers
// can always render *something* (the dashboard surfaces the type
// number so the founder can spot a new NewAPI channel type that
// needs catalog extension).
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

// classifyChannel resolves the final ProviderInfo for a channel given
// both its type AND its name. Name-based sub2API overrides type-based
// classification — so a type=8 (Custom) channel named "claude-pro-1"
// gets flagged as sub2API even though type 8 by itself is just "Custom".
//
// Resolution order:
//  1. type → catalog lookup
//  2. if name matches sub2APINamePatterns → upgrade IsSub2API to true
//     and append " · sub2API" to label so the badge is visible
func classifyChannel(channelType int, name string) ProviderInfo {
	info := LookupProvider(channelType)
	if !info.IsSub2API && isSub2APIByName(name) {
		info.IsSub2API = true
		info.Label = info.Label + " · sub2API"
	}
	return info
}

// isSub2APIByName checks the channel name against known sub2API
// markers. Case-insensitive substring match.
func isSub2APIByName(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range sub2APINamePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
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
		// Name-based sub2API override happens here: a type=8 Custom
		// channel named "claude-pro-1" gets flagged sub2API; same type
		// named "openrouter-mirror" stays standard.
		info := classifyChannel(raw.Type, raw.Name)
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
