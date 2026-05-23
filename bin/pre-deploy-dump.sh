#!/usr/bin/env bash
# Run an immediate production backup before a deploy.

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

gt_script_header "pre-deploy dump"
gt_remote "set -euo pipefail; sudo '$GT_DEPLOY_VPS_ROOT/backup-mysql.sh'; latest=\$(sudo ls -t '$GT_DEPLOY_BACKUP_DIR'/greentokey-*.sql.gz | head -1); test -n \"\$latest\"; echo \"OK: pre-deploy backup \$latest\""
echo "pre-deploy dump complete"
