// margin_view.go — gtk_service_margin_v VIEW + reader (PKG-2 Wave 4 D6).
//
// Why a VIEW (not a materialized table or denormalized column):
//
//	Per-service margin = revenue (svc.price_cny_cents_paid) - upstream cost
//	(SUM(usg.cost_cents) WHERE usage.order_id = svc.order_no AND
//	usage.source = 'service_order'). Both pieces of data already live in
//	their respective authoritative tables (gtk_service_order +
//	gtk_app_usage_log); a VIEW keeps the join logic in one place that
//	monitoring scripts (D9 check-margin-thresholds.sh) and admin UIs can
//	query without re-deriving the joins. Materialized denormalization
//	would require maintenance triggers + drift handling — premature for
//	v0 traffic levels.
//
// Why scope to status IN ('completed', 'refunded_post_delivery'):
//
//	Pending / running / refunded-pre-delivery orders haven't accrued cost
//	yet (or never will), so they distort the margin view if included.
//	'completed' is the typical happy-path billable state; the
//	'refunded_post_delivery' Wave-1 state IS still cost-incurring (the run
//	already fired) but the revenue is reversed at the financial layer —
//	we keep them in the view so margin reports can flag refund-after-
//	delivery as the loss event it is.
//
// Migration shape (extends commerce/session_migration.go):
//
//	The VIEW is created at boot via Migrate(). MySQL supports
//	CREATE OR REPLACE VIEW for idempotency. SQLite doesn't, so the SQLite
//	branch DROP-then-CREATE. Tests that exercise the VIEW seed both
//	tables + run Migrate first.
//
// Reader function:
//
//	QueryMarginByService(db, days) returns one row per service slug with
//	rolling revenue + upstream cost + margin %. Used by D9
//	check-margin-thresholds.sh to alert on services with margin < 30%.
//
// Architecture refs:
//   - docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md §D6
//   - commerce/cost_ledger.go (the source of usage.cost_cents)
//   - service/migration.go (the source of svc.price_cny_cents_paid + status enum)

package commerce

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// ServiceMarginRow is one row of the rolling-margin report. Cents are
// signed int64 (positive revenue, positive cost; margin can theoretically
// be negative if cost > revenue — that's the alert signal).
type ServiceMarginRow struct {
	// ServiceSlug identifies the service (e.g. "xhs-single-post").
	ServiceSlug string

	// RollingRevenueCents is the sum of price_cny_cents_paid for orders
	// in the rolling window.
	RollingRevenueCents int64

	// RollingCostCents is the sum of upstream LLM cost (cents) attributed
	// to those orders via gtk_app_usage_log.
	RollingCostCents int64

	// RollingMarginPct is the percentage margin: (rev - cost) / rev * 100.
	// Computed from RollingRevenueCents and RollingCostCents; reported as
	// a float for human readability. NaN-safe: returns 0 when revenue=0
	// (the row shouldn't be emitted in that case but defensive).
	RollingMarginPct float64
}

