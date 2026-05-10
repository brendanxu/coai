// retry_worker.go — drain worker for gtk_newapi_pending_provisions
// (PKG-3 PKG-TOKEN-PRODUCT-RENTAL, L23 §17 of the architecture doc).
//
// Why this exists: PKG-2's commerce.GrantEntitlement classifies ALL NewAPI
// failures as transient (v0 simplification per plan v2 §B3) and enqueues a
// row into gtk_newapi_pending_provisions. The user's gtk_user_plan row
// already says 'active' — i.e. the entitlement record is consistent with
// "user paid" — but the actual sk-xxx token has not yet been issued. This
// worker is what closes that gap: pulls pending rows, calls
// ProvisionForPlan, and either succeeds (user gets a binding row + key)
// or backs off + retries.
//
// Scope (v0):
//
//   - ONLY handles provision_type='token_plan'. The
//     'service_workflow_rights' enum value is reserved for future
//     entitlements that need NewAPI-side state changes (per-service
//     quota carve-outs); for v0 the worker logs + skips and the row
//     stays untouched. PKG that adds service_workflow_rights handling
//     will extend the dispatch switch here.
//
//   - Best-effort concurrency: the MySQL path uses SELECT ... FOR UPDATE
//     SKIP LOCKED so multiple greentokey replicas can run the worker
//     without double-processing the same row. SQLite (test driver)
//     ignores the locking clause; SQLite serializes writes at the
//     connection level so test correctness doesn't depend on it.
//
//   - All NewAPI errors are transient (matching commerce/entitlement.go
//     §B3 note). After retry_count >= maxRetries the row is marked
//     'failed' and bin/check-pending-provisioning.sh's threshold catches
//     persistent issues for human review.
//
// Cron alignment: bin/check-pending-provisioning.sh fires every 5 minutes
// and alerts on stuck rows older than 30 minutes. The worker pulses every
// 60s by default so a transient NewAPI blip resolves well before the cron
// alert fires.

package newapi

