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
// PKG-1: Source / OrderID / Provider added. Source classifies the call
// origin so revenue attribution can split chat-driven vs service-order
// usage. OrderID links to gtk_service_order.order_no when source='service_order'.
// Provider records the upstream label (openai/anthropic/deepseek/...) so
// per-provider margin math is one GROUP BY away.
type AppUsageLog struct {
	ID         int64
	UserID     int64
	PlanID     sql.NullInt64
	Service    string
	Source     string         // 'chat' | 'api' | 'service_order' | 'admin_test'
	OrderID    sql.NullString // gtk_service_order.order_no when applicable
	Provider   sql.NullString // 'openai' | 'anthropic' | 'deepseek' | etc.
	TokensUsed int64
	CostCents  int64
	CreatedAt  time.Time
}
