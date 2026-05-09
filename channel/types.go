package channel

import (
	"chat/globals"
)

type Channel struct {
	Id            int                 `json:"id" mapstructure:"id"`
	Name          string              `json:"name" mapstructure:"name"`
	Type          string              `json:"type" mapstructure:"type"`
	Priority      int                 `json:"priority" mapstructure:"priority"`
	Weight        int                 `json:"weight" mapstructure:"weight"`
	Models        []string            `json:"models" mapstructure:"models"`
	Retry         int                 `json:"retry" mapstructure:"retry"`
	Secret        string              `json:"secret" mapstructure:"secret"`
	Endpoint      string              `json:"endpoint" mapstructure:"endpoint"`
	Mapper        string              `json:"mapper" mapstructure:"mapper"`
	State         bool                `json:"state" mapstructure:"state"`
	Group         []string            `json:"group" mapstructure:"group"`
	Proxy         globals.ProxyConfig `json:"proxy" mapstructure:"proxy"`
	Reflect       *map[string]string  `json:"-"`
	HitModels     *[]string           `json:"-"`
	ExcludeModels *[]string           `json:"-"`
	CurrentSecret *string             `json:"-"`
}

type Sequence []*Channel

type Manager struct {
	Sequence          Sequence            `json:"sequence"`
	PreflightSequence map[string]Sequence `json:"preflight_sequence"`
	Models            []string            `json:"models"`
}

type Ticker struct {
	Sequence Sequence `json:"sequence"`
	Cursor   int      `json:"cursor"`
}

type Charge struct {
	Id        int      `json:"id" mapstructure:"id"`
	Type      string   `json:"type" mapstructure:"type"`
	Models    []string `json:"models" mapstructure:"models"`
	Input     float32  `json:"input" mapstructure:"input"`
	Output    float32  `json:"output" mapstructure:"output"`
	Anonymous bool     `json:"anonymous" mapstructure:"anonymous"`

	// CacheRead / CacheWrite5m / CacheWrite1h are the unit prices per 1k
	// cache-read / 5m-cache-write / 1h-cache-write tokens. When the
	// adapter (utils/buffer.go::Buffer.RecordUpstreamUsage) hands us
	// provider-truth UpstreamUsage, the billing layer multiplies each
	// class by its configured rate independently — that's the
	// "we never lose money" rule (M ≥ 1.0 per token class) from
	// docs/research/token-cache-AUDIT-and-billing-design.md §2.4.
	//
	// Unset (0) values fall back to Input in GetCacheRead/Write so an
	// outdated config never under-charges the customer. To pass cache
	// savings to the customer, operators set these explicitly to the
	// upstream multiplier × markup (e.g. CacheRead = Input × 0.13 for
	// Anthropic with markup=1.30: 0.1× upstream × 1.30 markup).
	CacheRead    float32 `json:"cache_read,omitempty"     mapstructure:"cache_read,omitempty"`
	CacheWrite5m float32 `json:"cache_write_5m,omitempty" mapstructure:"cache_write_5m,omitempty"`
	CacheWrite1h float32 `json:"cache_write_1h,omitempty" mapstructure:"cache_write_1h,omitempty"`

	Unset bool `json:"-" mapstructure:"-"`
}

type ChargeSequence []*Charge

type ChargeManager struct {
	Sequence         ChargeSequence     `json:"sequence"`
	Models           map[string]*Charge `json:"models"`
	NonBillingModels []string           `json:"non_billing_models"`
}
