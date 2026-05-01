// Pool aggregation — turns NewAPI's per-channel records into greentokey's
// "model pool" view. This is what the marketing landing's "14 款现货" grid
// and the dashboard's "模型池实时" widget consume.
//
// Aggregation rules:
//   * One model can be served by multiple channels (e.g. gpt-4o via
//     channel #1 official and channel #5 sub2API). The pool surfaces the
//     model once with its preferred provider (highest priority + lowest
//     latency among enabled channels for that model). Other-channel
//     entries fall through into ChannelDetails for the dashboard's
//     channel-detail view.
//   * Disabled channels (status != 1) are EXCLUDED from the model count
//     but INCLUDED in ChannelDetails so the dashboard can show "+3 在路上".
//   * sub2API ranking demoted: when a model has both an official-API and
//     a sub2API channel, official wins as the primary; sub2API surfaces
//     as a fallback / opt-in.

package newapi

import (
	"context"
	"sort"
	"sync"
	"time"
)

// ModelEntry is one row in the unified model pool. Each unique model id
// appears exactly once; ProviderLabel reflects the *primary* channel
// serving it (the highest-priority, lowest-latency enabled channel).
type ModelEntry struct {
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	ProviderLabel string `json:"provider_label"`
	IsSub2API     bool   `json:"is_sub2api"`
	// AverageLatencyMs is the latency of the primary channel for this
	// model (NewAPI's response_time field). 0 if not measured yet.
	AverageLatencyMs int `json:"average_latency_ms"`
	// CreditTier is greentokey's pricing tier ("light"/"standard"/"premium")
	// for this model — surfaced to the dashboard so users see "1 次 ≈
	// 1 credit" or "1 次 ≈ 3 credits" badge per model.
	CreditTier      string  `json:"credit_tier"`
	CreditTierLabel string  `json:"credit_tier_label"`  // "轻量" / "标准" / "高级"
	CreditPerCall   float64 `json:"credit_per_call"`    // 0.5 / 1.0 / 3.0
}

// PoolSnapshot is the high-level view consumed by /api/gtk/v1/pool.
//
// Marketing landing reads:
//   - TotalModels, EnabledChannels for "14 款现货"
//   - ComingSoon count for "+4 款在路上"
//   - AvgLatencyMs for "平均延迟 482ms"
//   - Channels[] for the model grid with per-card provider label
//
// Dashboard reads:
//   - Same plus per-channel response_time history (separate endpoint
//     when implemented; v0.9 uses just current snapshot).
type PoolSnapshot struct {
	GeneratedAt      time.Time     `json:"generated_at"`
	TotalModels      int           `json:"total_models"`
	EnabledChannels  int           `json:"enabled_channels"`
	ComingSoon       int           `json:"coming_soon"`
	AvgLatencyMs     int           `json:"avg_latency_ms"`
	Models           []ModelEntry  `json:"models"`
	Channels         []ChannelInfo `json:"channels"`
}

// pool snapshot cache — refreshing on every page load is too aggressive
// (admin API to NewAPI on every visitor request), and channel state changes
// at minute-scale not millisecond-scale. 30s TTL is the right balance.
var (
	cachedSnapshot     *PoolSnapshot
	cachedSnapshotMu   sync.RWMutex
	cachedSnapshotTime time.Time
	cacheTTL           = 30 * time.Second
)

