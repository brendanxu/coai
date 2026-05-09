# Token Cache 研究 A — Provider 端 prompt-caching API 调研

**Author**: tana (Agent A)
**Date**: 2026-05-09
**Scope**: Anthropic / OpenAI / DeepSeek / 国产 OpenAI 兼容厂商 (Qwen / Doubao / GLM / Moonshot) / NewAPI 网关层。
**Goal**: 给 design 决策喂数据,不替决策。锚点 = 民宿 SaaS 场景下我们 4 个候选 model (claude-sonnet, claude-haiku, deepseek-v3, qwen-max) 走 NewAPI 网关到底能不能 cache + 怎么 cache。

---

## 1. Anthropic Claude (原生 + Bedrock + Vertex)

### 1.1 触发方式
**显式 marker**。在 message content block / tool / system 上挂 `cache_control: {"type": "ephemeral", "ttl": "5m"}` 或 `"1h"`。最多 4 个 breakpoint per request。Caching references 整个 prompt(tools → system → messages 顺序)up to and including 标了 `cache_control` 的那个 block。([docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching))

### 1.2 最低 token 阈值 (按 model 不同)

| Model 组 | Min cacheable tokens |
|---|---|
| Claude Opus 4.7 / 4.6 / 4.5 | **4096** |
| Claude Sonnet 4.6 | **2048** |
| Claude Sonnet 4.5 / 4 / 3.7 | **1024** |
| Claude Opus 4.1 / 4 | **1024** |
| Claude Haiku 4.5 | **4096** |
| Claude Haiku 3.5 | **2048** |

低于阈值会**静默不缓存**(no error)。验证靠 response 里 `cache_creation_input_tokens` 或 `cache_read_input_tokens` 是否 > 0。([docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching))

### 1.3 TTL
- **Default `5m`** = 5 分钟,no extra cost,自动 refresh on hit。
- **`1h`** = 1 小时,**需要 beta header** `anthropic-beta: extended-cache-ttl-2025-04-01`,write 价格上浮。
- 同 request 可混用 5m + 1h(`1h` 必须排在 `5m` 前面)。([docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching))

### 1.4 计费倍率 (相对 base input price)
- 5m 写入: **1.25×**
- 1h 写入: **2.0×**
- 缓存读取: **0.1×** (= 10% of base input)

