# Codex Prompt — PKG-M1-④ Monthly cron worker

> cat | codex exec --full-auto

# 任务

每天 03:00 SGT 跑一个 cron job:**expire 过期的 gtk_user_plan rows**。Founder priority D3=A 选了"每天 1 次,跟 backup 对齐",非小时级精度。

工作目录:`/Users/brendanxu/tanaxu/greentokey/coai-v0.7-design`
分支:`feat/v0.16-admin-concierge-orders`

# 0. 你是谁 + 前置

greentokey L1 + L2 系统。`gtk_user_plan` 有 `expire_at DATETIME` + `status ENUM('active','expired','canceled')`。当前**没有 cron 把 expired 的 plan flip status**,导致过期 plan 仍然算 active(在 PKG-M1-③ 的 lookupActivePlanID 里被错误返回)。

读这些先:
- `plans/migration.go` schema gtk_user_plan(idx_user_status + idx_order)
- `main.go` 启动顺序:migration → seed → server.Run(看在哪挂 cron Start)
- 任何已有 cron pattern:`grep -rn "time.NewTicker\|cron\." --include="*.go"` 看现有约定

如果项目已有 cron framework(如 robfig/cron 或 stdlib ticker pool),用现有的。不要引入新 dep。

# 1. 要做什么

## 1.1 新建 `billing/cron.go` ~150 LoC

```go
// Package billing implements scheduled jobs for the L2 billing layer.
//
// Currently runs one job:
//   ExpirePlans — flips gtk_user_plan rows whose expire_at < NOW()
//                 from 'active' to 'expired'. Idempotent.
//
// Schedule: daily 03:00 SGT (matches existing /opt/greentokey/backup-mysql.sh
// cadence so DB load coincides with low-traffic window).
//
// Architecture:
//   main.go -> billing.StartCron(db, ctx)
//             |
//             +-> goroutine with stdlib time.Timer:
//             |   - Compute next 03:00 SGT
//             |   - Sleep until then
//             |   - Run ExpirePlans
//             |   - Repeat
//             +-> Returns immediately; cancel via ctx
//
// Stdlib only (no robfig/cron) because the schedule is trivial and
// the job count is one. Adding a dep for one job is over-engineered.
package billing

import (
    "chat/globals"
    "context"
    "database/sql"
    "fmt"
    "time"
)

// SGT = Asia/Singapore (UTC+8). Pinned because the founder's
// backup script + monitoring + on-call window all reference SGT.
var SGT = time.FixedZone("SGT", 8*60*60)

// CronTickHour is the SGT hour the daily job fires. 03:00 = trough
// hour for Asia traffic + matches backup-mysql.sh per founder D3=A.
const CronTickHour = 3

// StartCron launches the daily expiry job. Returns nil if the goroutine
// started cleanly. Cancel via ctx.Done(). Returns an error only if
// the initial goroutine fails to launch (rare).
//
// Idempotent at the process level: calling StartCron twice spawns two
// goroutines and ExpirePlans runs twice each day — undesirable. main.go
// must call this exactly once.
func StartCron(ctx context.Context, db *sql.DB) error {
    go func() {
        // Run once at startup if the job hasn't run today (best-effort —
        // no last-run state yet, so we just always run on boot to
        // catch up on any expired plans missed during downtime).
        if err := ExpirePlans(db); err != nil {
            globals.Warn(fmt.Sprintf("billing: startup ExpirePlans: %s", err))
        }

        for {
            sleepDur := nextTickDelay(time.Now())
            timer := time.NewTimer(sleepDur)
            select {
            case <-ctx.Done():
                timer.Stop()
                globals.Info("billing: cron stopped via ctx")
                return
            case <-timer.C:
                if err := ExpirePlans(db); err != nil {
                    globals.Warn(fmt.Sprintf("billing: ExpirePlans: %s", err))
                }
            }
        }
    }()
    return nil
}

// nextTickDelay returns the duration from `now` until the next
// 03:00 SGT. If now is before today's 03:00 SGT, returns time until
// today; otherwise returns time until tomorrow's 03:00 SGT.
//
// Exposed for testing (faked time injection).
func nextTickDelay(now time.Time) time.Duration {
    nowSGT := now.In(SGT)
    next := time.Date(nowSGT.Year(), nowSGT.Month(), nowSGT.Day(),
        CronTickHour, 0, 0, 0, SGT)
    if !nowSGT.Before(next) {
        next = next.AddDate(0, 0, 1)
    }
    return next.Sub(nowSGT)
}

// ExpirePlans flips gtk_user_plan rows with status='active' AND
// expire_at < CURRENT_TIMESTAMP to status='expired'. Idempotent —
// running twice is a no-op for the second run.
//
// Returns the number of rows flipped + any error. Logs at info level.
func ExpirePlans(db *sql.DB) error {
    res, err := globals.ExecDb(db, `
        UPDATE gtk_user_plan
        SET status = 'expired'
        WHERE status = 'active' AND expire_at < CURRENT_TIMESTAMP
    `)
    if err != nil {
        return fmt.Errorf("expire plans: %w", err)
    }
    n, _ := res.RowsAffected()
    if n > 0 {
        globals.Info(fmt.Sprintf("billing: expired %d gtk_user_plan rows", n))
    } else {
        globals.Debug("billing: no plans to expire")
    }
    return nil
}
```

