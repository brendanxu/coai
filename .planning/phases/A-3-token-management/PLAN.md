# PKG-A-3: Personal Access Tokens 管理 — Execution Plan

**Date**: 2026-05-17
**Base branch**: `feat/v0.22-token-launch` @ `b0db604`（v0.33.0-admin-orphan-cleanup LIVE）
**Working branch**: `feat/v0.34-token-management`
**Worktree**: `/Users/brendanxu/tanaxu/greentokey/coai-v0.22-token-checkout/`
**Estimate**: 14-16h（方案 B 多 token 已锁）
**Design**: [.planning/research/2026-05-17-pkg-a3-token-management-design.md](../../research/2026-05-17-pkg-a3-token-management-design.md)

## 0. Goal

让用户能在 greentokey 自有 admin hub + 用户 portal 完成 sk-xxx 全生命周期管理（创建 / 列表 / 改名 / 撤销 / 用量查询），完全替代登 NewAPI :3000 原生后台。

## 1. 锁定的架构决策（design 已综合）

| # | 决策 | 状态 |
|---|---|---|
| 1 | 多 token (1 user N sk-xxx) | ✅ 锁 |
| 2 | 不建 gtk_user_tokens mirror，复用 NewAPI tokens 表 | ✅ 锁 |
| 3 | Backend 调 NewAPI Go internal func（非 HTTP） | ✅ 锁 |
| 4 | sk-xxx 仅首次创建 response 返回一次明文 | ✅ 锁 |
| 5 | 撤销走 soft delete (NewAPI token.status=2) | ✅ 锁 |
| 6 | per-token usage 走 `gtk_app_usage_log` GROUP BY token_id | ✅ 锁 |
| 7 | 每用户 token 数量上限 = 10 | ✅ 锁（plan-time decision） |
| 8 | 撤销自己最后一个 token 禁止 | ✅ 锁（plan-time decision） |

## 2. Task Breakdown — 4 Waves with TDD

### Wave 1: Backend — NewAPI token wrapper（~4-5h）

**Goal**: 暴露 NewAPI Go 内部 token API 为 greentokey HTTP endpoint。

| Task | TDD | Atomic Commit | Est |
|---|---|---|---|
| 1.1 写 `newapi/admin_tokens.go` skeleton + `ListUserTokens` Go func wrapper | ✅ unit test first | `feat(newapi): admin_tokens.go scaffold + ListUserTokens` | 1h |
| 1.2 加 `UpdateToken`（改 name/expire/quota，复用 NewAPI 原生 PUT /api/token/） | ✅ unit test first | `feat(newapi): UpdateToken wrapper` | 0.5h |
| 1.3 加 `RevokeToken`（status=2 soft delete + 验证不撤销最后一个 active token） | ✅ unit test first（含 last-token guard 测试） | `feat(newapi): RevokeToken with last-token guard` | 1h |
| 1.4 加 `CreateUserToken`（强制 max 10 上限 check + 返回 plaintext sk-xxx 一次） | ✅ unit test first（含 10-token limit 测试 + plaintext leak 测试） | `feat(newapi): CreateUserToken with limit + one-time plaintext` | 1h |
| 1.5 加 `GetTokenUsage`（GROUP BY token_id from `gtk_app_usage_log`） | ✅ unit test with seed | `feat(newapi): GetTokenUsage per-token aggregation` | 0.5h |
| 1.6 在 `service/router.go` 注册 5 个 user-side + 3 个 admin-side HTTP routes | smoke test (curl) | `feat(api): wire token CRUD routes` | 0.5h |
| 1.7 wave-1 integration test：完整生命周期 (create → list → use → revoke → list) | ✅ integration | `test(token): full lifecycle integration` | 0.5h |

**Wave 1 验收**：`go test ./newapi/... ./service/...` 全绿 + 7 unit + 1 integration test pass + `go vet` 干净。

### Wave 2: Frontend User-side — `/setting/tokens`（~3-4h，复用 40%）

**Goal**: 用户能在 setting 页自助管理自己的 N 个 token。

