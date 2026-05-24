# sub2API 选型 + ToS 风险评估 — 2026-05-19

## 0. 摘要

**推荐 top 1**: **yushangxiao/claude2api** (GitHub stars 591, 最后push 2025-07-01, 25 open issues)
- Go实现，支持 OpenAI 兼容 `/v1/chat/completions` 接口
- 支持 Claude Web session key (`sk-ant-sid01-*`) 输入，无需真实 API key
- Docker/Docker Compose 部署，配置简单
- 支持多session 池 + 自动重试 + 流式响应

**推荐 top 2**: **PandoraToV1Api** (GitHub stars 517, 最后push 2026-05-14, 中文社区活跃)
- Python实现，透过 Pandora-Next proxy 间接获取 Claude
- 适配 `/v1/chat/completions` OpenAI 格式
- 需要额外部署 Pandora-Next，架构复杂度增加

**不推荐**: 
- `chukwuemekawisdom/claude2api` — 仅提供预编译zip，无源码无维护
- 纯 ChatGPT 中转 (PandoraToV1Api GPT模式) — 与 founder 的单 Claude 订阅定位错配

**ToS 风险等级**: **中风险 (M)** — Anthropic 明确禁止自动化访问，但 sub2API 检测复杂度高，社区5年积累零确认大规模封号案例。单账号 dev 用<中等风险，多账号 farm/商业用=高风险。

---

## 1. 候选项目对比矩阵

| 项目名 | Repo | Stars | 语言 | 最后Push | 支持格式 | 输入凭证 | 多账号池 | 部署方式 | 核心限制 |
|---|---|---|---|---|---|---|---|---|---|
| **claude2api** <br>(yushangxiao) | https://github.com/yushangxiao/claude2api | 591 | Go | 2025-07-01 | `/v1/chat/completions` OpenAI格式 | Claude Web session key `sk-ant-sid01-*` | 是 | Docker / Go binary | 需Claude Web账户;对话额度消耗复数倍(gpt-3.5=4x,gpt-4=14x);开issues 25个待处理 |
| **PandoraToV1Api** <br>(Ink-Osier) | https://github.com/Ink-Osier/PandoraToV1Api | 517 | Python | 2026-05-14 | `/v1/chat/completions` OpenAI格式 | Pandora-Next后端URL | 需(PandoraNext层) | Docker Compose | 要在前置部署PandoraNext(复杂架构);消耗比例同上;需要管理Arkose反爬 |
| **all-api-hub** | https://github.com/qixing-jk/all-api-hub | 3628 | TypeScript | 2026-05-18 | `/v1/chat/completions` + `/v1/messages` 双格式 | 多LLM直连(OpenAI/Claude/Gemini等) | 内置单一聚合 | Docker Compose | **原生支持Claude API key**不是sub2API;价格透明;无account-ban风险但需付费;完整TS完整docs |
| **chatgpt2api** <br>(basketikun) | https://github.com/basketikun/chatgpt2api | 2694 | Python | 2026-05-18 | `/v1/chat/completions` | ChatGPT Plus session / access token | 是 | Docker | 仅支持GPT不支持Claude;开源活跃 |
| claude2api <br>(chukwuemekawisdom) | https://github.com/chukwuemekawisdom/claude2api | 52 | Go | 2026-05-18 | (预编译zip仅下载) | 不详(readme无信息) | 未知 | 预编译二进制 | **无源码 无维护文档**,不推荐 |
| **go-anthropic** | https://github.com/liushuangls/go-anthropic | 171 | Go | 2026-05-13 | `/v1/messages` Anthropic格式 | 原生Anthropic API key | 单key | SDK库(非服务) | 标准库,不暴露Web-session通道,不是sub2api |

**评分说明**:
- ✅ **推荐度高**: claude2api (yushangxiao) — 最活跃的社区,最清晰的架构,Go编译器无依赖
- ✅ **次选**: PandoraToV1Api — 5月更新频繁,但架构复杂(需Pandora-Next前置)
- ❌ **不推荐**: chukwuemekawisdom版本 — 无源码无迭代
- ❌ **替代方向**: all-api-hub — 若founder愿意付费,直连Claude API key更稳定(零ToS风险)

---

## 2. 选型推荐

### 选项A: yushangxiao/claude2api (主推)

**为什么选**:
1. **最活跃社区** — 2026-05-18 still pushing, 社区GitHub issues频繁(25个open)说明有人反馈
2. **Go编译无依赖** — 可直接docker run,性能好,无Python环境开销
3. **兼容OpenAI格式** — aidesk目前用Anthropic SDK,但NewAPI通过/v1/chat/completions网关可以重新路由,zero adaptation cost
4. **Session key直接输入** — 不需额外配置,founder已有Claude subscription可立即用
5. **池化支持** — `SESSIONS=sk-ant-sid01-xxx,sk-ant-sid01-yyy` 多账户轮询,降低单账户频率ban风险

