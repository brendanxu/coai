# Deploy Plan — Path 1 W1-W5 to prod

**Date**: 2026-05-10
**Author**: tana
**Branch**: `feat/v0.16-admin-concierge-orders` @ `4e8879b` (pushed to origin)
**Target**: `api.greentokey.com` VPS, image `greentokey-coai:v1.0.0-cache-billing`
**Pre-condition**: Pivot v4 Gate 1 PASS (≥4 银+ in 8-10 民宿主对话, EOW2 = 2026-05-14)
**Post-condition**: 客户用 cache_control marker 真正触发上游 cache + 我们计费按 4 类正确算账

---

## 0. TL;DR — 4 步,~40 分钟

1. **Pre-flight**(5 min):Gate 1 绿灯确认 + 看当前 prod 健康
2. **Build + ship**(15 min):Mac 本地 vite build + Docker image + rsync VPS
3. **Migration smoke**(5 min):看 schema V2 字段 ALTER 跑完 + pricing seed 入库
4. **Apply charge config**(10 min):粘贴 ops playbook 的 yaml + restart + verify
5. **Smoke test**(5 min):curl 看 cache_creation/read 命中

零停机要求:不严格(本 wedge 用户量近 0)。如果未来流量起来,按 PKG-M5 SOP 蓝绿换。

---

## 1. Pre-flight Check

### 1.1 Gate 1 绿灯确认

per `docs/distribution/01-assignment-dm-templates.md` §1:
- ≥4 银+ → 绿,继续部署 ✅
- 2-3 银+ → 黄,founder 决定 deploy 还是延 4-6 对话
- 0-1 银+ → 红,**不部署**,Pivot v5 重做 wedge → 这套代码雪藏在 origin

founder 一句"Gate 1 绿了" tana 才动手。

### 1.2 当前 prod 健康

```bash
ssh greentokey
cd /opt/greentokey
docker compose ps
# 期望: greentokey-coai (running), greentokey-mysql, greentokey-redis, greentokey-caddy 全 healthy
docker compose logs --tail 20 coai | grep -i "error\|panic" || echo "no recent errors"
curl -fs https://api.greentokey.com/api/gtk/v1/pool > /dev/null && echo "pool endpoint OK"
```

### 1.3 备份当前生产

```bash
# 当前镜像 tag(留作 rollback 锚点)
docker compose -f /opt/greentokey/docker-compose.yml images coai
# 期望见到 greentokey-coai:v0.9.0-pivot-民宿-marketing 或类似

# DB snapshot(虽然 cron 每天 03:00 SGT 跑了 R2 backup,临 deploy 多一份保险)
docker exec greentokey-mysql mysqldump -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  --single-transaction chatnio > /tmp/chatnio-pre-w5-$(date +%Y%m%d-%H%M).sql
ls -la /tmp/chatnio-pre-w5-*.sql
```

---

## 2. Build + Ship

### 2.1 本地 build dist

VPS 内存 2GB build 前端会 OOM,用 split 模式:Mac 本地 vite build → rsync → VPS Docker 只编 Go。

```bash
cd /Users/brendanxu/tanaxu/greentokey/coai-v0.7-design

# Frontend build
cd app && npm install && npm run build && cd ..
# dist/ 应该 ~30s build 完,见 Pricing.* / Token.* / Index.* hash 文件

# 验证 dist 有效
ls -la dist/index.html dist/assets/ | head -10

# rsync dist + Go source 到 VPS staging dir
rsync -avz --delete \
  --exclude=node_modules --exclude=.git --exclude=app/node_modules \
  --exclude=*.log --exclude=*.test.go --exclude=*_test.go \
  ./ greentokey:/tmp/greentokey-w5-build/
```

### 2.2 VPS Docker build

```bash
ssh greentokey

cd /tmp/greentokey-w5-build

# 用 Dockerfile.split (per CLAUDE.md history 2026-04-30 — 2GB VPS 必须分阶段)
docker build -f Dockerfile.split -t greentokey-coai:v1.0.0-cache-billing . 2>&1 | tail -20

# 验证 image 大小
docker images greentokey-coai:v1.0.0-cache-billing
# 期望 ~96MB(类似 v0.9 size)
```

### 2.3 Update docker-compose tag + bring up

```bash
# 改 docker-compose.yml 的 coai image tag
cd /opt/greentokey
sudo sed -i 's|greentokey-coai:v0.9.0-pivot-民宿-marketing|greentokey-coai:v1.0.0-cache-billing|' docker-compose.yml
grep "greentokey-coai:" docker-compose.yml  # verify

docker compose up -d coai
# 期望 30s 内起来

# 看启动日志确认 migration 跑了
docker compose logs --tail 60 coai | grep -E "migration|Migrate|gtk_app_usage_log|gtk_provider_pricing|listening on"
```