| Task | 复用 | Atomic Commit | Est |
|---|---|---|---|
| 2.1 新建 `app/src/routes/setting/Tokens.tsx` 骨架 + 路由注册 | Dashboard.tsx Token panel 头部 | `feat(ui): /setting/tokens page scaffold` | 0.5h |
| 2.2 Token 列表组件（masked sk-tnx-xxx-suffix + name + usage + status） | Account.tsx CredField | `feat(ui): TokenList component with masked keys` | 1h |
| 2.3 "新建令牌"弹窗（name + expire + quota 字段 + 创建成功后明文一次性显示弹窗） | shadcn Dialog | `feat(ui): CreateTokenDialog with one-time plaintext reveal` | 1h |
| 2.4 "改名 / 撤销" 行内操作 + confirm dialog（特别警告最后一个 token） | shadcn AlertDialog | `feat(ui): rename + revoke actions with last-token warning` | 0.5h |
| 2.5 "用量明细" 抽屉（per-token 用量曲线 + 模型分布） | recharts | `feat(ui): TokenUsageDrawer with per-token breakdown` | 0.5-1h |
| 2.6 i18n keys 加 (cn + en) | dashboard.token.* / account.credentials.* 复用 | `chore(i18n): token-management keys cn/en` | 0.5h |

**Wave 2 验收**：`npm run build` 绿 + 浏览器手动验证全部 5 个 user action 工作（create / list / rename / revoke / usage view）。

### Wave 3: Frontend Admin-side — GtkAdmin 第 8 tab（~4-5h）

**Goal**: admin 能在 GtkAdmin 第 8 tab 看全局 token + 强制撤销 + 操作审计。

| Task | Atomic Commit | Est |
|---|---|---|
| 3.1 修 `app/src/admin/GtkAdmin.tsx`：Tab type 加 "tokens"，菜单加该项 | `feat(admin): add tokens tab to GtkAdmin` | 0.5h |
| 3.2 新建 `app/src/admin/components/TokensTab.tsx`：全局 token 列表 | `feat(admin): TokensTab global list with user filter` | 1.5h |
| 3.3 筛选器（user / status / 创建时间） | `feat(admin): TokensTab filters` | 0.5h |
| 3.4 "强制撤销" 操作（admin 可撤销任意 token，无 last-token 限制） | `feat(admin): admin force-revoke action` | 0.5h |
| 3.5 "操作审计" 弹窗（关联 `gtk_audit_log`） | `feat(admin): token audit log drawer` | 1h |
| 3.6 admin-side i18n + 整体 wire 验证 | `chore(i18n+admin): wire TokensTab to GtkAdmin` | 0.5h |

**Wave 3 验收**：`npm run build` 绿 + admin 能筛选、查看、撤销任意 token + 审计日志显示历史操作。

### Wave 4: Integration + Verify + Deploy prep（~2h）

| Task | Atomic Commit | Est |
|---|---|---|
| 4.1 端到端测试：用 sk-tnx-xxx 真实调 `/v1/chat/completions` → 看 `gtk_app_usage_log` 新行 → 看 admin tab 用量 +N | `test(e2e): token-create-use-audit lifecycle` | 0.5h |
| 4.2 撤销 token → 用旧 sk-xxx → 401 → admin tab 状态显示 revoked | (覆盖到 4.1 commit) | 0.25h |
| 4.3 边界测试：用户已有 10 token → 创建第 11 个 → 400 with `MAX_TOKENS_REACHED` | （覆盖到 4.1 commit） | 0.25h |
| 4.4 用户撤销自己最后一个 active token → 400 with `CANNOT_REVOKE_LAST_TOKEN` | （覆盖到 4.1 commit） | 0.25h |
| 4.5 写 deploy 报告 草稿 `docs/codex-reviews/25-v0.34.0-token-management-deploy.md` | `docs(prep): v0.34.0 deploy report skeleton` | 0.5h |
| 4.6 bump `bin/v22-deploy.sh` tags v0.33 → v0.34 | （在 deploy 时做，不预提交） | 0.25h |

**Wave 4 验收**：4 个边界 case 全 PASS + e2e lifecycle 通 + deploy report skeleton 就绪。

## 3. 文件清单（预估）

**新增文件**:
- `newapi/admin_tokens.go` (~200 LOC)
- `newapi/admin_tokens_test.go` (~250 LOC)
- `app/src/routes/setting/Tokens.tsx` (~300 LOC)
- `app/src/components/setting/TokenList.tsx` (~150 LOC)
- `app/src/components/setting/CreateTokenDialog.tsx` (~120 LOC)
- `app/src/components/setting/TokenUsageDrawer.tsx` (~150 LOC)
- `app/src/admin/components/TokensTab.tsx` (~250 LOC)

