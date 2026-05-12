# Token-Sale Code Review — 2026-05-13

**Scope**: 17 files (data flow: browse → checkout → webhook → redeem → provision → renew → usage)
**Reviewer**: gsd-code-reviewer (claude-opus-4-7 1M)
**Standards**: CLAUDE.md (global + project) + HANDOFF.md §10 + L23 architecture + TECHDEBT.md
**Constraint**: protect shipped-and-working code; flag only defects + measurable risks

---

## Summary

- **9 findings total**
- **Severity**: 1 BLOCKING / 3 HIGH / 3 MEDIUM / 2 LOW / 0 NIT
- **Top themes**:
  1. **LS webhook fires 2-3 events per subscription purchase, all routed to `RedeemPlanForOrder` with different `data.id` values** → multi-credit grant for one ¥99 purchase. The body-SHA256 idempotency does NOT catch this (different events = different bodies). This is the only BLOCKING item.
  2. **Hupijiao plan-path bypasses `commerce.ClosePaymentSession`** — the payment session row stays `pending` until the cron expires it, even after successful redeem. Reconciliation drift only, no money impact, but it breaks the L20 "self-validating commerce backbone" invariant.
  3. **Login redirect `?next=` parameter is not honored by `Auth.tsx`** — after 401-and-login, user lands on `/` instead of `/token-plans`, breaking the documented checkout-after-login UX path.

- **Overall code quality verdict: YELLOW**. The code is well-structured, idempotency primitives are mostly in place (`gtk_user_plan.order_id UNIQUE`, `gtk_webhook_event` SHA256 PK, monotonic `expired_at`/`renews_at` guards, hupijiao HMAC verify), and the L23 product-type boundary is respected. But BL-01 is a real double-credit-grant on the LS path that ships money loss the first time a USD customer hits multiple webhook events. The mainland path (hupijiao, single callback) is unaffected, which mitigates blast radius for the GTM-priority customer segment. Fix BL-01 before the first paying USD customer; everything else is post-launch.

---

## Architecture invariants (L23) — verification

| Invariant (architecture doc §) | Status | Evidence |
|---|---|---|
| §4 shared commerce backbone (one payment path) | **RESPECTED** | `payment/checkout.go:88`, `payment/hupijiao_checkout.go:88` both call `commerce.OpenPaymentSession`. Same `ProductToken` discriminator. |
| §8 `product_type` discriminator on gtk_plan | **RESPECTED** | `plans/migration.go:174` adds the column with default `'token'`; `tokenPlanSeed` writes `productType='token'`. |
| §9.1 refund/cancel decision table | **RESPECTED (token path only)** | `payment/dispatch_token.go:250` `RefundTokenPlan` exists; calls `commerce.RevokeEntitlement` which is idempotent. The hupijiao plan-redemption path has no equivalent refund hook yet — TECHDEBT-track for future. |
| §14 one-line rule (Token sells usage / Service sells outcomes / shared backbone) | **RESPECTED** | The plan-code dispatch in `payment/lemonsqueezy.go:402` is checked AFTER service-order branch (defense-in-depth comment at line 384). No service-order code reads `plan_code` or vice versa. |
| §16 1:1 binding `gtk_newapi_binding (coai_user_id)` | **RESPECTED** | `newapi/provision.go:90` uses `LoadBinding` keyed on coai_user_id; existing binding short-circuits to top-up rather than create. |
| §17 L20 automation prescription (consistency-check cron + check-pending-provisioning) | **RESPECTED for provisioning** | `newapi/retry_worker.go` ships full backoff + state machine; `bin/check-pending-provisioning.sh` referenced. Refund consistency check NOT yet shipped — already in P0-E TECHDEBT. |
| §18 PKG-M1-① recurring renewal | **PARTIALLY RESPECTED** | Renewal fires through `subscription_payment_success` event which redeems again — generates a fresh `gtk_user_plan` row for month 2. Good design. **BUT** see BL-01: month-1 also gets multi-redeemed by sibling events. |
| L18 改钱 GO (idempotent commerce mutations) | **VIOLATED at LS plan path** | See BL-01. `RedeemPlanForOrder` is idempotent **per orderID**, but the dispatcher uses 3 DIFFERENT orderIDs for a single purchase. |

---

## Findings

### BLOCKING — must fix before next USD-customer deploy

#### BL-01: LS plan dispatch routes 2-3 webhook events to `RedeemPlanForOrder` per purchase → multi-credit grant

**File:Line**: `payment/lemonsqueezy.go:407-417`

