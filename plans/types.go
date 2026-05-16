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
//
// PKG-1 (v0.17, L23): ProductType/BillingMode/QuotaGrant/ServiceID added.
// ProductType is the discriminator the shared commerce backbone (PKG-2)
// dispatches on. BillingMode is the finer-grained successor to the legacy
// Type field. QuotaGrant is a typed shortcut for the common "credits per
// period" case (parallel to QuotaConfig JSON). ServiceID nullable-FK
// links service-product plans to gtk_service rows.
type Plan struct {
	ID           int64
	Code         string
	Name         string
	Type         string         // legacy: 'subscription' | 'pack' (kept for back-compat)
	ProductType  string         // 'token' | 'service' — L23 discriminator
	BillingMode  string         // 'subscription' | 'one_time' | 'top_up' | 'manual'
	PriceCents   int64
	DurationDays int64
	QuotaGrant   sql.NullInt64  // typed credits-per-period shortcut
	ServiceID    sql.NullInt64  // FK to gtk_service for product_type='service'
	QuotaConfig  sql.NullString // raw JSON; parsed by callers
	IsActive     bool
	CreatedAt    time.Time
}

// UserPlan mirrors a row in gtk_user_plan. ExpireAt + Remaining are nullable;
// scan into the sql.Null* zero-values when absent.
//
// PKG-1: ProductType + CancellationReason added. ProductType inherited from
// Plan at write time so per-user filters don't need a join. CancellationReason
// captures audit-friendly free-text on status flip ('refund_full',
// 'cancel_at_period_end', 'admin_revoke', etc.).
type UserPlan struct {
	ID                 int64
	UserID             int64
	PlanID             int64
	SubscriptionID     sql.NullInt64  // LS subscription id for renewal chain (PKG-M1-①)
	ProductType        string         // 'token' | 'service' — inherited from Plan
	Status             string         // 'active' | 'expired' | 'canceled'
	CancellationReason sql.NullString // free-text reason when status != 'active'
	ExpireAt           sql.NullTime
	Remaining          sql.NullString // raw JSON; parsed by callers
	PurchasedAt        time.Time
	OrderID            string         // 'ls_<...>' or 'xhp_<...>' per PROJECT_BRIEF §"💳 支付"
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
//
// PKG-1: Source / OrderID added. Source classifies the call origin so revenue
// attribution can split chat-driven vs service-order usage. OrderID links to
// gtk_service_order.order_no when source='service_order'. (Provider was also
// added by PKG-1, but it duplicates the V2 Provider field below — merged into
// the V2 field as a single non-nullable string. Adapter path always sets a
// concrete provider value after parsing upstream usage; service_order path
// can fall back to '' which is fine for the per-provider GROUP BY.)
type AppUsageLog struct {
	ID         int64
	UserID     int64
	PlanID     sql.NullInt64
	Service    string
	Source     string         // 'chat' | 'api' | 'service_order' | 'admin_test' (PKG-1)
	OrderID    sql.NullString // gtk_service_order.order_no when applicable (PKG-1)
	TokensUsed int64          // legacy: sum of input + output + cache_write + cache_read
	CostCents  int64          // legacy: ClientChargeMicro / 10000 (whole cents only)
	CreatedAt  time.Time

	// V2 cache-aware fields. ModelID + Provider identify the upstream model
	// (e.g. "claude-sonnet-4.5", "anthropic"). The four token counts come
	// straight off the upstream usage payload; non-cache providers leave
	// CacheWriteTokens + CacheReadTokens at 0.
	//
	// Note: Provider here merges PKG-1's Provider sql.NullString — adapter
	// always sets a concrete value, so non-nullable string is sufficient.
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
//
// PKG-PRICING-DYNAMIC (2026-05-15): seven nullable display columns added so
// the public /gtk/v1/pricing endpoint can serve Pricing.tsx from DB instead
// of a hardcoded array. Rows with NULL display fields are upstream-tracking
// only and never appear in the public response (all-or-nothing rule).
type ProviderPricing struct {
	ID            int64
	Provider      string
	ModelID       string
	TokenType     string // 'input'|'output'|'cache_write_5m'|'cache_write_1h'|'cache_read'
	UpstreamPerM  float64
	EffectiveFrom time.Time
	Notes         sql.NullString

	// Display columns (all nullable — NULL = not published to public Pricing page).
	DisplayInCNYPerM   *float64 // ¥ per 1M input tokens  → Pricing.tsx priceIn
	DisplayOutCNYPerM  *float64 // ¥ per 1M output tokens → Pricing.tsx priceOut
	DisplayCreditsPerM *int64   // credits per 1M out     → Pricing.tsx creditsPerMOut
	DisplayName        *string  // friendly model name    → Pricing.tsx model
	VendorLabel        *string  // friendly vendor label  → Pricing.tsx vendor
	ContextSize        *string  // context window string  → Pricing.tsx context (e.g. "128k")
	CacheFlag          *string  // "true"|"cache_control"|"false" → Pricing.tsx cache
}

// BillingConfig mirrors a row in gtk_billing_config. Single-table key/value
// store; today's only key is 'markup_multiplier'.
type BillingConfig struct {
	K string
	V string
}
