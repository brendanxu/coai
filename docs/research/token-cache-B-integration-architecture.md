# Token Cache 集成架构设计 (Agent B)

> Scope: greentokey-internal architecture only. Provider API edges are
> Agent A; ROI math is Agent C. This doc answers: where do we wire the
> cache hook into our existing code, what changes, and how big is the
> blast radius.
>
> Ground truth verified in `service/runtime.go`, `service/types.go`,
> `service/seed.go`, `adapter/claude/{struct,chat,types}.go`,
> `adapter/openai/{struct,types}.go`. 0 hits on `cache_control`
> anywhere in the codebase today.

---

## 1. 现状梳理

### 1.1 实际 data flow (verified from source)

```
┌─────────────┐    ┌────────────────────────────┐    ┌──────────────────────┐    ┌──────────────┐
│  Customer   │ →  │ POST /api/gtk/v1/service/  │ →  │ executeAgent(ctx,    │ →  │  NewAPI      │
│  browser    │    │ run/:order_no              │    │   agent, userInput)  │    │  :3000       │
└─────────────┘    │  (RunOrderAPI in           │    │  in service/         │    │  /v1/chat/   │
                   │   service/runtime.go)      │    │  runtime.go:349-420  │    │  completions │
                   └────────────────────────────┘    └──────────────────────┘    └──────┬───────┘
                                                                                        │
                                                            (Bearer = service.runner_   │
                                                             api_key, NOT customer key) │
                                                                                        ▼
                                                                              ┌─────────────────┐
                                                                              │ Channel router  │
                                                                              │ (NewAPI v0.13.x)│
                                                                              │   ↓             │
                                                                              │ Provider:       │
                                                                              │  • OpenAI       │
                                                                              │  • Claude       │
                                                                              │  • DeepSeek     │
                                                                              │  • sub2API …    │
                                                                              └─────────────────┘
```

### 1.2 4 个可以注入 cache 的位置

| Position | Where the code lives | Pros | Cons |
|---|---|---|---|
| **(P1) Client / browser** | gtk frontend (sandbox + main) | Closest to user prompts; could "cache hint" what's static | Useless for cache_control — provider doesn't see it; just a UX label |
| **(P2) gtk handler / service runtime** | `service/runtime.go::executeAgent`, `executeAgentWithImages` | We own this code; one chokepoint for both Claude + OpenAI; can A/B per agent | Forces us to rewrite body schema from `map[string]string` to typed array, touches Claude+OAI symmetrically |
| **(P3) NewAPI gateway layer** | `coai-v0.7-design/adapter/{claude,openai}/chat.go` (the multi-provider router) | Gateway already touches the body anyway (channel routing, model_ratio); could inject `cache_control` per channel config | We'd have to fork `chat/adapter` and risk diverging from CoAI upstream; current adapters strip nothing but also forward nothing — they own re-serialisation |
| **(P4) Provider API edge** | Outside our control (Anthropic / OpenAI side) | OpenAI auto-caches at 1024-token boundary with zero code change | Claude requires explicit `cache_control` markers; can't be done from outside |

**Recommendation preview**: P2 = primary integration point (we own it),
P3 = optional pass-through fix (adapters currently re-serialise without
forwarding `cache_control` — see §4 risk). P1 = no-op. P4 = "free win"
for OpenAI but irrelevant for Claude/DeepSeek.

---

## 2. 关键架构决策 — 3 候选方案

### 方案 A: 改 service/runtime.go body schema

```
service/runtime.go::executeAgent
─────────────────────────────────
BEFORE                              AFTER
body := map[string]interface{}{     body := map[string]interface{}{
  "model": …,                         "model": …,
  "messages": []map[string]            "messages": []map[string]interface{}{
              string{                    {"role":"system","content":[
    {"role":"system",                      {"type":"text","text":SP,
     "content":SP},                         "cache_control":{"type":"ephemeral"}}
    {"role":"user",                      ]},
     "content":userInput},               {"role":"user","content":userInput},
  },                                   },
}                                   }
```

**Pros**:
- One file owns the change; both seeded agents benefit
- Symmetric to what `executeAgentWithImages` already does (it's already
  array-of-typed for the user message — content schema mismatch shrinks)
- Lets us A/B (env-flag `service.cache_enabled=true`) without touching
  callers
- Re-uses our existing test scaffolding (`runtime_test.go`)

**Cons**:
- system message now array-of-typed for **all** providers including
  ones that don't understand `cache_control` (DeepSeek, hunyuan, …);
  need to verify they tolerate the typed shape (OpenAI does — see
  `adapter/openai/types.go` `MessageContents` is already typed)
