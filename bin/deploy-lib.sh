#!/usr/bin/env bash
# Shared helpers for greentokey deploy automation.

set -euo pipefail

GT_DEPLOY_VPS_HOST="${GT_DEPLOY_VPS_HOST:-greentokey}"
GT_DEPLOY_VPS_ROOT="${GT_DEPLOY_VPS_ROOT:-/opt/greentokey}"
GT_DEPLOY_VPS_SOURCE="${GT_DEPLOY_VPS_SOURCE:-/opt/greentokey/coai-source}"
GT_DEPLOY_COMPOSE_FILE="${GT_DEPLOY_COMPOSE_FILE:-/opt/greentokey/docker-compose.yml}"
GT_DEPLOY_SERVICE="${GT_DEPLOY_SERVICE:-coai}"
GT_DEPLOY_IMAGE="${GT_DEPLOY_IMAGE:-greentokey-coai}"
GT_DEPLOY_DOMAIN="${GT_DEPLOY_DOMAIN:-https://api.greentokey.com}"
GT_DEPLOY_BACKUP_DIR="${GT_DEPLOY_BACKUP_DIR:-/opt/greentokey/data/backups}"
GT_DEPLOY_DRY_RUN="${GT_DEPLOY_DRY_RUN:-0}"

gt_repo_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd
}

gt_is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y) return 0 ;;
    *) return 1 ;;
  esac
}

gt_quote_cmd() {
  local first=1
  for arg in "$@"; do
    if [[ "$first" -eq 0 ]]; then
      printf ' '
    fi
    first=0
    printf '%q' "$arg"
  done
  printf '\n'
}

gt_run() {
  printf '+ '
  gt_quote_cmd "$@"
  if gt_is_true "$GT_DEPLOY_DRY_RUN"; then
    return 0
  fi
  "$@"
}

gt_remote() {
  local remote_cmd="$1"
  gt_run ssh "$GT_DEPLOY_VPS_HOST" "$remote_cmd"
}

gt_require_clean_tree() {
  local repo_root="$1"
  local allow_dirty="${2:-0}"
  if gt_is_true "$allow_dirty"; then
    echo "OK: dirty tree allowed by explicit override"
    return 0
  fi

  local status
  status="$(git -C "$repo_root" status --porcelain)"
  if [[ -n "$status" ]]; then
    echo "ERROR: worktree is dirty. Commit/stash changes or pass --allow-dirty for dry-run only." >&2
    echo "$status" >&2
    return 1
  fi
  echo "OK: worktree clean"
}

gt_current_sgt_hour() {
  if [[ -n "${GT_DEPLOY_NOW_HOUR:-}" ]]; then
    echo "$GT_DEPLOY_NOW_HOUR"
    return 0
  fi
  TZ=Asia/Singapore date +%H
}

gt_require_deploy_window() {
  local allow_outside="${1:-0}"
  if gt_is_true "$allow_outside"; then
    echo "OK: deploy window bypass allowed by explicit override"
    return 0
  fi

  local raw_hour hour
  raw_hour="$(gt_current_sgt_hour)"
  hour=$((10#$raw_hour))
  if (( hour < 11 || hour >= 14 )); then
    echo "ERROR: outside deploy window. SGT hour=$hour, allowed window is 11:00-14:00." >&2
    echo "Pass --allow-outside-window only for outage/security override or dry-run." >&2
    return 1
  fi
  echo "OK: deploy window allowed (SGT hour=$hour)"
}

gt_script_header() {
  local name="$1"
  echo "==> $name"
}
