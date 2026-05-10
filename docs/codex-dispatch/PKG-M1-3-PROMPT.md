# Codex Prompt — PKG-M1-③ Usage log writer

> cat | codex exec --full-auto

# 任务

把 4 类 token 计费数据(input / output / cache_write / cache_read)从 `Buffer.Upstream` 写到 `gtk_app_usage_log` V2 字段。每次 chat 调用 → 1 行 audit。

工作目录:`/Users/brendanxu/tanaxu/greentokey/coai-v0.7-design`
分支:`feat/v0.16-admin-concierge-orders`

# 0. 你是谁 + 已完成的前置

greentokey L1 token 网关 + L2 结算。Path 1(W1-W5)已落地:adapter 解析 upstream usage,Buffer 优先用 4 类计费,Charge interface 加了 cache 字段。**当前缺口**:`gtk_app_usage_log` 表 schema V2 已建好,但 **0 写入路径** — 每次 chat 完成 quota 被扣但 audit 数据没记录。

读这些文件先(必须):
- `globals/types.go::UpstreamUsage` — InputTokens/OutputTokens/CacheWriteTokens/CacheReadTokens/CacheTTL
- `utils/buffer.go::Buffer` — Upstream 字段 + RecordUpstreamUsage 方法 + Charge interface (含 GetCacheRead/Write5m/Write1h)
- `utils/tokenizer.go::CountUpstreamQuota` — 4 类计费已实现
- `manager/chat.go::CollectQuota` — 当前在 user.UseQuota 后**没有**写日志钩子
- `plans/migration.go` schema(看 gtk_app_usage_log V2 字段):
  ```
  id, user_id, plan_id NULLABLE, service, tokens_used, cost_cents,
  created_at, model_id, provider, input_tokens, output_tokens,
  cache_write_tokens, cache_read_tokens, cache_ttl,
  upstream_cost_micro, client_charge_micro, markup_multiplier
  ```

# 1. 要做什么

## 1.1 新建 `usage/writer.go` ~120 LoC

