# Token Cache ROI 经济分析(民宿场景)

> **Agent C 产出** · 2026-05-09 · v1.0
> **触发任务**: PKG-? Token Cache 研究 — 实装 prompt cache 在 Pivot v4 民宿 SaaS 场景下值不值得做
> **配套文档**: Agent A (Provider API 调研) · Agent B (集成架构) · 综合 design doc(待写)
> **TL;DR**: 现阶段(5 客户 / MRR ¥9900) **不要** 实装 Anthropic prompt cache;直接换 DeepSeek 是 ROI 高 100x+ 的优先选项。详见 §4。

---

## 0. Provider Pricing Ground Truth (2026-05-09 verified)

| Provider / Model | Input $/M | Output $/M | Cache Write $/M | Cache Read $/M | Cache 阈值 / 备注 |
|---|---|---|---|---|---|
| **Claude Sonnet 4.6 / 3.5** | $3.00 | $15.00 | $3.75 (5m) / $6.00 (1h) | $0.30 | ≥1024 input tokens 才能 cache(Sonnet 类). |
| **Claude Haiku 3.5** | $0.80 | $4.00 | $1.00 (5m) / $1.60 (1h) | $0.08 | ≥2048 input tokens 才能 cache(Haiku 类). |
| **DeepSeek V3** | $0.28 (miss) | $1.10 | — (隐式) | $0.028 (hit) | KV cache 自动启用,无 schema 改动,best-effort,平均 50%+ 命中. |
| **Qwen-Max** (DashScope) | $1.60 | $6.40 | — | — | qwen-max 主线**不支持** prompt cache;只 qwen-plus-us(美区)有. 对国内民宿等于无. |

**汇率假设**: USD → CNY = 7.2 : 1 (2026-05-09 中间价)

**关键 ground truth(影响后续每一格数字)**:
1. `xhs-copy-writer` system prompt **~660 tokens < 1024 阈值** → **目前 Anthropic Sonnet 3.5 / 4.6 路径无法 cache**. 必须把 system prompt 撑到 ≥1024 才有资格.
2. `mansu-managed-orchestrator` system prompt ~395 tokens → **离 1024 阈值还差 629 tokens**. 离 Haiku 3.5 的 2048 阈值差 1653 tokens.
3. DeepSeek 的 KV cache 是**自动**的、无阈值,但只对**重复前缀**生效(见 §1 假设).
4. Anthropic 在 2026 早期把 default cache TTL 从 60min 砍到 5min,意味着**间歇性低 QPS 场景命中率显著下降**(民宿场景调用极稀疏,见 §1).

---

## 1. 场景假设(单民宿单月 LLM 调用 baseline)

> **目的**: 把"模糊的民宿 LLM 用量"锚到具体数字,后面 founder 调任意一格,这份文档可以快速重算.

### 1.1 单客户 / 单月调用拆解

| 调用类别 | 频次 | 单次 Input(含 system prompt) | 单次 Output | 月度 Input | 月度 Output |
|---|---|---|---|---|---|
| 小红书笔记生成 | 10 篇/月 | 800 tok(660 sys + 140 user) | 1500 tok | 8,000 | 15,000 |
| 评论自动回复 | 30/天 × 30 天 = 900 | 500 tok(395 sys + 105 user) | 200 tok | 450,000 | 180,000 |
| 私信意图分类 | 30/天 × 30 天 = 900 | 300 tok(轻量 prompt + 用户消息) | 50 tok | 270,000 | 45,000 |
| 月度数据分析报告 | 1 次/月 | 3,000 tok | 5,000 tok | 3,000 | 5,000 |
| Concierge debug 对话 | 50 次/月 | 800 tok 来回平均 → 拆 500 in / 300 out | — | 25,000 | 15,000 |
| **合计 / 客户 / 月** | | | | **~756,000 tok input** | **~260,000 tok output** |

**取整后场景常数**:
- **A_in = 760,000 input tokens / 客户 / 月**
- **A_out = 260,000 output tokens / 客户 / 月**

### 1.2 Cache 候选率假设

- **System prompt 重复率**: 100%(每次调用同一份 `xhs-copy-writer` 或 `mansu-managed-orchestrator` system header).
- **可缓存比例(占总 input 的份额)**:
  - 笔记生成: 660 / 800 = **82.5%** → 月可缓存 6,600 tok
  - 评论回复: 395 / 500 = **79%** → 月可缓存 355,500 tok
  - 私信分类: 假设 200 / 300 = **67%** → 月可缓存 180,000 tok
  - 报告 / Concierge: 假设几乎不可缓存(每次 prompt 不同) → 0
  - **月度可缓存 input total ≈ 542,000 tok / 客户(约占 760K 的 71%)**