import (
	"chat/globals"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// provisionForPlanFn is the test-swappable handle on ProvisionForPlan,
// mirroring the seam pattern in commerce/entitlement.go. Tests in
// retry_worker_test.go swap this to a deterministic stub and restore on
// Cleanup.
//
// Why a function var instead of an interface: same trade-off as
// commerce/entitlement.go documents — the surface is one function, all
// callers are in this package, and the alternative (refactoring
// ProvisionForPlan to take an interface) would ripple into payment/ and
// commerce/ for no test-isolation gain.
var provisionForPlanFn = ProvisionForPlan

// loadPlanForRetryFn is the test-swappable handle on the per-row plan
// lookup. Tests inject a synthetic planRow without seeding gtk_plan
// fully (which would require importing plans/ test fixtures + the FK
// chain back through gtk_service).
var loadPlanForRetryFn = loadPlanForRetry

// Default backoff schedule. retry_count 0 → 1m, 1 → 5m, 2 → 15m,
// 3 → 1h, 4 → 6h. After the schedule is exhausted the worker uses the
// last value (6h). Real wall-clock used so tests must clock-control via
// last_attempt_at directly (the worker doesn't take a clock injector for
// v0; if test-flake from real-time arises, refactor).
var defaultBackoffSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// backoffFor returns the wall-clock interval that must elapse since
// last_attempt_at before a row at the given retry_count is eligible to
// re-run. Adds ±10% jitter so a queue full of rows that hit a brief
// outage at the same wall-clock minute don't all wake up together and
// pound NewAPI in a thundering herd.
func backoffFor(retryCount int) time.Duration {
	idx := retryCount
	if idx < 0 {
		idx = 0
	}
	if idx >= len(defaultBackoffSchedule) {
		idx = len(defaultBackoffSchedule) - 1
	}
	base := defaultBackoffSchedule[idx]
	// ±10% jitter. rand.Int63n requires a positive arg; base is always > 0.
	jitterRange := int64(base) / 5 // 20% of base = ±10% range
	if jitterRange <= 0 {
		return base
	}
	jitter := time.Duration(rand.Int63n(jitterRange) - jitterRange/2)
	return base + jitter
}

// claimedRow is the worker-internal projection of a pending queue row.
// Carries everything needed to drive the per-row provision attempt + the
// state-update writebacks.
type claimedRow struct {
	ID            int64
	UserID        int64
	PlanID        int64
	ProvisionType string
	RetryCount    int
}

// planRow is the worker-internal projection of the few gtk_plan columns
// the worker needs. Kept local (vs. importing plans.Plan) so the worker
// doesn't pull plans/ as a dependency just for a 3-column SELECT — and
// so the test seam can stub without faking a full plans.Plan.
type planRow struct {
	Code         string
	QuotaUnits   int64 // gtk_plan.quota_grant; 0 if NULL
	DurationDays int64
}

// DrainPendingProvisions reads up to maxBatch rows from the pending queue
// and attempts to provision each one. See the package doc for the full
// state-machine semantics.
//
// Returns the number of rows on which ProvisionForPlan was actually
// called (regardless of success/failure outcome) plus any DB driver
// error. Per-row provisioning failures are logged + recorded in
// last_error on the row; they don't propagate as a Go error from this
// function so a single bad row doesn't poison the rest of the batch.
//
// Default tuning: maxBatch=10 keeps wall-clock per drain bounded
// (NewAPI provisioning takes ~500ms-2s per call — 10 rows ~ 5-20s).
// maxRetries=5 with the default schedule covers ~7h of backoff total
// before a row is marked 'failed' for human review.
func DrainPendingProvisions(ctx context.Context, db *sql.DB, maxBatch int, maxRetries int) (int, error) {
	if maxBatch <= 0 {
		maxBatch = 10
	}
	if maxRetries <= 0 {
		maxRetries = 5
	}

	rows, err := selectClaimable(db, maxBatch)
	if err != nil {
		return 0, fmt.Errorf("newapi.DrainPendingProvisions: select: %w", err)
	}

	processed := 0
	for _, row := range rows {
		// ctx cancellation: bail out cleanly between rows so a shutdown
		// signal doesn't leave a row half-processed.
		if err := ctx.Err(); err != nil {
			return processed, err
		}

		// v0: only token_plan rows are drained. service_workflow_rights
		// is reserved (see package doc).
		if row.ProvisionType != "token_plan" {
			globals.Warn(fmt.Sprintf(
				"newapi.DrainPendingProvisions: row %d has unsupported provision_type=%q; skipping (no state change)",
				row.ID, row.ProvisionType))
			continue
		}

		if err := processRow(ctx, db, row, maxRetries); err != nil {
			// processRow only returns DB driver errors; per-row
			// provisioning failures are recorded in-row and return nil.
			// A driver error here means the DB itself is unhappy —
			// stop the batch so we don't burn time on more failed
			// writes.
			return processed, fmt.Errorf("newapi.DrainPendingProvisions: process row %d: %w", row.ID, err)
		}
		processed++
	}
	return processed, nil
}

// selectClaimable reads up to maxBatch rows that are due for a retry
// attempt. Filter: status IN ('pending','retrying'), AND the backoff
// interval since last_attempt_at has elapsed (NULL last_attempt_at = first
// attempt, always eligible).
//
// Backoff is enforced in Go (not SQL) because the per-row interval depends
// on retry_count; encoding the schedule in a CASE WHEN would couple the
// schema to the schedule and break engine-portability. Trade-off: we read
// rows that may be filtered out, but maxBatch caps the read volume.
func selectClaimable(db *sql.DB, maxBatch int) ([]claimedRow, error) {
	// Query 2x the batch size as a head-room buffer for the in-Go
	// backoff filter (rows might be 'retrying' but not yet eligible).
	// Caps the per-query DB load while still surfacing enough work.
	queryLimit := maxBatch * 2
	if queryLimit > 100 {
		queryLimit = 100
	}

	q := `
		SELECT id, user_id, plan_id, provision_type, retry_count, last_attempt_at
		FROM gtk_newapi_pending_provisions
		WHERE status IN ('pending', 'retrying')
		ORDER BY id ASC
		LIMIT ?`
	// MySQL: append SKIP LOCKED for multi-replica safety. SQLite ignores
	// FOR UPDATE silently — we use the simpler form there.
	if !globals.SqliteEngine {
		q = `
		SELECT id, user_id, plan_id, provision_type, retry_count, last_attempt_at
		FROM gtk_newapi_pending_provisions
		WHERE status IN ('pending', 'retrying')
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED`
	}

	rs, err := db.Query(q, queryLimit)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rs.Close()

	now := time.Now()
	var out []claimedRow
	for rs.Next() {
		var (
			row           claimedRow
			lastAttemptAt sql.NullString // see scanDateTime doc
		)
		if err := rs.Scan(
			&row.ID, &row.UserID, &row.PlanID, &row.ProvisionType,
			&row.RetryCount, &lastAttemptAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		// Backoff filter: if last_attempt_at + backoff > now, skip.
		if lastAttemptAt.Valid && lastAttemptAt.String != "" {
			parsed, perr := parseDateTime(lastAttemptAt.String)
			if perr != nil {
				// Treat unparseable timestamps as "first attempt" — log
				// + let the row drain. This shouldn't happen in
				// practice since both engines store ISO-8601-ish
				// strings; defensive against schema drift.
				globals.Warn(fmt.Sprintf(
					"newapi.DrainPendingProvisions: row %d has unparseable last_attempt_at=%q (%v); treating as eligible",
					row.ID, lastAttemptAt.String, perr))
			} else {
				eligibleAt := parsed.Add(backoffFor(row.RetryCount))
				if eligibleAt.After(now) {
					continue
				}
			}
		}
		out = append(out, row)
		if len(out) >= maxBatch {
			break
		}
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iter pending: %w", err)
	}
	return out, nil
}

// parseDateTime parses the wire-format DATETIME string both MySQL and
// SQLite produce. Layouts tried, in order:
//
//   - "2006-01-02 15:04:05.999999-07:00" (SQLite via go-sqlite3 with
//     _loc=auto / time-bound DSN — note the SPACE separator, not T)
//   - "2006-01-02 15:04:05" (MySQL DATETIME default + SQLite
//     CURRENT_TIMESTAMP without fractional / TZ)
//   - time.RFC3339 (forward-compat for drivers that switch to ISO-8601)
//
// Why we don't use sql.NullTime: it requires parseTime=true on MySQL
// (not currently set in connection/database.go) AND a column declared
// 'DATETIME' in SQLite (PreflightSql rewrites DATETIME → TEXT, so
// go-sqlite3's auto-parse never fires for our columns). NullString +
// manual parse is the engine-agnostic path.
func parseDateTime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("parseDateTime: unrecognized layout %q", s)
}

// loadPlanForRetry reads the three gtk_plan columns the worker needs to
// build a PlanSpec. Kept package-private so the test seam owns the
// stub-able function var (loadPlanForRetryFn).
func loadPlanForRetry(db *sql.DB, planID int64) (*planRow, error) {
	var (
		p          planRow
		quotaGrant sql.NullInt64
	)
	err := db.QueryRow(`
		SELECT code, quota_grant, duration_days
		FROM gtk_plan
		WHERE id = ?
	`, planID).Scan(&p.Code, &quotaGrant, &p.DurationDays)
	if err != nil {
		return nil, fmt.Errorf("select gtk_plan id=%d: %w", planID, err)
	}
	if quotaGrant.Valid {
		p.QuotaUnits = quotaGrant.Int64
	}
	return &p, nil
}

// processRow attempts ProvisionForPlan for one claimed row and updates
// the row's status accordingly. Returns a Go error ONLY for DB driver
// failures (caller stops the batch); provisioning failures are recorded
// in-row.
func processRow(ctx context.Context, db *sql.DB, row claimedRow, maxRetries int) error {
	// Look up the plan row to construct PlanSpec. This is the trade-off
	// vs. denormalizing quota_grant + code into the queue table at
	// enqueue time: the plan row is the source of truth for quota and
	// code, and reading it on retry means a plan-config update lands
	// for in-flight retries too.
	plan, err := loadPlanForRetryFn(db, row.PlanID)
	if err != nil {
		// Plan vanished or DB read failed. Record the error on the row
		// so check-pending-provisioning.sh can surface it; this is not
		// a transient NewAPI failure (the schema FK ON DELETE RESTRICT
		// should prevent the vanish case anyway), but bumping
		// retry_count + storing the message gets us audit signal.
		return recordProvisionFailure(db, row, fmt.Sprintf("load plan %d: %v", row.PlanID, err), maxRetries)
	}

	spec := PlanSpec{
		Code:       plan.Code,
		QuotaUnits: plan.QuotaUnits,
		// duration_days drives token expiry. 0 → never-expire (PlanSpec
		// treats zero ExpiresAt as the NewAPI -1 sentinel).
		ExpiresAt: expiryForPlan(plan),
		// Group intentionally empty: token-plan users default to NewAPI
		// 'default' group. SaveBinding normalizes empty → "default".
	}

	bind, provErr := provisionForPlanFn(ctx, db, row.UserID, spec)
	if provErr != nil {
		return recordProvisionFailure(db, row, provErr.Error(), maxRetries)
	}

	// Success: ProvisionForPlan already wrote the binding row via
	// SaveBinding. We only need to mark this queue row 'succeeded'.
	_ = bind // bind is the new Binding; nothing else to do with it here.
	return recordProvisionSuccess(db, row.ID)
}

// recordProvisionSuccess flips the queue row to status='succeeded' +
// stamps succeeded_at. Best-effort: a write failure here means we'll
// re-attempt next drain cycle (and ProvisionForPlan is idempotent at the
// binding-row level — SaveBinding upserts).
func recordProvisionSuccess(db *sql.DB, rowID int64) error {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_newapi_pending_provisions
		SET status = 'succeeded',
		    succeeded_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rowID)
	if err != nil {
		return fmt.Errorf("mark succeeded: %w", err)
	}
	globals.Info(fmt.Sprintf(
		"newapi.DrainPendingProvisions: row %d succeeded", rowID))
	return nil
}

