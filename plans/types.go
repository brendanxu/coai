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
type AppUsageLog struct {
	ID         int64
	UserID     int64
	PlanID     sql.NullInt64
	Service    string
	TokensUsed int64
	CostCents  int64
	CreatedAt  time.Time
}