**Why it matters**: LemonSqueezy fires THREE separate webhook events per first-month subscription purchase, all carrying the same `custom_data.{type, plan_code, user_id}`:

1. `order_created` — `data.id` = order UUID (e.g. `ord_xxx`)
2. `subscription_created` — `data.id` = subscription UUID (e.g. `sub_yyy`)
3. `subscription_payment_success` — `data.id` = invoice/payment UUID (e.g. `inv_zzz`)

The dispatch switch at line 408 accepts all three event names and passes `p.Data.ID` straight into `auth.RedeemPlanForOrder(..., p.Data.ID)`. Each call uses a DIFFERENT orderID, so `gtk_user_plan.order_id UNIQUE` does NOT dedup. The `gtk_webhook_event` SHA256 idempotency table also doesn't help — three different bodies hash to three different keys.

**Customer-visible impact**: a single ¥99 (~$14) USD purchase grants `3 × 5000 = 15,000 credits` instead of `5,000`. Founder loses 67% margin per USD sale silently.

**Mainland (hupijiao) path is unaffected** — hupijiao sends one callback per purchase with `transaction_id` as the dedup key, and `RedeemPlanForOrder` is idempotent per (orderID).

**Reproduction**: deploy current code; have an LS test-mode subscription complete in their dashboard; observe `gtk_user_plan` row count for the user = 3 instead of 1, and `quota.quota` = 3× the plan's `quota_config.quota`.

**Suggested fix direction**: choose ONE LS event to drive redemption. Two viable options:

- **Option A** (recommended): drive redemption from `order_created` only, ack the other two events with a log. `order_created` fires for every renewal too (each renewal LS creates a new order), so this preserves the "month N → new gtk_user_plan row" semantics.
- **Option B**: keep the multi-event accept, but derive a stable `orderID` for purpose of `gtk_user_plan.order_id` — e.g. the `subscription_id + period_index`. Riskier (requires period bookkeeping) and `data.id` is no longer the natural key.

A second test fixture must be added: same `samplePlanPayload` body with `eventName = subscription_created` and again with `subscription_payment_success`, asserting only one of them inserts a `gtk_user_plan` row. The existing `TestHandleWebhook_PlanCustomDataRedeemsQuota` only fires `order_created` and so misses this.

**Effort**: S (3-line dispatch switch tightening + 2 test fixtures).

---

### HIGH — fix soon (post-launch, before scale)

#### HI-01: Hupijiao plan-redemption path bypasses `commerce.ClosePaymentSession`

**File:Line**: `service/webhook_handler.go:238-258`

**Why it matters**: When the hupijiao callback's `attach="plan:CODE:user:ID"` route fires, it calls `auth.RedeemPlanForOrder` and returns "success" without ever calling `commerce.ClosePaymentSession(sessionID)`. The session row inserted at checkout (`payment/hupijiao_checkout.go:88`) stays in `pending` status until `ExpirePaymentSessions` cron flips it to `expired` — **even though the customer paid and got credits**.

Consequences:
- Founder dashboard / admin view shows "paid" plan + "expired" payment session = looks like fraud at first glance.
- `bin/check-refund-consistency.sh` and related L20 monitor scripts (architecture §17) will fire false-positive alerts because the session ledger and entitlement ledger disagree.
- No customer-visible impact, no money lost.

The LS path doesn't have this bug because `dispatch_service.go` (for service orders) calls `commerce.ClosePaymentSession` explicitly; but the LS *plan* path (`lemonsqueezy.go:402-418`) ALSO doesn't call `ClosePaymentSession`. So LS plans share this defect too.

**Suggested fix**: in both webhook handlers' plan-redemption branches, after `RedeemPlanForOrder` succeeds, parse the `session_id` out of attach (hupijiao) or `custom_data.greentokey_session_id` (LS) and call `commerce.ClosePaymentSession`. The hupijiao attach already encodes `:session:<id>` for this purpose (`payment/hupijiao_checkout.go:176`) but the callback handler doesn't parse it.

**Effort**: S (~15 LOC in two call sites + 1 test per).

---

#### HI-02: `Auth.tsx` ignores `?next=` query param after login

**File:Line**: `app/src/routes/Auth.tsx:61, 123` (and `app/src/routes/TokenPlans.tsx:131, 169` for the caller)

**Why it matters**: TokenPlans.tsx redirects unauthenticated buyers to `/login?next=/token-plans` (lines 131, 169). The intent is "after login, return to the checkout page". But `Auth.tsx::Login.onSubmit` and `DeepAuth` both unconditionally `router.navigate("/")` on success — `next` is parsed nowhere. Result: user clicks buy → 401 → login → lands on home page → has to navigate back to `/token-plans` → re-click buy. Friction at the worst possible moment of the funnel.