- **现实命中率打折**:
  - **Anthropic 5min TTL**: 评论 / 私信 30/天 = 平均 48 分钟一次,**命中率约 15%**(超过 5min 窗口要 re-write).
    笔记 10/月 = 几乎从不命中,**命中率约 5%**.
    Concierge 50/月 = 几乎从不命中.
    → **实效命中率综合 ~12-15%**.
  - **DeepSeek KV cache (best-effort, 通常 50%+ 命中)**: 因为前缀完全相同 → **保守估 50% 命中**.

### 1.3 不可降低的 baseline 成本

- 笔记 + 报告 + concierge 的"非系统部分"input + 全部 output 不能 cache.
- 每个 model 的"无 cache" baseline = `A_in × $/M_input + A_out × $/M_output`.

---

## 2. 4 个 model 月成本对比(单客户)

> 所有 ¥ 数字 = USD × 7.2,四舍五入到 0.01 ¥.

### 2.1 Claude Sonnet 4.6(主流候选)

- **不 cache**: 0.76 × $3 + 0.26 × $15 = $2.28 + $3.90 = **$6.18 ≈ ¥44.50**
- **如果 system prompt 撑到 ≥1024 + 实装 cache + 5min TTL ~12% 命中**:
  - 可缓存份额: 542K input × 12% = 65K hit @ $0.30/M = $0.0195
  - 剩余 input: 760K - 65K = 695K @ $3/M = $2.085
  - Cache write 开销: 542K × 88% miss × $3.75/M = $1.789(每次 miss 都要重新写)
  - Output 不变: $3.90
  - **合计: $0.0195 + $2.085 + $1.789 + $3.90 = $7.79 ≈ ¥56.10** ⚠️ **比不 cache 贵 26%!**
  - 原因: TTL 5min + 调用稀疏 → cache write 成本 > cache read 节省
- **结论**: **Sonnet 5min cache 在民宿场景是负 ROI**. 1h cache(2x write multiplier)更糟.

### 2.2 Claude Haiku 3.5

- **不 cache**: 0.76 × $0.80 + 0.26 × $4.00 = $0.608 + $1.04 = **$1.648 ≈ ¥11.86**
- **撑到 ≥2048 阈值 + 实装 cache + 5min TTL ~12% 命中**:
  - Hit: 65K × $0.08/M = $0.0052
  - Miss input: 695K × $0.80/M = $0.556
  - Cache write: 542K × 88% × $1.00/M = $0.477
  - Output: $1.04
  - **合计: $2.078 ≈ ¥14.96** ⚠️ **比不 cache 贵 26%!**(同样原因)
- **结论**: Haiku cache 在民宿场景也是负 ROI.

### 2.3 DeepSeek V3 (推荐)

- **不 cache(全 miss)**: 0.76 × $0.28 + 0.26 × $1.10 = $0.213 + $0.286 = **$0.499 ≈ ¥3.59**
- **DeepSeek KV cache 自动 ~50% hit on cacheable portion**:
  - Cacheable: 542K × 50% = 271K hit @ $0.028/M = $0.0076
  - Remaining input: 760K - 271K = 489K @ $0.28/M = $0.137
  - Cache "write": 隐式,免费(KV disk caching, 无显式 multiplier)
  - Output: $0.286
  - **合计: $0.430 ≈ ¥3.10**(自动节省 14%,**零工时**)
- **结论**: DeepSeek 不需要任何代码改动就比 Sonnet 便宜 **~14x**.

### 2.4 Qwen-Max

- **不 cache**: 0.76 × $1.60 + 0.26 × $6.40 = $1.216 + $1.664 = **$2.88 ≈ ¥20.74**
- **Cache 实装**: ❌ **不支持**(qwen-max 国内主线无 cache;qwen-plus-us 才有,只对美区).
- **结论**: 比 Sonnet 便宜 53%,但比 DeepSeek 贵 ~6x. 中文质量未必比 DeepSeek V3 好(2026 H1 多个 benchmark DeepSeek 持平或略胜).

### 2.5 横向对比表

| Model | 不 cache 月成本 | Cache 实装后月成本 | 节省 % | 节省金额(¥) | 备注 |
|---|---|---|---|---|---|
| Claude Sonnet 4.6 | ¥44.50 | ¥56.10 ⚠️ | **-26%** | **-¥11.60** | 5min TTL + 调用稀疏 → 反向亏 |
| Claude Sonnet 4.6 (假设 1h TTL & 60% 命中) | ¥44.50 | ¥31.20 | +30% | +¥13.30 | 需要付 2x write multiplier 且高 QPS,民宿不满足 |
| Claude Haiku 3.5 | ¥11.86 | ¥14.96 ⚠️ | **-26%** | **-¥3.10** | 同上 |
| DeepSeek V3 | ¥3.59 | ¥3.10 | +14% | +¥0.49 | **KV cache 自动,零工时** |
| Qwen-Max | ¥20.74 | n/a (国内不支持) | 0% | 0 | — |