```go
package usage

import (
    "chat/auth"
    "chat/channel"
    "chat/globals"
    "chat/plans"
    "chat/utils"
    "database/sql"
    "errors"
    "fmt"
)

// WriteUsageLog records one row in gtk_app_usage_log per chat call.
//
// Called from manager/chat.go::CollectQuota AFTER user.UseQuota succeeds.
// Sync-write (per founder D2 priority): keeps the call path simple,
// no goroutine race window between quota mutation and audit write.
//
// Idempotent: not strictly — every chat call gets a new row. The
// caller is responsible for not calling this twice per request.
//
// Failure-tolerant: returns error but caller should LOG (not panic).
// A missed audit row is recoverable (tail upstream provider stats);
// a panic in the chat path drops the user response.
func WriteUsageLog(
    db *sql.DB,
    user *auth.User,
    buffer *utils.Buffer,
    chargeInst *channel.Charge,
    serviceLabel string, // typically the model id; falls back to ""
) error {
    if buffer == nil || chargeInst == nil {
        return errors.New("usage: WriteUsageLog requires buffer + charge")
    }

    // Pull 4-class token counts. Two paths:
    //   1. Upstream truth (W2 emitted UpstreamUsage chunk) → use as-is
    //   2. tiktoken estimate (legacy fallback) → 50/50 split is the best
    //      we can do without hitting the upstream truth fields. Mirror
    //      utils/aggregator.go's stance and flag _estimated:true via the
    //      cache_ttl column being empty.
    var inT, outT, cacheW, cacheR int64
    var cacheTTL string
    if buffer.Upstream != nil {
        inT = int64(buffer.Upstream.InputTokens)
        outT = int64(buffer.Upstream.OutputTokens)
        cacheW = int64(buffer.Upstream.CacheWriteTokens)
        cacheR = int64(buffer.Upstream.CacheReadTokens)
        cacheTTL = buffer.Upstream.CacheTTL
    } else {
        // Legacy estimate path. Total = buffer.Times * average; split 50/50.
        // This matches usage/aggregator.go:63's stance pre-W2.
        total := int64(buffer.GetRecordQuota() * 1000) // approx tokens, very rough
        inT = total / 2
        outT = total - inT
    }

    // Look up upstream baseline cost for "we don't lose money" delta.
    // The CountUpstreamQuota helper in utils/tokenizer.go already
    // applies the markup; we want the underlying upstream cost too so
    // the audit row records both.
    clientCharge := utils.CountUpstreamQuota(chargeInst, buffer.Upstream)
    // Upstream cost = client charge ÷ markup. markup_multiplier from
    // gtk_billing_config (default 1.300). Reading it once per call is
    // cheap; consider a package-level cache if profiling shows it.
    markup := readMarkupMultiplier(db) // helper, default 1.300
    upstreamCost := clientCharge / markup

    // Convert to micro-USD (1e-6 USD precision; channel.Charge rates
    // are ¥/1k tokens, so this is actually micro-CNY in our schema).
    // The schema uses BIGINT so we can store any reasonable value.
    upstreamMicro := int64(upstreamCost * 1_000_000)
    clientMicro := int64(clientCharge * 1_000_000)

    // Resolve plan_id (best-effort) — pulled from gtk_user_plan WHERE
    // user_id=? AND status='active' ORDER BY expire_at DESC LIMIT 1.
    // NULL when user has no active plan (PAYG / trial).
    planID := lookupActivePlanID(db, user.GetID(db))

    // INSERT row. Both legacy fields (tokens_used, cost_cents) and V2
    // fields are populated so older readers (usage/aggregator.go) keep
    // working unchanged.
    totalTokens := inT + outT + cacheW + cacheR
    costCents := int64(clientCharge * 100) // ¥ → cents

    _, err := globals.ExecDb(db, `
        INSERT INTO gtk_app_usage_log (
            user_id, plan_id, service, tokens_used, cost_cents,
            model_id, provider, input_tokens, output_tokens,
            cache_write_tokens, cache_read_tokens, cache_ttl,
            upstream_cost_micro, client_charge_micro, markup_multiplier
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
        user.GetID(db), nullableInt64(planID), serviceLabel,
        totalTokens, costCents,
        buffer.Model, providerOf(buffer.Model),
        inT, outT, cacheW, cacheR, cacheTTL,
        upstreamMicro, clientMicro, markup,
    )
    if err != nil {
        return fmt.Errorf("usage: insert log: %w", err)
    }
    return nil
}

func readMarkupMultiplier(db *sql.DB) float32 {
    var v string
    if err := globals.QueryRowDb(db, `SELECT v FROM gtk_billing_config WHERE k='markup_multiplier'`).Scan(&v); err != nil {
        return 1.300 // fallback default per W1 seed
    }
    f, err := parseFloat32(v)
    if err != nil || f <= 0 {
        return 1.300
    }
    return f
}

func lookupActivePlanID(db *sql.DB, userID int64) sql.NullInt64 {
    var id sql.NullInt64
    err := globals.QueryRowDb(db, `
        SELECT plan_id FROM gtk_user_plan
        WHERE user_id = ? AND status = 'active'
        ORDER BY expire_at DESC LIMIT 1
    `, userID).Scan(&id)
    if err != nil {
        return sql.NullInt64{}
    }
    return id
}

