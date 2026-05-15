#!/usr/bin/env bash
# v22-deploy.sh — Mac-side deploy orchestrator for v0.22-token-launch.
#
# Wraps the multi-step deploy in subcommands so founder can paste a single
# short `!bash bin/v22-deploy.sh <step>` line per phase, avoiding the
# Claude Code input-box line-wrap that breaks long ssh/rsync commands.
#
# Run from anywhere (script self-anchors via ${BASH_SOURCE%/*}/..).
#
# Steps (run in order, founder verifies between each):
#   bin/v22-deploy.sh rsync   — back up current coai-source on VPS, then
#                               push fresh v0.22 source over rsync (excludes
#                               .git, node_modules; keeps app/dist pre-built)
#   bin/v22-deploy.sh build   — sudo docker build of v0.22.0-token-launch
#                               image on VPS using Dockerfile.split (Go-only,
#                               assumes Mac-built dist already in coai-source)
#   bin/v22-deploy.sh up      — bump docker-compose.yml image tag from
#                               v1.0.0-cache-billing-ux to v0.22.0-token-launch,
#                               then docker compose up -d coai. Watches boot
#                               for 30s + tails recent logs to surface migration
#                               issues (PKG-M1 chain + seedTokenPlans run here)
#   bin/v22-deploy.sh smoke   — curl prod endpoints + check token-99 row in
#                               gtk_plan (verifies seedTokenPlans actually ran)
#   bin/v22-deploy.sh rollback — emergency revert: bump tag back to
#                                v1.0.0-cache-billing-ux + compose up. Keeps
#                                the v0.22 image around for a follow-up retry.

set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VPS_HOST="greentokey"  # ssh alias (~/.ssh/config)
VPS_PATH="/opt/greentokey/coai-source"
PREV_TAG="v0.29.5-admin-polish"
NEW_TAG="v0.30.0-phase-1-excision"
COMPOSE_FILE="/opt/greentokey/docker-compose.yml"

step="${1:-help}"

