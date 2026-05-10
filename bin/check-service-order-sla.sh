#!/usr/bin/env bash
# bin/check-service-order-sla.sh — every 15 minutes (PKG-2 Wave 4 D9 #3)
#
# What it checks: counts gtk_service_order rows where status='running' and
# updated_at is more than 30 minutes ago. The agent runtime (RunOrderAPI →
# executeAgent) typically completes in 3-15 seconds; an order stuck in
# 'running' for 30+ minutes means either:
#   (a) the upstream call hung past our 60s HTTP client timeout,
#   (b) the runtime crashed mid-flight before releasing the lock,
#   (c) a deploy interrupted in-flight runs.
#
# Alert > 0 prompts founder to inspect via:
#   SELECT order_no, coai_user_id, agent_run_id, updated_at
#   FROM gtk_service_order WHERE status='running'
#   AND updated_at < NOW() - INTERVAL 30 MINUTE;
#
# Exit codes:
#	0 — no stuck runs
#	1 — at least one stuck run (count in alert log)
#	2 — query failed
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/service-order-sla-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"
SLA_MINUTES="${SLA_MINUTES:-30}"

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

QUERY="SELECT COUNT(*) FROM gtk_service_order
       WHERE status='running'
         AND updated_at < NOW() - INTERVAL ${SLA_MINUTES} MINUTE;"

if ! COUNT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi
COUNT="${COUNT//[^0-9]/}"
COUNT="${COUNT:-0}"

if [[ "$COUNT" -gt 0 ]]; then
  SAMPLES="$("${MYSQL[@]}" "
    SELECT order_no, COALESCE(agent_run_id, ''), updated_at
    FROM gtk_service_order
    WHERE status='running' AND updated_at < NOW() - INTERVAL ${SLA_MINUTES} MINUTE
    LIMIT 5;" 2>/dev/null | tr '\t' '|' | tr '\n' ' ' || true)"
  echo "$TS ALERT: ${COUNT} service order(s) stuck in 'running' >${SLA_MINUTES}m. Samples (order|run|updated): ${SAMPLES}" \
    | tee -a "$ALERT_LOG" >&2
  exit 1
fi

exit 0