// CreateServiceMarginView (re)creates gtk_service_margin_v. Called from
// commerce.Migrate after gtk_payment_session creation. Idempotent on both
// engines: MySQL via CREATE OR REPLACE; SQLite via DROP IF EXISTS + CREATE.
//
// The VIEW is created lazily — if either gtk_service_order or
// gtk_app_usage_log doesn't exist yet (atypical at boot, but defensive),
// the VIEW creation will fail gracefully with a wrapped error pointing at
// the missing dependency.
//
// SQLite test caveat: this VIEW is created in the test SQLite when the
// test fixture has BOTH gtk_service_order and gtk_app_usage_log tables.
// Tests that don't set up those tables should not call commerce.Migrate
// (which now also creates this VIEW). The session-only test fixtures
// already do — see commerce/session_test.go's setup.
func CreateServiceMarginView(db *sql.DB) error {
	if globals.SqliteEngine {
		// SQLite: DROP-then-CREATE for idempotency. The view body uses
		// the same SELECT shape as MySQL — SQLite is permissive about
		// our use of LEFT JOIN + COALESCE + aggregate functions inside a
		// view definition.
		if _, err := globals.ExecDb(db, `DROP VIEW IF EXISTS gtk_service_margin_v`); err != nil {
			return fmt.Errorf("drop gtk_service_margin_v: %w", err)
		}
		_, err := globals.ExecDb(db, `
			CREATE VIEW gtk_service_margin_v AS
			SELECT
			  svc.service_slug                        AS service_slug,
			  svc.created_at                          AS order_created_at,
			  svc.price_cny_cents_paid                AS price_cny_cents_paid,
			  COALESCE(SUM(usg.cost_cents), 0)      AS upstream_cost_cents,
			  svc.price_cny_cents_paid - COALESCE(SUM(usg.cost_cents), 0) AS margin_cents
			FROM gtk_service_order svc
			LEFT JOIN gtk_app_usage_log usg
			  ON usg.order_id = svc.order_no
			 AND usg.source   = 'service_order'
			WHERE svc.status IN ('completed', 'refunded_post_delivery')
			GROUP BY svc.id, svc.service_slug, svc.created_at, svc.price_cny_cents_paid
		`)
		if err != nil {
			return fmt.Errorf("create gtk_service_margin_v (sqlite): %w", err)
		}
		return nil
	}

	// MySQL: CREATE OR REPLACE for atomic idempotency. Same SELECT body.
	_, err := globals.ExecDb(db, `
		CREATE OR REPLACE VIEW gtk_service_margin_v AS
		SELECT
		  svc.service_slug                        AS service_slug,
		  svc.created_at                          AS order_created_at,
		  svc.price_cny_cents_paid                AS price_cny_cents_paid,
		  COALESCE(SUM(usg.cost_cents), 0)      AS upstream_cost_cents,
		  svc.price_cny_cents_paid - COALESCE(SUM(usg.cost_cents), 0) AS margin_cents
		FROM gtk_service_order svc
		LEFT JOIN gtk_app_usage_log usg
		  ON usg.order_id = svc.order_no
		 AND usg.source   = 'service_order'
		WHERE svc.status IN ('completed', 'refunded_post_delivery')
		GROUP BY svc.id, svc.service_slug, svc.created_at, svc.price_cny_cents_paid
	`)
	if err != nil {
		return fmt.Errorf("create gtk_service_margin_v (mysql): %w", err)
	}
	return nil
}

// QueryMarginByService returns one row per service slug with rolling
// totals over the last `days` days. Used by D9 check-margin-thresholds.sh
// to alert on services where margin drops below the operator threshold
// (30% per spec).
//
// Order is by margin ASC (worst margin first) so cron alerts surface the
// loss-leaders at the top of the output.
//
// `days` must be > 0; we return an error otherwise (avoids the
// "rolling 0d window" semantics confusion).
func QueryMarginByService(db *sql.DB, days int) ([]ServiceMarginRow, error) {
	if days <= 0 {
		return nil, fmt.Errorf(
			"commerce.QueryMarginByService: days must be > 0 (got %d)", days)
	}

	// Engine-specific date arithmetic: MySQL uses
	// DATE_SUB(NOW(), INTERVAL ? DAY); SQLite uses datetime('now', '-N days').
	// We branch on globals.SqliteEngine since the engines disagree on
	// '-N days' literal handling within a parameterized query.
	var (
		rows *sql.Rows
		err  error
	)
	if globals.SqliteEngine {
		rows, err = db.Query(`
			SELECT service_slug,
			       SUM(price_cny_cents_paid),
			       SUM(upstream_cost_cents)
			FROM gtk_service_margin_v
			WHERE order_created_at >= datetime('now', ?)
			GROUP BY service_slug
			ORDER BY (SUM(price_cny_cents_paid) - SUM(upstream_cost_cents)) ASC
		`, fmt.Sprintf("-%d days", days))
	} else {
		rows, err = db.Query(`
			SELECT service_slug,
			       SUM(price_cny_cents_paid),
			       SUM(upstream_cost_cents)
			FROM gtk_service_margin_v
			WHERE order_created_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
			GROUP BY service_slug
			ORDER BY (SUM(price_cny_cents_paid) - SUM(upstream_cost_cents)) ASC
		`, days)
	}
	if err != nil {
		return nil, fmt.Errorf("query gtk_service_margin_v: %w", err)
	}
	defer rows.Close()

	var out []ServiceMarginRow
	for rows.Next() {
		var (
			slug    string
			revenue sql.NullInt64
			cost    sql.NullInt64
		)
		if err := rows.Scan(&slug, &revenue, &cost); err != nil {
			return nil, fmt.Errorf("scan margin row: %w", err)
		}
		row := ServiceMarginRow{
			ServiceSlug:         slug,
			RollingRevenueCents: revenue.Int64,
			RollingCostCents:    cost.Int64,
		}
		if revenue.Valid && revenue.Int64 > 0 {
			row.RollingMarginPct = float64(revenue.Int64-cost.Int64) /
				float64(revenue.Int64) * 100.0
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate margin rows: %w", err)
	}
	return out, nil
}
