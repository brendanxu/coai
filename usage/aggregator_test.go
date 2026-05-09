// Aggregator unit tests. The strategy: stand up an in-memory sqlite DB,
// migrate plans (which owns gtk_app_usage_log), seed a hand-picked fixture,
// then assert SummaryFor / RecentCallsFor / HourlyFor return the math we
// expect. No HTTP, no auth — pure SQL behavior.
//
// Test data is intentionally tiny + obvious so a future reviewer can
// eyeball the expected sums without re-running the test.
package usage

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newAggDB stands up a fresh in-memory sqlite + migrates plans tables.
// Returns the DB plus a now-stamp pinned to a deterministic instant so
// "today midnight SGT" is reproducible.
//
// Pinned now = 2026-05-08 14:30 UTC = 2026-05-08 22:30 SGT, so today
// midnight SGT = 2026-05-08 00:00 SGT = 2026-05-07 16:00 UTC.
func newAggDB(t *testing.T) (*sql.DB, time.Time) {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}

	// Pinned now: 2026-05-08 14:30:00 UTC.
	now := time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC)
	return db, now
}

// insertUsage is a tiny seeder. createdAt is stored as UTC because that's
// what TodayBoundsSGT compares against (it returns UTC-tagged bounds).
func insertUsage(t *testing.T, db *sql.DB, userID int64, service string, tokens, costCents int64, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, tokens_used, cost_cents, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, userID, service, tokens, costCents, createdAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insert usage: %v", err)
	}
}

// --- TodayBoundsSGT ---

func TestTodayBoundsSGT(t *testing.T) {
	// 2026-05-08 14:30 UTC → 2026-05-08 22:30 SGT → today=2026-05-08 00:00 SGT.
	now := time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC)
	today, month := TodayBoundsSGT(now)

	wantToday := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC) // 2026-05-08 00:00 SGT
	wantMonth := time.Date(2026, 4, 30, 16, 0, 0, 0, time.UTC) // 2026-05-01 00:00 SGT

	if !today.Equal(wantToday) {
		t.Errorf("today: got %v want %v", today, wantToday)
	}
	if !month.Equal(wantMonth) {
		t.Errorf("month: got %v want %v", month, wantMonth)
	}
}

func TestTodayBoundsSGT_AcrossMidnight(t *testing.T) {
	// 2026-05-08 17:30 UTC = 2026-05-09 01:30 SGT → today must be 2026-05-09 SGT.
	now := time.Date(2026, 5, 8, 17, 30, 0, 0, time.UTC)
	today, _ := TodayBoundsSGT(now)
	wantToday := time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC) // 2026-05-09 00:00 SGT
	if !today.Equal(wantToday) {
		t.Errorf("today across midnight: got %v want %v", today, wantToday)
	}
}

// --- SummaryFor ---

func TestSummaryFor_AggregatesTodayAndMonth(t *testing.T) {
	db, now := newAggDB(t)
	today, month := TodayBoundsSGT(now)

	// User 42: 3 calls today (10:00 / 12:00 / 22:00 SGT) + 1 call earlier this
	// month + 1 call from another user (must be excluded).
	// SGT 10:00 = UTC 02:00; SGT 12:00 = UTC 04:00; SGT 22:00 = UTC 14:00.
	// "now" SGT = 22:30, so all 3 are <= asOf.
	insertUsage(t, db, 42, "claude-3-5-sonnet", 1000, 50, time.Date(2026, 5, 8, 2, 0, 0, 0, time.UTC))
	insertUsage(t, db, 42, "claude-3-5-haiku", 500, 20, time.Date(2026, 5, 8, 4, 0, 0, 0, time.UTC))
	insertUsage(t, db, 42, "gpt-4", 2000, 100, time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC))

	// month-only (5 days ago) — counts in month, not today
	insertUsage(t, db, 42, "claude", 800, 40, time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC))

	// noise: another user
	insertUsage(t, db, 99, "claude", 9999, 9999, time.Date(2026, 5, 8, 2, 0, 0, 0, time.UTC))

	agg := NewAggregator(db)
	asOf := now.In(SGTOffset) // SGT 22:30
	s, err := agg.SummaryFor(42, today, month, asOf)
	if err != nil {
		t.Fatalf("SummaryFor: %v", err)
	}

	// today: 50 + 20 + 100 = 170 cents = 1.70
	if s.TodaySpend != 1.70 {
		t.Errorf("TodaySpend: got %v want 1.70", s.TodaySpend)
	}
	// today tokens: 1000 + 500 + 2000 = 3500 (split 50/50: 1750/1750)
	if s.TodayTokensIn != 1750 || s.TodayTokensOut != 1750 {
		t.Errorf("token split: in=%d out=%d want 1750/1750", s.TodayTokensIn, s.TodayTokensOut)
	}
	// month: 50 + 20 + 100 + 40 = 210 cents = 2.10
	if s.MonthSpend != 2.10 {
		t.Errorf("MonthSpend: got %v want 2.10", s.MonthSpend)
	}
	// burn rate: 1.70 / 22.5h ≈ 0.0755 (asOf 22:30, todayStart 00:00 SGT → 22.5h)
	wantBurn := 1.70 / 22.5
	if abs(s.TodayBurnPerHour-wantBurn) > 0.0001 {
		t.Errorf("burn: got %v want %v", s.TodayBurnPerHour, wantBurn)
	}
	// Cache fields are zero (schema gap)
	if s.TodayCacheHitRate != 0 || s.TodayCacheSavings != 0 {
		t.Errorf("cache fields should be 0 (schema gap): hit=%v save=%v", s.TodayCacheHitRate, s.TodayCacheSavings)
	}
	// Budget nil
	if s.MonthBudget != nil {
		t.Errorf("MonthBudget should be nil")
	}
}

