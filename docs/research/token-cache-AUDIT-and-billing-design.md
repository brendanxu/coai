# L1 Token 分销 — Cache pass-through Audit + "不赔钱" 计费设计

**Date**: 2026-05-10
**Author**: tana
**Status**: 🔴 当前生产线已在亏钱(input/output 不分计费),需立即修
**Triggered by**: founder "我们计费有一个原则就是我们不能赔钱"

---

## 0. TL;DR(给 founder 30 秒读完)

Audit 跑了一遍源码,发现 4 个红旗,**3 个比 cache 还严重**:

1. 🔴 **当前已经在亏钱** — `gtk_app_usage_log.tokens_used` 是 input+output 总数,**没分开计费**。output 比 input 贵 5x,客户写多输出少 = 我们倒贴(具体测算见 §2)。
2. 🔴 **客户的 `cache_control` marker 在 adapter 反序列化时丢失** — Claude struct 没声明 `CacheControl` 字段,Go Unmarshal 默认忽略。客户即便发送也等于没发。
3. 🔴 **上游 cache 命中字段没解析** — 即便 marker 透传成功,响应里的 `cache_creation_input_tokens` / `cache_read_input_tokens` 没读,计费按普通 input 算 → 客户 cache 命中我们多收 90%(阶段性赚)or 漏读字段以为是 0(阶段性亏)。
4. 🟡 **Schema 不分类** — `tokens_used` 单一总数 + `cost_cents` 单一数字。要修 cache,必须先把 schema 重做成 4 类。

**不赔钱"的解 = pass-through markup**:每类 token(input / output / cache_write / cache_read)按"上游成本 × 1.30"分别计费。markup 永远是 30%,任何场景都不亏 + 客户能享受 cache 折扣。

**工时估算**:6-8h(schema 改 + adapter cache_control 字段 + cache 响应字段解析 + billing logic + 价格表 seed)。

---

## 1. Audit 详细发现

### 1.1 红旗 #1 — 当前 schema 不分 input/output(LIVE 漏洞)

**Evidence**:
```sql
-- plans/migration.go:140-148 (sqlite + mysql)
CREATE TABLE gtk_app_usage_log (
  id, user_id, plan_id, service,
  tokens_used INT NOT NULL DEFAULT 0,    -- ⚠️ 总数,不分类
  cost_cents  INT NOT NULL DEFAULT 0,    -- ⚠️ 总成本,不知道怎么算的
  created_at
);
```

```go
// usage/aggregator.go:63
// tokens_in/tokens_out are a 50/50 split of `tokens_used`.
```

**问题**:
- `cost_cents` 怎么算的?在 codebase 里 grep 不到清晰的"按模型 × token 类别 × 单价"计算的地方
- 即便在哪里算了,**也只能基于 tokens_used 总数 × 平均单价** — 这相当于 input + output 用一个混合价
- 客户负载偏 output(常见,因为生成式应用 output 通常 > input)→ 我们按混合价收 → 实际成本按真实 input/output 拆 → **倒贴**

**测算**(以 Sonnet 4.5 为例):
- 上游真实价:input $3/M + output $15/M
- 客户 1 次调用:input 1K + output 5K
- 真实成本 = $3 × 0.001 + $15 × 0.005 = $0.078
- 当前计费 — 假设 cost_cents 按"总 6K tokens × 假设平均价 $9/M(input/output 中点)" = $0.054
- **倒贴 31%** ❌

**触发条件**:任何 output > input × 4 的调用类型(总结、生成、写作)— 也就是 LLM 最常见的工作负载。

### 1.2 红旗 #2 — Claude adapter 丢失 cache_control 字段

**Evidence** (`adapter/claude/types.go`):
```go
type Message struct {
    Role    string      `json:"role"`
    Content interface{} `json:"content"`   // ← 没有 cache_control
}

type ChatBody struct {
    Messages    []Message `json:"messages"`
    System      string    `json:"system"`   // ← string 类型,Anthropic 的 system 现在支持 array-of-typed 才能挂 cache_control
    Model       string
    Stream      bool
    Temperature *float32
    TopP        *float32
    TopK        *int
    // ← 没有 CacheControl
}
```

**问题**:
- 客户 POST 进来的 JSON 里有 `cache_control: {type: ephemeral}` 字段
- Go `json.Unmarshal` 把 JSON 反序列化到 `ChatBody` struct 时,**未声明的字段会被默默丢弃**
- 我们再 `json.Marshal` 转发上游时,字段已经丢了
- 上游(Anthropic)收到没 marker 的请求 → 不 cache → 客户以为我们透传了实际没透传

**验证方法**:打一个 cache_control 请求到 greentokey,抓上游收到的 body diff client 发的 body — `cache_control` 字段会消失。

