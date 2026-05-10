# PKG-M1-1+2 Done Report

## Commit Status

1. `0339b96` - `feat(auth): RedeemPlanForOrder - provider-agnostic L2 plan redeem`
   Adds `auth.RedeemPlanForOrder`, atomic gtk_user_plan + quota recharge, idempotency tests, and fresh-schema UNIQUE `gtk_user_plan.order_id`.
2. `bb23737` - `feat(payment): LS webhook L2 plan dispatch`
   Routes LemonSqueezy `custom_data.type=plan` paid events to the shared redeem worker and keeps L3/legacy paths intact.
3. `161ed70` - `feat(service): hupijiao callback L2 plan dispatch`
   Parses `attach=plan:CODE:user:ID` after Hupijiao signature + paid-status verification and calls the shared redeem worker.
4. Docs report commit - this file is committed as the fourth PKG-M1-1+2 commit; exact hash is available from `git log -1` after commit creation.

## Tests

`auth/recharge_test.go`: 10 PASS

- Happy path creates one `gtk_user_plan` row and increments quota.
- Idempotent re-run with the same order leaves quota unchanged.
- Concurrent re-entry with the same order creates one plan row and one quota grant.
- Inactive plan errors with no DB changes.
- Unknown plan code errors with no DB changes.
- Non-positive quota config errors.
- Malformed quota config JSON errors.
- `expire_at` is approximately now + `duration_days`.
- Existing quota is incremented instead of replaced.
- Quota write failure rolls back the `gtk_user_plan` insert.

Additional coverage:

- `payment/lemonsqueezy_test.go`: plan custom_data webhook creates `gtk_user_plan`, increments quota, and skips legacy subscription upsert.
- `plans` migration test suite passes after making fresh `gtk_user_plan.order_id` unique.

Verification:

```bash
GOCACHE=/tmp/codex-go-cache go build ./... 2>&1 | grep -v "warning\|libwebp"
GOCACHE=/tmp/codex-go-cache go test ./auth/... ./payment/... -count=1 -vet=off 2>&1 | grep -E "FAIL|ok "
GOCACHE=/tmp/codex-go-cache go test ./plans -count=1 -vet=off
```

Observed:

```text
ok   chat/auth
ok   chat/payment
ok   chat/plans
```

Grep evidence:

```text
auth/recharge.go:31:func RedeemPlanForOrder(...)
payment/lemonsqueezy.go:296:auth.RedeemPlanForOrder(...)
service/webhook_handler.go:224:auth.RedeemPlanForOrder(...)
auth/recharge.go:100:ON DUPLICATE KEY UPDATE quota = quota + ?
auth/recharge.go:104:ON CONFLICT(user_id) DO UPDATE SET quota = quota + ?
```

## Issues Hit

- `globals.ExecDb` only accepts `*sql.DB`, not `*sql.Tx`, so the recharge worker uses explicit MySQL/SQLite UPSERT SQL inside the transaction instead of the existing quota helper.
- The prompt used both `plan_id` and `plan_code` for LS custom_data. The LS dispatcher accepts canonical `plan_code` plus `plan_id` as a compatibility alias, both treated as `gtk_plan.code`.
- Hupijiao callback parsing previously read `PostForm`; L2 attach support uses `Request.Form` after `ParseForm()` so GET query callbacks and POST form callbacks share the same verified signing path.
- `gtk_user_plan.order_id` was a plain index in fresh migration code. It is now unique for fresh schemas so the DB backs the redeem idempotency contract.

## Known Limitations

- The migration change makes fresh `gtk_user_plan.order_id` unique. If an already-created environment has the older non-unique index, ops should verify and add a unique index after confirming there are no duplicate `order_id` rows.
- LS redemption uses `p.Data.ID` as the provider order id, matching the prompt. If LemonSqueezy later sends renewal payment events where `data.id` is not unique per payment, the checkout metadata should include a provider payment/order id and pass that as `orderID`.

## Recommended Follow-Up

- Add checkout-side generation for Hupijiao `attach=plan:CODE:user:ID` and LS `custom_data.type=plan`.
- Add an ops migration/check script for existing `gtk_user_plan.order_id` uniqueness before production rollout.
- Add provider webhook fixtures once real LemonSqueezy and Hupijiao L2 payloads are captured.
