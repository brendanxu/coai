package auth

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newRechargeTestDB(t *testing.T) *sql.DB {
	t.Helper()

	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	seedRechargeSchema(t, db, true)
	return db
}

func newRechargeBrokenQuotaDB(t *testing.T) *sql.DB {
	t.Helper()

	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	seedRechargeSchema(t, db, false)
	return db
}

func seedRechargeSchema(t *testing.T, db *sql.DB, quotaHasUniqueUser bool) {
	t.Helper()

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (42)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}

	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}

	quotaDDL := `
		CREATE TABLE quota (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER UNIQUE,
			quota REAL,
			used REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	if !quotaHasUniqueUser {
		quotaDDL = `
			CREATE TABLE quota (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id INTEGER,
				quota REAL,
				used REAL
			);
		`
	}
	if _, err := db.Exec(quotaDDL); err != nil {
		t.Fatalf("seed quota table: %v", err)
	}
}

func seedRechargePlan(t *testing.T, db *sql.DB, code string, quota float32, active bool) {
	t.Helper()
	seedRechargePlanConfig(t, db, code, `{"quota": `+trimFloat(quota)+`, "models": ["claude-*"]}`, 30, active)
}

func seedRechargePlanConfig(t *testing.T, db *sql.DB, code, quotaConfig string, durationDays int, active bool) {
	t.Helper()
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan (code, name, type, price_cents, duration_days, quota_config, is_active)
		VALUES (?, ?, 'pack', 9900, ?, ?, ?)
	`, code, "Starter 100K", durationDays, quotaConfig, active); err != nil {
		t.Fatalf("seed plan %s: %v", code, err)
	}
}

func trimFloat(v float32) string {
	s := strings.TrimRight(strings.TrimRight((strconvFormatFloat(v)), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func strconvFormatFloat(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', 6, 32)
}

func quotaForUser(t *testing.T, db *sql.DB, userID int64) float64 {
	t.Helper()
	var quota float64
	if err := db.QueryRow(`SELECT quota FROM quota WHERE user_id = ?`, userID).Scan(&quota); err != nil {
		t.Fatalf("read quota for user %d: %v", userID, err)
	}
	return quota
}

func userPlanCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_user_plan`).Scan(&count); err != nil {
		t.Fatalf("count user plans: %v", err)
	}
	return count
}

func assertFloatNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("got %.6f want %.6f", got, want)
	}
}

func TestRedeemPlanForOrder_HappyPath(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, true)

	if err := RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_1"); err != nil {
		t.Fatalf("redeem: %v", err)
	}

	if count := userPlanCount(t, db); count != 1 {
		t.Fatalf("got %d gtk_user_plan rows, want 1", count)
	}
	assertFloatNear(t, quotaForUser(t, db, 42), 100000)
}

func TestRedeemPlanForOrder_IdempotentRerun(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, true)

	for i := 0; i < 2; i++ {
		if err := RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_repeat"); err != nil {
			t.Fatalf("redeem #%d: %v", i+1, err)
		}
	}

	if count := userPlanCount(t, db); count != 1 {
		t.Fatalf("got %d gtk_user_plan rows, want 1", count)
	}
	assertFloatNear(t, quotaForUser(t, db, 42), 100000)
}

func TestRedeemPlanForOrder_ConcurrentReentry(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, true)

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_concurrent")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent redeem returned error: %v", err)
		}
	}

	if count := userPlanCount(t, db); count != 1 {
		t.Fatalf("got %d gtk_user_plan rows, want 1", count)
	}
	assertFloatNear(t, quotaForUser(t, db, 42), 100000)
}

func TestRedeemPlanForOrder_InactivePlan(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, false)

	if err := RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_inactive"); err == nil {
		t.Fatal("inactive plan should fail")
	}
	if count := userPlanCount(t, db); count != 0 {
		t.Fatalf("got %d gtk_user_plan rows, want 0", count)
	}
}

func TestRedeemPlanForOrder_UnknownPlanCode(t *testing.T) {
	db := newRechargeTestDB(t)

	if err := RedeemPlanForOrder(db, 42, "missing-plan", "ls_order_missing"); err == nil {
		t.Fatal("unknown plan should fail")
	}
	if count := userPlanCount(t, db); count != 0 {
		t.Fatalf("got %d gtk_user_plan rows, want 0", count)
	}
}

func TestRedeemPlanForOrder_NonPositiveQuotaConfig(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "zero-quota", 0, true)

	if err := RedeemPlanForOrder(db, 42, "zero-quota", "ls_order_zero"); err == nil {
		t.Fatal("zero quota should fail")
	}
	if count := userPlanCount(t, db); count != 0 {
		t.Fatalf("got %d gtk_user_plan rows, want 0", count)
	}
}

func TestRedeemPlanForOrder_MalformedQuotaConfigJSON(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlanConfig(t, db, "bad-json", `{"quota":`, 30, true)

	if err := RedeemPlanForOrder(db, 42, "bad-json", "ls_order_bad_json"); err == nil {
		t.Fatal("malformed quota_config should fail")
	}
	if count := userPlanCount(t, db); count != 0 {
		t.Fatalf("got %d gtk_user_plan rows, want 0", count)
	}
}

func TestRedeemPlanForOrder_ExpireAtMath(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlanConfig(t, db, "starter-30d", `{"quota": 100000}`, 30, true)

	before := time.Now()
	if err := RedeemPlanForOrder(db, 42, "starter-30d", "ls_order_expire"); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	after := time.Now()

	var raw string
	if err := db.QueryRow(`SELECT expire_at FROM gtk_user_plan WHERE order_id = ?`, "ls_order_expire").Scan(&raw); err != nil {
		t.Fatalf("read expire_at: %v", err)
	}
	expireAt := parseRechargeTime(t, raw)
	min := before.AddDate(0, 0, 30).Add(-1 * time.Second)
	max := after.AddDate(0, 0, 30).Add(60 * time.Second)
	if expireAt.Before(min) || expireAt.After(max) {
		t.Fatalf("expire_at=%s outside [%s, %s]", expireAt, min, max)
	}
}

func TestRedeemPlanForOrder_UserWithExistingQuota(t *testing.T) {
	db := newRechargeTestDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, true)
	if _, err := db.Exec(`INSERT INTO quota (user_id, quota, used) VALUES (42, 123, 0)`); err != nil {
		t.Fatalf("seed existing quota: %v", err)
	}

	if err := RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_existing_quota"); err != nil {
		t.Fatalf("redeem: %v", err)
	}

	assertFloatNear(t, quotaForUser(t, db, 42), 100123)
}

func TestRedeemPlanForOrder_RollbackOnQuotaFail(t *testing.T) {
	db := newRechargeBrokenQuotaDB(t)
	seedRechargePlan(t, db, "starter-100k", 100000, true)

	if err := RedeemPlanForOrder(db, 42, "starter-100k", "ls_order_quota_fail"); err == nil {
		t.Fatal("quota write should fail")
	}
	if count := userPlanCount(t, db); count != 0 {
		t.Fatalf("quota failure should roll back gtk_user_plan insert; got %d rows", count)
	}
}

func parseRechargeTime(t *testing.T, raw string) time.Time {
	t.Helper()
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
	} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts
		}
	}
	t.Fatalf("cannot parse time %q", raw)
	return time.Time{}
}