// GetPoolSnapshot returns the current pool snapshot, refreshing from
// NewAPI when the cache is stale. Returns ErrNotConfigured if the
// admin token isn't set — callers degrade gracefully.
//
// Thread-safe: the read path takes RLock and may serve stale data
// while another goroutine is refreshing. Acceptable trade-off for
// a dashboard widget vs the alternative (single-flighted refreshes
// add complexity that doesn't pay off until we have many concurrent
// callers).
func GetPoolSnapshot(ctx context.Context) (*PoolSnapshot, error) {
	cachedSnapshotMu.RLock()
	snap := cachedSnapshot
	stale := time.Since(cachedSnapshotTime) > cacheTTL
	cachedSnapshotMu.RUnlock()
	if snap != nil && !stale {
		return snap, nil
	}

	cli, err := Default()
	if err != nil {
		return nil, err
	}

	channels, err := cli.ListChannels(ctx)
	if err != nil {
		// On NewAPI transient error: serve stale cache if we have one.
		if snap != nil {
			return snap, nil
		}
		return nil, err
	}

	fresh := buildSnapshot(channels)

	cachedSnapshotMu.Lock()
	cachedSnapshot = fresh
	cachedSnapshotTime = time.Now()
	cachedSnapshotMu.Unlock()

	return fresh, nil
}

// buildSnapshot is the pure aggregation function; testable without
// NewAPI in the loop. Given a channel list, it computes:
//   - the unified model list (deduplicated, primary-channel attribution)
//   - per-channel info (passthrough)
//   - aggregate counts + average latency
func buildSnapshot(channels []ChannelInfo) *PoolSnapshot {
	snap := &PoolSnapshot{
		GeneratedAt: time.Now(),
		Channels:    channels,
	}

	// Sum latencies + count enabled channels; identify "coming soon" as
	// disabled channels (operator marked them inactive but they're in
	// the catalog — surface them as roadmap badges).
	var latencySum, latencyCount int
	for _, ch := range channels {
		if ch.Status == 1 {
			snap.EnabledChannels++
		} else {
			snap.ComingSoon++
		}
		if ch.ResponseTime > 0 && ch.Status == 1 {
			latencySum += ch.ResponseTime
			latencyCount++
		}
	}
	if latencyCount > 0 {
		snap.AvgLatencyMs = latencySum / latencyCount
	}

	// Build the unified model list. Each model gets the "best" channel
	// as its primary (best = highest Priority, lowest ResponseTime).
	// Disabled channels are skipped — they don't add to the pool.
	type pick struct {
		ChannelInfo
		modelName string
	}
	picks := map[string]*pick{}
	for _, ch := range channels {
		if ch.Status != 1 {
			continue
		}
		for _, m := range ch.Models {
			cur, exists := picks[m]
			if !exists {
				picks[m] = &pick{ChannelInfo: ch, modelName: m}
				continue
			}
			// Prefer non-sub2API > higher Priority > lower ResponseTime
			better := false
			switch {
			case cur.IsSub2API && !ch.IsSub2API:
				better = true
			case !cur.IsSub2API && ch.IsSub2API:
				better = false
			case ch.Priority > cur.Priority:
				better = true
			case ch.Priority == cur.Priority && ch.ResponseTime > 0 &&
				(cur.ResponseTime == 0 || ch.ResponseTime < cur.ResponseTime):
				better = true
			}
			if better {
				picks[m] = &pick{ChannelInfo: ch, modelName: m}
			}
		}
	}

	models := make([]ModelEntry, 0, len(picks))
	for _, p := range picks {
		tier := CreditTierFor(p.modelName)
		var tierKey string
		switch tier {
		case TierLight:
			tierKey = "light"
		case TierStandard:
			tierKey = "standard"
		case TierPremium:
			tierKey = "premium"
		}
		models = append(models, ModelEntry{
			Model:            p.modelName,
			Provider:         p.Provider,
			ProviderLabel:    p.ProviderLabel,
			IsSub2API:        p.IsSub2API,
			AverageLatencyMs: p.ResponseTime,
			CreditTier:       tierKey,
			CreditTierLabel:  tier.Label(),
			CreditPerCall:    tier.Multiplier(),
		})
	}
	// Stable order: by provider then model name. Dashboard grid gets a
	// predictable layout per refresh.
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Model < models[j].Model
	})
	snap.Models = models
	snap.TotalModels = len(models)
	return snap
}
