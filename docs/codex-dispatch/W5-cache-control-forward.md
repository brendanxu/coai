# Codex Worker — L1 W5: Forward client cache_control marker

**Phase**: Path 1 final step (after W1–W4 commits 858b2ad, f8fe3a9, 5d2074a).
**Estimated effort**: 4–6h
**Output**: 1 PR, ~300 LoC, ~15 test cases.

---

## 0. Pre-context (read this fully before writing code)

W1–W4 already shipped:
- `gtk_app_usage_log` schema V2 + `gtk_provider_pricing` + `gtk_billing_config`
- Adapter Usage parsing for Claude / OpenAI / DeepSeek emits a terminal
  `globals.Chunk{UpstreamUsage: ...}` at end-of-stream
- `utils/buffer.go::Buffer.RecordUpstreamUsage` captures it
- `utils/tokenizer.go::CountUpstreamQuota` bills 4 token classes via
  `channel.Charge.GetCacheRead/Write5m/Write1h`
- 24 unit tests pass

What's still missing: when an Anthropic client sends a request with
`cache_control: {type: "ephemeral"}` markers in their body, our adapter
strips it (because `globals.Message.Content` is plain `string`,
`adapter/claude/types.go::ChatBody.System` is plain `string`). Result:
upstream Anthropic sees no marker → never caches → customer "auto cache"
flow works (OpenAI, DeepSeek auto-cache) but Anthropic explicit-cache
flow doesn't.

This worker plumbs the marker through the request path.

---

## 1. Files to read first

- `globals/types.go` — `Message`, `Chunk`, `UpstreamUsage` shapes
- `adapter/common/chat_props.go` — `ChatProps` is what every adapter
  receives (find this file; it's where you'd add CacheControl-bearing
  content)
- `adapter/claude/types.go` — current ChatBody / Message / MessageContent;
  this is where the typed body for Anthropic is emitted
- `adapter/claude/chat.go::GetChatBody` — the marshaling site
- `adapter/openai/types.go` — Message + ChatRequest shapes
- `manager/chat.go` — where ChatProps is constructed, line ~165
- `controller/chat.go` (if exists; otherwise `manager/`) — where the
  client request body is parsed
- `utils/buffer.go::Buffer` — has `Upstream` field; you may want a
  `CacheTTL` knob captured at request time so W4's billing splits
  cache_write_5m vs cache_write_1h correctly

---

## 2. Acceptance criteria

End-to-end behavior must be:

1. Client sends OpenAI-shaped request with `messages[i].content` as an
   array of typed blocks where one block carries
   `cache_control: {type: "ephemeral", ttl: "5m"}`. The marker survives
   round-trip:
   ```bash
   curl /v1/chat/completions \
     -H 'Authorization: Bearer sk-tnx-...' \
     -d '{
       "model": "claude-sonnet-4-5",
       "messages": [{
         "role": "system",
         "content": [
           {"type":"text","text":"<long system prompt ≥1024 tokens>"},
           {"type":"text","text":"...","cache_control":{"type":"ephemeral"}}
         ]
       }, {
         "role":"user","content":"hi"
       }]
     }'
   ```
   Upstream Anthropic receives a request body whose system block has
   `cache_control` preserved. Response contains `cache_creation_input_tokens > 0`
   on first call, `cache_read_input_tokens > 0` on subsequent calls
   within the TTL.

2. The TTL field ("5m" vs "1h") is captured into
   `Buffer.Upstream.CacheTTL` so W4's billing routes to
   `GetCacheWrite5m()` vs `GetCacheWrite1h()`. No new schema migrations
   needed — `gtk_app_usage_log.cache_ttl` already exists from W1.

3. Anthropic-native endpoint (if exposed) — `/v1/messages` — the body
   passes through verbatim. cache_control on system / messages / tools
   all preserved.

4. OpenAI clients keep working unchanged (the new typed content shape
   must coexist with the legacy `content: string` shape — many OpenAI
   clients still send strings). Negative test: existing E2E for
   `content: "hello"` still passes.

5. Pre-existing unit tests stay green:
   ```
   go test ./adapter/... ./plans/... ./utils/... -count=1 -vet=off
   ```

---

## 3. Recommended approach

### 3.1 Two-level content type

Change `globals.Message.Content` from `string` to a new type that can
hold both shapes:

```go
// globals/types.go
type Message struct {
    Role             string         `json:"role"`
    Content          MessageContent `json:"content"`  // was string
    Name             *string        ...
}

// MessageContent carries either a plain string (legacy OpenAI clients)
// or a list of typed blocks (Anthropic-style with cache_control).
// MarshalJSON / UnmarshalJSON dispatch by JSON shape.
type MessageContent struct {
    Plain  string
    Blocks []ContentBlock
}

type ContentBlock struct {
    Type         string        `json:"type"`
    Text         string        `json:"text,omitempty"`
    Image        *ImageBlock   `json:"image,omitempty"`
    CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
    Type string `json:"type"` // "ephemeral"
    TTL  string `json:"ttl,omitempty"` // "5m" or "1h"
}
```

