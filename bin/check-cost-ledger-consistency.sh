#!/usr/bin/env bash
# bin/check-cost-ledger-consistency.sh — daily 04:30 SGT
# (PKG-2 Wave 4 D9 #1)
#
# Cron-installed by bin/setup-crons.sh. Mirrors check-backup-freshness.sh
# pattern: read-only via gtk_monitor user, log to /opt/greentokey/data/logs,
# exit non-zero on alert.
#
# What it checks:
#
#	Every gtk_service_order with status='completed' since 2026-05-10 (the
#	PKG-2 Wave 4 D1 cutover where service_order calls began writing to
#	gtk_app_usage_log) MUST have at least one matching usage row keyed by
#	source='service_order' + order_id=order_no. A completed order with no
#	usage row indicates the cost-ledger write path silently failed.
#
# What it does NOT check:
#
#	NewAPI quota comparison (used_quota per user vs SUM cost_cents). The
#	NewAPI integration for cost reconciliation is deferred to a follow-up
#	PKG (would need a NewAPI admin REST round-trip per user — too heavy
#	for a daily cron).
#
# Exit codes:
#	0 — all completed orders have usage rows
#	1 — gap detected (count + sample order_nos in alert log)
#	2 — query failed (connectivity / auth)
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/cost-ledger-consistency-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"
# Cutover: pre-Wave-4 orders never wrote to gtk_app_usage_log; don't alert
# on them. Adjust if the Wave 4 deploy date shifts.
CUTOVER_DATE="${COST_LEDGER_CUTOVER:-2026-05-10}"

mkdir -p "$LOG_DIR"

[[ -f "$ENV_FILE" ]] || { echo "ERROR: $ENV_FILE missing" >&2; exit 9; }
set +u
# shellcheck source=/dev/null
source "$ENV_FILE"
set -u
[[ -n "${MYSQL_MONITOR_PASSWORD:-}" ]] || {
  echo "ERROR: MYSQL_MONITOR_PASSWORD missing — run bin/setup-monitor-user.sh first" >&2
  exit 9
}

TS="$(date -Iseconds)"

if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -q '^mysql$'; then
  MYSQL=(docker exec -i mysql mysql -u gtk_monitor "-p${MYSQL_MONITOR_PASSWORD}" "$DB_NAME" -Nse)
else
  MYSQL=(mysql -h 127.0.0.1 -u gtk_monitor "-p${MYSQL_MONITOR_PASSWORD}" "$DB_NAME" -Nse)
fi

QUERY="
  SELECT COUNT(*) FROM gtk_service_order svc
  WHERE svc.status = 'completed'
    AND svc.completed_at >= '${CUTOVER_DATE}'
    AND NOT EXISTS (
      SELECT 1 FROM gtk_app_usage_log usage
      WHERE usage.order_id = svc.order_no
        AND usage.source   = 'service_order'
    );
"

if ! GAP_COUNT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi
GAP_COUNT="${GAP_COUNT//[^0-9]/}"
GAP_COUNT="${GAP_COUNT:-0}"

if [[ "$GAP_COUNT" -gt 0 ]]; then
  SAMPLE_QUERY="
    SELECT order_no FROM gtk_service_order svc
    WHERE svc.status = 'completed'
      AND svc.completed_at >= '${CUTOVER_DATE}'
      AND NOT EXISTS (
        SELECT 1 FROM gtk_app_usage_log usage
        WHERE usage.order_id = svc.order_no AND usage.source = 'service_order'
      )
    LIMIT 10;
  "
  SAMPLES="$("${MYSQL[@]}" "$SAMPLE_QUERY" 2>/dev/null | tr '\n' ' ' || true)"
  echo "$TS ALERT: ${GAP_COUNT} completed service orders since ${CUTOVER_DATE} have no gtk_app_usage_log row. Samples: ${SAMPLES}" \
    | tee -a "$ALERT_LOG" >&2
  exit 1
fi

exit 0
