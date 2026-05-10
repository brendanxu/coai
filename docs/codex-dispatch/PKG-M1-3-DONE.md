# PKG-M1-3 Done Report

## Commit Status

Created commits:

1. `382cfb3` `feat(usage): WriteUsageLog — gtk_app_usage_log writer with 4-class breakdown`
2. `d661d6e` `feat(manager/chat): wire usage.WriteUsageLog into CollectQuota`
3. `docs: PKG-M1-3-DONE.md report`

## What Changed

- Added `usage.WriteUsageLog`, a synchronous per-chat-call writer for
  `gtk_app_usage_log`.
- Persisted V2 fields for upstream truth: `input_tokens`, `output_tokens`,
  `cache_write_tokens`, `cache_read_tokens`, `cache_ttl`,
  `upstream_cost_micro`, `client_charge_micro`, and `markup_multiplier`.
- Preserved legacy fields by writing `tokens_used` as the 4-class sum and
  `cost_cents` from the client charge.
- Added best-effort active plan lookup from `gtk_user_plan`, leaving
  `plan_id` NULL when no active plan exists.
- Added provider inference for `claude-*`, `gpt-*`, `deepseek-*`, and `qwen-*`.
- Wired `manager/chat.go::CollectQuota` to write the audit row after quota
  mutation, logging failures as non-fatal warnings.

## Tests

Added focused SQLite tests in `usage/writer_test.go` covering:

- Upstream 4-class token writes, including `PreferredCacheTTL` bridging to
  `cache_ttl='5m'`.
- Legacy no-upstream fallback with 50/50 input/output split.
- Markup math, markup override, and default fallback when config is missing.
- NULL `plan_id` without an active plan and active plan resolution.
- Provider inference for Anthropic, OpenAI, DeepSeek, and unknown models.

Verification run:

- `GOCACHE=/tmp/codex-go-cache go build ./... 2>&1 | grep -v "warning\|libwebp"` passed with no build errors.
- `GOCACHE=/tmp/codex-go-cache go test ./usage/... ./manager/... -count=1 -vet=off` passed:
  - `ok chat/usage`
  - `ok chat/manager`
- `grep -n "WriteUsageLog" usage/writer.go manager/chat.go` found writer and call-site matches.
- `grep -n "INSERT INTO gtk_app_usage_log" usage/writer.go` found exactly one insert statement.

## Notes

- `WriteUsageLog` resolves charge config from `channel.ChargeInstance` by
  `buffer.Model`, with `buffer.Charge` as a fallback for unset/missing global
  charge mappings.
- Audit write is intentionally synchronous and failure-tolerant; missed rows
  are logged but do not panic the chat path.