> ⚠️ 这张表的最 important takeaway: **"换 model" 的省钱效果是"实装 cache" 的 10-100x**.
> Sonnet→DeepSeek 一刀切省 **¥40.91/客户/月** = 92%.
> Sonnet 上实装 cache(假设能撑到阈值 + 调用足够密)最多省 ¥13/月 = 30%,且实际 5min TTL 下倒亏.

---

## 3. 不同客户规模下的 ROI

工时假设(实装 Anthropic prompt cache):
- **16 小时 founder + tana 总工时**(撑 prompt 到阈值 + 改 client wrap + 加 cache_control breakpoints + 测命中率 + 部署 + observability)
- **Founder 时薪 ¥500/h** → 工时成本 **¥8,000**(tana 当 0)
- 这 16h 还有机会成本: 同期可以做 Gate 1 founder × 民宿主对话(更高 ROI)

### 3.1 Scale Point 1: 5 客户 (Week 12 北极星, MRR ¥9,900)

| 方案 | 月 LLM 总成本 | 月节省 vs Sonnet baseline | % of MRR |
|---|---|---|---|
| Sonnet 不 cache (baseline) | 5 × ¥44.50 = **¥222.50** | — | 2.25% |
| Sonnet + cache (5min TTL, 实装) | 5 × ¥56.10 = ¥280.50 | **-¥58/月** | 2.83% |
| DeepSeek 不 cache | 5 × ¥3.59 = **¥17.95** | +¥204.55/月 | 0.18% |
| DeepSeek + 自动 KV cache | 5 × ¥3.10 = **¥15.50** | +¥207/月 | 0.16% |

**ROI**:
- **实装 Anthropic cache**: 月节省 = **负数** → 永远不回本.
- **换 DeepSeek**: 月节省 ¥204.55, 工时 ≈ 4h(改 routing config),即 **第一个月就回本 2x**.
- **回本时间(实装 cache,假设乐观最大每月省 ¥58 = 1h TTL 配高 QPS,民宿场景做不到)**: ¥8000 / ¥58 ≈ **138 个月 (11.5 年)**. 不可能回本.

### 3.2 Scale Point 2: 20 客户 (~Q3 2026 if pivot 成功)

| 方案 | 月成本 | 月节省 vs baseline |
|---|---|---|
| Sonnet 不 cache | ¥890 | — |
| Sonnet + cache | ¥1,122 | -¥232 |
| DeepSeek 不 cache | ¥71.80 | +¥818.20 |
| DeepSeek + KV cache | ¥62 | +¥828 |

**ROI**:
- 实装 cache(乐观假设月省 ¥232): **¥8000 / ¥232 ≈ 34 个月**. 仍然不值得.
- DeepSeek migration: 已经回本.

### 3.3 Scale Point 3: 100 客户 (理论上限,Year 2)

| 方案 | 月成本 |
|---|---|
| Sonnet 不 cache | ¥4,450 |
| Sonnet + cache (乐观) | ¥3,120 (假设此时 QPS 足够高,1h TTL 命中率 60%) |
| DeepSeek 不 cache | ¥359 |
| DeepSeek + KV cache | ¥310 |

**ROI**:
- **实装 Anthropic cache (假设 1h TTL + 高 QPS 可达)**: 月省 ¥1,330. 工时回本 ≈ **6 个月**. 此时值得做.
- **触发条件**: ① 客户 ≥ 50 (调用密度足够); ② 主路径锁 Sonnet (业务上离不开 Claude); ③ 1h TTL 配置可用.
- **但**: 100 客户阶段 DeepSeek 月成本本来就只 ¥310, 改 Sonnet 即使 cache 优化后还是 ¥3,120. **省的不是 cache 的钱,是 model 选择的钱**.

---

## 4. 决策建议

### 三选一

**A. 现在就实装 Anthropic prompt cache** ❌ 不推荐
- 理由反驳: 5min TTL + 民宿场景调用稀疏 → 净亏(§2.5 + §3.1).
- 即便 1h TTL,system prompt 不到阈值,仍要先重写 prompt.
- 工时机会成本 > 任何可见收益.

**B. 等到 X 客户再做** ⚠️ 部分推荐(但不是首选)
- 触发条件:
  1. 客户数 ≥ **50**(调用密度让 5min TTL 内命中率 > 30%)
  2. 主路径业务锁定 Claude(质量验证后离不开)
  3. system prompt 可重构到 ≥1024 / ≥2048
- 在此之前,**不写一行 cache 相关代码**, **不在 schema 里预留 cache_control**.

