# PKG-A-3 Codex Review Prompt

Run from repo root (on `feat/v0.22-token-launch` after merge):

```
codex review --base feat/v0.22-token-launch --title "PKG-A-3 token management" "
评审 PKG-A-3 token management 完整实现（15 个 commits, ~3000 LOC net new）：

Backend (Wave 1 + 1.5 + 1.5b)：
- newapi/admin_tokens.go: 5 user-side + 3 admin-side handlers via HTTP NewAPI REST API
- 关键风险: plaintext leak / last-token guard / max 10 limit — 都有专测试
- GetTokenUsage 走 NewAPI logs API (三条隔离原则)
- gtk_app_usage_log 加 token_id 字段 + idx + INSERT path

Frontend user-side (Wave 2):
- /setting/tokens 5 个 React 组件 + 28 i18n keys
- 一次性明文显示 sk-tnx-xxx (CreateTokenDialog)
- masked key + status badge + per-token usage drawer

Frontend admin-side (Wave 3):
- GtkAdmin 第 8 tab '令牌审计'
- TokensTab: 全局列表 + 用户搜 + status 客户端筛选 + 强制撤销 + 审计抽屉

重点查：
1. plaintext sk-xxx 是否真的只在 CreateUserToken response 出现一次（前后端协议）
2. last-token guard 是否能被客户端绕过（前端禁止 button 但 backend 也 enforce）
3. max 10 limit 是否有竞态（SELECT FOR UPDATE 或 transaction）
4. admin force-revoke 没 last-token guard 是否产品上合理（admin 责任自负）
5. GtkAdmin.tsx 加 tab 是否破坏既有 7 tabs（diff +14 LOC）
6. i18n cn/en 是否对称（48 个 key 各加 cn + en）
7. backend HTTP 调 NewAPI 失败时 graceful degrade（error envelope vs panic）
8. audit endpoint 真实实现 vs stub（agent 报告说真实但表可能不存在）

简洁。BLOCKING / HIGH 详写，MEDIUM 简写，LOW 跳过。
"
```
