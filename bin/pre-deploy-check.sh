#!/usr/bin/env bash
# Pre-deploy guardrail for greentokey CoAI deploys.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

ALLOW_DIRTY="${GT_DEPLOY_ALLOW_DIRTY:-0}"
ALLOW_OUTSIDE_WINDOW="${GT_DEPLOY_ALLOW_OUTSIDE_WINDOW:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    --allow-outside-window) ALLOW_OUTSIDE_WINDOW=1; shift ;;
    -h|--help)
      sed -n '2,80p' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

ROOT="$(gt_repo_root)"

gt_script_header "pre-deploy check"
echo "repo: $ROOT"
echo "dry_run: $GT_DEPLOY_DRY_RUN"

gt_require_clean_tree "$ROOT" "$ALLOW_DIRTY"
gt_require_deploy_window "$ALLOW_OUTSIDE_WINDOW"

if command -v gitleaks >/dev/null 2>&1; then
  gt_run gitleaks detect --no-banner --source "$ROOT"
else
  echo "WARN: gitleaks not found; skipping local secret scan"
fi

if git -C "$ROOT" rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
  upstream="$(git -C "$ROOT" rev-parse --abbrev-ref --symbolic-full-name '@{u}')"
  echo "OK: upstream configured: $upstream"
else
  echo "WARN: no upstream configured for current branch; deploy can continue, but remote consistency is manual"
fi

gt_remote "set -euo pipefail; latest=\$(sudo find '$GT_DEPLOY_BACKUP_DIR' -name 'greentokey-*.sql.gz' -mmin -1440 -print 2>/dev/null | sort | tail -1); test -n \"\$latest\"; echo \"OK: fresh backup \$latest\""

if git -C "$ROOT" diff --name-only HEAD -- docker-compose.yml Dockerfile.split infra 2>/dev/null | grep -q .; then
  gt_remote "cd '$GT_DEPLOY_VPS_ROOT' && sudo docker compose -f '$GT_DEPLOY_COMPOSE_FILE' config >/dev/null"
else
  echo "OK: no compose/runtime config changes detected in this worktree"
fi

echo "pre-deploy check passed"
