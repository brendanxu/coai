# Ops 手册 — Charge config 加 cache 字段

**Date**: 2026-05-10
**Author**: tana
**Pre-condition**: prod 必须跑 W5 image(含 commits `0e72388` `59c5b26` `bb9655b` `6cf8281` `4102cf4`)。当前 prod 跑 v0.9.0-pivot-民宿-marketing,**没有** cache 字段,运 charge config 加 cache 字段无效。
**Target**: 客户用 cache 时**真正享受**上游折扣(per founder "客户能通过 token cache 节约成本")。
**Margin invariant**: 即使加错 cache 字段,fallback 到 GetInput → 我们不亏(per "我们不能赔钱"原则)。

---

## 0. TL;DR

W5 commits 把 cache 字段从 `cache_control` 客户 marker → upstream → adapter 解析 → Buffer 计费一路接通。**但 charge config(运营 layer)还没把 cache 单价配好**,所以现在 cache 命中时客户被按 input 全价收 → 我们多赚 9 倍 markup,客户没感觉省钱。

加 charge config = 把上游 cache 折扣**转嫁** 给客户 + 我们守住 30% markup。

---

## 1. Math 推导(为什么是这些数字)

通用公式:
```
客户单价 = 上游单价(USD/M) × 7.2(汇率) ÷ 1000(per 1k tokens) × 1.30(markup)
        = 上游单价 × 0.00936
```

每个 model + token class 一行。

### 1.1 Anthropic Claude Sonnet 4.5

| 类别 | 上游 USD/M | × 7.2 / 1000 | × 1.30 markup | charge config 字段 ¥/1k |
|---|---|---|---|---|
| input | $3.00 | ¥0.0216 | ¥0.0281 | **`input: 0.0281`** |
| output | $15.00 | ¥0.108 | ¥0.1404 | **`output: 0.1404`** |
| cache_read | $0.30 | ¥0.00216 | ¥0.0028 | **`cache_read: 0.0028`** |
| cache_write_5m | $3.75 | ¥0.027 | ¥0.0351 | **`cache_write_5m: 0.0351`** |
| cache_write_1h | $6.00 | ¥0.0432 | ¥0.0562 | **`cache_write_1h: 0.0562`** |

### 1.2 Anthropic Claude Haiku 3.5

| 类别 | 上游 USD/M | charge 字段 ¥/1k |
|---|---|---|
| input | $0.80 | **`input: 0.0075`** |
| output | $4.00 | **`output: 0.0374`** |
| cache_read | $0.08 | **`cache_read: 0.0007`** |
| cache_write_5m | $1.00 | **`cache_write_5m: 0.0094`** |
| cache_write_1h | $1.60 | **`cache_write_1h: 0.0150`** |

### 1.3 DeepSeek V3

DeepSeek 自动 KV cache(无显式 write tier)。只配 cache_read,write 字段不写(fallback `GetInput × 1.25` 兜底,但 DeepSeek 永远不会写 cache → cache_write_tokens=0 → 实际不收 cache_write 钱)。

| 类别 | 上游 USD/M | charge 字段 ¥/1k |
|---|---|---|
| input | $0.28 | **`input: 0.0026`** |
| output | $1.10 | **`output: 0.0103`** |
| cache_read | $0.028 | **`cache_read: 0.00026`** |

### 1.4 OpenAI GPT-4o(可选,如果你们接 OpenAI 上游)

OpenAI auto-cache(50% 折扣,无 write tier)。

| 类别 | 上游 USD/M | charge 字段 ¥/1k |
|---|---|---|
| input | $2.50 | **`input: 0.0234`** |
| output | $10.00 | **`output: 0.0936`** |
| cache_read | $1.25 | **`cache_read: 0.0117`** |

> 这些是上游 base 单价 × markup 1.30。如果你想给客户更大折扣(降 markup 抢市场)→ 把 markup 改成 1.20 / 1.15 重算。**永远不能 < 1.0 否则赔钱**。

---

## 2. 完整 yaml snippet(可直接粘到 prod config.yaml)

