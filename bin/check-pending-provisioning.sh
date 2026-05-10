#!/usr/bin/env bash
# bin/check-pending-provisioning.sh — every 5 minutes (PKG-2 Wave 4 D9 #6)
#
# What it checks: gtk_newapi_pending_provisions rows in 'pending' or
# 'retrying' state older than 30 minutes. The PKG-1 retry queue should
# drain pending rows within minutes; persistent stuck rows mean either:
#   (a) the retry worker isn't running,
#   (b) a permanent NewAPI failure (malformed plan, account quota cap),
#   (c) the worker keeps hitting the same transient error past the
#       built-in backoff cap.
#
# Founder action on alert: inspect last_error column for samples + decide
# between manual NewAPI provision (admin UI) or dropping the row.
#
# Exit codes:
#	0 — no stuck pending rows
#	1 — at least one stuck row (count + samples in alert log)
#	2 — query failed
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/pending-provisioning-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"
STUCK_MINUTES="${PROVISION_STUCK_MINUTES:-30}"

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

QUERY="SELECT COUNT(*) FROM gtk_newapi_pending_provisions
       WHERE status IN ('pending','retrying')
         AND created_at < NOW() - INTERVAL ${STUCK_MINUTES} MINUTE;"

if ! COUNT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi
COUNT="${COUNT//[^0-9]/}"
COUNT="${COUNT:-0}"

if [[ "$COUNT" -gt 0 ]]; then
  SAMPLES="$("${MYSQL[@]}" "
    SELECT id, user_id, plan_id, status,
           COALESCE(LEFT(last_error, 80), ''), created_at
    FROM gtk_newapi_pending_provisions
    WHERE status IN ('pending','retrying')
      AND created_at < NOW() - INTERVAL ${STUCK_MINUTES} MINUTE
    ORDER BY created_at ASC
    LIMIT 5;" 2>/dev/null | tr '\t' '|' | tr '\n' ' ' || true)"
  echo "$TS ALERT: ${COUNT} pending provision(s) stuck >${STUCK_MINUTES}m. Samples (id|uid|plan|status|err|ts): ${SAMPLES}" \
    | tee -a "$ALERT_LOG" >&2
  exit 1
fi

exit 0
