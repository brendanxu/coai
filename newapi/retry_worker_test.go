// retry_worker_test.go — unit + integration coverage for the PKG-3 drain
// worker (newapi/retry_worker.go).
//
// Strategy:
//
//   - SQLite in-memory tests (mirror migration_test.go's newSqliteWithFKDeps
//     helper) so the schema, CHECK constraints, and FK chain are exercised
//     end-to-end without standing up MySQL.
//
//   - The newapi.ProvisionForPlan + plan-row lookup are stubbed via the
//     package-level `provisionForPlanFn` and `loadPlanForRetryFn` vars
//     declared in retry_worker.go. Each test that swaps a var also restores
//     the original on Cleanup so subsequent tests in the same `go test`
//     invocation see the production defaults.
//
//   - Backoff tests manipulate last_attempt_at directly (UPDATE ... SET
//     last_attempt_at = ?) instead of using a clock-injection seam. v0
//     trade-off: the worker takes time.Now() inline; injecting a clock
//     would mean an extra package-level var or a function param on every
//     public surface. We accept the trade-off because backoff is enforced
//     in Go, not SQL, so the eligibility check is deterministic given a
//     known last_attempt_at.

package newapi

import (
	"chat/globals"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// helpers ---------------------------------------------------------------

// seedPlanAndUser writes the FK targets the queue table needs: one auth
// row + one gtk_plan row. Returns the plan's id (always 1 in fresh DB).
//
// Uses ONLY the columns present in newSqliteWithFKDeps' minimal gtk_plan
// stub (id, code, name, type, price_cents, duration_days) — the PKG-1
// product_type / quota_grant columns are NOT in the stub schema. Tests
// that need quota_grant value inspection use stubPlanLoader to inject a
// synthetic planRow directly.
func seedPlanAndUser(t *testing.T, db *sql.DB, userID int64) int64 {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (?)`, userID); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO gtk_plan (code, name, type, price_cents, duration_days)
		VALUES ('plan-test','Plan Test','subscription',1500,30)
	`)
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	planID, _ := res.LastInsertId()
	// Stub the plan loader by default so worker tests don't depend on
	// the gtk_plan stub schema having the PKG-1 quota_grant column.
	// Tests that want the real loader (TestLoadPlanForRetry_RealLookup)
	// re-stub explicitly within the test body.
	stubPlanLoader(t, &planRow{
		Code:         "plan-test",
		QuotaUnits:   500000,
		DurationDays: 30,
	}, nil)
	return planID
}

