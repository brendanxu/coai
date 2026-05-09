// Package plans struct definitions. Pure data carriers — no methods, no
// validation, no persistence helpers. Sprint 4 (payment) and Sprint 3 (BYOK
// cost cap) will add DAO + service layers; this dispatch (T2.3) ships only
// the schema + scan-friendly Go types.
package plans

import (
	"database/sql"
	"time"
)

// Plan mirrors a row in gtk_plan. quota_config / Type / IsActive are kept as
// raw scan types (sql.NullString, string, bool) so callers decide how to
// validate ENUM values and parse JSON. Keeps this file zero-dependency.
type Plan struct {
	ID           int64
	Code         string
	Name         string
	Type         string         // 'subscription' | 'pack' (validated at write site, not here)
	PriceCents   int64
	DurationDays int64
	QuotaConfig  sql.NullString // raw JSON; parsed by callers
	IsActive     bool
	CreatedAt    time.Time
}

// UserPlan mirrors a row in gtk_user_plan. ExpireAt + Remaining are nullable;
// scan into the sql.Null* zero-values when absent.
type UserPlan struct {
	ID          int64
	UserID      int64
	PlanID      int64
	Status      string         // 'active' | 'expired' | 'canceled'
	ExpireAt    sql.NullTime
	Remaining   sql.NullString // raw JSON; parsed by callers
	PurchasedAt time.Time
	OrderID     string         // 'ls_<...>' or 'xhp_<...>' per PROJECT_BRIEF §"💳 支付"
}

// AppUsageLog mirrors a row in gtk_app_usage_log. PlanID is nullable for
// usage-without-active-plan scenarios (trials, pay-as-you-go, anonymous
// service-tier prototypes).
//
// V2 fields (2026-05-10) split tokens by class so billing never gets stuck
// at the "tokens_used 50/50 fake split" failure mode that usage/aggregator
// flagged. The legacy TokensUsed + CostCents fields are kept for
// backward-compatibility with existing readers and are written alongside
// the V2 fields by the billing calculator (TokensUsed = sum of all four
// token classes; CostCents = ClientChargeMicro / 10000).
type AppUsageLog struct {
	ID         int64
	UserID     int64
	PlanID     sql.NullInt64
	Service    string
	TokensUsed int64 // legacy: sum of input + output + cache_write + cache_read
	CostCents  int64 // legacy: ClientChargeMicro / 10000 (whole cents only)
	CreatedAt  time.Time

	// V2 cache-aware fields. ModelID + Provider identify the upstream model
	// (e.g. "claude-sonnet-4.5", "anthropic"). The four token counts come
	// straight off the upstream usage payload; non-cache providers leave
	// CacheWriteTokens + CacheReadTokens at 0.
	ModelID           string
	Provider          string
	InputTokens       int64
	OutputTokens      int64
	CacheWriteTokens  int64
	CacheReadTokens   int64
	CacheTTL          string // '5m' | '1h' | '' when not cache-applicable
	UpstreamCostMicro int64  // 1e-6 USD; what we paid the provider
	ClientChargeMicro int64  // 1e-6 USD; what the customer was charged
	MarkupMultiplier  float64
}

// ProviderPricing mirrors a row in gtk_provider_pricing. Owned by ops via
// the V2 seed in migration.go and any subsequent INSERT (never UPDATE) when
// upstream prices change.
type ProviderPricing struct {
	ID            int64
	Provider      string
	ModelID       string
	TokenType     string // 'input'|'output'|'cache_write_5m'|'cache_write_1h'|'cache_read'
	UpstreamPerM  float64
	EffectiveFrom time.Time
	Notes         sql.NullString
}

// BillingConfig mirrors a row in gtk_billing_config. Single-table key/value
// store; today's only key is 'markup_multiplier'.
type BillingConfig struct {
	K string
	V string
}