**Reproduction**: incognito browser → visit `/token-plans` → click "微信 / 支付宝 ¥99 立即开通" → get bounced to `/login?next=%2Ftoken-plans` → log in → observe URL = `/` instead of `/token-plans`.

**Suggested fix direction**: in `Auth.tsx::Login.onSubmit` (line ~123) and `DeepAuth.useEffect` (line ~61), read `getQueryParam("next")`, validate it's a same-origin path (starts with `/`, no `//`), and use it as the navigate target if present. Default to `/` only when absent or invalid.

**Effort**: S (~10 LOC in Auth.tsx + 1 unit test for next-param validation).

---

#### HI-03: Hupijiao success status string list is incomplete and untested

**File:Line**: `service/webhook_handler.go:230`

**Why it matters**: The handler accepts only `"OD" | "PAYED" | "PAID"` as paid-status indicators (line 230). Per xunhupay.com API spec evolutions, the field is sometimes named differently (`status`, `trade_status`) and the success values vary across hupijiao API versions:
- v1.1 API: paid status is `"OD"`
- Some newer responses use `"WP"` for unpaid, `"OD"` for ok, but in the wechat-pay-async-notify path the success indicator is `payed=true` boolean rather than `status`.

If hupijiao ships a backend update changing the status string (which is operator's hupijiao merchant version, not greentokey's), the callback handler silently 200s without redeeming, and the customer's money is taken without the plan activating. The fallback `c.String(http.StatusOK, "success")` at line 234 means hupijiao stops retrying and the customer is silently broken.

No test covers the unhappy path where status is unrecognized and redeem should NOT fire — just confirm the current allowlist is what's actually shipped by xunhupay.com today.

**Reproduction**: hard to trigger without a hupijiao test sandbox; verify by re-reading current xunhupay.com docs vs `service/webhook_handler.go:230` allowlist before each merchant API version bump.

**Suggested fix direction**: (a) make the allowlist configurable via viper key `hupijiao.paid_statuses` with the current 3-value list as default; (b) when status is unrecognized, log at WARN level (currently logs at INFO with `globals.Info`) so SRE catches a hupijiao API drift before customers complain; (c) add a `bin/check-hupijiao-callback-allowlist.sh` that polls the merchant version weekly and alerts on drift.

**Effort**: M (~30 LOC + viper key + 1 monitoring script + integration test against a captured payload fixture).

---

### MEDIUM — fix when convenient

#### MD-01: `payment/hupijiao_helpers.go` is a hand-copy of `service/checkout.go` helpers; drift undetected

**File:Line**: `payment/hupijiao_helpers.go:1-12` (self-documented motivation)

**Why it matters**: The author explicitly chose to duplicate `hupijiaoSign` + `randomNonce` + `postForm` from `service/checkout.go` to avoid an import cycle (correct call). But there's no test that exercises BOTH implementations against the same input and asserts byte-equality. If a future commit fixes a bug in one copy without touching the other, every callback signed by the diverged copy will fail HMAC verification at the receiver and either: (a) `service/webhook_handler.go::HupijiaoCallbackAPI` 401s legitimate callbacks (loss of payment confirmation), or (b) the merchant secret gets rotated to make tests pass and an attacker's stale signature is no longer rejected.

The author notes this with "Long-term: move to internal/hupijiao/" — agree, but until then a 5-LOC drift-detector test is the minimum mitigation.

**Suggested fix direction**: in `payment/hupijiao_checkout_test.go`, add a test that imports `service.HupijiaoSignForTesting` (or the equivalent unexported test helper) and asserts `payment.hupijiaoSign(p, s) == service.HupijiaoSign(p, s)` over a 3-input fixture. If the symbol isn't exported, this can be a test in `service/checkout_test.go` instead that imports nothing from `payment` (no cycle).

**Effort**: S (1 test, 10 LOC).

---

#### MD-02: `usage/writer.go::WriteUsageLog` swallows `lookupActivePlanID` error silently

**File:Line**: `usage/writer.go:125-136`

**Why it matters**: `lookupActivePlanID` returns `sql.NullInt64{}` on ANY error (DB read failure, table-missing, scan error, etc.). The caller has no way to distinguish "user genuinely has no active plan" from "DB hiccupped during read". For free-tier or pay-as-you-go usage this is correct behavior, but for a paid token-plan user, a transient DB read failure causes usage to be logged with `plan_id=NULL`. Later cost-ledger consistency checks (architecture §17 `bin/check-cost-ledger-consistency.sh`) will see a paid user with usage rows that don't link to their plan → false-positive alert; reconciliation requires manual joining.