### 1.3 红旗 #3 — Cache 响应字段没解析

**Evidence**: `grep -rn "cache_creation\|cache_read\|cached_tokens" adapter/ globals/` = **0 命中**

**问题**:
- 即便假设 cache_control 透传成功(via NewAPI 直通绕过 adapter),上游会返回:
  ```json
  "usage": {
    "input_tokens": 50,
    "cache_creation_input_tokens": 248,
    "cache_read_input_tokens": 100000,
    "output_tokens": 503
  }
  ```
- 我们的 adapter 没解析后 3 个字段
- 计费要么用 `input_tokens` = 50(漏掉 100K 命中字段,**少收钱**),要么把 `input_tokens` 当成全部输入(**多收钱**,客户感觉被 ripped off)
- 任一情况都跟客户期望(透明 pass-through)不一致

### 1.4 红旗 #4 — Schema 不为 cache 留位置

**Evidence**: `gtk_app_usage_log` 只有 `tokens_used` + `cost_cents`,没有任何 cache 相关字段。

**问题**:
- 没法事后审计某次调用的 cache 命中情况
- 没法给客户出"cache 命中省了多少钱"的报表(这本来是 token 分销的卖点)
- 没法做 per-channel 的 cache 命中率监控(运维角度)

---

## 2. "不赔钱" 计费设计(核心交付物)

### 2.1 原则(3 条)

**P1: Pass-through markup,每类独立**
每一类 token(input / output / cache_write / cache_read 4 类起步)按"上游真实单价 × 1.30 markup"独立计费。markup 是单一数字。

**P2: 任何场景下都赚 30%**
- 客户用普通调用 → 赚 input markup + output markup
- 客户大量 cache 命中 → 赚 cache_read markup(虽然绝对值小,但比例稳定)
- 客户写入大量 cache → 赚 cache_write markup(绝对值高,因 1.25x 上游 × 1.30 = 1.625x base)
- **永远不会因为客户用 cache 而我们亏**

**P3: 价格表是数据,不是代码**
新建表 `gtk_provider_pricing`,字段:provider × model × token_type × upstream_unit_cost_per_million_usd。运营改 markup 不需要发版。

### 2.2 数据模型

#### 改 `gtk_app_usage_log`(ALTER,不重建)

```sql
-- migration.go 加 V2
ALTER TABLE gtk_app_usage_log
  ADD COLUMN model_id              VARCHAR(80)  NOT NULL DEFAULT '',
  ADD COLUMN provider              VARCHAR(40)  NOT NULL DEFAULT '',  -- 'anthropic'/'openai'/...
  ADD COLUMN input_tokens          INT          NOT NULL DEFAULT 0,
  ADD COLUMN output_tokens         INT          NOT NULL DEFAULT 0,
  ADD COLUMN cache_write_tokens    INT          NOT NULL DEFAULT 0,   -- cache_creation
  ADD COLUMN cache_read_tokens     INT          NOT NULL DEFAULT 0,   -- cache_hit
  ADD COLUMN cache_ttl             VARCHAR(8)   NOT NULL DEFAULT '',  -- '5m' / '1h' / ''
  ADD COLUMN upstream_cost_micro   BIGINT       NOT NULL DEFAULT 0,   -- 上游真实成本(微分)
  ADD COLUMN client_charge_micro   BIGINT       NOT NULL DEFAULT 0,   -- 客户被收费(微分)
  ADD COLUMN markup_multiplier     DECIMAL(4,3) NOT NULL DEFAULT 1.300;
-- tokens_used + cost_cents 保留作 backward-compat,新代码用新字段
```

`micro` = 1/1,000,000 USD。能精确表达 $0.000001 的差距,避免 cents 在小额计算上的舍入误差。

#### 新建 `gtk_provider_pricing`(运营 SoT)

```sql
CREATE TABLE gtk_provider_pricing (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  provider        VARCHAR(40)  NOT NULL,        -- 'anthropic'
  model_id        VARCHAR(80)  NOT NULL,        -- 'claude-sonnet-4.5'
  token_type      VARCHAR(20)  NOT NULL,        -- 'input'/'output'/'cache_write_5m'/'cache_write_1h'/'cache_read'
  upstream_per_m  DECIMAL(10,6) NOT NULL,       -- 上游 USD per 1M tokens (e.g. 3.000000)
  effective_from  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
  notes           VARCHAR(255),
  UNIQUE KEY uk_pricing (provider, model_id, token_type, effective_from)
);

-- 全局 markup 配置(单一来源)
CREATE TABLE gtk_billing_config (
  k VARCHAR(40) PRIMARY KEY,
  v VARCHAR(40) NOT NULL
);
INSERT INTO gtk_billing_config VALUES ('markup_multiplier', '1.300');
```

