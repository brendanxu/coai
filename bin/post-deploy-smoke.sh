#!/usr/bin/env bash
# Post-deploy smoke checks for greentokey production.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    -h|--help)
      sed -n '2,80p' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

gt_script_header "post-deploy smoke"
gt_run curl -fsS -o /dev/null "$GT_DEPLOY_DOMAIN/token-plans"
gt_run curl -fsS -o /dev/null "$GT_DEPLOY_DOMAIN/api/gtk/v1/services"
gt_run curl -fsS -o /dev/null "$GT_DEPLOY_DOMAIN/api/gtk/v1/pool"
gt_remote "sudo docker compose -f '$GT_DEPLOY_COMPOSE_FILE' ps '$GT_DEPLOY_SERVICE'"
echo "post-deploy smoke passed"
