# HANDOFF — 2026-05-12 session restart

> **写于** 2026-05-12 SGT 00:05 by tana,founder 即将重启 Claude Code,用 `/gsd-next` 接力。
> 这份 doc 让新会话在 5 分钟内 100% 续接当前进度。
> **不替代** parent `/Users/brendanxu/tanaxu/greentokey/HANDOFF.md`(onboarding 长文档),只补 36 小时增量。

---

## 0. 30 秒读懂

- **生产 prod 已部署 token 套餐整条链路** — `greentokey-coai:v1.0.0-cache-billing-ux`(2026-05-10 22:51 SGT 上线,健康)
- **Founder 新 lock priority(2026-05-10 23:55 SGT)**:**token + 结算 是主任务,民宿 SaaS(L3) 可能重大技改升级 — L3 业务侧用占位即可,不深度开发**
- **Gate 1**(民宿主对话验证)截止 2026-05-14 — founder 仍未完成,但 priority memory 说**不阻塞 token 工作**
- **下一步焦点**:`/gsd-next` 应推荐 customer-facing UX(B1-B4)或 founder-blocking 任务(A2 channel / A5 LS variant)

---

## 1. 当前 prod 状态(2026-05-12 00:05 SGT)

```
api.greentokey.com (159.223.39.24 HK VPS)
  ├─ greentokey-coai:v1.0.0-cache-billing-ux  ← 新部署(2026-05-10 22:51)
  ├─ greentokey-mysql:8.0   ← chatnio + newapi DB
  ├─ greentokey-redis:7-alpine
  ├─ greentokey-caddy:2-alpine
  └─ greentokey-newapi (calciumion/new-api pin)

Boot logs confirm:
  ✅ newapi.DrainPendingProvisionsForever: starting; interval=1m0s  (v0.21)
  ✅ billing: no plans to expire                                    (v0.16 W④)
  ✅ /api/gtk/v1/pool endpoint 200 OK

NewAPI channels (prod):
  - id=1 type=43 DeepSeek deepseek-prod (status=1)
  - ⏸️ 没有 Anthropic / OpenAI / GPT-4o channel (founder 待加)

gtk_plan seeded 4 tiers:
  | id | code            | price ¥  | duration | quota_grant |
  |  1 | trial           |    0     | 7 天     |   50,000    |
  |  2 | starter-100k    |   99     | 30 天    | 1,000,000   |
  |  3 | pro-500k        |  499     | 30 天    | 5,500,000   |
  |  4 | enterprise-2m   | 2,499    | 30 天    | 30,000,000  |

gtk_provider_pricing seeded 16 行 (Sonnet 4.5 / Haiku 3.5 各 5 类 + DeepSeek V3 / GPT-4o 各 3 类)
gtk_billing_config: markup_multiplier=1.300

charge: yaml 已应用 cache 字段 (4 个 model groups id 100-103)
```

---

## 2. 本次 36 小时做了什么(2026-05-10 → 2026-05-12)

### 阶段 1:Path 1 token cache 全链路(5 commits)
- W1 schema V2 + provider pricing seed (`858b2ad`)
- W2 adapter parses upstream usage incl. cache fields (`f8fe3a9`)
- W3+W4 Buffer upstream-aware + Charge 4-class cache rates (`5d2074a`)
- W5(codex)MessageContent dual-shape + cache_control forward + Claude + OpenAI adapter (`0e72388` `59c5b26` `bb9655b` `6cf8281`)
- E2E mock-upstream test (`4102cf4`)

### 阶段 2:PKG-M1 payment runtime(3 codex worker 并行)
- W①+② Recharge — `auth.RedeemPlanForOrder` + LS / 虎皮椒 dispatch (`0339b96` `bb23737` `161ed70`)
- W③ Usage log — `usage.WriteUsageLog` 写入 gtk_app_usage_log (`382cfb3` `d661d6e`)
- W④ Monthly cron — `billing.StartCron` daily 03:00 SGT expire plans (`3cb5b3d` `d9913b6`)
- 各 worker 自己 commit,tana 整合 + push

