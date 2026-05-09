# Token Cache 最终决策(synthesis)

**Date**: 2026-05-09
**Author**: tana(综合 Agent A/B/C 产出)
**Status**: 🟢 RECOMMENDED — 待 founder 1 句话确认走 §3 的 Path C(换 DeepSeek + 雪藏 cache)
**Configs touched if approved**: NewAPI 后台 model routing default(0 行 Go 代码改动 + 1 处 channel config)

---

## TL;DR(2 行)

1. **不实装 prompt cache。** 5 客户阶段 Anthropic 5min TTL + 民宿稀疏调用 = cache write 成本 > read 节省 = **倒亏 26%**(Agent C 实测算账)。
2. **改换 model routing default → DeepSeek V3。** 4h 工时,5 客户阶段月省 ¥204(MRR 占比 2.25% → 0.16%),DeepSeek 自带 KV cache,**零代码改动**。Claude Sonnet 保留给 Concierge 通道作为 premium fallback。

> 省下的 12h 工时 → 投到 Gate 1 民宿主对话(DEV-PLAN.md 当前最大 risk)。

---

## 1. 三个 agent 的反论结构

3 个 agent 在同一个题目上得出**彼此矛盾**的结论 — 这正好是综合时该 surface 的关键张力。

| Agent | 推荐 | 理由 | 盲点 |
|---|---|---|---|
| **A (Provider API)** | 实装 Anthropic Sonnet 4.5 first | 1024 tok 最低门槛 + 5m TTL "1 hit 即回本" | 没看实际调用密度,假设了高 QPS |
| **B (Architecture)** | 实装方案 A(改 `executeAgent` body schema) | 工时 16-22h,6-7 个 codex worker 可拆并行 | 没核 system prompt 是否到 1024 阈值 |
| **C (ROI Economics)** | **不实装**,换 DeepSeek | 5min TTL + 民宿调用稀疏(评论 30/天 ≈ 48 分钟一次) → cache 命中率仅 ~12%,write 开销 > read 节省 = **净亏 26%** | 假设 founder 接受 model 切换的质量风险 |

**综合判断 = Agent C 赢**:Agent A 提供的"1 hit 即回本"门槛是**理论值**,Agent C 的"5min TTL + 民宿稀疏调用 → 实际命中率 12%"是**现实值**。在民宿场景,5min 内基本不会有第二次同 prompt 调用 — 写完即过期,纯赔写入 1.25x 溢价。

Agent B 的工时估算(16-22h)+ Agent C 的工时机会成本(¥8000)= **唯一回本路径是客户≥50 + Sonnet 锁主路径**。两个前提目前都不成立。

---

## 2. 关键数字(单客户 / 月,综合 §C)

| 方案 | 月成本 | vs Sonnet baseline | 工时 | 5 客户阶段月省 |
|---|---|---|---|---|
| Sonnet 不 cache(假设 baseline) | ¥44.50 | — | 0 | — |
| **Sonnet + 5min cache(实装)** | **¥56.10** ⚠️ | **-26%** | 16h | **-¥58/月**(亏) |
| Sonnet + 1h cache(假设 60% 命中,需高 QPS) | ¥31.20 | +30% | 18h | +¥66 |
| **DeepSeek V3(无 cache)** | ¥3.59 | +92% | 4h | **+¥204** ✅ |
| **DeepSeek V3(自动 KV cache)** | ¥3.10 | +93% | 4h | **+¥207** ✅✅ |
| Qwen-Max | ¥20.74 | +53% | 4h | +¥119 |

**核心 takeaway**:**"换 model" 的省钱效果是"实装 cache" 的 10-100x**。

---

## 3. 三条路径(Path A/B/C)

### Path A: 现在实装 Anthropic prompt cache ❌ 不推荐

- **前提**:把 system prompt 加长到 ≥1024 tokens(目前 660)+ 改 `executeAgent` body schema + 改 `adapter/claude/struct.go` forward `cache_control`
- **工时**:16-22h(Agent B 估算)
- **5 客户阶段 ROI**:**-1.4%**(月度负数,永远不回本)
- **20 客户 ROI**:¥232 月省,**34 个月**回本
- **100 客户 ROI**:¥1,330 月省,6 个月回本(此时值得做)
- **结论**:做早了。客户 ≥50 + 1h TTL 配置可用 + 主路径锁 Claude 后再启动。

### Path B: 等到客户 ≥50 再做 ⚠️ 部分推荐(自然演化)