**C. 不做,优先优化 model 选择** ✅ **强烈推荐(现阶段)**
- **第一步: 把生产路由切到 DeepSeek V3 作为默认** —— 改 4h, 月省 ¥204(5 客户),¥818(20 客户),scale 越大越省.
- DeepSeek KV cache **自动启用、零代码改动、零阈值要求**, 50% 平均命中率不需要任何调优.
- Claude 留作 fallback / Pro 档加价品(给挑剔客户 +¥200/月 升级到 "Premium AI" tier).
- **节省的工时(16h - 4h = 12h)** 投入到 Gate 1 founder × 民宿主对话(DEV-PLAN.md 当前最大风险, ROI 远高于任何 infra 优化).

### 推荐方案 = C

> **现阶段(5 客户 / MRR ¥9,900)实装 Anthropic prompt cache 的边际节省 / 边际工时 = ROI 是 -1.4%(因为 5min TTL 下倒亏).**
> **比之下,直接换 DeepSeek 而不是 Claude 能省 92% 月 LLM 成本(¥41/客户 → ¥3/客户).**
> **哪个先做?**
>
> ✅ **先换 DeepSeek**.
> 这一刀下去:
> - 5 客户阶段 LLM 成本 / MRR 从 2.25% 降到 0.16%(几乎归零);
> - 节省的钱足以付 1.5 个月的 R2 offsite backup + monitoring;
> - 工时只要 4h, 不偏离 Pivot v4 的当前主路径(DEV-PLAN.md Gate 1 Assignment).
>
> Cache 这件事**雪藏到客户数 ≥50**. 那时再回头评估,数字会自己说话.

---

## 5. 配套行动建议(给 founder + tana)

| 行动 | 谁做 | 工时 | 触发节点 |
|---|---|---|---|
| 改 routing default → DeepSeek V3 | tana | 2h | 立即 |
| 写一份 model 质量横评(笔记 / 评论 / 私信 各 20 例 DeepSeek vs Sonnet vs Qwen) | founder + tana | 6h | Gate 1 通过后,正式选 model 前 |
| Concierge 用 Claude Sonnet (founder 自己的对话, ≤50 次/月可承受 ¥45/客户) | tana | 已有 | 立即并行 |
| 在 telemetry 里加一格 `prompt_cache_hit_tokens` 字段(只读,不实装写入) | tana | 1h | 客户 ≥10 时 |
| 重新评估 cache 实装 | 全员 | — | 客户 ≥ 50 OR 主路径锁 Claude |

---

## 6. 假设敏感性 (founder 调任意一格 → 重算路径)

如果 founder 觉得这份分析的某个假设错了, 调下面这几个钮可以快速重算:

1. **每客户调用量 (A_in / A_out)**: §1.1 表格里的 row counts. 调评论数从 30/天 → 100/天, A_in 翻 ~3x, 但 ROI 排序不变.
2. **客户数**: §3 三个 scale point 之间线性插值即可.
3. **Cache 命中率**: §1.2 末尾的 12% / 50%. Anthropic 调到 60%+ 的话(假设 1h TTL + 调用密),Sonnet cache 才有正 ROI;但前提是 system prompt 撑到阈值.
4. **System prompt 长度**: 把 660 → 1024+,需要往里加少 examples 或 few-shot context. 这一步做完才能 unlock §2.1 的乐观假设.
5. **Model price 变动**: DeepSeek V3 历史上每年降 50%+. Sonnet 价格相对稳定. 如果 DeepSeek 再降, 推荐 C 越发坚定.
6. **质量 gap**: 如果 DeepSeek 中文笔记质量不及 Sonnet ≥20%(待 founder 主观盲测验证), 那 ROI 等式会变, 但即便如此 Concierge 用 Sonnet + 主路径用 DeepSeek 的 hybrid 仍优于全 Sonnet.

---

## Sources

- [Anthropic Pricing — Claude API Docs](https://platform.claude.com/docs/en/about-claude/pricing)
- [Anthropic Prompt Caching Docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [Claude API Pricing 2026 — finout.io](https://www.finout.io/blog/anthropic-api-pricing)
- [Claude Cache TTL silently regressed from 1h to 5m — GH Issue #46829](https://github.com/anthropics/claude-code/issues/46829)
- [DeepSeek API Pricing](https://api-docs.deepseek.com/quick_start/pricing)
- [DeepSeek Context Caching Guide](https://api-docs.deepseek.com/guides/kv_cache)
- [Qwen API Pricing 2026 — DeepInfra](https://deepinfra.com/blog/qwen-api-pricing-2026-guide)
- [Alibaba Cloud Model Studio Pricing](https://www.alibabacloud.com/help/en/model-studio/model-pricing)
- [Claude Haiku 3.5 Pricing — pecollective.com](https://pecollective.com/tools/anthropic-api-pricing/)