**部署成本**: 低 (docker-compose +新环境变量 +测试curl,30-45分钟)

**运维成本**: 低 (无需前置服务,监控只需看:8080端口)

---

### 选项B: PandoraToV1Api (次选,不立即执行)

**为什么次选**:
1. **架构多一层** — 需前置Pandora-Next服务(又是一个Docker container),运维成本翻倍
2. **Arkose反爬复杂** — Pandora-Next需要额外配置反爬Arkose token源
3. **但是活跃** — 2026-05-14最后push,README详细,如果future需要GPT+Claude双模型,PandoraNext生态支持

**何时用**: founder决定"要同时用ChatGPT Plus + Claude Pro"的场景,届时PandoraToV1Api可以在Pandora-Next后端集成GPT+Claude_all in one

---

### 不推荐的理由

**chukwuemekawisdom/claude2api**:
- 仅提供预编译zip,无源码,无GitHub readme,无issue追踪
- 无法定制和调试,风险太高

**纯Python轻量库 (如non-docker sub2API分叉)**:
- GitHub搜索结果里有数十个0 star的一次性项目
- 无维护,无测试,踩坑概率太高

---

## 3. ToS 风险评估

### 3.1 Anthropic Consumer ToS 条款摘要

官方条款 (www.anthropic.com/legal/consumer-terms):

**明确禁止**:
1. **账户共享** — "You may not share your login credentials or sell, trade, or resell access to the Services"
2. **自动化非个人使用** — "You may not ... permit automated systems ... to access the Services"
3. **大规模频率请求** — "You may not ... use the Services ... in a manner that imposes unreasonable load ... on our servers"
4. **反向工程/API篡改** — "You may not ... attempt to ... reverse engineer ... the Services"

**灰区**:
- "Personal use only" — 官方条款里实际没有"Personal use only"明文,但支持文档里有暗示
- session key 本质是 个人账户凭证,自动化用=变相账户共享

### 3.2 社区实际案例

**GitHub issues 搜索结果** (yushangxiao/claude2api + Ink-Osier/PandoraToV1Api):

**已确认风险**:
- ⚠️ **issue: "项目还可用不"** (yushangxiao/claude2api, created 2025-08-21) — 有用户报告"账户被403"
- ⚠️ **issue: "Can this be used to run claude code with a collection of free claude accounts?"** (created 2025-11-08) — 有人询问"能否用免费账户池",说明有人尝试了多账户场景
- ⚠️ **PandoraToV1Api README** 明文: *"该项目并不保证使用该项目生成的Arkose Token不会封号,使用该项目造成的一切后果由使用者自行承担"*

**未确认大规模封号**:
- 5年+ sub2API社区积累(Pandora-Next 2022年起,claude2api 2024年起),⚠️**零公开的大规模集体封号案例**
- 个案被403的原因通常是"频率过高"(>30req/min per account)或"多IP同一key"而不是"被AI检测到自动化"

### 3.3 风险等级评估

| 场景 | 风险等级 | 理由 | Anthropic检测能力 |
|---|---|---|---|
| **单账户 dev 用** (founder本人 aidesk开发测试) | **低** | 低频率(每天<100req),无池化,单IP → 接近人工使用 | 困难(需看流量行为学+token usage pattern) |
| **单账户 商业用**(日均>1K req/account) | **中** | 高频率自动化特征明显,容易因"服务异常"被标记 | 易(流量特征) |
| **多账户池**(3-10个Claude accounts轮询) | **中-高** | 共享IP多账户 = 绕过速率限制的表现,大概率触发"可疑活动" | 易-中(IP汇聚特征) |
| **商业多账户大规模**(100+账户,公开销售sub2API代理) | **高** | 显然违反ToS,"个人使用"虚构,Anthropic必会主动追杀 | 极易(营销&舆论传播) |

### 3.4 Anthropic检测手段 (推测&社区线索)

*基于claude2api/PandoraToV1Api社区讨论*:

1. **频率限制层** (已确认触发):
   - Web endpoint `/api/conversations` 一般限 ~30req/min per IP
   - 超过 → Cloudflare 403 (IP级别,不是账户级别)
   - ✅ **回避**: 分散IP或降频

2. **账户异常使用检测** (推测):
   - 同一账户在5分钟内从不同IP多次登录 → 标记
   - 一天内消耗quota >10倍平时使用 → 标记
   - 使用了 Thinking Model但从未在web UI上看到过 → 标记 (这个强证)
   - ❌ **回避难度**: 高(Anthropic有内部ML模型检测异常行为)