## 1.2 接通 main.go ~3 行

In `main.go` after `service.Migrate` + `service.SeedCatalog` (around line 145-155 per current shape), before `app.Run`:

```go
import (
    // ... existing imports ...
    "chat/billing"
    "context"
)

// ... in main, after migrations ...
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
if err := billing.StartCron(ctx, connection.DB); err != nil {
    globals.Warn(fmt.Sprintf("billing: cron start failed: %s", err))
}
```

(Pick the right insertion point by reading current main.go — there's already a `connection.DB` initialized + a `ctx`/`cancel` pattern may already exist. Use existing if so.)

## 1.3 测试 `billing/cron_test.go` ~200 LoC, 5-7 cases

Required cases:

1. **ExpirePlans: happy path** — seed 3 active plans, 1 with expire_at past, 1 future, 1 already expired → `ExpirePlans` flips only the past one, returns 1 affected
2. **ExpirePlans: idempotent** — call twice in a row; second call affects 0 rows, no error
3. **ExpirePlans: empty table** — no plans seeded → 0 affected, no error
4. **ExpirePlans: only canceled plans** — status='canceled' AND expire_at past → not touched (canceled is terminal, not flipped)
5. **nextTickDelay: before today's 3am** — now=02:00 SGT → returns ~1h
6. **nextTickDelay: after today's 3am** — now=04:00 SGT → returns ~23h
7. **nextTickDelay: exactly 3am** — now=03:00 SGT → returns 24h (next is tomorrow,not today)

For 5-7, pass injected `time.Time` to `nextTickDelay` directly — no need to fake `time.Now`.

For `StartCron` test, optionally one **integration case**:
- 8. **StartCron + ctx cancel** — start goroutine, cancel ctx after 50ms, verify goroutine exits within 100ms (no zombie)

# 2. Acceptance

```bash
go build ./... 2>&1 | grep -v "warning\|libwebp"  # 0 errors
go test ./billing/... -count=1 -vet=off -v
# Expect 5-7 cases all PASS
grep -n "billing.StartCron" main.go
# Expect 1 match
grep -n "func ExpirePlans\|func nextTickDelay\|func StartCron" billing/cron.go
# Expect 3 matches
```

# 3. Commit split (3 commits)

1. `feat(billing): cron — daily ExpirePlans job at 03:00 SGT` (billing/cron.go + test)
2. `feat(main): wire billing.StartCron into boot` (main.go ~3 行)
3. `docs: PKG-M1-4-DONE.md report`

# 4. Out of scope

- ❌ 不要做月度 quota 重置(LS subscription_updated webhook 未来 path,本 worker 只 expire)
- ❌ 不要 send email / SMS / push notification 给过期用户(P3 future)
- ❌ 不要 cron framework(stdlib ticker 够用)
- ❌ 不要做 admin UI 看 cron 历史(P3 future)
- ❌ 不要持久化 last-run 状态(daily 03:00 触发 + boot 时跑一次,不需要 leader election)
- ❌ 不要碰 gtk_app_usage_log(那是 PKG-M1-③ 的事)

# 5. Done report

`docs/codex-dispatch/PKG-M1-4-DONE.md`,同 W5 格式。

# 6. 风险

| 风险 | 缓解 |
|---|---|
| 多实例部署时 cron 重复跑(2 副本各跑一次)| ExpirePlans 是 idempotent UPDATE,跑 2 次结果同 1 次。本项目当前 1 实例,future 上 leader election 单独做。 |
| 时区配错(server 默认 UTC,SGT 换算错)| `time.FixedZone` 显式写死 +8,test case 5-7 验证 |
| Boot 时跑 ExpirePlans 失败导致服务起不来 | `StartCron` 的 goroutine 内部 catch + warn,不返回 error |
| Test 8 race condition (50ms 窗口) | 用 `time.After(100ms)` 多 buffer,失败重试 |

# 7. References

- `docs/codex-dispatch/W5-PROMPT-FOR-CODEX.md` — prompt 范本
- `infra/scripts/setup-r2-offsite.sh` — 03:00 SGT cadence reference
- `/opt/greentokey/backup-mysql.sh`(VPS only)— 已有 03:00 SGT job
