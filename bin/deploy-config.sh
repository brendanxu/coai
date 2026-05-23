#!/usr/bin/env bash
# Deploy one production config file with backup + service restart.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

LOCAL_FILE="${1:-}"
SERVICE="${2:-}"
if [[ -n "$LOCAL_FILE" && "$LOCAL_FILE" != --* ]]; then
  shift
else
  LOCAL_FILE=""
fi
if [[ -n "$SERVICE" && "$SERVICE" != --* ]]; then
  shift
else
  SERVICE=""
fi

REMOTE_PATH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote-path) REMOTE_PATH="$2"; shift 2 ;;
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    -h|--help)
      sed -n '2,100p' "$0"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$LOCAL_FILE" || -z "$SERVICE" ]]; then
  echo "Usage: bin/deploy-config.sh <local-file> <service> [--remote-path /opt/greentokey/file] [--dry-run]" >&2
  exit 1
fi

if [[ -z "$REMOTE_PATH" ]]; then
  REMOTE_PATH="$GT_DEPLOY_VPS_ROOT/$(basename "$LOCAL_FILE")"
fi

if ! gt_is_true "$GT_DEPLOY_DRY_RUN" && [[ ! -f "$LOCAL_FILE" ]]; then
  echo "ERROR: local file not found: $LOCAL_FILE" >&2
  exit 1
fi

STAMP="$(date +%Y%m%d-%H%M%S)"
TMP_REMOTE="/tmp/greentokey-config-$(basename "$LOCAL_FILE").$STAMP"

gt_script_header "deploy config $LOCAL_FILE -> $SERVICE"
echo "plan: backup $REMOTE_PATH"
echo "plan: sudo cp $REMOTE_PATH $REMOTE_PATH.bak.$STAMP"
gt_remote "set -euo pipefail; if [ -f '$REMOTE_PATH' ]; then sudo cp '$REMOTE_PATH' '$REMOTE_PATH.bak.$STAMP'; fi"

echo "plan: copy $LOCAL_FILE to $GT_DEPLOY_VPS_HOST:$TMP_REMOTE"
gt_run scp "$LOCAL_FILE" "$GT_DEPLOY_VPS_HOST:$TMP_REMOTE"

echo "plan: install config with sudo mv"
echo "plan: sudo mv $TMP_REMOTE $REMOTE_PATH"
gt_remote "set -euo pipefail; sudo mv '$TMP_REMOTE' '$REMOTE_PATH'; sudo chown root:root '$REMOTE_PATH'"

echo "plan: docker compose restart $SERVICE"
gt_remote "set -euo pipefail; cd '$GT_DEPLOY_VPS_ROOT' && sudo docker compose -f '$GT_DEPLOY_COMPOSE_FILE' restart '$SERVICE'"
echo "deploy config complete: $REMOTE_PATH -> $SERVICE"
