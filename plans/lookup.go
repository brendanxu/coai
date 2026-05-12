// Plan-row lookup helpers. Kept minimal — checkout endpoints (payment/
// checkout.go LS, payment/hupijiao_checkout.go RMB) need a typed read
// of an active plan by code to derive the audit price + plan name.
//
// Heavier reads (admin list / per-product filter / pagination) belong
// in their own DAO layer; this file is a single SELECT to avoid two
// near-identical inline queries in checkout.go and hupijiao_checkout.go.

package plans

import (
	"chat/globals"
	"database/sql"
	"errors"
	"fmt"
)

// ErrPlanNotFound is returned when LookupActivePlan can't find a row
// matching code with is_active=TRUE. Caller surfaces as 400 to the
// frontend (operator misconfig: plan disabled / code mistyped).
var ErrPlanNotFound = errors.New("plans: plan not found or inactive")

// LookupActivePlan reads a single gtk_plan row by code, scoped to
// is_active=TRUE. Returns ErrPlanNotFound when no match (sql.ErrNoRows
// translated for clean error-typing at call sites).
//
// Returned Plan has the audit fields checkout endpoints need:
//   ID / Code / Name / PriceCents / DurationDays / ProductType / BillingMode / IsActive
//
// CreatedAt is intentionally NOT selected — SQLite's mattn driver can't
// scan TEXT-stored datetime columns into *time.Time without driver flags
// the test harness doesn't enable. Checkout doesn't need it; admin reads
// that need created_at should write a separate dedicated query.
//
// QuotaConfig + QuotaGrant + Type are scanned but checkout endpoints
// don't need them (they're for redeem-side logic in auth.RedeemPlanForOrder).
func LookupActivePlan(db *sql.DB, code string) (*Plan, error) {
	if db == nil {
		return nil, errors.New("plans: LookupActivePlan requires db")
	}
	if code == "" {
		return nil, errors.New("plans: LookupActivePlan requires non-empty code")
	}

	var p Plan
	row := globals.QueryRowDb(db, `
		SELECT id, code, name, type, product_type, billing_mode,
		       price_cents, duration_days, quota_grant, service_id,
		       quota_config, is_active
		FROM gtk_plan
		WHERE code = ? AND is_active = TRUE
		LIMIT 1
	`, code)
	if err := row.Scan(
		&p.ID, &p.Code, &p.Name, &p.Type, &p.ProductType, &p.BillingMode,
		&p.PriceCents, &p.DurationDays, &p.QuotaGrant, &p.ServiceID,
		&p.QuotaConfig, &p.IsActive,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: code=%q", ErrPlanNotFound, code)
		}
		return nil, fmt.Errorf("plans: scan gtk_plan code=%q: %w", code, err)
	}
	return &p, nil
}
