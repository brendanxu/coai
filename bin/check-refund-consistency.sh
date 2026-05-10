#!/usr/bin/env bash
# bin/check-refund-consistency.sh — hourly (PKG-2 Wave 4 D9 #4)
#
# What it checks: surfaces gtk_user_plan rows where status='canceled' AND
# cancellation_reason starts with 'refund_' AND the row was purchased
# more than 1 hour ago. The plan flip is the LOCAL signal that a refund
# happened; the NewAPI token disable should follow within seconds via
# commerce.RevokeEntitlement(token) → newapi.DisableToken. Persistent
# rows past the 1h grace window indicate the local refund landed but the
# NewAPI side may not have (deferred-cleanup queue, NewAPI outage, etc).
#
# What it does NOT check:
#
#	The NewAPI side directly. NewAPI cross-check is a follow-up PKG (would
#	require admin REST round-trip per row). For now we surface the LOCAL
#	pattern so ops can drill into NewAPI manually if the count is non-zero.
#
# Exit codes:
#	0 — no recent refunds beyond the grace window
#	1 — at least one row matches (count + samples in alert log)
#	2 — query failed
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/refund-consistency-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"
GRACE_HOURS="${REFUND_GRACE_HOURS:-1}"

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

QUERY="SELECT COUNT(*) FROM gtk_user_plan
       WHERE status='canceled'
         AND cancellation_reason LIKE 'refund_%'
         AND purchased_at < NOW() - INTERVAL ${GRACE_HOURS} HOUR;"

if ! COUNT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi
COUNT="${COUNT//[^0-9]/}"
COUNT="${COUNT:-0}"

if [[ "$COUNT" -gt 0 ]]; then
  SAMPLES="$("${MYSQL[@]}" "
    SELECT user_id, plan_id, COALESCE(order_id, ''),
           cancellation_reason, purchased_at
    FROM gtk_user_plan
    WHERE status='canceled'
      AND cancellation_reason LIKE 'refund_%'
      AND purchased_at < NOW() - INTERVAL ${GRACE_HOURS} HOUR
    ORDER BY purchased_at DESC
    LIMIT 5;" 2>/dev/null | tr '\t' '|' | tr '\n' ' ' || true)"
  echo "$TS ALERT: ${COUNT} refunded user_plan(s) older than ${GRACE_HOURS}h — verify NewAPI token disable. Samples (uid|plan|order|reason|ts): ${SAMPLES}" \
    | tee -a "$ALERT_LOG" >&2
  exit 1
fi

exit 0
