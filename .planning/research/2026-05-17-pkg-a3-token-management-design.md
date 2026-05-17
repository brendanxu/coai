# PKG-A-3: 令牌管理（Personal Access Tokens）设计稿

**Date**: 2026-05-17
**Status**: 待 founder review 决策点 → 然后起 plan-phase
**Priority**: founder priority 1
**Estimate**: 12-16h（看决策点而定）

## 0. 三份研究员盘点综合

- [API surface](2026-05-17-newapi-token-api-surface.md) — NewAPI 已有 CreateToken/DisableToken 函数，缺 List + Update + Admin endpoints
- [DB schema](2026-05-17-token-db-schema.md) — L23 KEEP user↔NewAPI account 1:1 invariant；tokens 表已在 NewAPI 侧存在
- [UI references](2026-05-17-token-ui-references.md) — Dashboard.tsx + Account.tsx 有 40-50% 可复用组件；GtkAdmin 7 tabs 无 token tab

## 1. 架构决策（已综合两份研究员）

### 1.1 数据模型 — 不建 mirror 表 ✅

**结论**：复用 NewAPI 原生 `tokens` 表（已有 PKG-M6 AES-256-GCM 加密）。**不建** `gtk_user_tokens` 镜像表。

**理由**：
- NewAPI 集成为 Go library（非独立 service），内部调用 0 网络成本
- 减少同步复杂度（无双写一致性问题）
- 仍可写审计日志（用 `gtk_audit_log` 通用表，不是 token 专表）
- L23 invariant：binding 仍 1:1 (greentokey user ↔ NewAPI account)，**tokens 是 1:N under binding**

### 1.2 单 token vs 多 token（**待 founder 决策**）

| 维度 | A. 单 token (1 user 1 sk-xxx) | B. 多 token (1 user N sk-xxx) |
|---|---|---|
| 工时 | 10-12h | 14-16h |
| 用户体验 | 1 个 key, copy 复用 | dev/prod 分离 / 按项目命名 |
| L23 兼容 | ✅ | ✅（binding 仍 1:1） |
| 用量分析 | 用户级 | per-token 级（更细粒度） |
| 适合人群 | 民宿 SaaS 业主 | indie hacker / 开发者 |
| NewAPI 原生支持 | ✅ | ✅ |

**推荐**：**B（多 token）** — 工时只多 30%，但解决"用户要 dev/staging/prod 分离" 真实需求 + per-token usage 分析对 B 端客户价值大 + NewAPI 原生支持。

但如果 founder 当前只关心民宿 SaaS wedge，**A 也 OK**（民宿主不需要多 key）。

### 1.3 后端调用方式 — Go internal func ✅

**结论**：greentokey backend 调 NewAPI Go internal func（不走 HTTP API）。

**理由**：NewAPI 是 library 集成，已有 `newapi/client.go` wrapper 模式（`CreateToken` / `DisableToken`），continue 这条路。

## 2. 实施拆解（按多 token = B 方案）

### Backend (~4-5h)

新增 `newapi/admin_tokens.go`（参考现有 client.go 模式）：

```go
// User-side endpoints (5 个)
GET    /api/gtk/v1/tokens                       // 列出自己的 tokens
POST   /api/gtk/v1/tokens                       // 创建新 token（返回 sk-xxx 一次性明文）
PATCH  /api/gtk/v1/tokens/:id                   // 改 name / expire / quota
DELETE /api/gtk/v1/tokens/:id                   // 撤销（soft delete status=2）
GET    /api/gtk/v1/tokens/:id/usage             // 本 token 的用量明细

// Admin endpoints (3 个，仅 admin 角色)
GET    /api/gtk/v1/admin/tokens                 // 全局 token 列表（分页 + 按 user 筛选）
DELETE /api/gtk/v1/admin/tokens/:id             // 强制撤销任意 token
GET    /api/gtk/v1/admin/tokens/:id/audit       // 操作历史
```

每个 endpoint 内部调 NewAPI Go func（CreateToken/UpdateToken/ListTokens/DisableToken/GetTokenUsage）。

### Frontend — User Side (~3-4h，可复用 40%)

新建 `app/src/routes/setting/Tokens.tsx`（路由 `/setting/tokens`）：

