#!/usr/bin/env bash
# bin/setup-monitor-user.sh — one-shot creation of MySQL user 'gtk_monitor'
# with SELECT-only grants on chatnio.gtk_* (PKG-2 Wave 4 D8, Q4 GO).
#
# Why a dedicated read-only user (vs reusing root):
#
#	Cron monitoring scripts run unattended on the VPS and read a small set
#	of admin tables (gtk_service_order, gtk_app_usage_log, gtk_user_plan,
#	gtk_newapi_pending_provisions, gtk_service_margin_v). Granting them
#	root access would over-permission a fleet of scripts that should never
#	be able to UPDATE / DELETE / DROP. SELECT-only is the principle of
#	least privilege.
#
# Idempotency:
#
#	- If MYSQL_MONITOR_PASSWORD already exists in /opt/greentokey/.env,
#	  reuse it (don't regenerate — that would orphan the existing MySQL
#	  user with a now-unknown password).
#	- If the gtk_monitor MySQL user already exists, ALTER USER ...
#	  IDENTIFIED BY ... to ensure the password matches the .env value
#	  (handles the case where someone manually rotated the MySQL side
#	  but not .env, or vice-versa).
#	- Re-runnable safely.
#
# Run on VPS as root:
#
#	bash setup-monitor-user.sh
#
# Reads:
#	/opt/greentokey/.env  (MYSQL_ROOT_PASSWORD required)
#
# Writes:
#	/opt/greentokey/.env  (appends MYSQL_MONITOR_PASSWORD if absent)
#
# Created by PKG-2 Wave 4 D8 (2026-05-10).

set -euo pipefail

# --- Config ---------------------------------------------------------------

ENV_FILE="${ENV_FILE:-/opt/greentokey/.env}"
DB_NAME="${MYSQL_DB:-chatnio}"
MONITOR_USER="${MONITOR_USER:-gtk_monitor}"
MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"

# --- Pre-flight -----------------------------------------------------------

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found — run on the VPS or set ENV_FILE=" >&2
  exit 9
fi

# Source env (allow unset surrounding vars; we only care about a couple).
set +u
# shellcheck source=/dev/null
source "$ENV_FILE"
set -u

if [[ -z "${MYSQL_ROOT_PASSWORD:-}" ]]; then
  echo "ERROR: MYSQL_ROOT_PASSWORD missing from $ENV_FILE" >&2
  exit 9
fi

# Use docker exec if greentokey runs MySQL via docker-compose (typical
# VPS layout), otherwise direct mysql client. Detect via docker presence
# + a 'mysql' container.
MYSQL_CMD=()
if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -q '^mysql$'; then
  MYSQL_CMD=(docker exec -i mysql mysql -uroot "-p${MYSQL_ROOT_PASSWORD}")
elif command -v mysql >/dev/null 2>&1; then
  MYSQL_CMD=(mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -uroot "-p${MYSQL_ROOT_PASSWORD}")
else
  echo "ERROR: neither docker (with mysql container) nor mysql CLI found" >&2
  exit 9
fi

# --- Step 1: ensure MYSQL_MONITOR_PASSWORD in .env ------------------------

if grep -q '^MYSQL_MONITOR_PASSWORD=' "$ENV_FILE"; then
  echo "MYSQL_MONITOR_PASSWORD already in $ENV_FILE — reusing"
  # Re-source to pick up the value.
  set +u
  # shellcheck source=/dev/null
  source "$ENV_FILE"
  set -u
else
  # Generate 16-char random password (URL-safe alphanumerics).
  GENERATED_PASSWORD="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 16)"
  echo "" >> "$ENV_FILE"
  echo "# Added by setup-monitor-user.sh ($(date -Iseconds))" >> "$ENV_FILE"
  echo "MYSQL_MONITOR_PASSWORD=${GENERATED_PASSWORD}" >> "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  MYSQL_MONITOR_PASSWORD="$GENERATED_PASSWORD"
  echo "Generated + appended MYSQL_MONITOR_PASSWORD to $ENV_FILE (16 chars)"
fi

# --- Step 2: create or align the MySQL user -------------------------------
# CREATE USER IF NOT EXISTS is MySQL 5.7+. Follow with ALTER USER ...
# IDENTIFIED BY to re-apply the password idempotently (handles manual
# rotation drift between MySQL and .env).

cat <<SQL | "${MYSQL_CMD[@]}"
CREATE USER IF NOT EXISTS '${MONITOR_USER}'@'localhost' IDENTIFIED BY '${MYSQL_MONITOR_PASSWORD}';
ALTER USER '${MONITOR_USER}'@'localhost' IDENTIFIED BY '${MYSQL_MONITOR_PASSWORD}';
-- SELECT-only on greentokey gtk_* tables. Wildcard at the table level
-- because new gtk_* tables show up over time (Wave 1 added gtk_payment_session,
-- D6 surfaces gtk_service_margin_v as a VIEW — both auto-covered).
GRANT SELECT ON \`${DB_NAME}\`.\`gtk_%\` TO '${MONITOR_USER}'@'localhost';
FLUSH PRIVILEGES;
SQL

echo "Granted SELECT on ${DB_NAME}.gtk_* to ${MONITOR_USER}@localhost"

# --- Step 3: smoke-test the new user --------------------------------------

if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -q '^mysql$'; then
  SMOKE=(docker exec -i mysql mysql -u"$MONITOR_USER" "-p${MYSQL_MONITOR_PASSWORD}" "$DB_NAME" -Nse 'SELECT 1;')
else
  SMOKE=(mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u"$MONITOR_USER" "-p${MYSQL_MONITOR_PASSWORD}" "$DB_NAME" -Nse 'SELECT 1;')
fi
if RESULT="$("${SMOKE[@]}" 2>/dev/null)" && [[ "$RESULT" == "1" ]]; then
  echo "Smoke test passed: ${MONITOR_USER}@localhost can SELECT against ${DB_NAME}"
else
  echo "WARNING: smoke test failed — check MySQL logs + ENV_FILE password" >&2
  exit 1
fi

echo
echo "OK ${MONITOR_USER} is ready for cron monitoring scripts (D9)."