case "$step" in
  preflight)
    # Layer 1B (REVIEW.md follow-up 2026-05-13): defends against
    # "introduce-and-forget" bug pattern (HI-01 + HI-02 root cause).
    # If a recent commit introduces a new query-param / custom_data key
    # / attach segment, mechanically grep for ≥1 consumer. Zero hits =
    # likely forgot to wire upstream — block deploy.
    #
    # Run automatically as part of `preview-review`; can also be run
    # standalone via `bin/v22-deploy.sh preflight`.
    echo "==> Preflight: cross-grep recent protocol-field introductions"
    fail=0

    # Patterns added between $PREV_TAG and HEAD that introduce upstream
    # contracts (query params, custom_data keys, attach segments).
    # For each, the producer code MUST have ≥1 consumer in the repo.
    #
    # Structure: parallel arrays (producer_patterns + consumer_patterns +
    # labels). Was originally a bash associative array but `?` and `=` in
    # the keys triggered glob/assignment parsing — caught by our own
    # dogfood smoke 2026-05-13.
    labels=(
      "next_query_param"
      "ls_plan_custom_data"
      "hupijiao_attach_plan"
      "greentokey_session_id"
    )
    producer_patterns=(
      'getQueryParam.*next'        # "?next=" in URL → producer in TokenPlans.tsx etc.
      'checkout..custom...type'    # custom_data[type]=plan emitted at checkout
      'attach=plan:'               # attach=plan:CODE:user:ID in hupijiao request
      'greentokey_session_id'      # session id segment in LS custom_data + hupijiao attach
    )
    consumer_patterns=(
      'getQueryParam[[:space:]]*\("next'                   # Auth.tsx::nextPathFromQuery
      'planCodeFromCustomData|custom\[.type.\].*plan'      # dispatch reads type=plan
      'HasPrefix\(attach, "plan:"\)'                       # service/webhook_handler.go parser
      'sessionIDFromCustomData|parts\[4\].*"session"'      # session_id consumers
    )

    # set -e is enabled at file top; grep returns 1 on no-match which
    # would kill the script. Wrap each grep | head in `|| true` so the
    # "no producer found, skip" path doesn't trigger an early exit.
    for i in "${!labels[@]}"; do
      label="${labels[$i]}"
      producer="${producer_patterns[$i]}"
      consumer="${consumer_patterns[$i]}"

      # Producer exists somewhere in repo (regex form, exclude vendored)
      producer_hit=$(grep -rlE --include='*.go' --include='*.tsx' --include='*.ts' \
        --exclude-dir='node_modules' --exclude-dir='dist' --exclude-dir='.git' \
        "$producer" "$SRC_DIR" 2>/dev/null | head -3 || true)

      if [ -z "$producer_hit" ]; then
        continue   # producer not present — pattern doesn't apply
      fi

      # Producer exists → consumer regex must also have ≥1 hit
      consumer_hit=$(grep -rlE --include='*.go' --include='*.tsx' --include='*.ts' \
        --exclude-dir='node_modules' --exclude-dir='dist' --exclude-dir='.git' \
        "$consumer" "$SRC_DIR" 2>/dev/null | head -3 || true)

      if [ -z "$consumer_hit" ]; then
        echo "❌ INTRODUCE-AND-FORGET [$label]: producer present, no consumer matching /$consumer/"
        echo "   Producer found in:"
        echo "$producer_hit" | sed 's/^/     /'
        echo "   → Wire the consumer or remove the producer before deploy."
        fail=1
      else
        echo "  ✓ $label: producer + consumer both present"
      fi
    done

    if [ $fail -eq 0 ]; then
      echo "✅ Preflight clean — all introduced protocol fields have consumers"
    else
      echo ""
      echo "Preflight FAILED. Fix the issues above or use --skip-preflight to override."
      exit 1
    fi
    ;;

  preview-review)
    # Layer 1A (REVIEW.md follow-up 2026-05-13): physically prevents
    # ship-then-review pattern that left BL-01 in v0.22.0 for 6 hours.
    # Before any rsync, list changed Go/TS files since PREV_TAG and
    # spawn gsd-code-reviewer agent. If BLOCKING findings, exit 1.
    #
    # This is the gate. The recommended pre-deploy flow becomes:
    #   bin/v22-deploy.sh preflight        # cross-grep defender
    #   bin/v22-deploy.sh preview-review   # auto-review on changed files
    #   bin/v22-deploy.sh rsync            # only if first two pass
    echo "==> Listing files changed since $PREV_TAG"
    # Resolve PREV_TAG to a commit (it may not exist as a git tag yet;
    # fall back to comparing against origin/main).
    if git rev-parse --verify "$PREV_TAG" >/dev/null 2>&1; then
      base="$PREV_TAG"
    elif git rev-parse --verify origin/main >/dev/null 2>&1; then
      base="origin/main"
    else
      echo "❌ No comparison base found ($PREV_TAG or origin/main)"
      exit 1
    fi

    changed=$(git -C "$SRC_DIR" diff --name-only "$base"..HEAD \
      -- '*.go' '*.tsx' '*.ts' 'go.mod' 'go.sum' 2>/dev/null \
      | grep -v '_test\\.' | head -25)

    if [ -z "$changed" ]; then
      echo "ℹ No reviewable code changes since $base — skipping review"
      exit 0
    fi

    echo ""
    echo "Changed files (will be sent to gsd-code-reviewer):"
    echo "$changed" | sed 's/^/  /'
    echo ""
    echo "==> Spawning gsd-code-reviewer (this takes 2-5 minutes)"
    echo ""
    echo "MANUAL STEP REQUIRED: Until we have an agent-cli adapter,"
    echo "this step prints the briefing for you to paste into Claude/Codex."
    echo ""
    echo "─────────────────── BRIEFING ───────────────────"
    cat <<BRIEF
You are reviewing pre-deploy changes for v0.23+ on the greentokey CoAI
fork. Working dir: $SRC_DIR

Files changed since $base:
$(echo "$changed" | sed 's/^/  /')

Per docs/codex-reviews/18-token-sale-code-review.md standards:
- BLOCKING = ship-blocker (money loss, security, panic, data corruption)
- HIGH = fix before scale
- MEDIUM/LOW = defer per founder protect-good-code rule

Output severity-classified findings to STDOUT, NO file writes.
If ANY BLOCKING found → exit 1 in your wrapper.

Run review now. Return the verdict line as the final output:
  VERDICT: GREEN  (0 blocking, deploy approved)
  VERDICT: YELLOW (HIGH only, deploy at founder discretion)
  VERDICT: RED    (BLOCKING present, abort deploy)
