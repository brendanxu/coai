# Token Cache Audit — 重要发现 + Plan 修订(2026-05-10 中段)

**Status**: 🟡 W1 已完成,W2-W4 implementation paused 等 founder 决策方向
**Author**: tana
**Reads**: 已实施 W1(plans/migration.go schema V2 + 7 测试 PASS)。本文记录 implementation 中段发现的关键事实 + 修正后的方案。

---

## 0. TL;DR(给 founder 30 秒)

我做完 W1 后,继续 W3(adapter 解析 upstream cache 字段)时 grep 了一遍 codebase,发现:

1. ✅ **W1 干净落地**:`gtk_app_usage_log` schema V2 + `gtk_provider_pricing` + `gtk_billing_config` 全部建好,7 unit test PASS。
2. ❗ **`gtk_app_usage_log` 当前 0 写入路径**。整个 codebase 没有任何代码 INSERT 这张表 — 它是为 L3 民宿 SaaS 内部 marketplace 准备的,**还没接通**。
3. ❗ **真正的 token 分销计费走 CoAI 上游路径**(`utils/tokenizer.go::NumTokensFromMessages` 用 **tiktoken 客户端估算**,`utils/buffer.go::Buffer.GetQuota()` 按 viper `charge` config 单价算 quota),**完全不读上游真实 usage 字段**,**也没有 cache 概念**。
4. 🟢 **"不赔钱"在 tiktoken 估算路径下其实已经成立**(只要 charge config 单价 ≥ 上游真实单价),但**客户用 cache 我们仍按全价收钱** = 客户感觉被 ripped off,这是真正要解的题。

**Plan 修订 = 2 个路径 founder 选其一**:
- **Path 1: 改 CoAI 计费(高 risk fork 分叉)** = 6-8h,客户体验 cache 折扣
- **Path 2: 加 reconcile 监控层(低 risk,不动 fork)** = 3-4h,**先确保不亏**,客户 cache 折扣下个 sprint 再说

---

## 1. CoAI 上游计费路径(我之前的 audit 漏掉了)

### 1.1 实际路径

```
Customer POST /v1/chat/completions
  ↓
manager/chat.go::createChatTask
  ↓
NumTokensFromMessages(history, model, false)  ← tiktoken 客户端估算 input
  ↓
Buffer.Quota = CountInputQuota(charge, inputToken)
                       ↑ charge.GetInput()  ← viper charge config 单价
  ↓
[upstream chat happens]
  ↓
Buffer.Quota += CountOutputToken(charge, outputToken)
                       ↑ tiktoken 估算的 output ÷ 1000 × charge.GetOutput()
  ↓
manager/chat.go::CollectQuota
  ↓
user.UseQuota(db, quota)  ← UPDATE quota SET used = used + ?
```

**关键事实**:
- Input tokens = **tiktoken 本地估算**(`utils/tokenizer.go:49-83`)
- Output tokens = tiktoken 本地估算(基于 stream 累积的 response 字符串)
- **上游返回的 `usage` 字段从来没被读** — adapter/openai/types.go 的 ChatResponse struct **根本没有 Usage 字段**
- charge config 在 viper `charge` 节点里(`channel/charge.go::ChargeManager.Load`),运营改 yaml 即可
- cache 概念**不存在** — 所有 input 一视同仁按 input 单价收

### 1.2 这意味着什么

**对"我们不赔钱"原则**:
- 已经成立(假设)— charge config 配得对的话。每个 token 都按"我们配的单价"× 估算 token 数算钱
- 实际风险 = tiktoken 估算 vs 上游真实 token 数有偏差(通常 ±5%),长期累积可能小亏 / 小赚
- Cache 来了**不会亏**,因为我们一律按 input 全价收;但**多赚**(客户用 cache 上游收我们 0.1x,我们对客户收 1.0x → 9 倍 markup)

**对"客户用 cache 享受省钱"原则**:
- ❌ 不成立。客户即便发了 `cache_control` marker:
  1. 现状:adapter 在 marshal 时 strip 掉 marker → 上游不 cache → 客户没省钱
  2. 假设我们修了透传,marker 到上游了:上游真实只收我们 0.1x,**但 CoAI 不知道**,仍按全价收客户钱 → 客户没感觉省钱

### 1.3 charge config 长什么样(运营当前怎么配的)

```yaml
# 推测格式(基于 channel/charge.go 解析):
charge:
  - models: [claude-sonnet-4.5]
    type: token
    input: 0.04   # ¥/1k tokens
    output: 0.20  # ¥/1k tokens
```

founder 需要 verify 当前 prod `config.yaml` 的 charge 节点 vs 上游真实成本(USD × 7.2)是否一致或更高。如果**配低了**,我们已经在亏。

---

## 2. 修订后的 Path 1 vs Path 2

### Path 1: 改 CoAI 计费(高 risk,但客户体验完整)

实装步骤:
1. ✅ W1: schema 已 done
2. **新 W2** (~3h): adapter 加 Usage struct,解析 4 类 token 数(`adapter/claude/types.go` + `adapter/openai/types.go` + `adapter/deepseek/struct.go` 改 ChatResponse)
3. **新 W3** (~2h): `utils/tokenizer.go` 加 `CountUpstreamQuota(charge, upstream)`,`utils/buffer.go::Buffer` 加 `UpstreamUsage` 字段,`Buffer.GetQuota()` 优先用 upstream(回退 tiktoken)
4. **新 W4** (~2h): `channel/charge.go::Charge` 加 `GetCacheRead()` / `GetCacheWrite5m()` / `GetCacheWrite1h()`,viper config schema 扩展;客户用 cache 时按 cache 单价 ×折扣 收
5. **新 W5** (~1h): `manager/chat.go` 改 forward 客户的 `cache_control` marker(用 raw body pass-through 或 typed struct extension)