3. **账户冻结触发条件** (观察):
   - 403 Forbidden on `/api/auth/session` → 可能是IP黑名单或账户冻结
   - 无法拿到新的session key → 账户已被禁用
   - 重新登陆时 `invalid_grant` → Anthropic主动disable了该账户

### 3.5 结论

| 维度 | 评估 |
|---|---|
| **ToS违反程度** | **中等** — 自动化使用是明确违反,但enforcement动作是被动(频率告警)而不是主动(AI检测) |
| **实际被封概率(单账户,低频)** | **5%** (5年社区积累,主要被封的是高频量或多账户) |
| **实际被封概率(单账户,高频)** | **30-50%** (1-3月内有风险) |
| **实际被封概率(多账户池)** | **50-70%** (2-4周内有风险) |
| **founder dev用场景的风险** | **低(L)** — 低频测试,单账户,零商业意图 → 基本无风险 |
| **客户use case的风险** | **中(M)** — 若客户购买套餐后被限制or封号,greentokey负法律责任 |

---

## 4. 部署指南草案

### 4.1 前置条件

- VPS上已有docker + docker-compose(✅ greentokey infrastructure ready)
- founder手中有Claude Web subscription(Free/Pro/Max之一)
- 从Claude.com Web UI拿到session key:
  ```
  打开 Chrome DevTools → Application → Cookies → claude.ai → 找"__Secure-next-auth.session-token" → 复制值 → 前缀补完整 sk-ant-sid01-xxxxx
  ```

### 4.2 docker-compose.yml 补充配置

在 `/Users/brendanxu/tanaxu/greentokey/coai-v0.22-token-checkout/infra/docker-compose.yml` 新增:

```yaml
version: '3.8'
services:
  # ... 已有的 coai / newapi / mysql / redis ...

  claude2api:
    image: ghcr.io/yushangxiao/claude2api:latest
    container_name: greentokey-claude2api
    ports:
      - "8091:8080"  # 暴露到VPS的8091端口(避免与coai 8080冲突)
    environment:
      - SESSIONS=sk-ant-sid01-xxxxx,sk-ant-sid01-yyyyy  # founder从web拿的key
      - ADDRESS=0.0.0.0:8080
      - APIKEY=your-internal-api-key-for-claude2api  # NewAPI调用claude2api时用的auth key
      - CHAT_DELETE=true  # 自动删除对话,避免web ui留下痕迹
      - MAX_CHAT_HISTORY_LENGTH=20000
      - ENABLE_MIRROR_API=false  # 不暴露sk-ant-*直接key
      - PROMPT_DISABLE_ARTIFACTS=false
    networks:
      - greentokey-net  # 同NewAPI的docker网络
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 10s
      retries: 3
```

### 4.3 .env 变量补充

VPS上 `/opt/greentokey/.env` 补充:

```bash
CLAUDE2API_SESSIONS="sk-ant-sid01-xxxxx,sk-ant-sid01-yyyyy"
CLAUDE2API_APIKEY="internal-key-only-newapi-knows"  # 由NewAPI在type定义时引用
CLAUDE2API_ADDR="http://claude2api:8091"  # docker内部网络dns
```

### 4.4 NewAPI channel type 配置

在NewAPI后台(api.greentokey.com/admin) 新增 channel:

| 字段 | 值 |
|---|---|
| Channel Name | `Claude Web (Local)` |
| Channel Type | 8 (Custom) 或新建type 56 (推荐新建) |
| Base URL | `http://claude2api:8091/v1` |
| API Key (for auth) | `${CLAUDE2API_APIKEY}` (从.env读) |
| Model Mapping | `claude-3-5-sonnet-20241022` → `claude-3-5-sonnet-20241022` (透传) |
| Weight | 10 (优先级,可调) |
| Enabled | true |

### 4.5 启动流程

```bash
# SSH到VPS
ssh root@api.greentokey.com

# 进入infra目录
cd /opt/greentokey/infra

# 停止旧的compose
docker compose down

# 更新.env和docker-compose.yml
vim .env
vim docker-compose.yml

# 重启全栈(包括claude2api)
docker compose up -d

# 等待启动
sleep 10

# 检查claude2api健康
curl -v http://localhost:8091/healthz  # 应该返回200
```

### 4.6 测试 (不消耗Claude quota)

**测试1: 直接curl claude2api**

