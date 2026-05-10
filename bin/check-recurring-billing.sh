#!/usr/bin/env bash
# bin/check-recurring-billing.sh — every 6 hours (PKG-2 Wave 4 D9 #5)
#
# What it checks: gtk_user_plan rows where expire_at < NOW() AND
# status='active'. These are renewal-window misses: either the
# subscription_payment_success webhook didn't land OR the cron that
# extends expire_at on renewal hasn't run. Either way the user is in a
# split-brain state where they appear to have an active plan but the
# expire_at window has lapsed.
#
# Why every 6h (not hourly): renewals happen at most once per
# user/month; surfacing the gap on a 6-hour cadence is enough urgency
# while keeping cron noise low.
#
# Exit codes:
#	0 — no expired-but-active plans
#	1 — at least one (count + samples in alert log)
#	2 — query failed
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/recurring-billing-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"

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
       WHERE expire_at < NOW() AND status='active';"

if ! COUNT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi
COUNT="${COUNT//[^0-9]/}"
COUNT="${COUNT:-0}"

if [[ "$COUNT" -gt 0 ]]; then
  SAMPLES="$("${MYSQL[@]}" "
    SELECT user_id, plan_id, COALESCE(order_id, ''), expire_at
    FROM gtk_user_plan
    WHERE expire_at < NOW() AND status='active'
    ORDER BY expire_at ASC
    LIMIT 5;" 2>/dev/null | tr '\t' '|' | tr '\n' ' ' || true)"
  echo "$TS ALERT: ${COUNT} user_plan(s) expired but still status='active'. Samples (uid|plan|order|expire): ${SAMPLES}" \
    | tee -a "$ALERT_LOG" >&2
  exit 1
fi

exit 0