**Risk**:
- 改 5 个 CoAI 上游文件 → fork 分叉,以后 rebase CoAI 头疼
- 修改面大,容易引入 regression 影响所有 chat 调用
- 工时 8-10h(比原估 6-8h 多)

**收益**:
- 客户体验完整:发 marker → 实际省钱
- 我们利润率不变(每类 token 都加 markup)
- 营销卖点成立(差异化卖点 = "BYOK + cache 透传 + 多 provider")

### Path 2: Reconcile 监控层(低 risk,先止血)

实装步骤:
1. ✅ W1: schema 已 done(可保留作为未来 Path 1 的基础设施)
2. **R1** (~2h): 新建 `billing/reconcile.go`,在 manager/chat.go::CollectQuota 后挂 hook,**异步**调用一次 reconcile:
   - 从上游响应抓 真实 usage(需要 W2 的 adapter Usage parsing,但只读不影响计费)
   - 算"如果按 gtk_provider_pricing × markup 1.30 应该收多少 quota"
   - 跟 CoAI tiktoken 估算的 quota 对比
   - 差距 > 5% → 写日志报警 + 通知 founder
3. **R2** (~1h): 写一个 daily cron 任务跑 reconcile 报表,导出 CSV `docs/reports/reconcile-YYYYMMDD.csv`,founder 每周看一次
4. **R3** (~1h): adapter 加 Usage parsing(为 R1 服务),不改任何计费 logic

**Risk**:
- 几乎为 0 — 只读,不改 CoAI 计费 logic
- 客户体验**没改善** — 用 cache 还是按全价收

**收益**:
- 立即知道有没有亏 + 亏多少(数据驱动决定下一步)
- 1 周后我们能给 founder 一份"过去 7 天我们 vs 上游真实成本对比"报表
- 留 Path 1 作为未来选项,数据先证明 ROI

### Path 选择建议

我推荐 **Path 2 先做,Path 1 留观测后**。

理由:
- founder 在 Pivot v4 民宿 SaaS wedge 的 Gate 1 阶段,精力应该投 Gate 1 民宿主对话(不是 token 分销网关增强)
- token 分销不是当前 wedge 主路径(L3 民宿 SaaS 才是)
- Path 2 的 reconcile 报表能给我们 7 天后 ground truth — 那时我们才知道亏多少 / 是不是值得 Path 1 的 8-10h 工时
- W1 已经做的 schema work 不浪费(Path 1 的 W4 仍然要用)

---

## 3. 当前 W1 落地状态(可否提交)

### 已做
- `plans/migration.go` ALTER `gtk_app_usage_log` 加 10 字段(model_id, provider, input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, cache_ttl, upstream_cost_micro, client_charge_micro, markup_multiplier)
- 新建 `gtk_provider_pricing` 表 + seed 16 行(Anthropic Sonnet 4.5/Haiku 3.5 各 5 类、DeepSeek V3 3 类、OpenAI GPT-4o 3 类)
- 新建 `gtk_billing_config` 表 + seed `markup_multiplier=1.300`
- `plans/types.go` 加 V2 字段到 AppUsageLog struct + 新 ProviderPricing/BillingConfig 类型
- 7 unit test PASS:`go test ./plans/... -count=1` 全绿
- 双引擎兼容(SQLite + MySQL),idempotent(连跑 2 遍不出错)
- 老 V1 表的迁移路径测试覆盖(`TestUpgradeAppUsageLogV2_PreExistingV1`)

### 是否提交
W1 是**纯增量**,不破坏现有任何路径(`gtk_app_usage_log` 当前 0 写入,新字段也只是为未来准备)。可以安全 commit。

但**没接计费**之前,这些表是空的,意义不大。建议:
- A. **现在就 commit W1**,作为 path 2 reconcile 的基础设施(reconcile 写入 gtk_provider_pricing 查价 + 写 gtk_app_usage_log 留审计)
- B. **暂不 commit**,等 founder 决定 Path 1 / 2 方向后,W1 跟 path 选择一起 commit

---

## 4. founder 决策点(2 个)

**Q1**: Path 选择 — Path 1(改 CoAI 计费,8-10h)还是 Path 2(加 reconcile 监控,3-4h,先止血)?

**Q2**: W1(schema 已 done)是否现在 commit?
- A: 现在 commit(我推荐),作为后续工作的基础设施
- B: 等 Path 决定后一起 commit

第 3 个相关问题(可选):
**Q3**: Path 2 的 reconcile 工作要不要等 Pivot v4 Gate 1 通过后再做?Gate 1 是当前最大风险。

---

## 5. 我的修订建议

如果让我决定:
- 现在 commit W1(schema 落地,无 break risk)
- Pivot v4 Gate 1 后再决定 Path 1 vs 2
- 同时,我把 Path 2 (reconcile)写成 codex worker spec,等 Gate 1 通过后 founder dispatch
- L3 民宿 SaaS service marketplace 接通后,gtk_app_usage_log 自然有写入路径,4 类 token 数据自然累积(无需 reconcile 监控就有 data)

这条最不赔钱也不偏离 wedge。