BRIEF
    echo "─────────────────── END BRIEFING ───────────────────"
    echo ""
    echo "After review returns, run:"
    echo "  bin/v22-deploy.sh rsync  (if VERDICT: GREEN or YELLOW + founder GO)"
    echo "  bin/v22-deploy.sh rollback  (if VERDICT: RED + already deployed)"
    echo ""
    echo "TODO: replace manual paste with claude-cli adapter once the orchestrator"
    echo "      supports headless agent invocation from shell."
    ;;

  rsync)
    echo "==> 1. Backup current VPS coai-source/ (rollback safety)"
    ssh "$VPS_HOST" "sudo cp -a $VPS_PATH ${VPS_PATH}.bak.v1.0.$(date +%H%M)" \
      || { echo "❌ source backup failed"; exit 1; }
    echo "✅ source backed up"

    echo ""
    echo "==> 2. rsync v0.22 source from Mac → VPS"
    echo "    SRC: $SRC_DIR/"
    echo "    DST: $VPS_HOST:$VPS_PATH/"
    # --rsync-path uses sudo on the VPS so we can write files owned by deploy
    # but also ones with mixed perms. Excludes: .git (not needed at runtime),
    # node_modules (giant + rebuilt-not-needed), worktree's own bin/v22-deploy.sh
    # would otherwise loop back, so we let it through (trivial).
    rsync -az --delete \
      --exclude '.git' \
      --exclude 'node_modules' \
      --exclude '.playwright-mcp' \
      "$SRC_DIR/" "$VPS_HOST:$VPS_PATH/" \
      || { echo "❌ rsync failed"; exit 1; }
    echo "✅ rsync done"

    echo ""
    echo "==> 3. Verify new files exist on VPS"
    ssh "$VPS_HOST" "ls $VPS_PATH/payment/hupijiao_helpers.go $VPS_PATH/payment/hupijiao_checkout.go $VPS_PATH/plans/lookup.go 2>&1" \
      || { echo "❌ new files missing — rsync may have skipped them"; exit 1; }
    echo "✅ new v0.22 files present on VPS"
    ;;

  build)
    echo "==> docker build greentokey-coai:$NEW_TAG (Dockerfile.split, Go-only compile)"
    # -o ServerAliveInterval=30: send keepalive every 30s so long Go builds
    # (2-5 min on 2GB VPS) don't drop the SSH connection mid-way.
    ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=20 "$VPS_HOST" \
      "cd $VPS_PATH && sudo docker build -f Dockerfile.split -t greentokey-coai:$NEW_TAG . 2>&1 | tail -30" \
      || { echo "❌ docker build failed"; exit 1; }
    echo ""
    echo "==> Verify image exists"
    ssh "$VPS_HOST" "sudo docker images greentokey-coai:$NEW_TAG --format '{{.Tag}} {{.Size}} {{.CreatedAt}}'"
    echo "✅ build done"
    ;;

  up)
    echo "==> 1. Backup current docker-compose.yml"
    ssh "$VPS_HOST" "sudo cp $COMPOSE_FILE ${COMPOSE_FILE}.bak.v1.0.$(date +%H%M)" \
      || { echo "❌ compose backup failed"; exit 1; }

    echo ""
    echo "==> 2. Bump image tag $PREV_TAG → $NEW_TAG"
    ssh "$VPS_HOST" "sudo sed -i 's|greentokey-coai:$PREV_TAG|greentokey-coai:$NEW_TAG|' $COMPOSE_FILE"
    ssh "$VPS_HOST" "grep 'greentokey-coai' $COMPOSE_FILE"

    echo ""
    echo "==> 3. docker compose up -d coai"
    ssh "$VPS_HOST" "cd /opt/greentokey && sudo docker compose up -d coai 2>&1 | tail -10"

    echo ""
    echo "==> 4. Wait 8s then tail logs (look for migration panics)"
    sleep 8
    ssh "$VPS_HOST" "sudo docker compose -f $COMPOSE_FILE logs --tail 60 coai 2>&1 | tail -60"
    echo ""
    echo "↑ Read the log: should see 'plans.Migrate', 'commerce.Migrate',"
    echo "  'service.Migrate', 'newapi.Migrate' all succeed without panic."
    echo "  If you see 'panic:', call rollback step immediately."
    ;;

  smoke)
    echo "==> 1. /token-plans HTML 200 + JS bundle contains new CTA i18n keys"
    # SPA: curl returns HTML shell + <div id=root>; CTA text only renders
    # after JS hydrates. So we grep the JS bundle directly for our i18n
    # key (preserved verbatim through vite minification) instead of the
    # rendered text. False-negative previously due to grep on raw HTML.
    if ssh "$VPS_HOST" "grep -lq 'token.plan.cta.cny\\|token.plan.cta.usd' /opt/greentokey/coai-source/app/dist/assets/*.js" 2>/dev/null; then
      echo "✅ /token-plans bundle contains dual-rail CTA i18n keys"
    else
      # Fallback: HTML still 200 means SPA shell serves; check that at least.
      code=$(curl -s -o /dev/null -w "%{http_code}" https://api.greentokey.com/token-plans)
      echo "⚠ JS-bundle grep failed; HTML shell HTTP=$code (200 = SPA loads, manual browser-check needed)"
    fi

    echo ""
    echo "==> 2. /api/payment/checkout reachable (auth-gated)"
    code=$(curl -s -o /dev/null -w "%{http_code}" "https://api.greentokey.com/api/payment/checkout?plan_code=token-99")
    echo "    HTTP $code (200 with 'login required' envelope is OK; 404 = bad)"

    echo ""
    echo "==> 3. /api/payment/hupijiao/checkout reachable (auth-gated)"
    code=$(curl -s -o /dev/null -w "%{http_code}" "https://api.greentokey.com/api/payment/hupijiao/checkout?plan_code=token-99")
    echo "    HTTP $code (200 with 'login required' envelope OK; 404 = endpoint not wired)"

    echo ""
    echo "==> 4. Hupijiao callback handler reachable (POST without sig → 401 OR 500-not-configured)"
    # Two valid responses depending on whether hupijiao.merchant_secret is set:
    #   401: secret IS configured + signature mismatch (post-merchant-signup)
    #   500: secret NOT configured + handler refuses callback (pre-merchant-signup, current state)
    # 404 here would mean route not registered = real problem.
    code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "https://api.greentokey.com/api/gtk/v1/service/hupijiao-callback")
    case "$code" in
      401) echo "    HTTP 401 ✅ (merchant configured + signature missing — expected)" ;;
      500) echo "    HTTP 500 ✅ (merchant not configured yet — expected pre-signup)" ;;
      404) echo "    HTTP 404 ❌ route not registered — service/router.go drift?" ;;
      *)   echo "    HTTP $code ⚠ unexpected, eyeball the response body" ;;
    esac

    echo ""
    echo "==> 5. /api/gtk/v1/services + /api/gtk/v1/pool sanity"
    curl -s -o /dev/null -w "    services: %{http_code}\n" https://api.greentokey.com/api/gtk/v1/services
    curl -s -o /dev/null -w "    pool: %{http_code}\n" https://api.greentokey.com/api/gtk/v1/pool

    echo ""
    echo "==> 6. token-99 plan row exists in gtk_plan (proves seedTokenPlans ran)"
    ssh "$VPS_HOST" "sudo bash -c '. /opt/greentokey/.env; docker exec greentokey-mysql mysql -uroot -p\"\$MYSQL_ROOT_PASSWORD\" chatnio -e \"SELECT id, code, name, product_type, billing_mode, price_cents, duration_days, quota_grant FROM gtk_plan WHERE code=\\\"token-99\\\"\"'" \
      || echo "❌ token-99 lookup failed — seedTokenPlans may not have run"
    ;;

  rollback)
    echo "⚠️  Rolling back to $PREV_TAG"
    ssh "$VPS_HOST" "sudo sed -i 's|greentokey-coai:$NEW_TAG|greentokey-coai:$PREV_TAG|' $COMPOSE_FILE"
    ssh "$VPS_HOST" "grep 'greentokey-coai' $COMPOSE_FILE"
    ssh "$VPS_HOST" "cd /opt/greentokey && sudo docker compose up -d coai 2>&1 | tail -5"
    echo "✅ rolled back to $PREV_TAG"
    echo "    The $NEW_TAG image is still on disk — fix forward + retry 'up' when ready."
    ;;

  help|*)
    cat <<'HELP'
v22-deploy.sh — orchestrator for v0.22-token-launch deploy.
Usage: bash bin/v22-deploy.sh <step>

Steps (run in order, verify between each):
  rsync      Push v0.22 source to VPS (backs up current first)
  build      sudo docker build greentokey-coai:v0.22.0-token-launch
  up         Bump compose tag + docker compose up coai + watch logs
  smoke      Curl prod endpoints + verify token-99 plan row
  rollback   Revert to v1.0.0-cache-billing-ux (emergency)
HELP
    ;;
esac