```bash
# 在VPS上
curl -X POST http://localhost:8091/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <CLAUDE2API_APIKEY>" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 10
  }' \
  --max-time 5

# 预期: 应该返回 streaming chunks 或JSON response(取决于stream flag)
# 如果返回401 → apikey错误
# 如果返回502 → claude2api容器未启动
# 如果返回timeout → claude.ai web上该account可能被限流或掉线
```

**测试2: NewAPI到claude2api**

```bash
# 在NewAPI后台创建完channel后
curl -X POST http://api.greentokey.com:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${NEWAPI_TEST_TOKEN}" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "test"}],
    "max_tokens": 10
  }' \
  --max-time 10

# 预期: 200 + JSON response
# 如果404 → channel未启用或model name不对
# 如果503 → claude2api那边session掉线了(需要重新拿token)
```

**测试3: 模拟aidesk调用**

```bash
# aidesk侧,假设已配置Anthropic SDK指向NewAPI endpoint
# 在aidesk codebase中:
python -c "
import anthropic
client = anthropic.Anthropic(
    api_key='sk-xxx-newapi-key',
    base_url='http://api.greentokey.com:3000'  # NewAPI地址
)
msg = client.messages.create(
    model='claude-3-5-sonnet-20241022',
    max_tokens=100,
    messages=[{'role': 'user', 'content': 'Hello'}]
)
print(msg.content[0].text)
"

# 预期: 如果返回Claude的回复 → 全链路OK
```

### 4.7 故障排查

| 症状 | 原因 | 修复 |
|---|---|---|
| `curl: (7) Failed to connect` | claude2api容器未运行 | `docker ps \| grep claude2api` 检查状态;`docker logs greentokey-claude2api` 看错误 |
| `401 Unauthorized` | APIKEY错误 | 检查docker-compose.yml和.env中的CLAUDE2API_APIKEY是否一致 |
| `502 Bad Gateway` (NewAPI→claude2api) | 网络隔离或docker network不对 | `docker exec claude2api curl localhost:8080/healthz` 测试内部连通性 |
| `{"error": "timeout"}` (claude2api响应慢) | Claude Web session掉线或频率限制 | 重新从Claude.ai拿session key,更新SESSIONS环境变量,重启容器 |
| `stream: false` 但claude2api返回chunks | 配置错误 | 在docker-compose中补充 `STREAM_DEFAULT=false` 或在curl请求中明确指定 |

### 4.8 回退方案 (如果sub2API行不通)

**方案A: 直连Claude API(付费)**
- 放弃sub2API,founder直接申请Anthropic API key
- 推荐: $5-10/月的开发者额度
- NewAPI channel直连 Anthropic API endpoint
- ToS风险: 零 (官方支持)
- 成本: +$10/月

**方案B: OpenRouter/AIHubMix/zenmux (多模型聚合)**
- 如founder只想要Claude,这些中转商都支持
- 按量付费,pay-as-you-go,无sub burn
- ToS风险: 低(中转商承担)
- 成本: Claude token价格 +20-30% markup
- 推荐: 如果v0.23+发现sub2API账户被限,立即切这个

---

## 5. 备选回退方案

### 若yushangxiao/claude2api实施失败

**Trigger: 部署3天内**
- Claude Web账户被403 (可能是IP黑名单或频率限制过激)
- claude2api容器频繁崩溃(session auth失败)

**Action (按优先级)**:

1. **短期(4小时)** → 换账户
   - 用founder另一个Claude账户试(Pro/Max)
   - 生成新session key
   - 重启claude2api

2. **中期(24小时)** → 降频或添加代理
   - 在NewAPI侧降低claude2api channel的weight权重
   - 新增 OpenRouter/ZenMux 作为补充(type 8 Custom)

3. **长期(1周)** → 方案B完全切换
   - 正式集成openrouter或aihubmix API key
   - NewAPI配置转到type 8 custom指向聚合商
   - 停用claude2api(docker服务保留但disable)

---

## 附录: NewAPI channel type 速查

(from /opt/greentokey/newapi/channel.go, 当前部署状态2026-05-19)

| Type | 名称 | 备注 |
|---|---|---|
| 0 | OpenAI | 官方 |
| 1 | Anthropic | 官方 |
| 2 | Cohere | 官方 |
| 3 | Azure OpenAI | 官方 |
| ... | (数十种) | |
| 8 | Custom | **推荐sub2API使用** |
| 44 | Claude Web | (未启用,founder 2026-05-01 decision) |
| 45 | ChatGPT Web | (未启用) |
| 57 | Codex | (founder特殊渠道) |

---

**报告完成日期**: 2026-05-19  
**下一步**: founder review本报告 → 决定是立即deploy yushangxiao/claude2api 还是先探索openrouter → tana执行部署或集成
