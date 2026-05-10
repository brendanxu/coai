# Codex Prompt — PKG-M1-①+② Recharge worker

> cat | codex exec --full-auto

# 任务

实施 **L2 token 套餐充值**:LemonSqueezy + 虎皮椒 webhook 收到付款 → 创建 `gtk_user_plan` row + `user.IncreaseQuota(plan.quota_config.quota)`。Provider-agnostic,共享一个 redeem 函数。Idempotent。

工作目录:`/Users/brendanxu/tanaxu/greentokey/coai-v0.7-design`
分支:`feat/v0.16-admin-concierge-orders`(继续在这上面 commit)

# 0. 你是谁 + greentokey 三层架构

greentokey 是 BYOK + token 分销 + 民宿 SaaS 后端。3 层栈:
- **L1**:NewAPI 网关(转 token 调用到上游 LLM)
- **L2**(本任务范围):token 套餐 + 支付 — 客户买"100K tokens for ¥X / 月" 套餐,充进 quota,然后用 sk-tnx-... 调用 chat 时扣
- **L3**(不要碰):民宿 SaaS 服务市场,有自己的 `gtk_service_order` 表,一次性服务订单(不在本任务范围)

L2 + L3 共享同一套 LS / 虎皮椒 webhook 入口,但 **payload 里 custom_data 不同**:
- L2 token plan 充值:`meta.custom_data = {"plan_id": "<id>", "user_id": "<id>", "type": "plan"}`
- L3 service order:`meta.custom_data = {"order_no": "<order_no>", "user_id": "<id>", "type": "service"}`(已就绪)

本任务只处理 L2 path。L3 path 现有代码不动。

# 1. 已落地不动的代码

- `payment/lemonsqueezy.go::HandleWebhook` — LS webhook HMAC + idempotent + classify + dispatch (~580 行)。**不重写**,只在已有 dispatch 上加 L2 分支。
- `service/webhook_handler.go::MarkOrderPaid` — L3 service order 的 status flip,**不动**。
- `service/webhook_handler.go::HupijiaoCallback` — 虎皮椒 GET callback handler,~250 行,只服务 L3 service。**加 L2 分支**。
- `auth/quota.go::IncreaseQuota / DecreaseQuota / SetUsedQuota` — quota 数学,**直接复用**。
- `plans/migration.go` schema:
  ```sql
  gtk_plan(id, code UNIQUE, name, type ENUM('subscription','pack'), price_cents,
           duration_days, quota_config JSON, is_active, created_at)
  gtk_user_plan(id, user_id FK auth, plan_id FK gtk_plan, status ENUM('active','expired','canceled'),
                expire_at, remaining JSON, purchased_at, order_id UNIQUE,
                indexes: idx_user_status, idx_order)
  ```
  `quota_config` JSON example: `{"quota": 100000, "models": ["claude-*"]}` — operator-set per plan.
  `order_id` UNIQUE = idempotency anchor。

# 2. 要做什么

## 2.1 新建 `auth/recharge.go` ~150 LoC

