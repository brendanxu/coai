# PKG-M1-4 Done Report

## Commit Status

Created commit split:

1. `3cb5b3d` `feat(billing): cron — daily ExpirePlans job at 03:00 SGT`
2. `d9913b6` `feat(main): wire billing.StartCron into boot`
3. `docs: PKG-M1-4-DONE.md report`

## What Changed

- Added `billing.StartCron(ctx, db)` with a stdlib `time.Timer` loop.
- Added `billing.ExpirePlans(db)` to flip only `gtk_user_plan` rows where `status='active'` and `expire_at < CURRENT_TIMESTAMP`.
- Added explicit SGT scheduling via `time.FixedZone("SGT", 8*60*60)` and `CronTickHour = 3`.
- Wired the cron worker into `main.go` after migrations and catalog seed, before server startup.
- Added SQLite-backed tests for expiry behavior and schedule math.

## Tests

Verification run:

- `GOCACHE=/tmp/codex-go-cache go test ./billing/... -count=1 -vet=off -v` passed.
- `GOCACHE=/tmp/codex-go-cache go build ./...` passed.

Requested grep evidence:

- `grep -n "billing.StartCron" main.go` returns 1 match at `main.go:163`.
- `grep -n "func ExpirePlans\\|func nextTickDelay\\|func StartCron" billing/cron.go` returns 3 matches.

## Risk Notes

- Multi-instance deployments can run the job once per replica. The `UPDATE` is idempotent, so duplicate runs converge to the same row state.
- Server local timezone does not affect scheduling because the next tick is computed in fixed SGT.
- Startup expiry failure is logged and does not block service boot.

## Known Limitations

- No persisted last-run state or leader election is included; this matches the current single-instance deployment assumption.
- The worker only expires plans. It does not reset monthly quota, notify users, or write cron history.