This isn't a money-loss bug today (the usage row still has `user_id`, `cost_cents`, `client_charge_micro`), but it makes per-plan margin analysis brittle.

**Suggested fix direction**: log the error path at debug level so SRE can grep for it; consider promoting to warn if the rate of `plan_id=NULL` usage rows for users with active subscriptions exceeds some threshold. No code-structure change needed.

**Effort**: S (3 LOC, 1 log line).

---

#### MD-03: `seedTokenPlans` is silently a no-op on SQLite; ops-pre-seed in MySQL also a silent no-op

**File:Line**: `plans/migration.go:137-165`

**Why it matters**: Two distinct ops scenarios that both result in confusing silent no-ops:

1. **SQLite path** (lines 138-140): `if globals.SqliteEngine { return nil }`. Documented and justified (test-collision avoidance), but anyone running greentokey locally with SQLite for dev gets a checkout that 400s on plan_code=token-99 because the seed silently didn't run.
2. **MySQL ops pre-seed** (lines 142-150): If an ops engineer manually `INSERT INTO gtk_plan` with `code='token-99'` BEFORE first v0.22 boot, the seed skips that code via the row-exists guard. Reasonable, but means ops can "fork" the seed price/quota by 1 byte and have it silently win forever. No log line surfaces that "seed for `token-99` was skipped because already exists".

**Suggested fix direction**:
- (a) Add a single info-level log line per skipped code: `globals.Info("plans.seedTokenPlans: code %s already exists, skipping seed", code)`. Visibility cost: ~5 LOC.
- (b) For SQLite dev: have a separate `SeedDevTokenPlans(db)` helper that the dev runner calls explicitly, or add a viper flag `plans.seed_on_sqlite=true` for opting in.

**Effort**: S (5 LOC for the log; opt-in flag is M).

---

### LOW — leave for now

- **LO-01** `payment/hupijiao_helpers.go:67-75` `randomNonce`'s fallback path uses `time.Now().UnixNano()` formatted as hex. The collision-tolerance comment is correct (signature is the actual security boundary), but the format result is 16 chars only if UnixNano fits in 16 hex chars (it always does on 64-bit platforms — 16 hex = 64 bits — but the safety isn't asserted). Leave as-is; the fallback path is unreachable on Linux production hosts and adding a test for an unreachable branch is over-engineering.
- **LO-02** `service/webhook_handler.go:269` falls back to `orderNo` as `hupijiaoTxID` when hupijiao doesn't send a separate `transaction_id`. Idempotency still works (the next callback will share the same orderNo), but if hupijiao ever changes their callback shape to send a real `transaction_id` mid-deploy, in-flight orders will have one row keyed by `orderNo` and the retry by `transaction_id` — split-brain risk. Vanishingly small; if it happens the manual reconciliation cost is low.

---

### NIT — none worth listing.

---

## What's good (PROTECT — do not change in future refactors)

These are load-bearing decisions that should NOT be re-touched in subsequent "let's clean this up" passes:

1. **`gtk_user_plan.order_id UNIQUE` + `RedeemPlanForOrder` two-phase idempotency** (`auth/recharge.go:39-53`): the SELECT-then-INSERT-then-fallback-on-dup-key pattern is correct under concurrent webhook delivery. The `TestRedeemPlanForOrder_ConcurrentReentry` test (8-goroutine race) genuinely validates this. Don't simplify to single-query `INSERT ... ON CONFLICT DO NOTHING` without re-checking the test passes — the explicit transaction boundaries are what give the quota recharge atomicity with the plan-binding insert.

2. **HMAC + freshness + body-SHA256 triple defense on LS webhook** (`payment/lemonsqueezy.go:122-208`): `verifySignatureWithFreshness` + `classifyEvent` state machine + 5% probabilistic cleanup is a well-thought-out idempotency architecture. The 5-min freshness window, permissive-on-absent-header fallback, and 503-on-in-flight retry behavior are deliberately tuned to LS's retry semantics. Don't tighten any of these knobs without measuring the LS test-mode delivery patterns first.

3. **Monotonic guards I3 + I4** (`payment/dispatch_token.go:161-175, 228-236`): the `AND (renews_at IS NULL OR renews_at <= ?)` and `AND (expired_at IS NULL OR expired_at <= ?)` clauses prevent out-of-order webhook delivery from rolling back fresher state. Explicitly called out as Codex P1 fix from 2026-04-27. NEVER simplify these UPDATE statements.