**修改文件**:
- `service/router.go` (+8 routes, ~30 LOC)
- `app/src/router.tsx` (+1 route, ~5 LOC)
- `app/src/admin/GtkAdmin.tsx` (+tab, ~15 LOC)
- `app/src/resources/i18n/cn.json` (+~25 keys)
- `app/src/resources/i18n/en.json` (+~25 keys)

**总 LOC 预估**: +~1500（前端 ~970 / 后端 ~450 / i18n ~80）

## 4. Risks & Mitigations

| Risk | Probability | Mitigation |
|---|---|---|
| NewAPI `ListTokens` Go internal func 不存在，要自己写 query | 中 | 已在估算里 +1h buffer (Task 1.1) |
| 改 admin/GtkAdmin.tsx 触发 ts/build 错误（1489 LOC 大文件） | 中 | Wave 3 第一个 commit 只加 type + menu，最小破坏面 + 立即 build 验证 |
| Token plaintext leak（明文不小心二次显示） | 高严重 | Task 1.4 写明确 leak unit test + Wave 4 e2e 验证 |
| max 10 token limit 在 high-concurrency 下被竞态绕过 | 低 | Task 1.4 DB-level lock（NewAPI tokens 表 user_id unique 不够，要 transaction） |
| 用户撤销 token 后已 in-flight 请求继续用旧 token 返成功 | 低 | NewAPI 自身缓存 token，撤销后最长 N 秒 race window，文档说明即可 |
| 改 newapi/ 包等于改 fork 上游 → merge 上游成本 | 中 | `admin_tokens.go` 单独新文件，最小侵入；不改既有 NewAPI 函数 |

## 5. Deploy Plan

**Trigger**: Wave 4 全 PASS + founder review PLAN execution

**Steps**:
1. `git checkout feat/v0.22-token-launch && git merge --no-ff feat/v0.34-token-management`
2. Bump `bin/v22-deploy.sh`: PREV `v0.33.0-admin-orphan-cleanup` → NEW `v0.34.0-token-management`
3. `git push origin feat/v0.22-token-launch`
4. `bin/v22-deploy.sh rsync` → `build` → `up` → `smoke`
5. Visual verify: 用户登录 → /setting/tokens 能用 + admin /admin/gtk?tab=tokens 能用
6. 写完整 `docs/codex-reviews/25-v0.34.0-token-management-deploy.md`

**Pre-deploy review**:
- `codex review --base feat/v0.22-token-launch --title "PKG-A-3 token management"`
- 必须 0 BLOCKING + 0 HIGH（涉及 auth + quota，要 strict）

## 6. Rollback

- `bash bin/v22-deploy.sh rollback` → 30s 回到 v0.33.0
- DB 无 migration（只用 NewAPI 现有表 + `gtk_app_usage_log` 现有字段），rollback 0 数据风险
- 用户在 v0.34 期间创建的 sk-xxx 仍然有效（NewAPI tokens 表保留），rollback 后用户能继续用，只是 UI 看不到管理界面

## 7. Acceptance Criteria（founder 验收）

- [ ] 用户 portal 能创建 token，第一次创建后明文 sk-tnx-xxx 一次显示
- [ ] 用户列表显示所有 token（masked），名字 + 创建时间 + 已用量 + 状态
- [ ] 撤销 token 后旧 sk-xxx 调 API 返 401
- [ ] 创建第 11 个 token 返 400 + 友好提示
- [ ] 撤销最后一个 active token 返 400 + 友好提示
- [ ] admin GtkAdmin 8 个 tab，新 "令牌审计" tab
- [ ] admin 能按 user / status / 时间筛选全局 token
- [ ] admin 能强制撤销任意 token（包括用户最后一个）
- [ ] admin 能查看 token 操作审计历史
- [ ] codex review 0 BLOCKING / 0 HIGH
- [ ] e2e lifecycle 测试通过

## 8. Out of Scope（明确不做）

- ❌ IP whitelist per token（NewAPI 支持但本 PKG 不暴露 UI）
- ❌ Model whitelist per token（同上）
- ❌ Group / tier per token（同上，留给未来 PKG）
- ❌ Token expire 自动续期（用户自助 update expire 已支持，不做 auto）
- ❌ Token 操作 webhook（如 "token revoked" 通知用户）

## 9. Next Action

founder review 本 PLAN.md → 说 "GO 起 Wave 1" → 我夜间按 Wave 1→2→3→4 顺序执行。
每 Wave 完成后亲自跑 verify + 上报，遇 BLOCKING 立即 stop。

明早预期：v0.34.0-token-management 候选 build OK，等 founder 跑 codex review + 视觉验收 → deploy。