// enqueuePending inserts one row into gtk_newapi_pending_provisions in
// the given starting state. Returns the new row id.
func enqueuePending(t *testing.T, db *sql.DB, userID, planID int64, provisionType, status string, retryCount int) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO gtk_newapi_pending_provisions
		  (user_id, plan_id, provision_type, status, retry_count)
		VALUES (?, ?, ?, ?, ?)
	`, userID, planID, provisionType, status, retryCount)
	if err != nil {
		t.Fatalf("enqueue pending (status=%s): %v", status, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// readRow reads a row's relevant fields for assertion. Scans the
// timestamp columns into sql.NullString rather than sql.NullTime
// because PreflightSql rewrites SQLite DATETIME columns to TEXT, and
// go-sqlite3's auto-parse only fires for columns whose declared type
// includes "datetime" / "date" / "timestamp" in the schema. Tests
// observe Valid + non-empty as the truth signal — production never
// reads these columns from queue rows (the worker only writes them).
//
// The returned struct mirrors PendingProvision but with NullString
// timestamps so tests don't need to handle parse errors.
func readRow(t *testing.T, db *sql.DB, id int64) testPendingRow {
	t.Helper()
	var p testPendingRow
	err := db.QueryRow(`
		SELECT id, user_id, plan_id, provision_type, status, retry_count,
		       last_attempt_at, last_error, succeeded_at, failed_at
		FROM gtk_newapi_pending_provisions WHERE id = ?
	`, id).Scan(
		&p.ID, &p.UserID, &p.PlanID, &p.ProvisionType, &p.Status, &p.RetryCount,
		&p.LastAttemptAt, &p.LastError, &p.SucceededAt, &p.FailedAt,
	)
	if err != nil {
		t.Fatalf("read row id=%d: %v", id, err)
	}
	return p
}

// testPendingRow is the SQLite-test-friendly mirror of PendingProvision.
// Only difference: timestamps as NullString (see readRow doc).
type testPendingRow struct {
	ID            int64
	UserID        int64
	PlanID        int64
	ProvisionType string
	Status        string
	RetryCount    int
	LastAttemptAt sql.NullString
	LastError     sql.NullString
	SucceededAt   sql.NullString
	FailedAt      sql.NullString
}

// stubProvisioner installs a provisionForPlanFn that returns the given
// (binding, err) pair and counts invocations. Returns the counter; the
// production default is restored on test Cleanup.
func stubProvisioner(t *testing.T, b *Binding, returnErr error) *int64 {
	t.Helper()
	var calls int64
	prev := provisionForPlanFn
	provisionForPlanFn = func(ctx context.Context, db *sql.DB, coaiUserID int64, spec PlanSpec) (*Binding, error) {
		atomic.AddInt64(&calls, 1)
		if returnErr != nil {
			return nil, returnErr
		}
		// Mirror what the real ProvisionForPlan does for a successful
		// provision: write a binding row so a follow-up readback sees
		// it. We use synthetic NewAPI ids so the UNIQUE constraint
		// doesn't collide across test cases.
		bind := b
		if bind == nil {
			bind = &Binding{
				CoaiUserID:     coaiUserID,
				NewapiUserID:   coaiUserID + 1000,
				NewapiTokenID:  coaiUserID + 2000,
				NewapiTokenKey: fmt.Sprintf("sk-stub-%d", coaiUserID),
				LastKnownQuota: spec.QuotaUnits,
			}
		}
		if err := SaveBinding(db, bind); err != nil {
			return nil, fmt.Errorf("stub SaveBinding: %w", err)
		}
		return bind, nil
	}
	t.Cleanup(func() { provisionForPlanFn = prev })
	return &calls
}

// stubPlanLoader installs a loadPlanForRetryFn that returns the given
// planRow regardless of plan id. Restores the production default on
// Cleanup. Used by tests that don't want to seed gtk_plan separately.
func stubPlanLoader(t *testing.T, p *planRow, returnErr error) {
	t.Helper()
	prev := loadPlanForRetryFn
	loadPlanForRetryFn = func(db *sql.DB, planID int64) (*planRow, error) {
		if returnErr != nil {
			return nil, returnErr
		}
		return p, nil
	}
	t.Cleanup(func() { loadPlanForRetryFn = prev })
}

// newWorkerSqliteDB mirrors migration_test.go's newSqliteWithFKDeps but
// opens the SQLite connection with `_loc=auto` so DATETIME columns
// auto-parse into time.Time when scanned into sql.NullTime. The shared
// helper in migration_test.go uses the bare `:memory:` DSN — fine for
// its tests (which only check NULL nulltime cells), but the worker
// tests UPDATE last_attempt_at via CURRENT_TIMESTAMP and read it back
// through PendingProvision.LastAttemptAt → that scan path needs the
// datetime parser enabled.
//
// See: github.com/mattn/go-sqlite3 docs §"DATETIME" — auto-parse only
// fires when the connection's timezone setting is non-zero.
func newWorkerSqliteDB(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	// `_loc=auto` enables go-sqlite3's DATETIME → time.Time auto-parse.
	// We don't use `cache=shared` because each test should get an
	// isolated in-memory DB; the bare `:memory:` form does that.
	db, err := sql.Open("sqlite3", ":memory:?_loc=auto")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE gtk_plan (
		  id            INTEGER PRIMARY KEY AUTOINCREMENT,
		  code          TEXT    NOT NULL UNIQUE,
		  name          TEXT    NOT NULL,
		  type          TEXT    NOT NULL,
		  price_cents   INTEGER NOT NULL,
		  duration_days INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("seed gtk_plan stub: %v", err)
	}
	return db
}

// tests -----------------------------------------------------------------

func TestDrainPendingProvisions_HappyPath(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 42)
	rowID := enqueuePending(t, db, 42, planID, "token_plan", "pending", 0)

	calls := stubProvisioner(t, nil, nil)

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("provision calls = %d, want 1", got)
	}

	got := readRow(t, db, rowID)
	if got.Status != "succeeded" {
		t.Errorf("status = %q, want 'succeeded'", got.Status)
	}
	if !got.SucceededAt.Valid {
		t.Errorf("succeeded_at not set")
	}
	if got.LastError.Valid && got.LastError.String != "" {
		t.Errorf("last_error = %q, want empty/null on success", got.LastError.String)
	}
}

func TestDrainPendingProvisions_TransientFailure(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 43)
	rowID := enqueuePending(t, db, 43, planID, "token_plan", "pending", 0)

	transientErr := errors.New("newapi: read tcp 1.2.3.4:443: i/o timeout")
	stubProvisioner(t, nil, transientErr)

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}

	got := readRow(t, db, rowID)
	if got.Status != "retrying" {
		t.Errorf("status = %q, want 'retrying'", got.Status)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry_count = %d, want 1", got.RetryCount)
	}
	if !got.LastError.Valid || got.LastError.String != transientErr.Error() {
		t.Errorf("last_error = %q (valid=%v), want %q",
			got.LastError.String, got.LastError.Valid, transientErr.Error())
	}
	if !got.LastAttemptAt.Valid {
		t.Errorf("last_attempt_at not set")
	}
	if got.FailedAt.Valid {
		t.Errorf("failed_at should not be set on transient failure")
	}
}

func TestDrainPendingProvisions_MaxRetries(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 44)
	// Start at retry_count=4 so one more failure (→ 5) hits maxRetries=5.
	rowID := enqueuePending(t, db, 44, planID, "token_plan", "retrying", 4)
	// Backdate last_attempt_at far past the 6h backoff window so the row
	// is eligible immediately.
	if _, err := db.Exec(`
		UPDATE gtk_newapi_pending_provisions
		SET last_attempt_at = ?
		WHERE id = ?
	`, time.Now().Add(-24*time.Hour), rowID); err != nil {
		t.Fatalf("backdate last_attempt_at: %v", err)
	}

	stubProvisioner(t, nil, errors.New("newapi: persistent 500"))

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}

	got := readRow(t, db, rowID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want 'failed'", got.Status)
	}
	if got.RetryCount != 5 {
		t.Errorf("retry_count = %d, want 5", got.RetryCount)
	}
	if !got.FailedAt.Valid {
		t.Errorf("failed_at not set")
	}
}

func TestDrainPendingProvisions_BackoffRespected(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 45)
	// Two rows for the same user-plan at retry_count=1 (5min backoff
	// schedule slot). Row "fresh" was just attempted (10s ago, NOT
	// eligible). Row "stale" was attempted long ago (10min, eligible).
	freshID := enqueuePending(t, db, 45, planID, "token_plan", "retrying", 1)
	staleID := enqueuePending(t, db, 45, planID, "token_plan", "retrying", 1)
	if _, err := db.Exec(`
		UPDATE gtk_newapi_pending_provisions SET last_attempt_at = ? WHERE id = ?
	`, time.Now().Add(-10*time.Second), freshID); err != nil {
		t.Fatalf("backdate fresh: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE gtk_newapi_pending_provisions SET last_attempt_at = ? WHERE id = ?
	`, time.Now().Add(-10*time.Minute), staleID); err != nil {
		t.Fatalf("backdate stale: %v", err)
	}

	calls := stubProvisioner(t, nil, nil)

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1 (only stale row eligible)", processed)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("provision calls = %d, want 1", got)
	}

	freshAfter := readRow(t, db, freshID)
	if freshAfter.Status != "retrying" {
		t.Errorf("fresh row status = %q, want unchanged 'retrying'", freshAfter.Status)
	}
	if freshAfter.RetryCount != 1 {
		t.Errorf("fresh row retry_count = %d, want unchanged 1", freshAfter.RetryCount)
	}

	staleAfter := readRow(t, db, staleID)
	if staleAfter.Status != "succeeded" {
		t.Errorf("stale row status = %q, want 'succeeded'", staleAfter.Status)
	}
}