4. **Plan-code validation at the boundary** (`payment/checkout.go:62-78` + `payment/hupijiao_checkout.go:61-80` + `plans/lookup.go:37-65`): the customer-supplied `plan_code` query param is looked up against `gtk_plan WHERE is_active=TRUE` before anything is embedded into provider URLs or attach strings. A malicious customer cannot inject `plan_code=admin-secret-100k-credits` because the lookup gates it. This is the right shape for an injection defense — do not "optimize" by trusting the customer-supplied string.

5. **Hupijiao callback CHECK on `provider` allowlist** (`service/webhook_handler.go:61-63`): rejecting unknown payment providers at the MarkOrderPaid boundary stops attackers from arbitrarily inflating order state via a malformed callback. The `manual` extension (D5 + D7) is deliberately allowlisted, not implicit. Don't relax this.

6. **`commerce.RevokeEntitlement` idempotency on already-canceled plans** (`commerce/entitlement.go:279`+ via `payment/dispatch_token.go:250` `RefundTokenPlan`): refund webhook can fire multiple times — the revoke is a no-op the second time. Critical for the 90d refund window scenario.

7. **NewAPI provisioning retry queue with bounded backoff** (`newapi/retry_worker.go` entirety): NewAPI transient outage doesn't lose the customer's payment — the gtk_user_plan row already says active, and the worker drains the queue with 1m / 5m / 15m / 1h / 6h backoff schedule. The whole drain-with-jitter pattern is well-designed.

---

## TECHDEBT cross-ref

- **TECHDEBT-confirmed P0-E** (`docs/TECHDEBT.md:26`): the L23 commerce backbone is locked in DEV-PLAN but Stage B hasn't shipped the consistency-check crons (architecture §17). HI-01 (hupijiao plan path bypasses `ClosePaymentSession`) is exactly the kind of drift the missing `bin/check-refund-consistency.sh` would catch. Not a NEW debt item — confirms the existing P0-E need.
- **TECHDEBT-confirmed P0-G** (line 28-29): the missing prod deploy of menu-bar token-meter is orthogonal but related — once it ships, the dashboard view will surface BL-01's multi-credit grants visually to founder before any customer complains. Don't conflate; they're independent items.
- **No P2 confirmations from this review** — none of the existing P2 items overlap the 17-file scope reviewed here.

---

## Out-of-scope flags

Things noticed that warrant a separate review pass (NOT addressed here):

1. **`commerce/entitlement.go::GrantEntitlement` (esp. the `ProductToken` branch)** — referenced from this scope but the internals weren't reviewed. The `RevokeEntitlement` half is reachable from `dispatch_token.go::RefundTokenPlan`, so its idempotency invariants need their own audit before refund flows go live for USD customers.
2. **`commerce/session.go::ClosePaymentSession`** — the disambiguation read for "rows==0" handles 3 states (already-paid, terminal, not-found) gracefully. But the not-found path returns nil (intentionally tolerant of webhook-arrives-before-session-commit race). This needs a paired test that demonstrates the race actually self-heals via the downstream grant-entitlement idempotency claim in the comment.
3. **`newapi/client.go` admin REST API calls** — `ProvisionForPlan` chains `SearchUserByUsername` → `CreateUser` → `UpdateUserQuota` → `CreateToken`. None of these are reviewed here. If any one returns an error mid-chain, partial state lands (e.g. NewAPI user created but no token, or token created with wrong quota). The retry queue catches the WHOLE chain failure but not partial-chain failures. Worth a separate audit specifically of the half-success modes.
4. **`commerce/admin_ops.go::MarkPaid` for ProductToken** — there's a comment at line 55 ("for ProductToken use the LemonSqueezy dashboard") suggesting the admin manual-mark-paid path is intentionally NOT available for tokens. That's a defensible product decision but should be verified — concierge flow for a high-touch USD customer might want it.
5. **The `gtk_plan` MySQL DDL for `quota_grant`** (`plans/migration.go:181-191`): adds the column as `BIGINT NULL`, but the `tokenPlanSeed` always writes a positive value. The seed could have its NOT NULL constraint added in a later migration to prevent ops-side `NULL` inserts that would break `RedeemPlanForOrder`'s NULL-tolerance path.

---

_Reviewed: 2026-05-13_
_Reviewer: gsd-code-reviewer (claude-opus-4-7 1M)_
_Depth: deep (cross-file analysis)_