```yaml
# /opt/greentokey/data/coai/config/config.yaml — charge: 节点
# 如果当前已有 charge: 节点,合并 / 替换以下条目
# 如果 prod 没接对应 model,先在 admin UI 加 channel 再启用 charge

charge:
  - id: 100
    type: token
    models:
      - claude-sonnet-4-5
      - claude-sonnet-4-5-20250929
      - claude-3-5-sonnet
      - claude-3-7-sonnet-latest
    input: 0.0281
    output: 0.1404
    cache_read: 0.0028
    cache_write_5m: 0.0351
    cache_write_1h: 0.0562
    anonymous: false

  - id: 101
    type: token
    models:
      - claude-haiku-3-5
      - claude-3-5-haiku-latest
    input: 0.0075
    output: 0.0374
    cache_read: 0.0007
    cache_write_5m: 0.0094
    cache_write_1h: 0.0150
    anonymous: false

  - id: 102
    type: token
    models:
      - deepseek-chat
      - deepseek-v3
      - deepseek-reasoner
    input: 0.0026
    output: 0.0103
    cache_read: 0.00026
    anonymous: false

  - id: 103
    type: token
    models:
      - gpt-4o
      - gpt-4o-2024-11-20
      - gpt-4o-mini
    input: 0.0234
    output: 0.0936
    cache_read: 0.0117
    anonymous: false
```

> **id 编号选 100+** 避免跟既有 charge 条目 ID 冲突。如果 100-103 已被占,改 200 / 300 / etc。

---

## 3. 两条应用路径(选一条)

### 路径 A — 直接编辑 VPS yaml(最简单)

```bash
ssh greentokey   # 用现有 ~/.ssh/config alias

# 1. 备份当前 config
sudo cp /opt/greentokey/data/coai/config/config.yaml \
        /opt/greentokey/data/coai/config/config.yaml.bak.$(date +%Y%m%d-%H%M%S)

# 2. 编辑 charge 节点(找到 charge: 块,合并 §2 的 4 个条目)
sudo nano /opt/greentokey/data/coai/config/config.yaml
# 或 sudo vim,按你 preference

# 3. 重启 coai 让 viper 重 load
cd /opt/greentokey
docker compose restart coai

# 4. 看 30 秒日志确认起来了
docker compose logs --tail 30 coai
# 期望看到 "Listening on :8094" + "charge loaded N rules"
```

### 路径 B — Admin API curl(在线热更新,不重启)

需要先用 `tana` 账号登录拿 session token。

```bash
# 1. Login 拿 session token (greentokey 用 cookie 还是 bearer?)
#    确认登录后 admin endpoint 是否能直接用同 cookie。
#    看 prod 实测 — 如果 cookie-based,curl 加 -c/-b 即可。
#    如果 bearer-based,文档化到 ENV var ADMIN_TOKEN=...

# 2. 给每个 model 组 POST 一次:
curl -X POST https://www.greentokey.com/api/admin/charge/set \
  -H "Authorization: Bearer <admin-session-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "id": 100,
    "type": "token",
    "models": ["claude-sonnet-4-5", "claude-sonnet-4-5-20250929"],
    "input": 0.0281,
    "output": 0.1404,
    "cache_read": 0.0028,
    "cache_write_5m": 0.0351,
    "cache_write_1h": 0.0562,
    "anonymous": false
  }'

# 期望响应: {"status":true,"error":""}

# 3. 4 个 model 组重复 step 2(只换 -d body)

# 4. 验证:list charge
curl https://www.greentokey.com/api/admin/charge/list \
  -H "Authorization: Bearer <admin-session-token>"
# 应该看到 4 个新 entries 含 cache_* 字段
```

> **路径 A 推荐**:简单 + 低 risk。Founder 已经会 ssh greentokey + docker compose,流程熟。
> **路径 B 优势**:不重启,不停服,但需要确认 cookie/bearer auth 实测怎么走。

---

## 4. 验证 cache 折扣真实生效(端到端)

部署 W5 + 应用 charge config 后,跑这个 smoke:

```bash
# 假设 sk-tnx-customer 是测试客户 token(founder 自己的 dev token)
# 假设 ANTHROPIC channel 已在 NewAPI 后台配好(type 14)

# 第 1 次调用:发 cache_control marker,system prompt ≥1024 tokens
# 期望响应 cache_creation_input_tokens > 0
SYSTEM_PROMPT=$(printf "Lorem ipsum dolor sit amet. %.0s" {1..200})
curl -X POST https://api.greentokey.com/v1/chat/completions \
  -H "Authorization: Bearer ${GTK_CUSTOMER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SYSTEM_PROMPT" '{
    model: "claude-sonnet-4-5",
    messages: [
      {role:"system", content:[{type:"text", text:$s, cache_control:{type:"ephemeral", ttl:"5m"}}]},
      {role:"user", content:"hi"}
    ]
  }')"

# 第 2 次:同 system prompt,不同 user message
# 期望响应 cache_read_input_tokens > 0
curl -X POST https://api.greentokey.com/v1/chat/completions \
  -H "Authorization: Bearer ${GTK_CUSTOMER_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SYSTEM_PROMPT" '{
    model: "claude-sonnet-4-5",
    messages: [
      {role:"system", content:[{type:"text", text:$s, cache_control:{type:"ephemeral", ttl:"5m"}}]},
      {role:"user", content:"hi again"}
    ]
  }')"

# 在 VPS MySQL 看 quota usage(用户应该被收的钱)
# UPDATE: 当前 gtk_app_usage_log 没接通,只能看 user.quota.used 减少多少
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "SELECT u.username, q.quota, q.used FROM chatnio.auth u JOIN chatnio.quota q ON u.id=q.user_id WHERE u.username='customer';"
```

期望:
- 第 1 次扣的钱 ≈ system_tokens × cache_write_5m + user_tokens × input + output_tokens × output
- 第 2 次扣的钱 ≈ system_tokens × **cache_read** + user_tokens × input + output_tokens × output(显著少于第 1 次)

如果第 2 次扣的钱跟第 1 次差不多 → cache 没命中 / Buffer 没切到 upstream-aware 路径 / charge 字段未生效。

---

## 5. Markup 调整指引(如果想给客户更大折扣抢市场)

| markup | 客户感受 | 我们利润率 |
|---|---|---|
| 1.30 | 上游折扣完全转嫁 + 30% markup(当前推荐) | 30% 毛利 |
| 1.20 | 折扣转嫁 + 20% markup,客户更便宜 | 20% 毛利 |
| 1.15 | 折扣转嫁 + 15% markup,接近成本价 | 15% 毛利 |
| 1.05 | 几乎转嫁全部,只赚 5% 服务费 | 5% 毛利 — 危险,不能再降 |
| < 1.0 | **赔钱**,违背 "不赔钱" 原则 | 负 |

要换 markup 直接重算 §1 表格 × 新 markup,改 charge config 即可。

---

## 6. 何时执行这步(时间表)

| 时机 | 动作 | 理由 |
|---|---|---|
| 现在 | **不执行**(prod 是 v0.9 image,无 cache 字段) | 改了也不读 |
| W5 image 部署后 | **路径 A 编辑 yaml + restart**(15 分钟) | 一次性完成 |
| Pivot v4 Gate 1 通过(EOW2 = 2026-05-14)后 | 部署 W5 image + 应用 charge | 跟主线 deploy 节奏对齐 |
| 接第一个真实 Anthropic 客户后 | smoke 测试 §4(2 调用看 cache 命中) | 真实 traffic 验证 |

---

## 7. 不做的事

- ❌ 不要把 admin token 提交到 git(`9XEBNqNr4e8kcILZ` 等只在 memory + VPS)
- ❌ 不要在没 cache_read upstream 字段的 provider(zhipuai / hunyuan / etc)上配 cache_read — 浪费 yaml 行
- ❌ 不要 markup < 1.0
- ❌ 不要在 prod 实际接 Anthropic channel 之前应用这个 config(无副作用,但浪费时间)

---

## 8. 跨引用

- W5 commits: `0e72388` / `59c5b26` / `bb9655b` / `6cf8281` / `4102cf4`
- W4 计费实现: `utils/tokenizer.go::CountUpstreamQuota`
- W4 fallback 逻辑: `channel/charge.go::GetCacheRead/Write5m/Write1h`(unset → input baseline,确保不亏)
- 上游单价 ground truth: `docs/research/token-cache-A-provider-api-survey.md`
- 不亏数学证明: `utils/upstream_billing_test.go::TestCountUpstreamQuota_NeverLoseMoney`