func TestDrainPendingProvisions_BatchLimit(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 46)
	// Enqueue 10 rows under the same user. We use a custom stub that
	// does NOT call SaveBinding so we don't blow up on the binding
	// UNIQUE constraint (newapi_user_id) when the second row tries to
	// write the same synthetic id for the same user.
	for i := 0; i < 10; i++ {
		enqueuePending(t, db, 46, planID, "token_plan", "pending", 0)
	}

	prev := provisionForPlanFn
	var calls int64
	provisionForPlanFn = func(ctx context.Context, db *sql.DB, coaiUserID int64, spec PlanSpec) (*Binding, error) {
		atomic.AddInt64(&calls, 1)
		return &Binding{CoaiUserID: coaiUserID}, nil
	}
	t.Cleanup(func() { provisionForPlanFn = prev })

	processed, err := DrainPendingProvisions(context.Background(), db, 3, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 3 {
		t.Errorf("processed = %d, want 3 (maxBatch)", processed)
	}
	if got := atomic.LoadInt64(&calls); got != 3 {
		t.Errorf("provision calls = %d, want 3", got)
	}

	// Verify the remaining 7 rows are untouched.
	var pendingCount int64
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM gtk_newapi_pending_provisions WHERE status = 'pending'
	`).Scan(&pendingCount); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if pendingCount != 7 {
		t.Errorf("pending count after drain = %d, want 7", pendingCount)
	}
}

func TestDrainPendingProvisions_UnsupportedProvisionType(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 47)
	rowID := enqueuePending(t, db, 47, planID, "service_workflow_rights", "pending", 0)

	// Provisioner should NOT be called for an unsupported type.
	calls := stubProvisioner(t, nil, nil)

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 0 {
		t.Errorf("processed = %d, want 0 (unsupported type skipped)", processed)
	}
	if got := atomic.LoadInt64(calls); got != 0 {
		t.Errorf("provision calls = %d, want 0", got)
	}

	got := readRow(t, db, rowID)
	if got.Status != "pending" {
		t.Errorf("status = %q, want unchanged 'pending'", got.Status)
	}
	if got.RetryCount != 0 {
		t.Errorf("retry_count = %d, want unchanged 0", got.RetryCount)
	}
	if got.LastError.Valid {
		t.Errorf("last_error should be unchanged NULL; got %q", got.LastError.String)
	}
}

func TestPendingProvisionStats(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 48)

	// 2 pending, 1 retrying, 3 succeeded, 1 failed.
	for i := 0; i < 2; i++ {
		enqueuePending(t, db, 48, planID, "token_plan", "pending", 0)
	}
	for i := 0; i < 1; i++ {
		enqueuePending(t, db, 48, planID, "token_plan", "retrying", 1)
	}
	for i := 0; i < 3; i++ {
		enqueuePending(t, db, 48, planID, "token_plan", "succeeded", 0)
	}
	for i := 0; i < 1; i++ {
		enqueuePending(t, db, 48, planID, "token_plan", "failed", 5)
	}

	pending, retrying, succeeded, failed, err := PendingProvisionStats(db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if pending != 2 {
		t.Errorf("pending = %d, want 2", pending)
	}
	if retrying != 1 {
		t.Errorf("retrying = %d, want 1", retrying)
	}
	if succeeded != 3 {
		t.Errorf("succeeded = %d, want 3", succeeded)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

func TestPendingProvisionStats_Empty(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pending, retrying, succeeded, failed, err := PendingProvisionStats(db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if pending+retrying+succeeded+failed != 0 {
		t.Errorf("expected all-zero stats on empty queue; got p=%d r=%d s=%d f=%d",
			pending, retrying, succeeded, failed)
	}
}

func TestDrainPendingProvisionsForever_Cancellation(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// No queue work — we just want to verify the goroutine exits when
	// ctx is canceled. Use a short interval so the test doesn't wait
	// for the default 60s tick.
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		DrainPendingProvisionsForever(ctx, db, 100*time.Millisecond)
		close(done)
	}()

	// Let one tick fire so we know the loop is running.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Goroutine exited as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("DrainPendingProvisionsForever did not exit within 2s of ctx cancellation")
	}
}

// TestDrainPendingProvisions_PlanLookupFailure exercises the
// loadPlanForRetryFn error path: the queue row should be marked as a
// retry (with the plan-load error in last_error), not crash the batch.
func TestDrainPendingProvisions_PlanLookupFailure(t *testing.T) {
	db := newWorkerSqliteDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	planID := seedPlanAndUser(t, db, 49)
	rowID := enqueuePending(t, db, 49, planID, "token_plan", "pending", 0)

	// Stub the plan loader to return an error — simulates "plan was
	// retired" or "DB transient hiccup on the plans read".
	stubPlanLoader(t, nil, errors.New("simulated plan read failure"))
	// Provisioner should NOT be called when plan lookup fails.
	calls := stubProvisioner(t, nil, nil)

	processed, err := DrainPendingProvisions(context.Background(), db, 10, 5)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d, want 1", processed)
	}
	if got := atomic.LoadInt64(calls); got != 0 {
		t.Errorf("provision calls = %d, want 0 (plan lookup failed first)", got)
	}

	got := readRow(t, db, rowID)
	if got.Status != "retrying" {
		t.Errorf("status = %q, want 'retrying'", got.Status)
	}
	if !got.LastError.Valid {
		t.Errorf("last_error should be set")
	}
}

// TestBackoffFor_Bounds sanity-checks the schedule lookup for negative,
// in-range, and overflow retry_count values.
func TestBackoffFor_Bounds(t *testing.T) {
	cases := []struct {
		retryCount int
		wantBase   time.Duration
	}{
		{-5, 1 * time.Minute}, // clamped to idx=0
		{0, 1 * time.Minute},
		{1, 5 * time.Minute},
		{2, 15 * time.Minute},
		{3, 1 * time.Hour},
		{4, 6 * time.Hour},
		{5, 6 * time.Hour},  // clamped to last
		{99, 6 * time.Hour}, // clamped to last
	}
	for _, c := range cases {
		got := backoffFor(c.retryCount)
		// ±10% jitter, so we accept anything in [base*0.9, base*1.1].
		lo := time.Duration(float64(c.wantBase) * 0.89)
		hi := time.Duration(float64(c.wantBase) * 1.11)
		if got < lo || got > hi {
			t.Errorf("backoffFor(%d) = %s, want within [%s, %s] (base=%s ± 10%%)",
				c.retryCount, got, lo, hi, c.wantBase)
		}
	}
}

// TestLoadPlanForRetry_RealLookup uses the real loadPlanForRetry (no
// stub) to verify the SQL projection scans cleanly into the planRow
// struct. Builds its own gtk_plan schema directly (rather than reusing
// newSqliteWithFKDeps) because the migration_test.go stub omits the
// PKG-1 quota_grant column to keep that test focused on PKG-1 schema
// shape; loadPlanForRetry SELECTs quota_grant so we need it present.
func TestLoadPlanForRetry_RealLookup(t *testing.T) {
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE gtk_plan (
		  id            INTEGER PRIMARY KEY AUTOINCREMENT,
		  code          TEXT    NOT NULL UNIQUE,
		  quota_grant   INTEGER,
		  duration_days INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("create gtk_plan: %v", err)
	}
	res, err := db.Exec(`
		INSERT INTO gtk_plan (code, quota_grant, duration_days)
		VALUES ('plan-real-lookup', 500000, 30)
	`)
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	planID, _ := res.LastInsertId()

	got, err := loadPlanForRetry(db, planID)
	if err != nil {
		t.Fatalf("loadPlanForRetry: %v", err)
	}
	if got.Code != "plan-real-lookup" {
		t.Errorf("code = %q, want 'plan-real-lookup'", got.Code)
	}
	if got.QuotaUnits != 500000 {
		t.Errorf("quota_units = %d, want 500000", got.QuotaUnits)
	}
	if got.DurationDays != 30 {
		t.Errorf("duration_days = %d, want 30", got.DurationDays)
	}

	// Also exercise the NULL quota_grant branch — should yield 0.
	if _, err := db.Exec(`
		INSERT INTO gtk_plan (code, quota_grant, duration_days)
		VALUES ('plan-null-quota', NULL, 0)
	`); err != nil {
		t.Fatalf("seed null-quota plan: %v", err)
	}
	var nullPlanID int64
	if err := db.QueryRow(`SELECT id FROM gtk_plan WHERE code = ?`, "plan-null-quota").Scan(&nullPlanID); err != nil {
		t.Fatalf("read null-quota plan id: %v", err)
	}
	got2, err := loadPlanForRetry(db, nullPlanID)
	if err != nil {
		t.Fatalf("loadPlanForRetry null-quota: %v", err)
	}
	if got2.QuotaUnits != 0 {
		t.Errorf("null quota_grant → QuotaUnits = %d, want 0", got2.QuotaUnits)
	}
	if got2.DurationDays != 0 {
		t.Errorf("duration_days = %d, want 0", got2.DurationDays)
	}
}

// TestSqliteEngineFlagSetUnderTest is a defensive guard — newSqliteWithFKDeps
// flips globals.SqliteEngine to true and registers a Cleanup to restore
// it. If a future refactor breaks that contract, the worker's MySQL-only
// branches would silently activate inside SQLite tests and explode.
func TestSqliteEngineFlagSetUnderTest(t *testing.T) {
	_ = newSqliteWithFKDeps(t)
	if !globals.SqliteEngine {
		t.Fatal("globals.SqliteEngine should be true under newSqliteWithFKDeps")
	}
}