```go
package auth

import (
    "chat/connection"
    "chat/globals"
    "chat/plans"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "time"
)

// QuotaConfig is the parsed shape of gtk_plan.quota_config JSON.
// Currently only `quota` is consumed by recharge; `models` is a future
// allowlist that the chat handler will filter against (out of scope).
type QuotaConfig struct {
    Quota  float32  `json:"quota"`
    Models []string `json:"models,omitempty"`
}

// RedeemPlanForOrder is the provider-agnostic redeem entrypoint. Both
// LemonSqueezy and 虎皮椒 webhook handlers call this once their event
// is verified + classified as L2 token plan (custom_data.type=="plan").
//
// Idempotent: gtk_user_plan.order_id is UNIQUE; second call with the
// same orderID is a no-op (returns nil + logs).
//
// Atomicity: creates gtk_user_plan row AND IncreaseQuota in a single
// SQL transaction. Either both succeed or both rollback — partial
// state ("quota added but no plan binding") is impossible.
func RedeemPlanForOrder(
    db *sql.DB,
    userID int64,
    planCode string,   // gtk_plan.code, e.g. "starter-100k"
    orderID string,    // LS order id OR hupijiao trade_no — UNIQUE across both providers
) error {
    if userID == 0 || planCode == "" || orderID == "" {
        return errors.New("auth: RedeemPlanForOrder requires userID + planCode + orderID")
    }

    tx, err := db.Begin()
    if err != nil { return fmt.Errorf("redeem: begin tx: %w", err) }
    defer tx.Rollback() // safe no-op after Commit

    // 1. Idempotency check — has this orderID redeemed before?
    var existing int64
    if err := tx.QueryRow(`SELECT id FROM gtk_user_plan WHERE order_id = ?`, orderID).Scan(&existing); err != nil {
        if !errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("redeem: check existing: %w", err)
        }
    } else {
        globals.Info(fmt.Sprintf("auth: order %s already redeemed (gtk_user_plan id=%d) — idempotent no-op", orderID, existing))
        return nil
    }

    // 2. Read plan + parse quota_config.
    var plan plans.Plan
    var rawConfig sql.NullString
    err = tx.QueryRow(`
        SELECT id, code, name, type, price_cents, duration_days, quota_config, is_active
        FROM gtk_plan WHERE code = ? AND is_active = TRUE
    `, planCode).Scan(&plan.ID, &plan.Code, &plan.Name, &plan.Type, &plan.PriceCents, &plan.DurationDays, &rawConfig, &plan.IsActive)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("redeem: plan code=%q not found or inactive", planCode)
        }
        return fmt.Errorf("redeem: read plan: %w", err)
    }
    var cfg QuotaConfig
    if rawConfig.Valid && rawConfig.String != "" {
        if err := json.Unmarshal([]byte(rawConfig.String), &cfg); err != nil {
            return fmt.Errorf("redeem: parse quota_config %q: %w", rawConfig.String, err)
        }
    }
    if cfg.Quota <= 0 {
        return fmt.Errorf("redeem: plan %q has non-positive quota %f", planCode, cfg.Quota)
    }

    // 3. INSERT gtk_user_plan.
    expireAt := time.Now().AddDate(0, 0, int(plan.DurationDays))
    _, err = tx.Exec(`
        INSERT INTO gtk_user_plan (user_id, plan_id, status, expire_at, order_id, purchased_at)
        VALUES (?, ?, 'active', ?, ?, CURRENT_TIMESTAMP)
    `, userID, plan.ID, expireAt, orderID)
    if err != nil {
        return fmt.Errorf("redeem: insert gtk_user_plan: %w", err)
    }

    // 4. IncreaseQuota — use the existing UPSERT path so users without a
    // quota row get one created (CreateInitialQuota wasn't called for
    // every signup path).
    _, err = tx.Exec(`
        INSERT INTO quota (user_id, quota, used) VALUES (?, ?, ?)
        ON DUPLICATE KEY UPDATE quota = quota + ?
    `, userID, cfg.Quota, 0., cfg.Quota)
    // SQLite variant: ON CONFLICT(user_id) DO UPDATE SET quota = quota + ?
    if err != nil {
        // Check if SQLite path needed.
        if globals.SqliteEngine {
            _, err = tx.Exec(`
                INSERT INTO quota (user_id, quota, used) VALUES (?, ?, ?)
                ON CONFLICT(user_id) DO UPDATE SET quota = quota + ?
            `, userID, cfg.Quota, 0., cfg.Quota)
        }
        if err != nil {
            return fmt.Errorf("redeem: increase quota: %w", err)
        }
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("redeem: commit: %w", err)
    }

    globals.Info(fmt.Sprintf("auth: redeemed plan=%s for user=%d order=%s quota+=%f",
        planCode, userID, orderID, cfg.Quota))
    return nil
}
```

