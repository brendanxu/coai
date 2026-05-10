// Package billing implements scheduled jobs for the L2 billing layer.
//
// Currently runs one job:
//
//	ExpirePlans - flips gtk_user_plan rows whose expire_at < NOW()
//	              from 'active' to 'expired'. Idempotent.
//
// Schedule: daily 03:00 SGT (matches existing /opt/greentokey/backup-mysql.sh
// cadence so DB load coincides with low-traffic window).
//
// Architecture:
//
//	main.go -> billing.StartCron(ctx, db)
//	          |
//	          +-> goroutine with stdlib time.Timer:
//	              - Compute next 03:00 SGT
//	              - Sleep until then
//	              - Run ExpirePlans
//	              - Repeat
//	          +-> Returns immediately; cancel via ctx
//
// Stdlib only (no robfig/cron) because the schedule is trivial and the job
// count is one. Adding a dependency for one job is over-engineered.
package billing

import (
	"chat/globals"
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SGT = Asia/Singapore (UTC+8). Pinned because the founder's backup script,
// monitoring, and on-call window all reference SGT.
var SGT = time.FixedZone("SGT", 8*60*60)

// CronTickHour is the SGT hour the daily job fires. 03:00 = trough hour for
// Asia traffic + matches backup-mysql.sh per founder D3=A.
const CronTickHour = 3

// StartCron launches the daily expiry job. Returns nil if the goroutine
// started cleanly. Cancel via ctx.Done(). Returns an error only if the initial
// goroutine fails to launch (rare).
//
// Idempotent at the process level: calling StartCron twice spawns two
// goroutines and ExpirePlans runs twice each day, which is undesirable.
// main.go must call this exactly once.
func StartCron(ctx context.Context, db *sql.DB) error {
	go func() {
		// Run once at startup to catch up on expired plans missed during
		// downtime. There is no persisted last-run state yet.
		if _, err := ExpirePlans(db); err != nil {
			globals.Warn(fmt.Sprintf("billing: startup ExpirePlans: %s", err))
		}

		for {
			timer := time.NewTimer(nextTickDelay(time.Now()))
			select {
			case <-ctx.Done():
				timer.Stop()
				globals.Info("billing: cron stopped via ctx")
				return
			case <-timer.C:
				if _, err := ExpirePlans(db); err != nil {
					globals.Warn(fmt.Sprintf("billing: ExpirePlans: %s", err))
				}
			}
		}
	}()
	return nil
}

// nextTickDelay returns the duration from now until the next 03:00 SGT. If now
// is before today's 03:00 SGT, returns time until today; otherwise returns time
// until tomorrow's 03:00 SGT.
//
// Exposed for testing via same-package tests.
func nextTickDelay(now time.Time) time.Duration {
	nowSGT := now.In(SGT)
	next := time.Date(nowSGT.Year(), nowSGT.Month(), nowSGT.Day(),
		CronTickHour, 0, 0, 0, SGT)
	if !nowSGT.Before(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(nowSGT)
}

// ExpirePlans flips gtk_user_plan rows with status='active' AND expire_at <
// CURRENT_TIMESTAMP to status='expired'. Idempotent: running twice is a no-op
// for the second run.
//
// Returns the number of rows flipped plus any error. Logs at info/debug level.
func ExpirePlans(db *sql.DB) (int64, error) {
	res, err := globals.ExecDb(db, `
		UPDATE gtk_user_plan
		SET status = 'expired'
		WHERE status = 'active' AND expire_at < CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, fmt.Errorf("expire plans: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		globals.Info(fmt.Sprintf("billing: expired %d gtk_user_plan rows", n))
	} else {
		globals.Debug("billing: no plans to expire")
	}
	return n, nil
}
