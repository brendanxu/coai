#!/usr/bin/env bash
# Short production canary loop after deploy.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

DURATION_MINUTES="${1:-60}"
if [[ "${1:-}" != "" && "${1:-}" != --* ]]; then
  shift
fi
INTERVAL_SECONDS="${GT_DEPLOY_CANARY_INTERVAL_SECONDS:-30}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    --interval-seconds) INTERVAL_SECONDS="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,80p' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

gt_script_header "canary ${DURATION_MINUTES}m"
if gt_is_true "$GT_DEPLOY_DRY_RUN"; then
  echo "+ canary loop ${DURATION_MINUTES}m every ${INTERVAL_SECONDS}s against $GT_DEPLOY_DOMAIN"
  echo "+ curl -fsS -o /dev/null $GT_DEPLOY_DOMAIN/api/gtk/v1/services"
  echo "+ curl -fsS -o /dev/null $GT_DEPLOY_DOMAIN/api/gtk/v1/pool"
  exit 0
fi

end_epoch=$(( $(date +%s) + DURATION_MINUTES * 60 ))
while (( $(date +%s) < end_epoch )); do
  ts="$(date -Iseconds)"
  curl -fsS -o /dev/null "$GT_DEPLOY_DOMAIN/api/gtk/v1/services"
  curl -fsS -o /dev/null "$GT_DEPLOY_DOMAIN/api/gtk/v1/pool"
  echo "$ts OK"
  sleep "$INTERVAL_SECONDS"
done
echo "canary passed"