**Verify before writing the actual code**:
- Read `auth/quota.go` to confirm UPSERT shape (MySQL `ON DUPLICATE KEY UPDATE` vs SQLite `ON CONFLICT`)
- Read `plans/types.go` to confirm Plan struct shape
- Read `globals/sql.go` to use `globals.ExecDb` / `globals.QueryRowDb` if they handle dialect dispatch (they do)

If `globals.ExecDb` handles dialect — refactor to use it instead of raw `tx.Exec`.

## 2.2 LS webhook 加 L2 分支 (~30 行 in payment/lemonsqueezy.go)

Find the dispatch sites at `payment/lemonsqueezy.go:340-360` (where `MarkOrderPaid` is called). Add a branch BEFORE the existing call:

```go
// Detect L2 token plan vs L3 service order via custom_data.
type customData struct {
    Type     string `json:"type"`      // "plan" (L2) | "service" (L3)
    PlanCode string `json:"plan_code"` // L2 only
    UserID   int64  `json:"user_id"`   // both
    OrderNo  string `json:"order_no"`  // L3 only
}
var cd customData
if err := json.Unmarshal(p.Meta.CustomData, &cd); err != nil {
    globals.Warn("ls webhook: malformed custom_data, falling through to L3 path")
}

if cd.Type == "plan" && cd.PlanCode != "" && cd.UserID > 0 {
    // L2 token plan path — call auth.RedeemPlanForOrder.
    err := auth.RedeemPlanForOrder(db, cd.UserID, cd.PlanCode, p.Data.ID)
    if err != nil {
        return fmt.Errorf("ls webhook: redeem plan: %w", err)
    }
    return nil // skip L3 MarkOrderPaid path
}

// fall through to existing L3 MarkOrderPaid call (unchanged)
return service.MarkOrderPaid(db, orderNo, p.Data.ID, "lemonsqueezy")
```

Find where `p.Meta.CustomData` is parsed in current code; if it's already typed as map/struct, extend that type instead of redefining `customData`. **Don't break the existing L3 flow.**

## 2.3 虎皮椒 callback 加 L2 分支 (~30 行 in service/webhook_handler.go)

`HupijiaoCallback` currently parses query params (虎皮椒 sends GET callback). Add custom data via `attach` field (虎皮椒 supports a free-text passthrough field):
- 虎皮椒 checkout pre-fills `attach=plan:starter-100k:user:42`
- Callback returns it verbatim
- Parse and dispatch to RedeemPlanForOrder

```go
attach := c.Query("attach")
if strings.HasPrefix(attach, "plan:") {
    parts := strings.Split(attach, ":")
    if len(parts) == 4 {
        planCode := parts[1]
        userID, _ := strconv.ParseInt(parts[3], 10, 64)
        if err := auth.RedeemPlanForOrder(connection.DB, userID, planCode, hupijiaoTxID); err != nil {
            globals.Warn(fmt.Sprintf("hupijiao redeem failed: %v", err))
            // don't tell hupijiao we failed — let them retry
            c.String(http.StatusInternalServerError, "fail")
            return
        }
        c.String(http.StatusOK, "success")
        return
    }
}
// fall through to existing L3 MarkOrderPaid path
```

## 2.4 测试 (`auth/recharge_test.go` ~250 LoC, 8-10 cases)

Mirror the harness in `payment/migration_test.go` (in-memory SQLite, seed auth + plans tables).