// recordProvisionFailure bumps retry_count + writes last_error +
// last_attempt_at. If retry_count would exceed maxRetries, marks the row
// 'failed' instead so the cron alert / admin dashboard surfaces it.
func recordProvisionFailure(db *sql.DB, row claimedRow, lastError string, maxRetries int) error {
	newRetryCount := row.RetryCount + 1
	// Truncate last_error to fit a TEXT column without exploding row
	// width (MySQL TEXT is 65535 bytes; we stay well under).
	if len(lastError) > 1024 {
		lastError = lastError[:1024]
	}

	if newRetryCount >= maxRetries {
		_, err := globals.ExecDb(db, `
			UPDATE gtk_newapi_pending_provisions
			SET status = 'failed',
			    retry_count = ?,
			    last_error = ?,
			    last_attempt_at = CURRENT_TIMESTAMP,
			    failed_at = CURRENT_TIMESTAMP,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, newRetryCount, lastError, row.ID)
		if err != nil {
			return fmt.Errorf("mark failed: %w", err)
		}
		globals.Warn(fmt.Sprintf(
			"newapi.DrainPendingProvisions: row %d hit maxRetries=%d, marked failed; last_error=%s",
			row.ID, maxRetries, lastError))
		return nil
	}

	_, err := globals.ExecDb(db, `
		UPDATE gtk_newapi_pending_provisions
		SET status = 'retrying',
		    retry_count = ?,
		    last_error = ?,
		    last_attempt_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, newRetryCount, lastError, row.ID)
	if err != nil {
		return fmt.Errorf("mark retrying: %w", err)
	}
	globals.Info(fmt.Sprintf(
		"newapi.DrainPendingProvisions: row %d retry_count=%d, last_error=%s",
		row.ID, newRetryCount, lastError))
	return nil
}

// expiryForPlan converts gtk_plan.duration_days into a wall-clock
// ExpiresAt. duration_days <= 0 → zero time = never-expire (PlanSpec
// converts to NewAPI sentinel -1).
func expiryForPlan(plan *planRow) time.Time {
	if plan.DurationDays <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(plan.DurationDays) * 24 * time.Hour)
}

