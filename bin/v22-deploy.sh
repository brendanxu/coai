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
PREV_TAG="v0.22.0-token-launch"
NEW_TAG="v0.22.1-bl01-hotfix"
COMPOSE_FILE="/opt/greentokey/docker-compose.yml"

step="${1:-help}"

case "$step" in
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
    ssh "$VPS_HOST" "cd $VPS_PATH && sudo docker build -f Dockerfile.split -t greentokey-coai:$NEW_TAG . 2>&1 | tail -20" \
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
    echo "==> 1. /token-plans HTML 200 + new CTA text present"
    if curl -sf https://api.greentokey.com/token-plans | grep -qE "微信 / 支付宝|WeChat / Alipay"; then
      echo "✅ /token-plans renders new dual-rail CTA"
    else
      echo "❌ /token-plans missing new CTA text — check coai container"
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
    echo "==> 4. Hupijiao callback still public (POST without sig → 401)"
    code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "https://api.greentokey.com/api/gtk/v1/service/hupijiao-callback")
    echo "    HTTP $code (401 expected — signature missing)"

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
