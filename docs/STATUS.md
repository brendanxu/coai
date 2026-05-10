# greentokey 阶段性状态(token cache + 不赔钱计费)

**Date**: 2026-05-10
**Author**: tana(写给 founder + 团队 + 未来的自己)
**Sub-project**: Path 1 — L1 token 分销网关 cache 透传 + 4 类不赔钱计费

> 这个 doc 是 Path 1 的"现在到哪 / 还能做什么 / 客户怎么用"的入口。整个 greentokey 项目的更宏观 backlog 在 [`../../DEV-PLAN.md`](../../DEV-PLAN.md)。

---

## 一、一句话总结

**代码全做完了,生产环境还没部署,客户暂时还不能用。下一个动作是等 Pivot v4 Gate 1(EOW2=2026-05-14)的民宿主对话出绿灯,然后 40 分钟部署上线。**

---

## 二、Path 1 是什么

把 greentokey 的 L1 token 分销网关从「按 tiktoken 估算计费、不懂 cache、可能赔钱」升级成:

1. **客户能享受 cache 省钱** — 客户用 Anthropic / OpenAI / DeepSeek 时,prompt cache 命中按 cache 单价(便宜 90%)收钱
2. **我们永远不赔钱** — 所有计费按上游真实成本 × 1.30 倍(数学证明 markup ≥ 1.0 = 任何场景都赚 30%)
3. **数据透明** — 每次调用记 4 类 token(input / output / cache_write / cache_read)+ 实际成本 + 实际收费,给客户看"省了多少"做营销卖点

---

## 三、阶段进度

```
[研究] ─────────────────────────── ✅ DONE
[Audit 4 红旗] ─────────────────── ✅ DONE
[W1-W5 工程实施] ────────────────── ✅ DONE  (52+ tests PASS)
[Ops 准备] ────────────────────── ✅ DONE  (charge-config + deploy-plan docs)
[Push GitHub 备份] ────────────── ✅ DONE  (origin @ 752dc07)
[Gate 1 民宿主对话] ───────────── ⏸️ 进行中 (EOW2 = 2026-05-14)
[部署到 prod] ──────────────────── ⏸️ 等绿灯
[实际客户用上] ──────────────────── ⏸️ 等 NewAPI 配 Anthropic channel
```

---

## 四、当前 prod 实际状态

**生产**:`api.greentokey.com`,Docker image `greentokey-coai:v0.9.0-pivot-民宿-marketing`(2026-05-01 部署)

**prod 上有什么**:
- ✅ 民宿 marketing landing page
- ✅ 套餐 + 支付层(LemonSqueezy + Alipay)
- ✅ NewAPI 网关(channel id=1 = SunoAPI,跟 cache 无关)
- ✅ greentokey 三层架构基础设施

**prod 上没什么**:
- ❌ W1-W5 cache 字段(本 sub-project 的代码,还没部署)
- ❌ Anthropic / DeepSeek / OpenAI channel(NewAPI 后台只有 SunoAPI)

**所以**:即使客户注册付费拿 sk-tnx-... API key,**也调用不了任何 LLM**。token 分销业务本质上还没开张。

---

## 五、客户能用吗?分情况答

### A. 今天(2026-05-10)

❌ 不能用 cache 折扣(也调用不了 LLM,因为没 channel)。

### B. Gate 1 绿灯 + 部署 W5 image 后

⚠️ 仍然不能,因为还需要 founder 在 NewAPI 后台手动加 Anthropic channel。

### C. 你加 Anthropic channel + 应用 charge config 后(目标态)

✅ 完全可以。客户做 3 步:

```python
import openai
client = openai.OpenAI(
    base_url="https://api.greentokey.com/v1",
    api_key="sk-tnx-xxxxxxxxx"
)

response = client.chat.completions.create(
    model="claude-sonnet-4-5",
    messages=[
        {"role": "system", "content": [{
            "type": "text",
            "text": "<≥1024 token system prompt>",
            "cache_control": {"type": "ephemeral", "ttl": "5m"}
        }]},
        {"role": "user", "content": "your question"}
    ],
)
```

5 分钟内同 system prompt 再调用 → cache hit → 我们按 0.1× input 收(便宜 90%)→ 客户感觉省钱。

---

## 六、Founder 现在能做的事(2026-05-10 ~ EOW2)

### 1. (最重要)Gate 1 民宿主沟通

- 文件:[`../distribution/01-assignment-dm-templates.md`](../distribution/01-assignment-dm-templates.md)
- 目标:8-10 个大理环洱海民宿主线下/微信沟通,30 分钟一对一
- 截止:**2026-05-14 EOD**(还有 4 天)
- 决策门:≥4 银+ 绿;2-3 银+ 黄;0-1 银+ 红
- 完成后给 tana 一句"Gate 1 结果:X 银 Y 金 Z 铜",我帮判绿黄红

### 2. (技术准备,可选)在 NewAPI 后台先加 channel

登录 https://api.greentokey.com:3000 admin,加 Anthropic channel(type=14,key=sk-ant-...)。这步不影响代码部署,只影响 smoke test 能不能跑。

### 3. (今天能做)review 部署计划

[`./ops/W5-deploy-plan.md`](./ops/W5-deploy-plan.md) 348 行,11 sections。重点 §1.1 Gate 1 判定 + §4 charge config + §5 smoke test + §6 rollback。

---

## 七、Gate 1 之后的部署流程(40 分钟同步 + 24h 监控)