func TestSummaryFor_BurnRateClampedAtOneHour(t *testing.T) {
	// At 00:14 SGT (just after midnight) we'd otherwise divide by 0.23h
	// and report a 4× burn rate. Clamp to 1h.
	db, _ := newAggDB(t)
	// Now = 2026-05-08 00:14 SGT = 2026-05-07 16:14 UTC.
	// today midnight SGT = 2026-05-07 16:00 UTC (same day SGT).
	now := time.Date(2026, 5, 7, 16, 14, 0, 0, time.UTC)
	today, month := TodayBoundsSGT(now)

	insertUsage(t, db, 7, "model", 100, 50, time.Date(2026, 5, 7, 16, 5, 0, 0, time.UTC))

	agg := NewAggregator(db)
	s, err := agg.SummaryFor(7, today, month, now.In(SGTOffset))
	if err != nil {
		t.Fatalf("SummaryFor: %v", err)
	}
	// Should report 0.50 / 1.0h = 0.50 (not 0.50 / 0.23 ≈ 2.17)
	if s.TodayBurnPerHour != 0.50 {
		t.Errorf("burn clamp failed: got %v want 0.50", s.TodayBurnPerHour)
	}
}

// --- RecentCallsFor ---

func TestRecentCallsFor_OrdersByCreatedDesc(t *testing.T) {
	db, _ := newAggDB(t)

	// 3 calls; insert oldest-first to prove ORDER BY created_at DESC works.
	insertUsage(t, db, 1, "claude", 1000, 100, time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC))
	insertUsage(t, db, 1, "haiku", 500, 50, time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC))
	insertUsage(t, db, 1, "gpt-4", 2000, 200, time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	agg := NewAggregator(db)
	calls, err := agg.RecentCallsFor(1, 10)
	if err != nil {
		t.Fatalf("RecentCallsFor: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("want 3 calls, got %d", len(calls))
	}
	// Newest first
	if calls[0].Model != "gpt-4" {
		t.Errorf("first call should be gpt-4, got %s", calls[0].Model)
	}
	if calls[2].Model != "claude" {
		t.Errorf("last call should be claude, got %s", calls[2].Model)
	}
	// Cost conversion: 200 cents = 2.00
	if calls[0].Cost != 2.00 {
		t.Errorf("cost: got %v want 2.00", calls[0].Cost)
	}
	// Token split 50/50
	if calls[0].TokensIn != 1000 || calls[0].TokensOut != 1000 {
		t.Errorf("split: in=%d out=%d want 1000/1000", calls[0].TokensIn, calls[0].TokensOut)
	}
	// Cache fields zero
	if calls[0].CacheRead != 0 || calls[0].CacheSavings != 0 {
		t.Errorf("cache fields should be 0")
	}
}

func TestRecentCallsFor_LimitDefaultAndCap(t *testing.T) {
	db, _ := newAggDB(t)
	for i := 0; i < 15; i++ {
		insertUsage(t, db, 1, "m", 1, 1, time.Date(2026, 5, 8, 10, i, 0, 0, time.UTC))
	}
	agg := NewAggregator(db)

	// limit=0 → default 10
	calls, _ := agg.RecentCallsFor(1, 0)
	if len(calls) != 10 {
		t.Errorf("default limit: got %d want 10", len(calls))
	}

	// explicit limit
	calls, _ = agg.RecentCallsFor(1, 5)
	if len(calls) != 5 {
		t.Errorf("explicit 5: got %d want 5", len(calls))
	}
}

// --- HourlyFor ---

func TestHourlyFor_BucketsByHour(t *testing.T) {
	db, now := newAggDB(t)
	today, _ := TodayBoundsSGT(now)

	// 2 calls in 10:00 hour, 1 call in 12:00 hour (UTC, since sqlite stores
	// as text and our bucket expr reads back what we stored).
	insertUsage(t, db, 1, "m", 100, 50, time.Date(2026, 5, 8, 10, 5, 0, 0, time.UTC))
	insertUsage(t, db, 1, "m", 200, 75, time.Date(2026, 5, 8, 10, 30, 0, 0, time.UTC))
	insertUsage(t, db, 1, "m", 300, 100, time.Date(2026, 5, 8, 12, 15, 0, 0, time.UTC))

	agg := NewAggregator(db)
	buckets, err := agg.HourlyFor(1, today)
	if err != nil {
		t.Fatalf("HourlyFor: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("want 2 buckets, got %d (%+v)", len(buckets), buckets)
	}
	// First bucket = 10:00 hour (ascending)
	if buckets[0].Hour != "2026-05-08T10:00" {
		t.Errorf("bucket[0].Hour: got %q want 2026-05-08T10:00", buckets[0].Hour)
	}
	if buckets[0].Calls != 2 {
		t.Errorf("bucket[0].Calls: got %d want 2", buckets[0].Calls)
	}
	if buckets[0].Spend != 1.25 {
		t.Errorf("bucket[0].Spend: got %v want 1.25", buckets[0].Spend)
	}
	if buckets[1].Hour != "2026-05-08T12:00" {
		t.Errorf("bucket[1].Hour: got %q want 2026-05-08T12:00", buckets[1].Hour)
	}
	if buckets[1].Calls != 1 || buckets[1].Spend != 1.00 {
		t.Errorf("bucket[1]: calls=%d spend=%v want 1/1.00", buckets[1].Calls, buckets[1].Spend)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
