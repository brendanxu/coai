// Package usage implements the menu bar app's per-user usage feed:
//
//   GET /api/v1/usage/me — authenticated, scoped by sk-xxx → user_id.
//
// The package is read-only. It aggregates rows from gtk_app_usage_log (owned
// by package plans) and never writes. All shapes mirror the spec in
// docs/codebase-map design 2026-05-08.
//
// ⚠️  Schema mismatch noted at L7 invariant boundary
//   gtk_app_usage_log today carries (id, user_id, plan_id, service,
//   tokens_used, cost_cents, created_at). The menu bar response shape
//   asks for tokens_in / tokens_out / model / cache_read /
//   cache_savings — none of which exist. Per dispatch, we surface what
//   we have and set top-level "_estimated":true so the menu bar can
//   render a "data partial" badge until a migration extends the table.
//
// TODO migration (founder approval gate):
//   ALTER TABLE gtk_app_usage_log
//     ADD COLUMN model VARCHAR(64) NULL,
//     ADD COLUMN tokens_in INT NOT NULL DEFAULT 0,
//     ADD COLUMN tokens_out INT NOT NULL DEFAULT 0,
//     ADD COLUMN cache_read_input_tokens INT NOT NULL DEFAULT 0,
//     ADD COLUMN cache_savings_cents INT NOT NULL DEFAULT 0;
//   Mirror in plans/migration.go MySQL + sqlite branches.
//   Wave 2 / post-Gate-1 task; do not auto-merge per L18.
package usage

import (
	"chat/globals"
	"database/sql"
	"fmt"
	"time"
)

// Currency reported in summary. CoAI is CNY-only today; multi-currency is a
// Stage 3 deferred per design doc Open Question #5.
const Currency = "CNY"

// SGTOffset is the user-visible "today midnight" anchor used until the
// menu bar app can advertise its own TZ. Spec §"Implementation notes":
//
//   "since (ISO date, default = today midnight in user's TZ; for now
//    hardcode UTC+8/SGT)"
var SGTOffset = time.FixedZone("SGT", 8*60*60)

// Summary is the always-present top block: today's spend + month rollup +
// burn-per-hour + cache stats. Cache fields are zero until the schema gains
// per-call cache columns (see file-level migration TODO).
type Summary struct {
	TodaySpend        float64   `json:"today_spend"`
	TodayTokensIn     int64     `json:"today_tokens_in"`
	TodayTokensOut    int64     `json:"today_tokens_out"`
	TodayBurnPerHour  float64   `json:"today_burn_per_hour"`
	TodayCacheHitRate float64   `json:"today_cache_hit_rate"`
	TodayCacheSavings float64   `json:"today_cache_savings"`
	MonthSpend        float64   `json:"month_spend"`
	MonthBudget       *float64  `json:"month_budget"` // nullable: no budget feature yet
	AsOf              time.Time `json:"as_of"`
}

// RecentCall is one row from the last-N-calls feed. Per the migration TODO
// above, model is sourced from `service` (the closest existing column) and
// tokens_in/tokens_out are a 50/50 split of `tokens_used`.
type RecentCall struct {
	Ts           time.Time `json:"ts"`
	Model        string    `json:"model"`
	TokensIn     int64     `json:"tokens_in"`
	TokensOut    int64     `json:"tokens_out"`
	Cost         float64   `json:"cost"`
	CacheRead    int64     `json:"cache_read"`
	CacheSavings float64   `json:"cache_savings"`
}

// HourlyBucket aggregates spend + call count for one hour of today. The
// menu bar's 24-bar SVG reads this slice. Bucket "hour" is an ISO string
// truncated to the hour (e.g. "2026-05-08T20:00").
type HourlyBucket struct {
	Hour  string  `json:"hour"`
	Spend float64 `json:"spend"`
	Calls int64   `json:"calls"`
}

// Aggregator owns SQL access. Stateless; safe to share across requests. The
// constructor exists so tests can swap in a sqlite handle without going
// through connection.DB.
type Aggregator struct {
	DB *sql.DB
}

// NewAggregator wraps a *sql.DB. nil-safe in the sense that all methods
// will return their respective query error rather than panic; callers
// should still pass a non-nil DB in production.
func NewAggregator(db *sql.DB) *Aggregator {
	return &Aggregator{DB: db}
}

// hourBucketExpr returns the dialect-specific SQL fragment that buckets
// created_at to ISO "YYYY-MM-DDTHH:00". MySQL DATE_FORMAT vs sqlite
// strftime — both produce strings the menu bar can sort + render.
func hourBucketExpr() string {
	if globals.SqliteEngine {
		return `strftime('%Y-%m-%dT%H:00', created_at)`
	}
	return `DATE_FORMAT(created_at, '%Y-%m-%dT%H:00')`
}