// DrainPendingProvisionsForever runs DrainPendingProvisions in a loop on
// the given interval. Returns when ctx is canceled. Used by main.go's
// boot goroutine.
//
// Per-tick errors are logged but don't stop the loop — a transient DB
// hiccup shouldn't kill the worker. The loop sleeps `interval` between
// ticks regardless of whether the previous tick processed work or not;
// a smarter scheme (sleep less when there's backlog) is a follow-up.
func DrainPendingProvisionsForever(ctx context.Context, db *sql.DB, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	globals.Info(fmt.Sprintf(
		"newapi.DrainPendingProvisionsForever: starting; interval=%s", interval))

	// Tick once immediately so a freshly-booted process drains any
	// backlog without waiting for the first interval.
	if _, err := DrainPendingProvisions(ctx, db, 0, 0); err != nil && !errors.Is(err, context.Canceled) {
		globals.Warn(fmt.Sprintf(
			"newapi.DrainPendingProvisionsForever: initial drain failed: %v", err))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			globals.Info("newapi.DrainPendingProvisionsForever: context canceled, exiting")
			return
		case <-ticker.C:
			if _, err := DrainPendingProvisions(ctx, db, 0, 0); err != nil && !errors.Is(err, context.Canceled) {
				globals.Warn(fmt.Sprintf(
					"newapi.DrainPendingProvisionsForever: tick failed: %v", err))
			}
		}
	}
}

// PendingProvisionStats returns the current count of queue rows by
// status. Useful for ops dashboards + the bin/check-pending-provisioning.sh
// cron's threshold check. Engine-agnostic; one query, four counts.
//
// Returns (pending, retrying, succeeded, failed, error).
func PendingProvisionStats(db *sql.DB) (pending, retrying, succeeded, failed int64, err error) {
	rows, err := db.Query(`
		SELECT status, COUNT(*)
		FROM gtk_newapi_pending_provisions
		GROUP BY status
	`)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("newapi.PendingProvisionStats: query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			status string
			count  int64
		)
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("newapi.PendingProvisionStats: scan: %w", err)
		}
		switch status {
		case "pending":
			pending = count
		case "retrying":
			retrying = count
		case "succeeded":
			succeeded = count
		case "failed":
			failed = count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("newapi.PendingProvisionStats: iter: %w", err)
	}
	return pending, retrying, succeeded, failed, nil
}