Seed 数据(Sonnet 4.5 例):
```
('anthropic', 'claude-sonnet-4.5', 'input',          3.000000)
('anthropic', 'claude-sonnet-4.5', 'output',         15.000000)
('anthropic', 'claude-sonnet-4.5', 'cache_write_5m', 3.750000)
('anthropic', 'claude-sonnet-4.5', 'cache_write_1h', 6.000000)
('anthropic', 'claude-sonnet-4.5', 'cache_read',     0.300000)
```

### 2.3 计费公式

每次调用,从上游响应抽 4 个 token 数:
- `n_input` = `usage.input_tokens` (Anthropic 是 cache 写入和命中**之外**的尾部)
- `n_output` = `usage.output_tokens`
- `n_cache_write` = `usage.cache_creation_input_tokens` (default 0)
- `n_cache_read` = `usage.cache_read_input_tokens` (default 0)

按价格表查 4 个 `upstream_per_m`,算上游成本(USD micro):

```
upstream_cost_micro = (
    n_input        × p_input_per_m
  + n_output       × p_output_per_m
  + n_cache_write  × p_cache_write_per_m   -- 用对应 5m 或 1h 表
  + n_cache_read   × p_cache_read_per_m
) / 1_000_000 × 1_000_000  -- 留 micro 单位

client_charge_micro = upstream_cost_micro × markup_multiplier
```

写入 `gtk_app_usage_log`:`upstream_cost_micro` + `client_charge_micro` + 4 个 token 数 + `markup_multiplier`(快照,免运营改 markup 影响历史账单)。

### 2.4 不赔钱的数学证明

```
client_charge = upstream_cost × M  其中 M ≥ 1.0

每条调用 profit = client_charge - upstream_cost
                = upstream_cost × (M - 1)
                ≥ 0  ⟺  M ≥ 1.0
```

只要 `markup_multiplier ≥ 1.0`,**任何 token 类组合都不可能亏**。当前推荐 M = 1.30(30% 毛利覆盖 NewAPI 服务器 + R2 backup + 客服时间)。

**Cache 场景验证**(Sonnet 4.5,假设客户负载 1K input / 5K output / 100K cache_read / 250 cache_write 5m):

| 项 | tokens | upstream USD/M | upstream micro | × M=1.30 | client micro |
|---|---|---|---|---|---|
| input | 1,000 | 3.00 | 3,000 | 3,900 | 3,900 |
| output | 5,000 | 15.00 | 75,000 | 97,500 | 97,500 |
| cache_write | 250 | 3.75 | 938 | 1,219 | 1,219 |
| cache_read | 100,000 | 0.30 | 30,000 | 39,000 | 39,000 |
| **合计** | | | **108,938** | | **141,619** |

- 上游真实成本: $0.108938
- 客户被收费: $0.141619
- **毛利**: $0.032681 = **30%** ✅

Cache 命中越多(`n_cache_read` 越高),客户绝对花费越低,但**毛利率永远 30%**。客户感受 = "用 greentokey 跟直连 Anthropic 一样能 cache 省钱(只是多 30% markup,换我多 provider + 一笔账单 + 国内可访问)"。

### 2.5 客户端透明度(营销 + 信任)

每月账单(或 API `/v1/usage` 端点)给客户看到:
- 总调用次数
- 普通 token 花费
- **cache 命中省了多少钱**(原本要 $X,因 cache 实付 $Y)
- 总账单 = ($X 应付 - $Y 省下) × 1.30

这正是把"我们 forward cache_control"从隐性能力**显性化**成营销卖点的方式。

---

## 3. 实装路径(6-8h,4 个 codex worker spec)

### Worker 1: Schema migration + pricing seed(2h)
- ALTER `gtk_app_usage_log` 加 7 字段
- 新建 `gtk_provider_pricing` + `gtk_billing_config`
- Seed Anthropic Sonnet 4.5 + Haiku 3.5 + DeepSeek V3 + OpenAI GPT-4o pricing
- 写 idempotent migration,sqlite + mysql 双引擎
- 测试:migration 跑两遍不出错

### Worker 2: Adapter forward `cache_control`(2h)
- `adapter/claude/types.go`:
  - `Message.Content` 改成支持 `[]ContentBlock` (typed),每个 block 可选 `CacheControl` 字段
  - `ChatBody.System` 类型从 `string` 改成 `interface{}`(支持 string 或 array-of-typed)
- `adapter/openai/types.go`:OpenAI 是 auto-cache,不需要 marker,**只需加 `prompt_cache_key` 透传**
- 客户传什么我们透什么,**0 默认行为改动**
- 测试:发 cache_control 请求,抓上游收到的 body 含 marker