// SummaryFor returns the Summary block scoped to user_id.
//
// today and month boundaries are computed by the caller (handler) so the
// aggregator stays pure-functional and testable.
//
// burn_per_hour = today_spend / hours_elapsed_today (clamped ≥ 1h to avoid
// 100x spikes in the first minutes after midnight).
//
// cache_hit_rate + cache_savings = 0 (schema-missing). The handler sets
// the top-level _estimated flag.
func (a *Aggregator) SummaryFor(userID int64, todayStart, monthStart, asOf time.Time) (Summary, error) {
	var s Summary
	s.AsOf = asOf

	// today aggregates
	row := globals.QueryRowDb(a.DB, `
		SELECT
		  COALESCE(SUM(cost_cents), 0),
		  COALESCE(SUM(tokens_used), 0)
		FROM gtk_app_usage_log
		WHERE user_id = ? AND created_at >= ?
	`, userID, todayStart)
	var todayCostCents, todayTokens int64
	if err := row.Scan(&todayCostCents, &todayTokens); err != nil {
		return s, fmt.Errorf("today aggregate: %w", err)
	}
	s.TodaySpend = float64(todayCostCents) / 100.0
	// 50/50 split until schema split lands. Documented in file header.
	s.TodayTokensIn = todayTokens / 2
	s.TodayTokensOut = todayTokens - s.TodayTokensIn

	// burn-per-hour. clamp denominator ≥ 1h so 03:14 doesn't show 24×.
	hoursElapsed := asOf.Sub(todayStart).Hours()
	if hoursElapsed < 1 {
		hoursElapsed = 1
	}
	s.TodayBurnPerHour = s.TodaySpend / hoursElapsed

	// month aggregate
	row = globals.QueryRowDb(a.DB, `
		SELECT COALESCE(SUM(cost_cents), 0)
		FROM gtk_app_usage_log
		WHERE user_id = ? AND created_at >= ?
	`, userID, monthStart)
	var monthCostCents int64
	if err := row.Scan(&monthCostCents); err != nil {
		return s, fmt.Errorf("month aggregate: %w", err)
	}
	s.MonthSpend = float64(monthCostCents) / 100.0

	// MonthBudget left nil. CacheHitRate / CacheSavings zero per schema gap.
	return s, nil
}

// RecentCallsFor returns the most recent N calls (default 10).
// service column is reported as model, tokens_used is split 50/50 into
// in/out. cache_read + cache_savings are zero until schema migration.
func (a *Aggregator) RecentCallsFor(userID int64, limit int) ([]RecentCall, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := globals.QueryDb(a.DB, `
		SELECT created_at, service, tokens_used, cost_cents
		FROM gtk_app_usage_log
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent calls query: %w", err)
	}
	defer rows.Close()

	out := make([]RecentCall, 0, limit)
	for rows.Next() {
		var rc RecentCall
		var costCents, tokens int64
		// Scan created_at as string for engine portability: go-sqlite3 returns
		// TEXT for DATETIME unless _parseTime=true is on the DSN, while
		// go-sql-driver/mysql can hand back time.Time. Parsing here keeps
		// the package agnostic.
		var createdAtStr string
		if err := rows.Scan(&createdAtStr, &rc.Model, &tokens, &costCents); err != nil {
			return nil, fmt.Errorf("recent calls scan: %w", err)
		}
		rc.Ts = parseUsageTime(createdAtStr)
		rc.TokensIn = tokens / 2
		rc.TokensOut = tokens - rc.TokensIn
		rc.Cost = float64(costCents) / 100.0
		// CacheRead, CacheSavings = 0 (schema gap)
		out = append(out, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent calls iter: %w", err)
	}
	return out, nil
}

// HourlyFor returns a sparse list of hours with usage today (only hours
// that had ≥1 call appear; the menu bar pads zeros for empty hours
// client-side). Sort ascending by hour string for deterministic rendering.
func (a *Aggregator) HourlyFor(userID int64, todayStart time.Time) ([]HourlyBucket, error) {
	q := fmt.Sprintf(`
		SELECT %s AS hour, COALESCE(SUM(cost_cents), 0), COUNT(*)
		FROM gtk_app_usage_log
		WHERE user_id = ? AND created_at >= ?
		GROUP BY hour
		ORDER BY hour ASC
	`, hourBucketExpr())

	rows, err := globals.QueryDb(a.DB, q, userID, todayStart)
	if err != nil {
		return nil, fmt.Errorf("hourly query: %w", err)
	}
	defer rows.Close()

	out := make([]HourlyBucket, 0, 24)
	for rows.Next() {
		var hb HourlyBucket
		var costCents int64
		if err := rows.Scan(&hb.Hour, &costCents, &hb.Calls); err != nil {
			return nil, fmt.Errorf("hourly scan: %w", err)
		}
		hb.Spend = float64(costCents) / 100.0
		out = append(out, hb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hourly iter: %w", err)
	}
	return out, nil
}

// parseUsageTime accepts the string forms sqlite + MySQL hand back for
// DATETIME columns and returns a UTC-tagged time.Time. Returns the zero
// value if neither layout matches; the caller (RecentCallsFor) treats
// the zero value as "unknown" and emits it as the JSON zero, which the
// menu bar renders as "—".
func parseUsageTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// TodayBoundsSGT returns (todayStart, monthStart) in SGT, then converted
// to UTC for SQL comparison. The DB's created_at column is stored in
// the connection's session TZ — for sqlite that's effectively UTC, for
// MySQL it depends on @@session.time_zone. We use UTC-tagged times so
// the comparison is meaningful in either case.
func TodayBoundsSGT(now time.Time) (today, month time.Time) {
	sgt := now.In(SGTOffset)
	todaySGT := time.Date(sgt.Year(), sgt.Month(), sgt.Day(), 0, 0, 0, 0, SGTOffset)
	monthSGT := time.Date(sgt.Year(), sgt.Month(), 1, 0, 0, 0, 0, SGTOffset)
	return todaySGT.UTC(), monthSGT.UTC()
}