Required cases:
1. **Happy path** — fresh order, plan exists, user exists → gtk_user_plan row + quota incremented by `quota_config.quota`
2. **Idempotent re-run** — same orderID called twice, second call no-op, quota unchanged
3. **Concurrent re-entry** — UNIQUE(order_id) constraint surfaces; both callers see "already redeemed"
4. **Inactive plan** — plan.is_active=false → returns error, no DB changes
5. **Unknown plan code** — returns error, no DB changes
6. **Non-positive quota_config** — plan exists but quota=0 → returns error
7. **Malformed quota_config JSON** — returns error
8. **expire_at math** — duration_days=30 → expire_at ≈ NOW + 30 days (within 60s tolerance)
9. **User with existing quota** — IncreaseQuota path adds (not replaces)
10. **Tx rollback on quota fail** — simulate quota INSERT failure → gtk_user_plan row also rolled back

Use a faked time helper for case 8 if useful.

# 3. Acceptance criteria

```bash
go build ./... 2>&1 | grep -v "warning\|libwebp"  # 0 errors
go test ./auth/... ./payment/... -count=1 -vet=off 2>&1 | grep -E "FAIL|ok "
# Expect:
# ok    chat/auth       (含新 recharge_test.go all PASS)
# ok    chat/payment    (旧 webhook tests 仍 PASS)
```

Grep evidence:
```bash
grep -n "RedeemPlanForOrder" auth/recharge.go payment/lemonsqueezy.go service/webhook_handler.go
# Expect ≥ 3 matches (1 declaration + 2 call sites)
grep -n "ON DUPLICATE KEY\|ON CONFLICT" auth/recharge.go
# Expect ≥ 2 (MySQL + SQLite branches)
```

# 4. Commit split (4 commits)

1. `feat(auth): RedeemPlanForOrder — provider-agnostic L2 plan redeem` (auth/recharge.go + test)
2. `feat(payment): LS webhook L2 plan dispatch` (lemonsqueezy.go branch)
3. `feat(service): hupijiao callback L2 plan dispatch` (webhook_handler.go branch)
4. `docs: PKG-M1-1+2-DONE.md report`

Commit message format follows W5 commits — see git log for examples (`0e72388`, `59c5b26`, `bb9655b`, `6cf8281`).

# 5. Out of scope

- ❌ 不要改 `service/webhook_handler.go::MarkOrderPaid`(L3 路径,跟 L2 无关)
- ❌ 不要 plan/admin UI(P3 work)
- ❌ 不要做月度 quota 重置(那是 PKG-M1-④,平行 worker 在做)
- ❌ 不要写入 gtk_app_usage_log(那是 PKG-M1-③,平行 worker)
- ❌ 不要 seed gtk_plan rows(operator 的事,test 自己 seed)
- ❌ 不要碰 `app/src/`(前端)
- ❌ 不要 push origin(tana 整合后统一 push)

# 6. Done report

完成时写到 `docs/codex-dispatch/PKG-M1-1+2-DONE.md`(类似 W5-DONE.md 格式):
- 4 commit hashes + 一句话描述
- 测试 case 列表 + PASS 数
- 任何踩过的坑(SQLite vs MySQL UPSERT、tx rollback、JSON 解析等)
- 已知 limitation
- 推荐 follow-up

# 7. 风险 + 缓解

| 风险 | 缓解 |
|---|---|
| LS Meta.CustomData 当前 struct 不允许加 PlanCode 字段 | 把 customData 定义在 lemonsqueezy.go 顶部,跟现有 struct 共存 |
| Tx rollback 不覆盖 quota INSERT 失败 | 测试 case 10 强制 verify |
| 虎皮椒 attach 长度限制 | 用 `plan:CODE:user:ID` 短格式(< 60 字符)|
| 重复 webhook 同时进入 race | UNIQUE(order_id) 约束兜底,test case 3 验证 |

# 8. Pre-context references

- W5 codex prompt 范本: `docs/codex-dispatch/W5-PROMPT-FOR-CODEX.md`
- W5 done report: `docs/codex-dispatch/W5-DONE.md`
- 整体 plan: `docs/STATUS.md` + DEV-PLAN.md §0
- Founder priority memory: ~/.claude/projects/-Users-brendanxu-tanaxu-greentokey/memory/founder-priority-token-billing-not-saas.md