This is the biggest change in the worker — `globals.Message.Content` is
referenced in ~30 files. Most callers just want `String()`; provide an
accessor:

```go
func (m MessageContent) String() string {
    if m.Plain != "" { return m.Plain }
    var b strings.Builder
    for _, blk := range m.Blocks {
        if blk.Type == "text" { b.WriteString(blk.Text) }
    }
    return b.String()
}
```

Migrate call sites incrementally: `msg.Content` → `msg.Content.String()`
where the caller wants a flat string. Use `gofmt -r` or sed.

### 3.2 Adapter forwarding

Claude:
- `adapter/claude/types.go::ChatBody.System` change `string` →
  `interface{}` (string for the simple case, array of typed blocks
  with cache_control for the cache case)
- `adapter/claude/chat.go::GetSystemPrompt` returns either a string or
  an array depending on whether any source block carries cache_control
- `adapter/claude/chat.go::GetMessages` already takes
  `[]globals.Message` — extend to forward cache_control on each
  ContentBlock when present

OpenAI:
- `adapter/openai/types.go::Message` already supports
  `MessageContents` (typed array). Add `CacheControl` to
  `MessageContent`.
- For OpenAI auto-cache the marker is informational; OpenAI ignores
  unknown fields gracefully. Forwarding doesn't hurt and helps any
  OpenAI-compatible upstream that supports cache_control (some
  proxies do).

DeepSeek: KV cache is automatic — don't strip `cache_control`, forward
it (DeepSeek ignores unknown fields).

### 3.3 TTL capture into Buffer

Once you've parsed `cache_control.ttl` from the request, stash it on
`utils.Buffer.PreferredCacheTTL` (new field, "" / "5m" / "1h").
After the upstream chat completes, when you call
`buffer.RecordUpstreamUsage(usage)`, copy
`PreferredCacheTTL` into `usage.CacheTTL` so
`CountUpstreamQuota` routes cache_write to the right bucket.

---

## 4. Tests

Unit:
- `globals/message_marshal_test.go` — round-trip both content shapes:
  `{"content": "hello"}` and `{"content": [{"type":"text",...}]}`
- `adapter/claude/cache_control_test.go` — given a Message with
  cache_control on a system block, GetChatBody emits a body whose
  System field is the array shape with the marker preserved
- `adapter/openai/cache_control_test.go` — same for OpenAI

Integration smoke (manual; no infra in CI yet):
- Hit Anthropic test endpoint with a >1024-token system prompt + marker
  twice. First call response has `cache_creation_input_tokens > 0`,
  second call has `cache_read_input_tokens > 0`. Read
  `gtk_app_usage_log` rows — `cache_write_tokens` and
  `cache_read_tokens` populated, `markup_multiplier=1.300`.

---

## 5. Verification commands (paste in PR description)

```bash
go test ./... -count=1 -vet=off                   # all green
go build ./...                                      # zero errors
git log --oneline -5                                # commits visible
grep -rn "cache_control" adapter/ globals/          # ≥3 hits per package
grep -rn "PreferredCacheTTL" utils/ manager/        # 1+ hit
```

---

## 6. Out of scope (do NOT do)

- Image / multi-modal content blocks: existing typed support survives,
  don't refactor.
- Reasoning models content shape: keep ReasoningContent unchanged.
- Operator UI / admin dashboard for managing cache_control defaults:
  another sprint.
- gtk_app_usage_log writer: still nil — wired separately when L3
  service marketplace is connected (or a new worker for L1 logging
  pipe).

---

## 7. Risks + mitigations

| Risk | Mitigation |
|---|---|
| Refactoring globals.Message.Content breaks 30 files | Start with String() accessor, sed-mass-replace, then Type-fix. Run full test suite before each commit. |
| OpenAI clients sending plain string break under typed Content | MessageContent.UnmarshalJSON detects shape (string vs array) and routes; tests cover both. |
| Anthropic-native /v1/messages clients lose cache_control | Add raw-body bypass in middleware: if request body root is Anthropic-shaped (has `system: array`), forward bytes verbatim to adapter. |
| Buffer.PreferredCacheTTL not propagated to billing | Test directly: construct Buffer with PreferredCacheTTL="5m", call RecordUpstreamUsage, check resulting Upstream.CacheTTL. |
| Existing reasoning chunk path mutates content string | Keep MessageContent.Plain as the writeback target for reasoning text — reasoning code only reads/writes Plain. |
