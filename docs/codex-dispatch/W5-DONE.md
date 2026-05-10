# L1 W5 Done Report

## Commit Status

Git commits could not be created in this sandbox. The worktree files are writable, but the git index lives outside the writable root at:

`/Users/brendanxu/tanaxu/greentokey/.git/modules/coai/worktrees/coai-v0.7-design/index.lock`

`git add` fails with `Operation not permitted`, so there are no W5 commit hashes from this run. Intended commit split:

1. `feat(globals): MessageContent — typed dual-shape (string|array) with cache_control`
2. `feat(adapter/claude): forward cache_control marker in system + content blocks`
3. `feat(adapter/openai): forward cache_control marker on content blocks`
4. `feat(utils/buffer): PreferredCacheTTL — bridge client marker to billing`

## What Changed

- Added `globals.MessageContent`, `ContentBlock`, `CacheControl`, and `ImageURL` with dual JSON shape support.
- Preserved content arrays in `manager.transform()` instead of flattening them before adapter routing.
- Updated legacy text-only callers to use `Content.String()` and string write paths to use `MessageContent{Plain: ...}`.
- Forwarded Anthropic `cache_control` through Claude `system` blocks and message content blocks.
- Forwarded `cache_control` through OpenAI-compatible typed content blocks while keeping non-vision plain string requests on the old JSON shape.
- Added `Buffer.PreferredCacheTTL` so upstream cache-write usage is stamped with the customer-declared `5m` or `1h` TTL for W4 billing.

## Tests

Added 16 focused tests across:

- `globals/message_test.go`
- `manager/cache_control_test.go`
- `adapter/claude/cache_control_test.go`
- `adapter/openai/cache_control_test.go`
- `utils/preferred_ttl_test.go`

Verification run:

- `GOCACHE=/tmp/codex-go-cache go build ./...` passed.
- `GOCACHE=/tmp/codex-go-cache go test ./globals ./manager ./adapter/claude ./adapter/openai ./utils -count=1 -vet=off` passed.
- `GOCACHE=/tmp/codex-go-cache go test $(GOCACHE=/tmp/codex-go-cache go list ./... | grep -v '^chat/service$') -count=1 -vet=off` passed.
- Full `go test ./...` is blocked by sandbox networking: `chat/service` uses `httptest.NewServer`, which cannot bind `[::1]:0` here.

Grep evidence:

- `cache_control` in `adapter/claude`, `adapter/openai`, `globals`: 12 matches.
- `PreferredCacheTTL` in `utils/buffer.go`: present.
- `MessageContent` in `globals/types.go`: present.

## Issues Hit

- Git commit creation is blocked because worktree git metadata is outside the writable sandbox.
- The default Go build cache under `~/Library/Caches/go-build` is not writable here, so verification must use `GOCACHE=/tmp/codex-go-cache`.
- A direct `utils.NewBuffer` test with `gpt-3.5-turbo` triggered `tiktoken-go` network loading and recursive fallback in the sandbox. The TTL detection was factored into `preferredCacheTTL()` and tested without network access.
- Full test suite is otherwise stopped only by `chat/service` local port binding restrictions.

## Next Steps

- Run the four intended commits in a non-sandboxed shell or with writable git metadata.
- Add operator channel config fields for `cache_read`, `cache_write_5m`, and `cache_write_1h` where production pricing needs explicit cache tiers.
- Manually verify against Anthropic with two identical calls: first response should report `cache_creation_input_tokens > 0`; second should report `cache_read_input_tokens > 0`.

## Known Limitations

- This covers the OpenAI-compatible `/v1/chat/completions` relay path. No separate Anthropic-native `/v1/messages` controller path was found in the inspected bind route.
- Non-Anthropic adapters continue to flatten content blocks for provider formats that only accept plain text; this preserves existing compatibility but only Claude/OpenAI-compatible paths forward `cache_control`.