经济门槛: 5m TTL 下,**1 次命中即回本**((1.25 - 0.1) < 1.0,write 比 base 多收 0.25,read 省 0.9)。1h TTL 下,**2 次命中回本**。([Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing); [finout pricing guide 2026](https://www.finout.io/blog/anthropic-api-pricing))

### 1.5 命中识别
Response `usage` 里:
```json
{
  "input_tokens": 50,                  // 未缓存的尾部
  "cache_creation_input_tokens": 248,  // 写入 cache 的 token 数
  "cache_read_input_tokens": 100000,   // 命中 cache 的 token 数
  "output_tokens": 503
}
```
混用 5m+1h 时还会有 `usage.cache_creation.ephemeral_5m_input_tokens` / `ephemeral_1h_input_tokens` 子字段。([docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching))

### 1.6 API 兼容性
**Provider 私有 schema** (Anthropic Messages API)。OpenAI Chat Completions 调用 Claude 时 cache_control 不存在,需要走 NewAPI 的 Anthropic 直通路径或 Anthropic adapter。

---

## 2. OpenAI (GPT-4o / GPT-5 / o-series)

### 2.1 触发方式
**自动**。No code changes. 但可选 `prompt_cache_key` 参数提升路由命中率(≈ 15 req/min/key 上限,超过会 spillover 到其它机器降低命中)。([Azure Foundry docs 2026-04-14](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/prompt-caching); [openai cookbook](https://developers.openai.com/cookbook/examples/prompt_caching_201))

### 2.2 最低 token 阈值
**1024 tokens**,且首 1024 token 必须**byte-identical**。命中后每多 128 个相同 token 增量命中。一字符不同 → cache miss。

### 2.3 TTL
- `prompt_cache_retention: "in_memory"` (default for ≤ gpt-5.4 系列): 5–10 分钟 idle 清掉,最长 1 小时强制驱逐。
- `prompt_cache_retention: "24h"` (default for gpt-5.5+ 系列): 最长 24 小时,KV tensor offload 到 GPU-local storage。`gpt-4.1` / `gpt-5.x` 系列支持。
- 旧模型 (gpt-4o 等) **不支持** 24h。([Azure Foundry docs](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/prompt-caching))

### 2.4 计费倍率
**Cached input 折扣** = 25% of standard input price (即 0.25×,75% off)。Provisioned Throughput 部署可达 100% off。**没有 write 溢价**(免费写入 + 折价读取)。([OpenAI pricing announcement](https://openai.com/index/api-prompt-caching/); Azure docs)

### 2.5 命中识别
```json
{
  "usage": {
    "prompt_tokens": 1566,
    "prompt_tokens_details": { "cached_tokens": 1408 }
  }
}
```
`cached_tokens` 永远存在(< 1024 时为 0)。

### 2.6 API 兼容性
**OpenAI Chat Completions schema 原生**。所有 OpenAI 兼容下游(包括 Azure、NewAPI relay)默认透传响应字段。

---

## 3. DeepSeek (V3 / V3.2 / R1 / V4)

### 3.1 触发方式
**自动 (Context Caching on Disk)**。"enabled by default for all users, allowing them to benefit without needing to modify their code." 不需要 marker 不需要参数。([deepseek news 0802](https://api-docs.deepseek.com/news/news0802); [kv_cache guide](https://api-docs.deepseek.com/guides/kv_cache))

### 3.2 最低 token 阈值
**Doc 没明确数字**。社区共识 ≈ 几百 token 起触发(disk-based caching 阈值通常较低)。Best-effort,no guarantee。

### 3.3 TTL
**没有公布精确 TTL**。原文: "Once the cache is no longer in use, it will be automatically cleared, usually within a few hours to a few days." 比 Anthropic/OpenAI 的分钟级 TTL 长得多 — 这是 disk-based 的优势。

### 3.4 计费倍率 (USD per 1M tokens, deepseek-chat)
- Input cache miss: **$0.14**
- Input cache hit: **$0.014** (= 0.1×, 90% off; 实际 doc 写 $0.0028 是新 V4 价目,V3 historical 是 $0.014)
- Output: $0.28

**No write surcharge** + 10% read = 比 OpenAI 还便宜的命中价。**1 次命中即赚**(cache miss 没溢价,hit 直接打 1 折)。([deepseek pricing](https://api-docs.deepseek.com/quick_start/pricing))

### 3.5 命中识别
```json
{
  "usage": {
    "prompt_cache_hit_tokens": 1234,
    "prompt_cache_miss_tokens": 567
  }
}
```
**注意字段名跟 OpenAI 不一样**。NewAPI 已识别两种命名(见 §6)。

### 3.6 API 兼容性
**OpenAI Chat Completions schema 兼容** + DeepSeek-specific extension fields。

---

## 4. 国产 OpenAI 兼容厂商

### 4.1 Qwen (Alibaba DashScope)

| 项 | Explicit cache | Implicit cache |
|---|---|---|
| 触发 | `cache_control: {"type": "ephemeral"}` marker | 自动,无法关 |
| Min tokens | **1024** per cache block | **256** (低于不缓存) |
| TTL | 5 分钟 (固定) | 不公开,自动清 |
| Write 价 | 1.25× | 1.0× (不溢价) |
| Hit 价 | **0.1×** (90% off) | **0.2×** (80% off) |
| 命中识别 | `usage.prompt_tokens_details.cached_tokens` + `cache_creation_input_tokens` | 同 implicit |
| 模型 | qwen3-max / qwen3.5-plus / qwen-plus / qwen3.5-flash / qwen-flash / qwen3-coder-plus / qwen3-coder-flash / qwen3-vl-plus 等 |
| API schema | 同时支持 OpenAI Chat Completions + DashScope native;cache_control marker 在 OpenAI 兼容路径上**会被转发** |

**Qwen 是 5 个 provider 里设计最像 Anthropic 的**(显式 marker + 1024 min + 5min TTL + 0.1× hit)。([Alibaba Model Studio context-cache 2026-03-31 update](https://www.alibabacloud.com/help/en/model-studio/context-cache))

### 4.2 Doubao (字节火山方舟)
- **支持** Context API(显式 cache 创建/引用模式,跟 OpenAI 完全不同)+ implicit prefix cache。
- 文档导航页面可见,但 detail page 通过 WebFetch 抓不到(需要 JS render)。具体 min tokens / TTL / pricing **本调研未拿到原文 URL 的精确数字**,需要二次确认。
- 模型: doubao-1.6 / doubao-pro / doubao-seed 等。
- API: 火山方舟自有 schema(非 OpenAI 兼容)+ OpenAI 兼容 endpoint(后者 cache 行为可能受限)。([volcengine 82379 navigation](https://www.volcengine.com/docs/82379/1398933))

**Caveat**: Doubao 这一项是本调研最弱的一格,如果 design 决策需要 commit Doubao 路线,founder/agent 必须打开 volcengine 控制台拿到精确 spec。

### 4.3 GLM (智谱 / Z.AI)
- **自动 (implicit)**。识别相同/高相似前缀,无需 marker。
- 支持模型: GLM-4.5 / 4.6 / 4.7 / GLM-5 全系。
- Min tokens: doc 未明确。
- TTL: doc 写"reasonable time limits, will recalculate after expiration",未给数字。
- Pricing: cache hit ≈ **0.5×** 原价 (50% off,跟 OpenAI 的 0.25× 比偏弱)。GLM-5 文档另写 cached at $0.20/M (= 0.2× 标价)— **两个数字打架**,需要二次确认按模型 SKU。
- 命中识别: `usage.prompt_tokens_details.cached_tokens` (OpenAI 兼容)。
- API: **OpenAI Chat Completions 完全兼容**。([Z.AI cache docs](https://docs.z.ai/guides/capabilities/cache))

### 4.4 Moonshot Kimi
- **混合**: K2 / K2.6 = 自动 prefix cache(75% off,= 0.25× 类似 OpenAI);moonshot-v1 = explicit cache(单独 `/caching` endpoint 创建 cache 对象 + token 引用)。
- 自动 cache 命中识别: `usage.cached_tokens` (但**位置非标准**,在 `choices[].usage.cached_tokens` 而非 `usage.prompt_tokens_details.cached_tokens` — NewAPI 在 `relay/channel/openai/relay-openai.go` 显式注释了这点并兼容)。
- Min tokens / TTL: doc 未明确(K2 自动 cache 无门槛说明;moonshot-v1 explicit 有 storage 计费)。
- API schema: K2 = OpenAI Chat Completions 兼容;moonshot-v1 explicit = 私有 endpoint。([apiyi K2.6 guide](https://help.apiyi.com/en/kimi-k2-6-api-integration-guide-en.html))

---

## 5. NewAPI 网关层 (我们用的版本: v0.13.x branch)

**结论: 全透传 + 全计费**,不 strip cache_control,响应字段三套都识别。

### 5.1 Request 方向 (cache_control 透传)
NewAPI 源码里 cache_control 在多个 DTO 里被原样保留:
- `dto/claude.go`: `CacheControl json.RawMessage \`json:"cache_control,omitempty"\``
- `dto/openai_request.go`: 同上,即使 OpenAI 兼容入口也不剥离
- `relay/common/override_test.go` 有 test case 验证 `{"cache_control":{"type":"ephemeral"}}` 透传

意味着:**我们从 client 发的 cache_control marker,NewAPI 不会丢**。([NewAPI source](https://github.com/QuantumNous/new-api))

### 5.2 Response 方向 (usage 字段识别)
NewAPI 同时识别三家命名:
- `dto/claude.go`: `CacheCreationInputTokens` + `CacheReadInputTokens`
- `dto/openai_response.go`: `CachedTokens` + `PromptCacheHitTokens`
- `relay/channel/openai/relay-openai.go` 注释: "智普的cached_tokens在标准位置: usage.prompt_tokens_details.cached_tokens / Moonshot的cached_tokens在非标准位置: choices[].usage.cached_tokens" — 已专门 patch 处理

### 5.3 计费 (channel_affinity)
`service/channel_affinity.go` 里有 `CachedTokens` + `PromptCacheHitTokens` 字段,UI 层 (`ChannelAffinityUsageCacheModal.jsx`) 把 cache 命中独立计入用量统计。这意味着 **NewAPI 会把 cache 命中 token 单独计费**,founder 配 channel 时可以给 cache hit 单独 ratio。

### 5.4 Streaming patch
`relay/channel/claude/relay-claude.go` 在 SSE `message_delta` 事件里专门把 `cache_creation_input_tokens` / `cache_read_input_tokens` 注入 patch (`setMessageDeltaUsageInt`) — 即使流式响应也保住 cache 计数。

---

## 6. 表 1: Provider 支持矩阵

| Provider | 触发方式 | Min tokens | TTL | 计费倍率 (write / hit) | 命中识别字段 | API schema |
|---|---|---|---|---|---|---|
| **Anthropic Claude** | Explicit marker `cache_control` | 1024 (Sonnet 4.5) / 2048 (Sonnet 4.6) / 4096 (Opus 4.5+, Haiku 4.5) | 5m default / 1h beta header | **5m: 1.25× / 0.1×**;1h: 2.0× / 0.1× | `cache_creation_input_tokens` + `cache_read_input_tokens` | Anthropic Messages 私有 |
| **OpenAI GPT-4o/5/o-series** | 自动 + 可选 `prompt_cache_key` | 1024 (前缀必须 byte-identical) | in_memory: 5–10min idle / max 1h;24h: 24h (gpt-5.5+ default) | **0× / 0.25×** (无 write 溢价,75% off hit) | `usage.prompt_tokens_details.cached_tokens` | OpenAI Chat Completions 原生 |
| **DeepSeek V3/V4** | 自动 (disk cache) | 不公开 (估几百) | hours to days | **0× / ≈0.1×** (90% off) | `prompt_cache_hit_tokens` + `prompt_cache_miss_tokens` | OpenAI 兼容 + DeepSeek ext |
| **Qwen DashScope** | Explicit `cache_control` 或 implicit | 1024 (explicit) / 256 (implicit) | 5min (explicit) | Explicit: 1.25× / 0.1× ;Implicit: 1.0× / 0.2× | `usage.prompt_tokens_details.cached_tokens` (+ `cache_creation_input_tokens`) | OpenAI 兼容 + DashScope native |
| **NewAPI 网关 (v0.13.x)** | 全透传 (不 strip) + 全识别 | 取决于上游 | 取决于上游 | 上游计费倍率 + 可设 channel-level ratio for cache hit | 三套字段全认 (`cache_creation/read_input_tokens` + `cached_tokens` + `prompt_cache_hit_tokens`) + Moonshot 非标准位置 patch | 入口可 OpenAI / Anthropic / Gemini 三选,各自透传 |

---

## 7. 表 2: 我们 4 个候选 model 走 NewAPI 到底能不能 cache + 怎么 cache

| Model | 上游 provider | 触发 | 我们要做的事 (concrete) | 通过 NewAPI 后能否 cache |
|---|---|---|---|---|
| **claude-sonnet-4.5** (主力,民宿场景多模态 + 长 system prompt) | Anthropic 直连 (api.anthropic.com) | Explicit `cache_control` | (1) 把 `executeAgent` 的 `map[string]string` body 改成 typed array(参考 `executeAgentWithImages`),让 system block 能挂 marker。(2) system prompt 必须 ≥ 1024 token 才能挂 marker — 当前 xhs 660 token / mansu 395 token **都不够**,需要把 brand kit / few-shot examples / guideline 揉进 system prompt 凑到 ≥ 1024。(3) marker 挂在 system 末位 + tool 定义末位即可。(4) NewAPI 透传 cache_control 0 改动。(5) 配 Anthropic channel 时给 `cache hit` token 独立 ratio (= 上游 0.1×)。 | ✅ 完全可以,5m / 1h 都行 |
| **claude-haiku-4.5** (备选 / 廉价 fallback) | Anthropic 直连 | Explicit `cache_control` | 同 Sonnet,但**门槛抬到 4096 token**。当前 system prompt 远不够。要 cache 必须把 long context (历史小红书爆款样本 / 民宿 SOP / 客户问答样本) 一起塞到 system prefix。否则 silently 不缓存,白做。 | ✅ 但实际门槛高,需要凑长 prompt |
| **deepseek-v3** (国产廉价 fallback) | DeepSeek 直连或经 NewAPI 第三方 channel | 自动 | **零代码改动**。NewAPI 透传 + 识别 `prompt_cache_hit_tokens`。命中是 "best-effort",我们能做的是**把高频内容(brand voice / few-shot)放在 prompt 最前面**,提高自动命中率。无需 marker。 | ✅ 完全可以,0 改动 |
| **qwen-max / qwen3-max** (国产高端 fallback,中文场景表现好) | DashScope 经 NewAPI OpenAI-compatible channel | Implicit auto + 可选 explicit marker | **Path A (推荐起步)**: 0 改动,靠 implicit cache (256 token 起,0.2× hit)。**Path B (后期优化)**: 同 Claude 做法挂 cache_control marker,拿到 0.1× hit + 1024 token 门槛。Qwen 在 OpenAI 兼容路径上接受 cache_control。 | ✅ Path A 立刻可用;Path B 跟 Anthropic 共享代码改动 |

---

## 8. Gotchas (重要的坑,会影响 design)

1. **Anthropic 4 个 breakpoint 上限**: 一个 request 最多挂 4 个 `cache_control`。"automatic caching" 自己消耗 1 个 slot。L3 agent runtime 如果想同时 cache (a) tool 定义 (b) system prompt (c) brand kit (d) few-shot examples,就**正好 4 个**,无 spare。

2. **Anthropic cache 是 organization-scoped**,不是 model-scoped — 同一个 org 下不同 model 共享 cache 池。**但** 2026-02-05 之后转 workspace-level isolation,需要确认我们的 Anthropic workspace 设置不会切割多租户。

3. **Anthropic 20-block lookback window**: 如果 conversation 超过 20 个 block 还没显式挂 marker,system prompt 那段 cache 会**飞出窗口失效**。L3 长对话(民宿主问 30 轮)必须主动挂 marker,不能靠 automatic。

4. **OpenAI 前 1024 byte-identical**: 我们如果把 `当前时间 / 用户 ID / 当前订单号` 塞到 system prompt 头部,**永远 cache miss**。所有动态信息必须放尾部。

5. **OpenAI cache 是 organization-level**,且 `prompt_cache_key` ≈ 15 req/min 后会 spillover — L3 高并发时需要按用户 hash 多个 key,否则命中率断崖。

6. **DeepSeek TTL 不公开 + best-effort**: 不能把 DeepSeek cache hit 写进 SLA。"hours to days" 听起来美好,但生产高峰期可能被驱逐。

7. **Qwen explicit + implicit 是互斥的两种模式**,不能在同一个 request 里混合。需要 design 决定走哪条。implicit 0 改动但 hit 价 0.2×;explicit 1.25× write + 0.1× hit,**single-shot 就贵 0.05×,5+ 命中才有显著优势**。

8. **Claude Haiku 4.5 把门槛从 1024 拉到 4096** — 这是 2025 年下半年改的。如果 Agent B 的 design 默认 "Haiku 价格便所以选 Haiku",**实际命中比 Sonnet 4.5 还差**(因为 system prompt 不够长)。

9. **NewAPI 计费 ratio**: NewAPI 默认会**按上游原价比例**给 cache hit 计费(channel_affinity 表里有独立 cached_tokens 列)。但 founder 必须**主动在 channel 配置里设 cache hit ratio**,否则可能按 base input ratio 全价收用户的费 → 我们拿到上游折扣但用户没拿到 → 短期赚息差,但长期风险:用户用第三方 LLM tracker 一对就发现。

10. **Moonshot cached_tokens 在非标准位置** (`choices[].usage.cached_tokens` 而非 `usage.prompt_tokens_details.cached_tokens`)。NewAPI 已 patch,但**任何绕过 NewAPI 直连 Moonshot 的代码路径都要自己处理**。

11. **L3 service runtime `executeAgent` 用 `map[string]string`**: 当前签名 (`map[string]string`) 物理上**没法挂 cache_control**。要 cache 必须重构成 typed array (跟 `executeAgentWithImages` 一致)。Agent B 的 integration 设计必须解决这个 schema 升级。

12. **Cache 不影响 output**: 所有 provider 都明确 cache 不改 model 输出 — 但 cache 本身**不能跨 model 复用**(claude-sonnet 的 cache 给不到 claude-haiku),所以多 model fallback 路由会浪费 cache 写入。

---

## 9. 建议: 如果只能挑一个 provider 先实装,挑哪个

**挑 Anthropic Claude (Sonnet 4.5),原因 = ROI 最确定。**

锚点:Sonnet 4.5 的 5m TTL **1 次命中即回本** (write 1.25× → read 0.1×,差 0.9× ≈ -0.25× write surcharge = 净赚 0.65×)。L3 民宿 agent 在一通客户对话里 system prompt + brand kit + few-shot examples 几乎不变,典型场景 8–15 turn,**5 分钟内必然 ≥ 5 次命中**,经济收益 ≈ (cache_size × 5 × 0.9) − (cache_size × 0.25) ≈ 4.25× cache_size × base_input_price 的纯节省。OpenAI 75% off 看起来高,但 5–10min idle 就清,且民宿对话经常在用户那边卡 30s+ 反而触发 eviction;DeepSeek 自动免费但 TTL "几小时到几天" 的 best-effort 不能写进 SLA;Qwen explicit 跟 Claude 思路一致但中文模型质量在内容生成场景上 Claude > Qwen。**Sonnet 4.5 的 1024 token 门槛**也是 4 个候选里最低的(Haiku 4.5 反而 4096),意味着 xhs-copy-writer (660 tok) + brand kit + 1 个 few-shot 就到门槛,实装成本最低。**前提**:Agent B 的 integration 必须把 `executeAgent` 重构成 typed array(已经是必做项)+ system prompt 重组为 stable 在前 / dynamic 在后。

---

## Sources

- [Anthropic Prompt Caching docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [Anthropic Pricing](https://platform.claude.com/docs/en/about-claude/pricing)
- [Anthropic API Pricing 2026 guide (Finout)](https://www.finout.io/blog/anthropic-api-pricing)
- [Bedrock Claude prompt caching](https://docs.aws.amazon.com/bedrock/latest/userguide/prompt-caching.html)
- [Vertex AI Claude prompt caching](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/partner-models/claude/prompt-caching)
- [OpenAI Prompt Caching announcement](https://openai.com/index/api-prompt-caching/)
- [Azure Foundry OpenAI prompt caching (2026-04-14 updated)](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/prompt-caching)
- [OpenAI Cookbook — Prompt Caching 201](https://developers.openai.com/cookbook/examples/prompt_caching_201)
- [DeepSeek Context Caching on Disk announcement (news 0802)](https://api-docs.deepseek.com/news/news0802)
- [DeepSeek KV Cache guide](https://api-docs.deepseek.com/guides/kv_cache)
- [DeepSeek Pricing](https://api-docs.deepseek.com/quick_start/pricing)
- [Alibaba Cloud Qwen Context Cache (2026-03-31 updated)](https://www.alibabacloud.com/help/en/model-studio/context-cache)
- [Volcengine Doubao Context Cache navigation](https://www.volcengine.com/docs/82379/1398933) — *content extraction limited; needs founder follow-up*
- [Z.AI / GLM Context Caching docs](https://docs.z.ai/guides/capabilities/cache)
- [Moonshot Kimi Context Caching (platform.moonshot.ai)](https://platform.moonshot.ai/docs/guide/use-context-caching-feature-of-kimi-api) — redirected 301
- [apiyi Kimi K2.6 integration guide](https://help.apiyi.com/en/kimi-k2-6-api-integration-guide-en.html)
- [NewAPI source (QuantumNous/new-api)](https://github.com/QuantumNous/new-api) — verified via `gh search code` for `cache_control`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cached_tokens`, `prompt_cache_hit_tokens`
- [LiteLLM prompt caching docs](https://docs.litellm.ai/docs/completion/prompt_caching)
- [Vercel AI Gateway automatic caching](https://vercel.com/docs/ai-gateway/models-and-providers/automatic-caching)