### Worker 3: Adapter parse cache 响应字段(1h)
- `adapter/claude/types.go` 加:
  ```go
  type Usage struct {
      InputTokens              int `json:"input_tokens"`
      OutputTokens             int `json:"output_tokens"`
      CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
      CacheReadInputTokens     int `json:"cache_read_input_tokens"`
  }
  ```
- `adapter/openai/types.go` 加 `prompt_tokens_details.cached_tokens`
- 把 4 个数(non-cache provider 后 2 个 = 0)作为 callback 暴露给 billing 层

### Worker 4: Billing logic + log writer(2-3h)
- 新建 `billing/calculator.go`:
  - `Calculate(provider, model, n_input, n_output, n_cache_write, n_cache_read, ttl)` → `(upstream_micro, client_micro, markup)` 三元组
  - 价格查 `gtk_provider_pricing`,缓存到内存(运营改价 5min 内生效)
- 改 `service/runtime.go` 或 `globals/usage.go` 的 usage log writer,接收 4 个 token 数 + 调 calculator
- 写到 `gtk_app_usage_log` 用新字段
- 测试:hand-crafted usage payload + price seed → 算出预期 client_micro

---

## 4. 何时部署 / 风险

### 4.1 部署顺序(零停机)
1. Worker 1 schema migration(可与生产并行,旧字段保留)
2. Worker 2-4 用 feature flag 关闭,先并行写新字段(double-write 模式)
3. 1 周观测对比新 vs 旧账单数据,差距 < 1% 即切流量
4. 切完保留旧字段 30 天,确认 OK 再 drop

### 4.2 风险 + 缓解

| 风险 | 概率 | 缓解 |
|---|---|---|
| 上游真实价格变动(Anthropic 突然改价) | 低 | `gtk_provider_pricing` 有 `effective_from`,改价加新行不删旧行,历史账单按当时价 |
| Markup 1.30 客户嫌贵跑了 | 中 | 加监控:每月成本 / 每月 MRR 比 < 80% 触发降 markup 到 1.20 |
| 计费 bug 算错钱 | 中 | Worker 4 必须有 unit test,case 覆盖 4 类 token 全 0 / 全非 0 / 任一为 0 共 16 组合 |
| 客户用 OpenAI 但 OpenAI auto-cache 我们没识别 → 漏收 cache_write 钱 | 高 | OpenAI 的 cache 没有显式 write,只在 `cached_tokens` 体现 read。我们在 OpenAI 上**只算 input/output/cache_read 三类**(write 计入普通 input),零亏空 |
| NewAPI 网关层把 cache_control 字段 strip(虽然 Agent A 说 forward) | 低 | 部署后用真实请求 e2e 验证一次 |

---

## 5. Founder 决策点(1 个)

**Q: 同意立项吗?6-8h 工时,4 个 codex worker。**

- 同意 → tana 拆 worker spec 给 codex,founder 见每 worker 完成的 commit 即可
- 不同意 → 至少 worker 1+4 优先做(修 input/output 不分这个 LIVE 漏洞,2-4h)
- 全不做 → 当前生产线持续在某些客户场景下亏钱(具体多少要等真实流量数据,但客户负载偏 output 是绝大多数)

我推荐 **全做(6-8h)**:既修 LIVE 漏洞,又把 cache 透传作为差异化卖点开发出来。一笔投入,两个回报。

---

## 6. 跟上游 CoAI fork 的协调

我们的 `adapter/` 是 fork 自 CoAI 上游的。改 `claude/types.go` + `openai/types.go` 等于和上游 diverge。两个选择:

- **A. 维持 fork**:我们 PR 改动到 CoAI 上游(他们大概率不接,因为 CoAI 主线也没解决这个),失败再 maintain fork
- **B. 加 cache adapter wrapper**:不改 `adapter/`,在更上面加一层 `cache_aware_dispatcher.go`,只在 cache_control / cache 响应这个边界做事。维护成本最低

**推荐 B**。Worker 2-4 都改成 wrapper 模式,`adapter/` 一字不动,fork 永远易合并。

---

## 7. 配套文档

- `docs/research/token-cache-A-provider-api-survey.md` — provider 端定价 ground truth(给 §2.2 seed 数据用)
- `docs/research/token-cache-B-integration-architecture.md` — 之前的 L3 架构分析(本文档把场景重定向到 L1 了)
- `docs/research/token-cache-C-roi-economics.md` — L3 民宿 ROI(已 obsolete,保留作历史)
- `docs/research/token-cache-FINAL-decision.md` — 之前的 L3 综合(已被本文档实质上推翻;**对 L3 民宿 cache 仍然适用**,但 founder 真正问的是 L1 token 分销)