- Breaking Anthropic schema in OpenAI-style: `cache_control` on a
  `system` array element works for Anthropic via NewAPI Claude adapter,
  **but** for OpenAI passthrough the adapter must not strip it (NewAPI
  re-serialises through `adapter/openai/types.go::Message` which lacks
  the field — cache_control would be silently dropped, falling back to
  OpenAI's automatic caching)

**LoC estimate**: `service/runtime.go` ~30 LoC (body builder helper +
flag check), `service/types.go` 0 LoC (system_prompt stays string in
DB), `runtime_test.go` ~40 LoC (3 new test cases: cache enabled / not
enabled / Claude vs OpenAI shape).

### 方案 B: NewAPI gateway 加 cache 中间件

```
                         ┌─── greentokey owns ───┐
service/runtime.go ─────→│ (no change, send raw  │──── HTTP ────→ NewAPI :3000
   body=map[string]string│  body)                │                 │
                         └───────────────────────┘                 ▼
                                                          ┌──────────────────┐
                                                          │ NewAPI middleware│
                                                          │ (NEW): inspects  │
                                                          │ channel.type +   │
                                                          │ system msg →     │
                                                          │ injects          │
                                                          │ cache_control if │
                                                          │ provider ==      │
                                                          │ anthropic AND    │
                                                          │ tokens ≥1024     │
                                                          └────────┬─────────┘
                                                                   ▼
                                                             provider fan-out
```

**Pros**:
- service/runtime.go untouched — zero risk to working call path
- One middleware can serve future agents too (no per-call wiring)
- Channel-level config (NewAPI admin UI) can toggle cache per provider

**Cons**:
- **Requires forking NewAPI** (we pin `tanaxu626/coai 3048a493` already
  but treat it as upstream-tracking; this would become a real fork)
- NewAPI v0.13.x's `chat/adapter/openai/types.go::Message.Content` is
  `MessageContents` (typed) but the **system role** typically arrives as
  a plain string — middleware would have to detect and rewrite the
  shape, then re-serialise. Higher complexity than option A by a
  factor of 2-3x.
- Channel re-config (NEWAPI_ADMIN_TOKEN) to wire it on; founder action
- Diagnostics harder: cache hit/miss metrics live in NewAPI, not in our
  greentokey DB → would need `gtk_cache_metric` to scrape

**LoC estimate**: NewAPI fork ~150 LoC across 3 files (middleware +
struct extension + admin config), greentokey ~0 LoC. **But** carries
ongoing maintenance cost on every NewAPI upgrade.

### 方案 C: greentokey 加 cache adapter (新文件)

```
service/runtime.go::executeAgent
        │
        ▼
service/cache_adapter.go (NEW)
  ├─ ExtractSystem(body) → systemText
  ├─ ShouldCache(provider, tokens) → bool
  ├─ InjectMarker(body, systemText) → body'
  └─ NormalizeMessage(body') for non-Anthropic providers
        │
        ▼
HTTP POST → NewAPI
```

**Pros**:
- New file = no existing test breakage
- Pure unit-testable: input body → output body
- Easy to feature-flag and to disable per-agent

**Cons**:
- It's basically Option A but with a redundant abstraction layer.
  We'd still mutate the body in `executeAgent`, just dispatched
  via a helper. The "decision point" is still in runtime.go.
- More files to navigate during incident response

**LoC estimate**: `service/cache_adapter.go` ~100 LoC, `runtime.go`
~10 LoC delta, `cache_adapter_test.go` ~80 LoC.

### 推荐 = 方案 A

**One-line reason**: We own `service/runtime.go`, the chokepoint is
already there, both seeded agents flow through it, and rewriting
`map[string]string` → `[]map[string]interface{}` is a 30-line patch
that mirrors what `executeAgentWithImages` already does for the user
message. Forking NewAPI (B) for ≤2 agents is over-engineering; a
separate adapter file (C) is just A in two pieces.

---

## 3. system prompt 长度问题

verified: `xhs-copy-writer` system_prompt = 1440 chars / **~660
tokens**, `mansu-managed-orchestrator` = 855 chars / **~395 tokens**.
Anthropic's `cache_control` minimum is 1024 tokens. Both miss.

### 解法 1: 拉长 system prompt → ≥1024 tokens

```
Trade-off: each request sends MORE input tokens (full 1024+) but
cached read is ~0.1× cost. break-even = call_count × (extra_tokens
× 0.1) saved vs (extra_tokens × 1.0) spent on first call. With
mansu-managed = monthly subscriber making 30+ calls/month, extending
system prompt from 395→1100 tokens pays back after call #3.
Risk: longer prompt = more attack surface for jailbreaks, harder
to evolve.
```

### 解法 2: Batch system + few-shot examples 一起 cache

```
Trade-off: 大理民宿场景一致性高 — same brand voice, same ROI rules,
same "避雷词清单". Pre-pend 5-10 high-quality canonical examples
("good caption A", "good caption B", …) to system prompt, total
≈1500 tokens, then cache the whole prefix. Different customers
share the same cached prefix because user input is in a separate
message. **This is the highest-ROI option** for xhs-copy-writer
because output quality goes up alongside cost going down.
Risk: examples leak across customers if any include real names —
must be fully synthetic.
```

### 解法 3: 不 cache,改用 OpenAI auto-cache

```
Trade-off: OpenAI gpt-4o-mini auto-caches at 1024-token boundary
with zero code change (no marker required). xhs-copy-writer
already uses gpt-4o-mini → if we extend system prompt to 1024+
via solution 2, OpenAI caches it for free. mansu-managed-
orchestrator uses deepseek-r1 → no auto-cache, would need solution 1.
Risk: OpenAI cache is opaque (no hit/miss reporting at our layer)
and Anthropic-specific work becomes orphaned if we ever re-route
xhs-copy-writer to Claude.
```

**Cross-cutting recommendation**: do solution 2 for both agents
(extend prompts to ~1500 tokens with synthetic examples). It works
on OpenAI auto-cache today AND gives us the marker target for
Claude later. Output quality lift > cache savings on first 100 calls.

---

## 4. 实装方案 A 的代码改动清单

| File | LoC delta | Why |
|---|---|---|
| `service/runtime.go` | ~30 | Body builder rewrite for `executeAgent` (system msg → array-of-typed); helper `buildCachedSystemMessage(prompt string, enabled bool)`; same fix in `executeAgentWithImages` (already array-of-typed for user, just extend system) |
| `service/types.go` | ~5 | Optional: add `CacheEnabled bool` to `Agent` struct + DB column `gtk_agent.cache_enabled TINYINT(1) DEFAULT 0`. Lets us toggle per agent without redeploy. |
| `service/seed.go` | ~3 | Set `CacheEnabled = true` on the 2 seed agents once cached prompts are extended to ≥1024 tokens (gates rollout) |
| `service/migration.go` | ~5 | ALTER TABLE migration for `gtk_agent.cache_enabled` |
| `adapter/claude/types.go` | ~5 | Add `CacheControl *struct{Type string} \`json:"cache_control,omitempty"\`` to `MessageContent` (Anthropic format on system message blocks). NewAPI's Claude adapter currently passes through whatever JSON body it receives — verify by integration test. |
| `adapter/openai/types.go` | ~5 | Same field on `MessageContent`. OpenAI ignores it (no-op), DeepSeek likewise — but it future-proofs and unifies. |
| `adapter/claude/chat.go::GetChatBody` | ~10 | Currently `System` is plain `string` — Anthropic's cache control needs `system` to be an **array** of typed blocks. Change `ChatBody.System` from `string` to `interface{}` so we can pass array when cache hint is set. |
| `service/runtime_test.go` | ~80 | 5 new test cases: cache disabled body shape unchanged / cache enabled adds marker / Claude vs OpenAI shape difference / extension path with images / fallback when prompt <1024 tokens (don't add marker — wasted) |
| `service/seed_test.go` | ~10 | Assert seeded agents have correct CacheEnabled value + prompt length |
| **NEW** `service/cache_metric.go` (optional) | ~60 | Records `cache_creation_input_tokens` + `cache_read_input_tokens` from response if present. Stores in `gtk_cache_metric (order_no, agent_slug, hit, tokens_cached, tokens_read, ts)` for ROI reporting. |
| **NEW** migration: `gtk_cache_metric` table | 1 SQL | only needed if we want server-side ROI tracking. Agent C's analysis decides cost/value. |

**Total**: ~213 LoC across 6 existing files + 1 optional new file +
~90 test LoC. Tractable for a single PR.

**Hidden risk**: `adapter/claude/chat.go` `GetChatBody` returns
`ChatBody.System` as `string`. Anthropic API's `cache_control` requires
system to be `[{"type":"text","text":"...","cache_control":{...}}]`.
Switching `System` field type from `string` to `interface{}` ripples to
every consumer of the Claude adapter. Need to check that NewAPI's
Claude path doesn't choke. This is the biggest unknown — open question
for Agent A.

---

## 5. Codex spec 拆分

After 方案 A is locked, suggested workers (each = 1 dispatch):

| # | Worker | Est | Notes |
|---|---|---|---|
| W1 | `service/runtime.go` body schema rewrite + tests (cache OFF as default; flag-gated) | 3-4h | Pure scaffolding, no behavioural change yet |
| W2 | `service/seed.go` + `seed_test.go` extend prompts to ~1500 tokens with synthetic 大理民宿 examples | 4-6h | Most product-impactful — quality + cache marker target |
| W3 | `adapter/claude/{types,chat}.go` schema flex (System = interface{}, MessageContent.CacheControl) | 2-3h | Verify NewAPI Claude path still serializes correctly |
| W4 | `adapter/openai/types.go` MessageContent.CacheControl pass-through field | 1h | Trivial; no-op on OpenAI but unblocks future routing |
| W5 | `gtk_cache_metric` table + `service/cache_metric.go` ROI capture | 2-3h | Optional — only if Agent C says ROI is worth measuring |
| W6 | E2E smoke: enable flag in staging, fire 10 calls per agent, verify usage.cache_creation_input_tokens > 0 | 2h | Run on api.greentokey.com against real NewAPI |
| W7 | Founder-facing dashboard widget showing cache hit rate per agent | 2-3h | Stretch — depends on W5 |

Total: ~16-22h dev. Sequencing: W1+W3+W4 in parallel (independent
files), then W2 (depends on agent prompt rewrite), then W6 smoke,
then W5 + W7 if ROI proves out.

---

## 6. Gotchas

### Cache 跟 model 绑定还是 cross-model 共享?
**Anthropic**: cache key = exact model + exact prefix. Bumping model
version (claude-3-5-sonnet → claude-4) invalidates cache. greentokey
seed pins `gpt-4o-mini` and `deepseek-r1` → only OpenAI auto-cache
applies today. If we rotate models on agent_seeds.Version bump (which
already happens, see seed.go:41 comment), cache resets — by design,
since prompt also changes.

### Cache TTL 过期怎么处理?
Anthropic: 5 min default (paid tier sometimes 1h). For monthly
subscriber pattern (mansu-managed-orchestrator running daily), the
cache will mostly miss day-to-day — only useful within a single
batch run. Solution: make the orchestrator burst its calls
(generate 30 captions in one HTTP cycle, not 30 separate cycles).
**Architectural note for runtime.go**: today RunOrderAPI is one
order = one call. Subscription billing means many orders per
month per customer, all flowing through different HTTP requests
with seconds-to-hours between them. **Cache TTL will dominate
our hit rate, not prompt length.**

### Multi-tenant: do different 民宿 share cache?
Yes — cached prefix is keyed only on (model, prefix bytes). Two
different 民宿 hitting the same agent share the same cached prefix
(because system prompt is identical). User input is in a separate
message and is NOT cached. **Privacy**: zero customer data leaks
through cache because user input isn't in the cached portion.
Confirm this in solution 2 examples — synthetic only.

### Ratelimit: cache reads still count against TPM?
Yes. Anthropic counts `cache_read_input_tokens` toward TPM at 0.1×
rate; OpenAI auto-cache counts cached reads at 0.5× toward TPM.
For mansu-managed bursting 30 calls in a tight loop, this matters.
runtime.go has no per-customer ratelimit today; if cache pushes
us into the tight-loop pattern, may need per-runner-key rate
budget.

### NewAPI: forward cache_control or strip?
**Open question — for Agent A.** We need confirmation that:
1. NewAPI v0.13.x's `chat/adapter/claude/chat.go::GetChatBody`
   serialises a non-string `System` field correctly when we change
   the type
2. NewAPI's request transformer doesn't sanitise unknown fields like
   `cache_control` out of the JSON before forwarding upstream
3. NewAPI returns `usage.cache_creation_input_tokens` /
   `cache_read_input_tokens` in the response back to us (so we can
   record metrics in `gtk_cache_metric`)

If any of these is "no", we're forced into 方案 B (fork NewAPI)
or accept blind cache (works but no observability).

### v0.15 multimodal interaction
`executeAgentWithImages` mixes text + image_url in the user message.
Cache marker on the system message is unaffected. **But**: if Agent A
finds Anthropic supports caching images in the user message
(experimental?), and our xhs-copy-writer often re-uses the same 大理
landmark photos across customers, that could 10x ROI. Worth a footnote
to Agent C's economic analysis.