期望见到:
- `Listening on :8094`
- 如果 migration log 详尽:`gtk_app_usage_log v2 columns added`,`gtk_provider_pricing seeded N rows`,`gtk_billing_config markup_multiplier=1.300`

如果有 migration 错误,**立即 rollback**(见 §6)。

---

## 3. Migration Smoke

### 3.1 Verify schema V2

```bash
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "DESCRIBE chatnio.gtk_app_usage_log;" | grep -E "input_tokens|cache_write_tokens|cache_read_tokens|markup_multiplier"
```

期望见到 4 行:
```
input_tokens          int(11)         NO    0
output_tokens         int(11)         NO    0
cache_write_tokens    int(11)         NO    0
cache_read_tokens     int(11)         NO    0
markup_multiplier     decimal(4,3)    NO    1.300
```

### 3.2 Verify pricing seed

```bash
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "SELECT provider, model_id, token_type, upstream_per_m FROM chatnio.gtk_provider_pricing ORDER BY provider, model_id, token_type;"
```

期望 16 行 — Anthropic Sonnet 4.5(5)+ Haiku 3.5(5)+ DeepSeek V3(3)+ GPT-4o(3)。

### 3.3 Verify billing config

```bash
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "SELECT k, v FROM chatnio.gtk_billing_config;"
```

期望:
```
k                    v
markup_multiplier    1.300
```

如果三个 verify 任一不通过,立即 rollback。

---

## 4. Apply Charge Config(per X 选项 ops playbook)

跟着 `docs/ops/charge-config-cache-fields.md` 路径 A:

```bash
# Backup
sudo cp /opt/greentokey/data/coai/config/config.yaml \
        /opt/greentokey/data/coai/config/config.yaml.bak.$(date +%Y%m%d-%H%M%S)

# 看现有 charge: 节点
grep -A 30 "^charge:" /opt/greentokey/data/coai/config/config.yaml | head -50

# 编辑 — 把 docs/ops/charge-config-cache-fields.snippet.yaml 的 4 个 entries 合并进去
sudo nano /opt/greentokey/data/coai/config/config.yaml
# 注意: 既有 entries 如果已含 claude / deepseek / gpt-4o,删掉再粘新 entries
# 如果 ID 100-103 已被占,改 200/300

# Reload
docker compose restart coai

# Verify reload
docker compose logs --tail 30 coai | grep -i "charge\|listening"

# 看 charge 现状(用 admin endpoint)
curl -s https://api.greentokey.com/api/admin/charge/list \
  -H "Authorization: Bearer <admin-session-token>" | jq '.data[]' | head -50
# 期望见到 cache_read / cache_write_5m / cache_write_1h 字段
```

---

## 5. Smoke Test — 端到端 cache 命中

per `docs/ops/charge-config-cache-fields.md` §4。**前提**:NewAPI 后台已配 Anthropic channel(type 14)。如果还没配,这步只能用 mock 验证(已在 commit `4102cf4` 完成)。

如果有真 Anthropic key:

```bash
# 配 channel(在 NewAPI 后台 admin UI 加 type 14, key=sk-ant-...)
# 然后:
SYSTEM_PROMPT=$(printf 'Lorem ipsum dolor sit amet. %.0s' {1..200})

# Round 1: cache write
curl -s -X POST https://api.greentokey.com/v1/chat/completions \
  -H "Authorization: Bearer sk-tnx-<test-customer-token>" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SYSTEM_PROMPT" '{
    model: "claude-sonnet-4-5",
    messages: [
      {role:"system", content:[{type:"text", text:$s, cache_control:{type:"ephemeral", ttl:"5m"}}]},
      {role:"user", content:"smoke 1"}
    ],
    stream: false
  }')" | jq '.usage'

# 期望: cache_creation_input_tokens > 1024

# Round 2: cache read (within 5min window)
curl -s -X POST https://api.greentokey.com/v1/chat/completions \
  -H "Authorization: Bearer sk-tnx-<test-customer-token>" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SYSTEM_PROMPT" '{
    model: "claude-sonnet-4-5",
    messages: [
      {role:"system", content:[{type:"text", text:$s, cache_control:{type:"ephemeral", ttl:"5m"}}]},
      {role:"user", content:"smoke 2"}
    ],
    stream: false
  }')" | jq '.usage'

# 期望: cache_read_input_tokens > 1024 (matches round 1's cache_creation count)

# 验证客户被收的钱:
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "SELECT u.username, q.quota, q.used FROM chatnio.auth u JOIN chatnio.quota q ON u.id=q.user_id WHERE u.username LIKE '%test%' ORDER BY q.used DESC LIMIT 3;"
```

如果 round 2 的 quota.used 增量明显小于 round 1(因为 cache_read 单价 = input × 0.1) → cache 真实生效 ✅