```
┌─────────────────────────────────────────┐
│ 我的 API 令牌              [+ 新建令牌] │
├─────────────────────────────────────────┤
│ 📋 production-key      sk-tnx-***-abcd  │
│    创建于 2026-05-01   已用 2.3M tokens │
│    [复制] [改名] [撤销]                 │
├─────────────────────────────────────────┤
│ 📋 dev-key             sk-tnx-***-wxyz  │
│    创建于 2026-04-28   已用 145K tokens │
│    [复制] [改名] [撤销]                 │
└─────────────────────────────────────────┘
```

复用：
- Dashboard.tsx L77-300 的 Token panel 4 stats 卡 → 移到本页头部
- Account.tsx L325-392 `CredField` → 用作"创建后一次性显示 sk-xxx"对话框

新写：列表 / 创建 form / 改名 / 撤销 confirm dialog

### Frontend — Admin Side (~4-5h，新写)

GtkAdmin 加第 8 tab "tokens"（修改 `app/src/admin/GtkAdmin.tsx`）：

```
┌──────────────────────────────────────────────────────────────┐
│ 令牌审计                              [筛选: 用户/状态/时间] │
├──────────────────────────────────────────────────────────────┤
│ 用户          | 令牌名     | 状态  | 已用    | 创建     | 操作 │
│ alice@xx.com  | prod-key   | 活跃  | 2.3M    | 5/1      | [撤销] │
│ bob@xx.com    | dev-key    | 撤销  | 156K    | 4/28     | [审计] │
└──────────────────────────────────────────────────────────────┘
```

新写：全局表 + 按 user/status 筛选 + 强制撤销 + 操作审计弹窗。

### i18n (~0.5h)

新增 i18n key（约 30 个），20% 可复用现有（`dashboard.token.*` / `account.credentials.*`）。

### 测试 + Verify (~1-2h)

- 创建 token → 用 sk-xxx 真实调 `/v1/chat/completions` → 看到 used_quota +N
- Admin 强制撤销 → 用旧 sk-xxx 再调 → 401
- 过期 token 自动失效
- 边界：创建第 11 个 token 时是否有上限 / 撤销自己最后一个 token 是否禁止

## 3. 工时汇总

| 阶段 | 工时（A 单 token） | 工时（B 多 token） |
|---|---|---|
| Backend wrapper + endpoints | 3-4h | 4-5h |
| Frontend user-side | 2-3h | 3-4h |
| Frontend admin-side | 3-4h | 4-5h |
| i18n | 0.5h | 0.5h |
| 测试 + verify | 1-2h | 1-2h |
| **合计** | **10-12h** | **14-16h** |

## 4. 必须 founder 拍板的决策点

| # | 决策 | 影响 | 推荐 |
|---|---|---|---|
| 1 | 单 token vs 多 token | 工时 10 vs 16h | **多 token (B)** — 工时只 +30%，B 端价值大 |
| 2 | 不建 gtk_user_tokens 镜像表 | 0 工时（推荐已锁） | 锁定 |
| 3 | 后端用 Go internal func | 0 工时（推荐已锁） | 锁定 |
| 4 | sk-xxx 首次创建后一次性明文显示 | UX 细节 | 锁定（行业标准） |
| 5 | token 撤销 = soft delete（status=2） | 0 工时差 | 锁定（NewAPI 原生支持） |
| 6 | per-token usage 来源 | gtk_app_usage_log GROUP BY token_id（已能） | 锁定 |
| 7 | 用户 token 数量上限 | 防滥用 | 推荐：每用户 max 10 个 |
| 8 | 是否同时上线"撤销自己最后一个 token 禁止" | 1h | 推荐：上 |

## 5. 依赖 + 风险

**依赖**：
- A-1 阶段 2 deploy 完（GtkAdmin 7→8 tab 改动在干净 base 上做）
- 不需要新 DB migration（复用 NewAPI tokens 表）

**风险**：
- NewAPI ListTokens 内部 func 可能没暴露 → 需自己封装 query — 工时 +1h（已纳入估算）
- "首次创建后明文一次性显示"需要前后端协议保证 plaintext key 只在 create response 出现一次 — 标准做法但要测细致

## 6. 下一步建议

founder 拍 8 个决策点（推荐选项已加粗）→ 起 `/gsd-plan-phase PKG-A-3`，跑细化 plan + 执行。

跟 A-1 阶段 2 deploy 同步：
- A-1 阶段 2 codex 评审 OK → merge + deploy v0.33.0
- 起 A-3 plan-phase（base `feat/v0.22-token-launch` @ post-A-1-阶段 2）
- 一两天后 A-3 done → deploy v0.34.0-token-management

**典型路线**：今晚 codex review + A-1 阶段 2 deploy；明天起 A-3 plan-phase。