### 阶段 3:v0.21 UX sprint merge(gstack 3 agent 并行 resolve)
- 12 文件 conflict(plans/* schema 双方都改 / payment / service / main / frontend / i18n)
- 3 group 并行 resolve(~10 min wall vs 1-2h serial)
- Cross-group bridge fix: `service/runtime.go` + `service/order_view.go`(latent type collision)
- ChatFloating dangling refs cleanup
- Merge commit `773bc80`

### 阶段 4:Deploy + 上线
- Mac vite build → rsync VPS → Docker.split build → tag `v1.0.0-cache-billing-ux`
- DB snapshot `/tmp/chatnio-pre-v1-20260510-1417.sql.gz`
- Schema migration smoke PASS(V2 + PKG-1 字段全部 present)
- Charge config yaml append 4 entries + restart
- gtk_plan seed 4 tiers

**13 commits + 1 merge,push origin/feat/v0.16-admin-concierge-orders @ `773bc80`**

---

## 3. Founder priority memory(⚠️ /gsd-next 必读)

文件:`~/.claude/projects/-Users-brendanxu-tanaxu-greentokey/memory/founder-priority-token-billing-not-saas.md`

**Rule(strengthened 2026-05-10 23:55)**:
> "token套餐整条链路才是重要的任务,民宿这个服务板块可能还要进行一次重大技改升级,因此现在都不重要,都在那时占位即可,不要深度开发"

**这意味着**:
- ✅ **优先做** L1 token 网关 + L2 结算运行时 + customer dashboard + admin UI + 安全 + cache 卖点可视化
- ❌ **不做** L3 民宿 SaaS 深度功能(内容生成 / ROI / 评论 / 私信 / OpenClaw 集成 / `service/runtime.go` 真实 agent loops)— 占位即可,可能要重做
- ⚠️ 任何 founder 让做 L3 work,先 surface freeze rule + 问"L3 重构方向已经定了吗?"

---

## 4. 立即可做的 next steps(按优先级)

### 🔴 P0 — 阻塞客户真用产品的最后 2 步(founder dashboard 工作)

**A2: NewAPI 后台加 Anthropic / OpenAI channel**(等 founder API key)
- Login https://api.greentokey.com:3000(密码 `<see memory/credentials.md>`)
- 新建 channel:type=14 Anthropic / type=1 OpenAI / 填 sk-ant-... 或 sk-proj-...
- 测试 ping
- 加完 channel,token 套餐才能真正调用 LLM

**A5: LemonSqueezy 后台创建 4 个 variant**(等 founder dashboard)
- 对应 gtk_plan 4 个 code:trial / starter-100k / pro-500k / enterprise-2m
- custom_data 加 `{"type":"plan","plan_code":"starter-100k"}` 之类
- 这样 LS webhook → RedeemPlanForOrder 端到端通

### 🟠 P1 — Customer self-serve UX(tana 可推动 codex 写,推荐并行 3 worker)

| 块 | 工时 | codex spec 需要写 |
|---|---|---|
| B1 Customer signup → checkout UX flow | 4-6h | ❌ |
| B2 Customer dashboard(quota / usage / cache savings) | 8-10h | ❌ |
| B3 API key 自助(generate / revoke / rotate sk-tnx-...) | 4-6h | ❌ |
| B4 月度 statement email | 3-4h | ❌ |

**B2 是最大块**,需要先 design discussion。B1 + B3 可并行 codex(类似 PKG-M1 模式)。

### 🟡 P2 — 运营效率 + 安全(production-grade)

- C1 Admin UI cache 字段表单(避免 yaml 手编)
- C2 Channel 健康度 + cache 命中率 dashboard
- C3 PKG-D4-b alerting(quota / charge / channel 异常)
- D1 PKG-M6 token at-rest encryption
- D2 PKG-D5 customer deletion script(首付费客户前必做)

### ❄️ 冻结(per founder priority)

- L3-1..6 民宿 SaaS 业务功能(内容生成 / 评论 / 私信 / ROI / OpenClaw 集成 / `service/runtime.go` agent)
- Stream C / PKG-OPENCLAW-1..4

---

## 5. 已知风险 + 限制

| 项 | 状态 | 备注 |
|---|---|---|
| `TestSeedCatalog_PreservesOperatorEdits` 失败 | 🟡 pre-existing | 不是本会话引入,inherits from base。`service/seed.go` xhs-copy-writer v1→v2 升级 clobber operator edits。pre-deploy 已 verify 不阻塞。 |
| i18n cn.json / en.json 有冗余 keys | 🟡 deferred | merge 时 union 双方,可能有未用的 v0.16 marketing keys。等需求时清理。 |
| Anthropic-native `/v1/messages` route | 🟡 deferred | W5 只覆盖 OpenAI-compatible `/v1/chat/completions`。客户走 Anthropic native 不生效。 |
| gtk_app_usage_log 0 真实写入路径 | ⚪ 正常 | `WriteUsageLog` 接通了,但 prod 没真实 chat call 调用过(没 channel)→ 表是空的。接 channel 后第 1 个 chat call 自动写入。 |
| Charge config 单价(0.0281 等) | ⚪ 默认 | markup 1.30,Founder 可改 markup 重算所有(per docs/ops/charge-config-cache-fields.md §5)。 |

---

## 6. 关键文件索引(从最重要排)

| 文件 | 用途 |
|---|---|
| `docs/STATUS.md`(本 worktree) | Path 1 阶段性状态,founder-facing |
| `docs/ops/W5-deploy-plan.md` | 部署 playbook(已执行) |
| `docs/ops/charge-config-cache-fields.md` | 运营 charge config 修改指南 |
| `docs/codex-dispatch/PKG-M1-*-DONE.md` | 3 个 codex worker 完工报告 |
| `docs/codex-dispatch/W5-DONE.md` | W5 codex 完工报告 |
| `docs/research/token-cache-A-provider-api-survey.md` | 上游 ground truth(provider pricing) |
| `docs/research/token-cache-AUDIT-and-billing-design.md` | "不亏" 数学证明 + 4 类计费设计 |
| `~/.claude/projects/-Users-brendanxu-tanaxu-greentokey/memory/founder-priority-token-billing-not-saas.md` | **founder priority 锁定边界** |
| `~/.claude/projects/-Users-brendanxu-tanaxu-greentokey/memory/credentials.md` | NewAPI / DB / R2 / Alipay 凭据 |
| `../DEV-PLAN.md`(parent worktree) | PKG ledger,整个 backlog |
| `../HANDOFF.md`(parent worktree) | onboarding 长文档(15 min 读完) |
| `../CLAUDE.md` | 项目历史 + 雪藏清单 |

---

## 7. /gsd-next 建议路径

新会话起来后,这是建议的 routing:

```
1. 读本 HANDOFF.md(5 min)
2. 读 founder-priority-token-billing-not-saas.md 锁定边界
3. 查 git log --oneline -20 看最近 commits 状态
4. 问 founder:"上次会话(36 小时)deploy 完成 + token 套餐整条链路就位。
                priority C 是 token + 结算 主路径。
                下一步:A2 channel 配置(等你)/ A5 LS variant(等你)
                       / B1-B4 customer UX(我可以推动 codex 并行) — 选哪个?"
5. 根据 founder 答案 routing:
   - A2 / A5 → 帮 founder draft 操作 step-by-step(他自己跑 dashboard)
   - B1 / B3 → 写 codex worker spec(类似 PKG-M1 模式)dispatch
   - B2 → /gsd-discuss-phase 或 /gsd-plan-phase 先 design
   - 其他 → /gsd-progress + /gsd-help 看 backlog
```

---

## 8. 重要的 git 状态

**当前 branch**:`feat/v0.16-admin-concierge-orders`(已 push origin)
**HEAD**:`773bc80 merge: feat/v0.21-cleanup-dead-routes into v0.16 — token + 结算 + UX sprint cohesion`
**Backup tags**:`backup-pre-v0.21-merge-{date}-{time}`(rollback 锚点)

**Dirty files**(本会话前已存在的 working tree 改动,**不要 commit**):
- `.gitignore` 1 行 newline 加上(无意义)
- `app/src/components/{Hero, Message, MessageTokenMeter}.tsx`(早期 v0.6 工作残留)
- `app/src/resources/i18n/{ja, ru, tw}.json`(早期 i18n 工作)
- `app/src/routes/Contact.tsx` / `Docs.tsx` / `Privacy.tsx`(早期工作)
- `app/src/components/token-display/*`(token-display sandbox 残留)

这些**不属于本会话 token + 结算工作**,founder 决定是否 clean(可能这是别的分支的 partial 工作)。

---

## 9. 12 commits 在 origin/feat/v0.16-admin-concierge-orders

```
773bc80 merge: feat/v0.21-cleanup-dead-routes into v0.16 — token + 结算 + UX sprint
0c857f1 docs: PKG-M1-1+2-DONE.md report
161ed70 feat(service): hupijiao callback L2 plan dispatch
bb23737 feat(payment): LS webhook L2 plan dispatch
0339b96 feat(auth): RedeemPlanForOrder — provider-agnostic L2 plan redeem
e1d76a4 docs: PKG-M1-3-DONE.md report
d661d6e feat(manager/chat): wire usage.WriteUsageLog into CollectQuota
382cfb3 feat(usage): WriteUsageLog — gtk_app_usage_log writer with 4-class breakdown
af9d898 docs: PKG-M1-4-DONE.md report
d9913b6 feat(main): wire billing.StartCron into boot
3cb5b3d feat(billing): cron — daily ExpirePlans job at 03:00 SGT
... (W5 / W3+4 / W2 / W1 + ops docs earlier)
```

---

## 10. 一段话给新 tana 起的话

```
你接手 greentokey 项目。上 36 小时,我做完了 token 套餐整条链路 + UX
sprint merge + deploy。代码在 origin/feat/v0.16-admin-concierge-orders
@ 773bc80,prod 跑 v1.0.0-cache-billing-ux 镜像,健康。

founder 上次 lock priority(2026-05-10 23:55):token + 结算 是主线,
民宿 SaaS 可能重大技改升级 — 占位即可不深度开发。

立即可做的事(按 priority):
- A2 NewAPI Anthropic channel(等 founder 给 API key)
- A5 LemonSqueezy variant 4 个(等 founder dashboard)
- B1-B4 customer self-serve UX(可推 codex 并行)

读这份 HANDOFF + memory/founder-priority-token-billing-not-saas.md
就能 100% 接手。剩下问 founder 选 path。
```

— tana,2026-05-12 SGT 00:05