---

## 6. Rollback Path

如果 §3 任一 verify 失败、§5 客户用不了、或者出 panic:

```bash
ssh greentokey
cd /opt/greentokey

# 1. 改回旧 image tag
sudo sed -i 's|greentokey-coai:v1.0.0-cache-billing|greentokey-coai:v0.9.0-pivot-民宿-marketing|' docker-compose.yml

# 2. 起旧 container
docker compose up -d coai

# 3. 看日志确认起来
docker compose logs --tail 30 coai | grep -i "listening\|error"

# 4. config.yaml 也回滚(charge cache 字段在旧代码会被 viper 静默丢弃,但留着无害)
# sudo cp /opt/greentokey/data/coai/config/config.yaml.bak.YYYYMMDD-HHMMSS \
#         /opt/greentokey/data/coai/config/config.yaml
# docker compose restart coai
```

**Schema migration 不可逆**(ALTER TABLE ADD COLUMN 不会丢数据,字段保留)— 这是 idempotent 设计的好处:旧代码不读新字段,新字段闲置无害。**不需要回滚 schema**。

如果 R2 backup 也要 restore:
```bash
ssh greentokey
sudo /opt/greentokey/bin/restore-mysql-from-r2.sh <date>
# (per PKG-M5 SOP, runbook 在 docs/ops-sop.md §6)
```

---

## 7. Post-deploy 监控 24-72h

per CLAUDE.md MONITOR 阶段:

```bash
# 后台 24h canary
/canary
# (gstack skill, founder 触发,自动每 N 分钟看 prod 日志 + 关键 metric)

# 手动看新表的 row 数(确认 gtk_app_usage_log 没爆增/异常)
docker exec greentokey-mysql mysql -uroot -p"$(grep MYSQL_ROOT_PASSWORD /opt/greentokey/.env | cut -d= -f2)" \
  -e "SELECT COUNT(*), AVG(cache_read_tokens), AVG(cache_write_tokens) FROM chatnio.gtk_app_usage_log;"
# 注意当前 0 写入路径,这里值都是 0,正常 — 等 L3 service marketplace 接通后自然累积
```

---

## 8. 改 README / CHANGELOG

deploy 稳定 24-72h 后:

```bash
# /gsd-docs-update — gstack skill auto-updates README + CLAUDE.md
# Founder 触发即可
```

加一条 entry 到 CLAUDE.md "## 历史" 段:
```
- 2026-05-XX:**v1.0.0-cache-billing 部署到 prod** — Path 1 (token cache + 4 类计费 + 不赔钱) 全链路上线。L1 token 分销网关增强,客户用 cache_control marker 真实享受上游折扣。Refs: feat/v0.16-admin-concierge-orders @ 4e8879b。
```

---

## 9. Time-table 总览

| 步骤 | 时间 | Founder 还是 tana |
|---|---|---|
| Gate 1 PASS 通知 | (取决 EOW2 = 2026-05-14) | founder |
| §1 Pre-flight | 5 min | tana(读 docker logs) |
| §2 Build + ship | 15 min | tana(local build)+ founder(VPS sudo) |
| §3 Migration smoke | 5 min | tana |
| §4 Charge config | 10 min | tana(草拟 yaml diff)+ founder(VPS sudo nano) |
| §5 Smoke test | 5 min(无 Anthropic channel)/ 30 min(配 channel + run) | tana |
| §7 Monitor canary | 24h 后台 | founder(/canary) |
| §8 Docs update | 5 min | founder(/gsd-docs-update) |

**总:同步 40 min + 24h 后台监控**。

---

## 10. 不做的事

- ❌ 不在 Gate 1 红灯时部署(Pivot v4 雪藏代码留 origin)
- ❌ 不绕过 §1 pre-flight 直接 build(产线 panic 调试比省 5 分钟贵 10 倍)
- ❌ 不在没 Anthropic channel 的情况下跑 §5(cache 命中是空 NewAPI 调用)
- ❌ 不 force push origin(本 branch 已 push,后续只 fast-forward)

---

## 11. 跨引用

- `docs/research/token-cache-A-provider-api-survey.md` — 上游 ground truth
- `docs/research/token-cache-AUDIT-and-billing-design.md` — billing 数学
- `docs/ops/charge-config-cache-fields.md` — 运营 §4 详细 ops
- `adapter/claude/cache_e2e_test.go` — 离线 mock 验证(已通)
- `docs/distribution/01-assignment-dm-templates.md` — Gate 1 决策门
- DEV-PLAN.md L5 invariant — Gate 1 阻塞条件
- `docs/p1-deploy-runbook.md` + `docs/ops-sop.md` — 既有 prod deploy SOP(本 doc 是其特化版)