per [`./ops/W5-deploy-plan.md`](./ops/W5-deploy-plan.md):

| 步骤 | 时间 | 谁做 |
|---|---|---|
| §1 Pre-flight + DB snapshot | 5 min | tana(读 docker logs)|
| §2 Mac vite build + rsync VPS + Docker.split | 15 min | tana + founder sudo |
| §3 Migration smoke(verify schema V2 + 16 row pricing seed) | 5 min | tana |
| §4 Charge config yaml 编辑 + restart | 10 min | tana(草拟)+ founder sudo nano |
| §5 端到端 smoke(2 curl 看 cache 命中) | 5 min | tana(需要 Anthropic channel)|
| §7 24h /canary 监控 | 后台 | founder(/canary skill)|
| §8 docs update + CLAUDE.md 历史 | 5 min | founder(/gsd-docs-update)|

---

## 八、本地 dev 跑测试(任何时候都能做)

```bash
cd /Users/brendanxu/tanaxu/greentokey/coai-v0.7-design

# 全套测试(52+ cases)
go test ./globals ./manager ./adapter/claude ./adapter/openai ./adapter/deepseek ./utils ./plans \
  -count=1 -vet=off

# 单独看 e2e mock-upstream 测试详细输出
go test ./adapter/claude/... -count=1 -vet=off -run "E2E" -v
```

期望:全部 ok,7 packages 全 PASS。

---

## 九、风险 + 不确定性

### 已 mitigated

- ✅ **不会赔钱** — 数学证明在 `utils/upstream_billing_test.go::TestCountUpstreamQuota_NeverLoseMoney`,5 子场景全 PASS
- ✅ **fork 上游 risk** — globals.Message.Content 类型改动 touches 30+ 文件,codex 帮忙完成所有 .String() 适配
- ✅ **客户 OpenAI 兼容性** — plain string content 路径保留,旧 SDK 不需改代码
- ✅ **schema migration idempotent** — 连跑 2 遍不出错(`TestUpgradeAppUsageLogV2_PreExistingV1`)

### 仍存在

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| Gate 1 红灯 → Pivot v4 重做 | 中 | 5 天 build 沉没 | 这套 Path 1 代码留 origin 不删,L1 网关增强对任何未来 wedge 都有用 |
| 部署后没真实 Anthropic 客户用 cache | 高 | cache 功能空载 | 不影响其他功能,charge fallback 到 input 单价不亏 |
| Anthropic-native `/v1/messages` 客户跳过 OpenAI-compatible 路径 | 低 | 这部分客户 cache 不生效 | 后续 worker 的事 |
| 前端 admin UI 没加 cache 字段表单 | 中 | 运营只能 yaml/curl 改 | 等需求再加 |
| 上游 model 厂商改价 | 低 | 价格表过期 | `gtk_provider_pricing` 表 append-only,运营加新 row 不影响历史账单 |

---

## 十、配套文档定位

| 想看什么 | 看哪个 |
|---|---|
| 整体阶段性状态(本文件)| 这个 |
| 部署详细步骤 | [`./ops/W5-deploy-plan.md`](./ops/W5-deploy-plan.md) |
| 运营改 charge config | [`./ops/charge-config-cache-fields.md`](./ops/charge-config-cache-fields.md) |
| 数学证明 | `utils/upstream_billing_test.go` |
| Gate 1 怎么判 | [`../distribution/01-assignment-dm-templates.md`](../distribution/01-assignment-dm-templates.md) |
| 为什么 Pivot v4 = 民宿 SaaS | [`../strategy/2026-04-30-pivot-v4-民宿-saas.md`](../strategy/2026-04-30-pivot-v4-民宿-saas.md) |
| 整个项目 backlog | [`../../DEV-PLAN.md`](../../DEV-PLAN.md) |
| 历史背景(BYOK 旧线雪藏) | [`../../CLAUDE.md`](../../CLAUDE.md) "## 历史" 段 |

---

## 十一、Founder 常见 Q&A

**Q1: 我现在能给客户演示吗?**
- ✅ 可以演示 https://api.greentokey.com 主页(民宿 marketing)
- ✅ 可以演示 https://www.greentokey.com 套餐 + 支付页面
- ❌ 不能演示「这个 API 调用支持 cache 省钱」(还没部署 + 没 channel)
- 客户问什么时候能用:Gate 1 绿灯后 48 小时内(2 天部署 + 24h canary)。

**Q2: 我自己想用我们的 API 测试**
- 部署 + 加 Anthropic channel + 拿 sk-tnx-test token,然后 curl(详见本文 §五.C)。

---

## 十二、Path 1 commits 清单(在 origin/feat/v0.16-admin-concierge-orders)

```
752dc07 docs(ops):           W5 deploy plan
4e8879b docs(ops):           charge-config cache fields
4102cf4 test(adapter/claude): e2e mock-upstream Path 1 cache flow
6cf8281 feat(utils/buffer):   W5-4 PreferredCacheTTL bridge
bb9655b feat(adapter/openai): W5-3 forward cache_control on content blocks
59c5b26 feat(adapter/claude): W5-2 forward cache_control system + content
0e72388 feat(globals):        W5-1 MessageContent dual-shape + cache_control
a014b32 docs(billing):        W5 codex prompt
69a2425 docs(billing):        W5 codex spec
5d2074a feat(billing):        W3+W4 Buffer upstream-aware + Charge 4-class
f8fe3a9 feat(billing):        W2 adapter parses upstream usage
858b2ad feat(billing):        W1 schema + provider pricing seed
```
