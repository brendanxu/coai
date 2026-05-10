#!/usr/bin/env bash
# bin/check-margin-thresholds.sh — daily 04:45 SGT
# (PKG-2 Wave 4 D9 #2)
#
# What it checks: queries gtk_service_margin_v (D6 VIEW) for the last
# 7 days and alerts if any service slug's margin drops below the operator
# threshold (default 30%).
#
# Threshold rationale: with cogs as upstream LLM cost, a sustained margin
# below 30% means we're either under-priced on a service or upstream
# pricing shifted. Either is worth a human look — alert daily so the
# signal doesn't go stale.
#
# Exit codes:
#	0 — all services above threshold OR no data in window
#	1 — at least one service below MARGIN_PCT_MIN
#	2 — query failed
#	9 — config missing
#
# Created by PKG-2 Wave 4 D9 (2026-05-10).

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
LOG_DIR="${LOG_DIR:-/opt/greentokey/data/logs}"
ALERT_LOG="$LOG_DIR/margin-threshold-alert.log"
DB_NAME="${MYSQL_DB:-chatnio}"
WINDOW_DAYS="${MARGIN_WINDOW_DAYS:-7}"
MIN_PCT="${MARGIN_PCT_MIN:-30}"

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

# Aggregate revenue + cost per service slug over the rolling window. Compute
# margin_pct on the SQL side so we get integer comparison (avoid bash float).
QUERY="
  SELECT service_slug,
         SUM(price_cny_cents_paid)        AS rev,
         SUM(upstream_cost_cents)         AS cost,
         CASE WHEN SUM(price_cny_cents_paid) > 0
              THEN ROUND(100 * (SUM(price_cny_cents_paid) - SUM(upstream_cost_cents))
                         / SUM(price_cny_cents_paid), 2)
              ELSE 0 END                  AS margin_pct
    FROM gtk_service_margin_v
   WHERE order_created_at >= DATE_SUB(NOW(), INTERVAL ${WINDOW_DAYS} DAY)
GROUP BY service_slug
HAVING margin_pct < ${MIN_PCT}
ORDER BY margin_pct ASC;
"

if ! RESULT="$("${MYSQL[@]}" "$QUERY" 2>/dev/null)"; then
  echo "$TS ALERT: gtk_monitor query failed (auth / connectivity)" | tee -a "$ALERT_LOG" >&2
  exit 2
fi

if [[ -n "$RESULT" ]]; then
  echo "$TS ALERT: services with margin <${MIN_PCT}% over last ${WINDOW_DAYS}d:" | tee -a "$ALERT_LOG" >&2
  echo "$RESULT" | while IFS=$'\t' read -r slug rev cost pct; do
    echo "$TS         service=${slug} rev=${rev}c cost=${cost}c margin=${pct}%" | tee -a "$ALERT_LOG" >&2
  done
  exit 1
fi

exit 0
