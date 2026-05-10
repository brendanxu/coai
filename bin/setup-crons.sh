#!/usr/bin/env bash
# bin/setup-crons.sh — install cron entries for the 6 D9 monitoring
# scripts (PKG-2 Wave 4 D10). Idempotent: re-running is a no-op (a
# marker comment in the crontab is the dedupe key).
#
# Run on VPS as root:
#
#	bash setup-crons.sh
#
# Pre-requisites (in this order):
#   1. bin/setup-monitor-user.sh — creates the gtk_monitor MySQL user.
#   2. The 6 D9 scripts present at $OPS_DIR (default /opt/greentokey/scripts).
#      The setup script copies bin/check-*.sh into $OPS_DIR if invoked from
#      the repo, so a fresh deploy can run this once after rsync.
#
# Mirrors the pattern from infra/scripts/setup-r2-offsite.sh sections 6-7.
#
# Created by PKG-2 Wave 4 D10 (2026-05-10).

set -euo pipefail

OPS_DIR="${OPS_DIR:-/opt/greentokey/scripts}"
CRON_MARKER="# tana-pkg2-wave4-monitoring-2026-05-10"

# --- Step 1: copy scripts to ops dir (if running from repo) -----------------

mkdir -p "$OPS_DIR"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Detect: are we being run from the repo's bin/ (Mac, then ssh+install) or
# directly on VPS (in which case the bin/ files arrived via prior rsync)?
SCRIPTS=(
  check-cost-ledger-consistency.sh
  check-margin-thresholds.sh
  check-service-order-sla.sh
  check-refund-consistency.sh
  check-recurring-billing.sh
  check-pending-provisioning.sh
)

for s in "${SCRIPTS[@]}"; do
  if [[ -f "$SCRIPT_DIR/$s" ]]; then
    cp "$SCRIPT_DIR/$s" "$OPS_DIR/$s"
    chmod +x "$OPS_DIR/$s"
    echo "  installed $OPS_DIR/$s"
  elif [[ -f "$OPS_DIR/$s" ]]; then
    echo "  $OPS_DIR/$s already present (skipping copy)"
  else
    echo "  WARN: $s not found in $SCRIPT_DIR or $OPS_DIR — skipping" >&2
  fi
done

# --- Step 2: install cron entries ------------------------------------------

if crontab -l 2>/dev/null | grep -q "$CRON_MARKER"; then
  echo "PKG-2 Wave 4 monitoring cron already installed — skipping"
  exit 0
fi

# Note on cadences:
#   04:30 SGT — daily; cost-ledger-consistency. Picks the quiet hour after
#               R2 backup (03:00) finishes.
#   04:45 SGT — daily; margin-thresholds. Daily summary alert.
#   */15 *   — every 15 minutes; service-order SLA (30m stuck threshold).
#   17 *     — hourly; refund-consistency (1h grace window).
#   0 */6    — every 6h; recurring-billing.
#   */5 *    — every 5 minutes; pending-provisioning (30m stuck threshold).

(
  crontab -l 2>/dev/null || true
  cat <<EOF

$CRON_MARKER
30 4 * * *      $OPS_DIR/check-cost-ledger-consistency.sh
45 4 * * *      $OPS_DIR/check-margin-thresholds.sh
*/15 * * * *    $OPS_DIR/check-service-order-sla.sh
17 * * * *      $OPS_DIR/check-refund-consistency.sh
0 */6 * * *     $OPS_DIR/check-recurring-billing.sh
*/5 * * * *     $OPS_DIR/check-pending-provisioning.sh
EOF
) | crontab -

echo "Installed PKG-2 Wave 4 monitoring crons (marker: $CRON_MARKER)"
echo
echo "Active cron jobs:"
crontab -l | grep -A 6 "$CRON_MARKER" || true
echo
echo "Logs land in /opt/greentokey/data/logs/*-alert.log"
echo "Cron mailx surfaces non-zero exits as email (check root MAILTO)."
