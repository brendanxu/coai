# Contributing to greentokey CoAI fork

> **Audience**: tana (AI agents) + founder (occasional). This doc captures
> the quality-gate process that grew out of the 2026-05-13 cross-AI code
> review (docs/codex-reviews/18-token-sale-code-review.md) and the
> "ship-then-review" failure pattern it surfaced (BL-01 / HI-01 / HI-02
> all caught by retroactive review instead of pre-deploy review).
>
> **Process invariant**: code that touches money paths MUST pass through
> the gates below BEFORE reaching prod. Other changes (UI, docs, i18n,
> non-money refactors) are SOFTER but still benefit from the gates.

---

## TL;DR — the four gates

For ANY change to greentokey CoAI fork code, before deploy:

1. **`bin/v22-deploy.sh preflight`** — mechanical cross-grep for
   introduce-and-forget bugs (added a producer? must have a consumer).
2. **`bin/v22-deploy.sh preview-review`** — auto-spawn gsd-code-reviewer
   on changed files. Block on BLOCKING findings.
3. **`go test ./... && pnpm exec tsc --noEmit`** — pre-deploy sanity.
   No new RED tests. (CI also enforces this — see
   `.github/workflows/ci-greentokey.yaml`.)
4. **Money-path TDD** — see "Money paths" section below.

If a change is documentation-only or pure-UI-content (i18n strings,
non-behavioral component tweaks), you MAY skip gate 2 + 4 with a
1-line note in the commit message: `[skip-money-gate: doc-only]`.

---

## Money paths — what they are + why they're gated

A "money path" is any code that:
- Reads or mutates customer payment / quota / billing state
- Validates or rejects payment provider webhooks
- Calls upstream model providers and charges credits
- Provisions or revokes access tokens

### Money-path packages (current as of 2026-05-13)

- `payment/` (LemonSqueezy + Hupijiao checkout + dispatch)
- `auth/recharge.go` (RedeemPlanForOrder — atomic plan + quota grant)
- `commerce/` (entitlement, session, cost ledger, dispatch)
- `service/webhook_handler.go::HupijiaoCallbackAPI` (plan redeem branch)
- `newapi/client.go` (provisioning) + `newapi/retry_worker.go`
- `usage/writer.go` (4-class billing)
- `billing/cron.go` (renewal / expiration cron)

### The TDD rule for money paths

Changes to money-path code MUST follow red-then-green:

1. **RED first**: write the test for the new behavior FIRST. Confirm
   it fails before any production code change.
2. **GREEN second**: minimum code change to make the test pass.
3. **REFACTOR third**: only after green, can you simplify.

Why: BL-01 (LS triple-redeem) was a textbook case of NOT doing TDD.
The dispatcher accepted 3 event types. If I had written
`TestHandleWebhook_PlanCustomData_SubscriptionCreatedDoesNotRedeem`
BEFORE editing the dispatch case, the bug would have been impossible
to ship — the test would have caught it at the RED stage.

### Money-path exception

If you're fixing a bug that was JUST discovered in production, the
RED test STILL goes first. Write the test that reproduces the bug,
confirm RED, then fix. The 2 BL-01 fixture tests added in commit
`562ec68` follow this pattern retroactively.

### Out-of-money-path changes

Pure UI / i18n / docs / non-money refactors don't require TDD.
Defaults to a sanity-check `go vet ./...` and `pnpm exec tsc --noEmit`.

---

## The introduce-and-forget pattern (and how preflight catches it)

When you add a new field to a producer's output (URL param, custom_data
key, attach segment, query string), the corresponding consumer parser
MUST exist or be added in the same change. Three real cases caught by
review on 2026-05-13:

- **HI-01**: `payment/hupijiao_checkout.go` added `:session:SESSID`
  segment to the hupijiao `attach` field; `service/webhook_handler.go`'s
  parser still required exactly 4 colon-separated parts. Result:
  callbacks containing the new segment got rejected.
- **HI-02**: `app/src/routes/TokenPlans.tsx` started redirecting 401s
  to `/login?next=/token-plans`; `Auth.tsx::onSubmit` and `DeepAuth`
  both ignored the `next` query param and always navigated to `/`.
  Result: dropping users at the wrong place mid-checkout.
- **BL-01** (related pattern): `payment/lemonsqueezy.go` accepted 3
  event names through the same dispatch path, but
  `auth.RedeemPlanForOrder`'s idempotency assumed 1 event per purchase.

The `preflight` step (added to `bin/v22-deploy.sh`) cross-greps these
patterns. If it expands the producer regex but finds zero consumer
regex hits, exit 1.

Add new producer/consumer pairs by editing the `patterns` associative
array at the top of the `preflight)` case in `v22-deploy.sh`.

---

## Pre-deploy checklist

Before any deploy (paste copy-ready):

```bash
# 0. Verify no uncommitted changes
git status
# Should be clean. If WIP, stash or commit.

# 1. preflight (cross-grep)
bash bin/v22-deploy.sh preflight

# 2. preview-review (auto-spawn reviewer agent)
bash bin/v22-deploy.sh preview-review
# Paste briefing into Claude/Codex, get VERDICT line back

# 3. Tests + tsc
go test ./... -count=1 -vet=off | grep -E "^FAIL|^ok " | head -25
cd app && pnpm exec tsc --noEmit && cd ..

# 4. If VERDICT: GREEN, deploy normally
bash bin/v22-deploy.sh rsync
bash bin/v22-deploy.sh build
bash bin/v22-deploy.sh up
bash bin/v22-deploy.sh smoke
```

---

## When to update this doc

- Adding a new money-path package → list it under "Money-path packages"
- Adding a new introduce-and-forget pattern → add to the preflight
  patterns array + describe here
- Finding a new failure mode that pre-deploy gates missed → add a new
  gate, document the rationale in this doc + reference the REVIEW.md
  finding

This doc is meant to grow as we hit new patterns. Forward-only.

---

## Linked artifacts

- `docs/codex-reviews/18-token-sale-code-review.md` — original review
  that surfaced BL-01 / HI-01 / HI-02
- `bin/v22-deploy.sh` — deploy orchestrator with preflight + preview-review
- `.github/workflows/ci-greentokey.yaml` — CI gates (when present)
- `.githooks/pre-commit` — local commit-time gates (run after
  `bin/install-hooks.sh`)
