#!/usr/bin/env bash
# Top-level greentokey deploy orchestrator.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

SERVICE="${1:-}"
if [[ -n "$SERVICE" && "$SERVICE" != --* ]]; then
  shift
else
  SERVICE=""
fi

VERSION_TAG=""
CANARY_MINUTES="${GT_DEPLOY_CANARY_MINUTES:-60}"
ALLOW_DIRTY="${GT_DEPLOY_ALLOW_DIRTY:-0}"
ALLOW_OUTSIDE_WINDOW="${GT_DEPLOY_ALLOW_OUTSIDE_WINDOW:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --coai-version) VERSION_TAG="$2"; shift 2 ;;
    --canary-minutes) CANARY_MINUTES="$2"; shift 2 ;;
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    --allow-outside-window) ALLOW_OUTSIDE_WINDOW=1; shift ;;
    -h|--help)
      sed -n '2,120p' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if [[ "$SERVICE" != "coai" ]]; then
  echo "Usage: bin/deploy.sh coai --coai-version <tag> [--dry-run]" >&2
  exit 1
fi

if [[ -z "$VERSION_TAG" ]]; then
  echo "ERROR: --coai-version <tag> is required for coai deploy" >&2
  exit 1
fi

common_flags=()
if gt_is_true "$GT_DEPLOY_DRY_RUN"; then
  common_flags+=(--dry-run)
fi

preflight_flags=()
if gt_is_true "$GT_DEPLOY_DRY_RUN"; then
  preflight_flags+=(--dry-run)
fi
if gt_is_true "$ALLOW_DIRTY"; then
  preflight_flags+=(--allow-dirty)
fi
if gt_is_true "$ALLOW_OUTSIDE_WINDOW"; then
  preflight_flags+=(--allow-outside-window)
fi

run_step() {
  local script="$1"
  shift
  echo "==> $script $*"
  gt_run bash "$SCRIPT_DIR/$script" "$@"
}

gt_script_header "deploy coai orchestration"
run_step pre-deploy-check.sh ${preflight_flags[@]+"${preflight_flags[@]}"}
run_step pre-deploy-dump.sh ${common_flags[@]+"${common_flags[@]}"}
run_step deploy-coai.sh "$VERSION_TAG" ${common_flags[@]+"${common_flags[@]}"}
run_step post-deploy-smoke.sh ${common_flags[@]+"${common_flags[@]}"}
run_step canary.sh "$CANARY_MINUTES" ${common_flags[@]+"${common_flags[@]}"}
echo "deploy orchestration complete"