- **触发**:客户数 ≥50 且 ≥3 个月稳定 OR 主路径业务质量验证锁 Claude
- **现在做**:**仅在 telemetry 表加 1 列 `prompt_cache_hit_tokens`(只读)** 用于未来观测,不实装写入路径
- **工时今天**:0(等触发)
- **结论**:不需要 founder 立刻决策,自然到位

### Path C: 不做 cache,优先优化 model 选择 ✅ **强烈推荐**

立即行动:

| 行动 | 谁做 | 工时 | 触发 |
|---|---|---|---|
| **NewAPI 后台 channel config 切 routing default → DeepSeek V3** | tana | 2h | 立即(founder 一句话 GO) |
| Sonnet 保留作 Concierge 通道(founder 自己跟客户对话用,≤50 次/月,可承受 ¥45/客户) | tana | 0(已有) | 立即并行 |
| 写 model 质量横评(笔记 / 评论 / 私信 各 20 例 DeepSeek vs Sonnet vs Qwen) | founder + tana | 6h | Gate 1 通过后,选 model 之前 |
| 在 telemetry 加 `prompt_cache_hit_tokens` 字段(只读) | tana | 1h | 客户 ≥10 时 |
| 重新评估 cache 实装 | 全员 | — | 客户 ≥50 OR 主路径锁 Claude |

**省下的 12h 工时**:全部投入 Gate 1 民宿主对话(`docs/distribution/01-assignment-dm-templates.md` § Founder 待办)。

---

## 4. Founder 1 个决策点

> **Q: 同意 Path C(切 DeepSeek default + 雪藏 cache)吗?**
>
> - 同意 → tana 立即去 NewAPI 后台改 channel config(2h)。Concierge 路径继续走 Sonnet。Gate 1 对话排上日程。
> - 不同意,要走 Path A → tana 启动 6-7 个 codex worker 实装 cache(16-22h),Gate 1 推后。
> - 不同意,要走 Path B → tana 写 1 行 telemetry stub,其它什么都不做,继续 Gate 1。

---

## 5. 关键 gotcha 抓出来(从 3 份 doc 摘 4 条最重要的)

1. **(Agent A)** Claude Haiku 4.5 cache 阈值是 **4096 tok**,不是 Sonnet 系的 1024 — 民宿便宜路线如果想用 Haiku + cache 就得撑到 4096,不可行。
2. **(Agent A)** NewAPI v0.13.x **实测 forward `cache_control` 标记**(`gh search code` 验证过)。我们如果未来要走 Path A,网关层不需要 fork。
3. **(Agent B)** `executeAgent` body 用 `map[string]string`,**必须重写 schema** 才能加 cache_control。`executeAgentWithImages` 已是 array-of-typed,天然能加。
4. **(Agent C)** DeepSeek V3 单价 $0.28 input / $1.10 output,比 Sonnet **便宜 11x input / 14x output**。即使 Sonnet 顶级 cache 优化都追不上 DeepSeek 的 baseline。

---

## 6. 配套文档

- `docs/research/token-cache-A-provider-api-survey.md` — Agent A 完整 5 provider 调研 + NewAPI passthrough 验证 + 12 个 gotcha
- `docs/research/token-cache-B-integration-architecture.md` — Agent B 6 章架构设计 + 3 候选方案 + 改动清单
- `docs/research/token-cache-C-roi-economics.md` — Agent C 经济模型 + 4 model × 3 scale 对比 + 假设敏感性

如果 founder 调任意假设(每客户调用量 / 客户数 / system prompt 长度 / cache 命中率 / model price),按 §C-6 § 重算。

---

## 7. 时间轴 + commit 触发

如果 Path C 通过:
- Today: founder 一句话 "GO Path C"
- +2h: tana 改 NewAPI channel config(routing default → DeepSeek V3)
- +1h:写 1 笔 commit + 部署到 prod
- +0h: founder 排 Gate 1 DM 模板 → 7 天内发出 8-10 民宿主

如果 Founder 走 Path A(违推荐):
- Today: founder GO + 接受 ROI 倒亏的成本
- +6h: tana 拆 6-7 codex worker spec
- +16-22h: 实装完成
- Gate 1 推后 ≥3 天

---

## 8. 结束 Notes

- 3 个 agent 共消耗 ~360K tokens,本次综合 ~12K tokens(本 doc)
- Agent A/B/C 全部 push 到 `docs/research/token-cache-{A,B,C}-*.md`
- 本 doc 是综合后的 **founder-actionable** 文件 — A/B/C 是支持文件,不需要 founder 全读
- 决策权在 founder。tana 推荐 Path C,但不强行执行;founder 一句话定方向。