func providerOf(model string) string {
    // Mirror gtk_provider_pricing seed labels.
    switch {
    case strings.HasPrefix(model, "claude-"):  return "anthropic"
    case strings.HasPrefix(model, "deepseek-"):return "deepseek"
    case strings.HasPrefix(model, "gpt-"):     return "openai"
    case strings.HasPrefix(model, "qwen-"):    return "qwen"
    default: return ""
    }
}
```

(Helper functions `parseFloat32` and `nullableInt64` — implement inline or use stdlib.)

## 1.2 接通到 manager/chat.go ~10 行

In `manager/chat.go::CollectQuota` (current shape ~30 行 at L28-43), append after `user.UseQuota`:

```go
// Audit-log the call to gtk_app_usage_log. Best-effort — never panic
// the chat path. Service label = model id (W3 path) when buffer.Model
// is set; falls back to chargeInst.GetModels()[0] otherwise.
if err := usage.WriteUsageLog(db, user, buffer, chargeInst, buffer.Model); err != nil {
    globals.Warn(fmt.Sprintf("usage: log write failed (non-fatal): %s", err))
}
```

`chargeInst` needs to be plumbed through. Today `CollectQuota(c, user, buffer, plan, err)` doesn't take it. Two options:
- A. Add `chargeInst *channel.Charge` parameter to CollectQuota signature
- B. Re-resolve from `channel.ChargeInstance.GetCharge(buffer.Model)` inside WriteUsageLog

Pick B (less invasive,1 fewer parameter to thread).

## 1.3 测试 `usage/writer_test.go` ~200 LoC, 6-8 cases

Required cases:
1. **Happy path with upstream usage** — Buffer.Upstream set with all 4 classes → row written with 4 class fields populated correctly, `cache_ttl='5m'` from PreferredCacheTTL bridge
2. **Fallback path no upstream** — Buffer.Upstream nil → row written with input+output 50/50 split, cache_* fields = 0
3. **Markup math** — clientCharge / markup ≈ upstreamCost within 1e-5 tolerance
4. **markup_multiplier override** — set gtk_billing_config to "1.500" → math reflects it
5. **markup_multiplier missing** — table empty → falls back to 1.300 default
6. **NULL plan_id** — user has no active plan → plan_id is NULL
7. **Active plan resolution** — user has 2 plans (one expired, one active) → picks active one
8. **Provider inference** — model "claude-sonnet-4-5" → provider="anthropic"; model "gpt-4o" → "openai"; model "deepseek-v3" → "deepseek"; unknown → ""

Use in-memory SQLite, seed auth + quota + gtk_user_plan + gtk_billing_config tables.

# 2. Acceptance

```bash
go build ./... 2>&1 | grep -v "warning\|libwebp"  # 0 errors
go test ./usage/... ./manager/... -count=1 -vet=off | grep -E "FAIL|ok "
# Expect: ok chat/usage / ok chat/manager
grep -n "WriteUsageLog" usage/writer.go manager/chat.go
# Expect ≥ 2 matches
grep -n "INSERT INTO gtk_app_usage_log" usage/writer.go
# Expect 1 match
```

# 3. Commit split (3 commits)

1. `feat(usage): WriteUsageLog — gtk_app_usage_log writer with 4-class breakdown` (usage/writer.go + test)
2. `feat(manager/chat): wire usage.WriteUsageLog into CollectQuota` (manager/chat.go ~10 行)
3. `docs: PKG-M1-3-DONE.md report`

# 4. Out of scope

- ❌ Async / goroutine writing(D2=A 同步路径)
- ❌ Customer dashboard 显示(P2 work,本 worker 只 backend)
- ❌ Aggregation queries(usage/aggregator.go 现有 50/50 fake split 仍存,future cleanup 单独做)
- ❌ Carbon log 写入(那是 carbon/log.go 不同系统)
- ❌ 改 gtk_app_usage_log schema(W1 已固定)

# 5. Done report

`docs/codex-dispatch/PKG-M1-3-DONE.md`,同 W5 格式。

# 6. 风险

| 风险 | 缓解 |
|---|---|
| `chargeInst.GetCacheRead()` 返回 0 时 lookup_active_plan SQL 出错 | Test case 5 + 6 |
| Charge config 没配 cache 字段时 `markup × 0 = 0` | W4 fallback 已加(GetCacheRead → GetInput) |
| 高 QPS 下 SQL writer 阻塞 chat 响应 | 同步写 < 1ms in-process(per W4 调研),不阻塞 |
| 失败时 panic 导致 chat 卡死 | 用 globals.Warn 不 panic;test case 验证 |

# 7. References

- `docs/codex-dispatch/W5-PROMPT-FOR-CODEX.md` — prompt 范本
- `docs/research/token-cache-AUDIT-and-billing-design.md` §2.4 — 不亏数学证明
- `utils/upstream_billing_test.go::TestCountUpstreamQuota_NeverLoseMoney` — 5 mix 子场景
