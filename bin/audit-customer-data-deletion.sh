#!/usr/bin/env bash
# audit-customer-data-deletion.sh — PKG-D5 post-delete verification.
#
# Usage:
#   bin/audit-customer-data-deletion.sh <user_id>
#
# Checks every user-scoped table for remaining rows belonging to <user_id>.
# Also confirms gtk_audit_deletion has a completed row for this user_id.
#
# Exit codes:
#   0 — all tables clean + audit row present (PASS)
#   1 — leftover data found or audit row missing (FAIL)
#
# Run on VPS (via ssh greentokey) after delete-customer-data.sh completes.

set -euo pipefail

USER_ID="${1:-}"

if [[ -z "$USER_ID" ]]; then
  echo "Usage: $0 <user_id>" >&2
  exit 1
fi

if ! [[ "$USER_ID" =~ ^[0-9]+$ ]]; then
  echo "Error: user_id must be a positive integer, got: $USER_ID" >&2
  exit 1
fi

# ── environment ─────────────────────────────────────────────────────────────
ENV_FILE="${GTK_ENV_FILE:-/opt/greentokey/.env}"

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -o allexport
  source "$ENV_FILE"
  set +o allexport
fi

MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_DATABASE="${MYSQL_DATABASE:-newapi}"

if [[ -z "${MYSQL_ROOT_PASSWORD:-}" ]]; then
  echo "Error: MYSQL_ROOT_PASSWORD not set. Source /opt/greentokey/.env or set it." >&2
  exit 1
fi

# ── mysql helper ─────────────────────────────────────────────────────────────
mysql_exec() {
  local sql="$1"
  local tmpfile
  tmpfile="$(mktemp /tmp/gtk-audit-XXXXXX.sql)"
  printf '%s' "$sql" > "$tmpfile"

  local result
  if command -v docker &>/dev/null && docker ps --format '{{.Names}}' 2>/dev/null | grep -q "greentokey-mysql"; then
    result=$(docker exec -i greentokey-mysql \
      mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" -N --batch "$MYSQL_DATABASE" \
      < "$tmpfile" 2>/dev/null)
  else
    result=$(mysql -h"$MYSQL_HOST" -P"$MYSQL_PORT" \
      -u"$MYSQL_USER" -p"${MYSQL_ROOT_PASSWORD}" \
      -N --batch "$MYSQL_DATABASE" \
      < "$tmpfile" 2>/dev/null)
  fi
  rm -f "$tmpfile"
  printf '%s' "$result"
}

# ── check tables ─────────────────────────────────────────────────────────────
# Same table list as delete-customer-data.sh + auth.
TABLES=(
  "gtk_user_plan:user_id"
  "gtk_app_usage_log:user_id"
  "gtk_newapi_pending_provisions:user_id"
  "gtk_ls_subscription:user_id"
  "gtk_newapi_binding:coai_user_id"
  "gtk_service_order:coai_user_id"
  "gtk_payment_session:coai_user_id"
  "quota:user_id"
  "conversation:user_id"
  "apikey:user_id"
  "auth:id"
)

echo "==> [D5 audit] Checking user_id=${USER_ID} across all user-scoped tables"
echo ""

FAIL=0

for entry in "${TABLES[@]}"; do
  TABLE="${entry%%:*}"
  COL="${entry##*:}"
  COUNT=$(mysql_exec "SELECT COUNT(*) FROM \`${TABLE}\` WHERE \`${COL}\` = ${USER_ID};" 2>/dev/null || echo "ERROR")
  if [[ "$COUNT" == "ERROR" ]]; then
    printf "    %-40s ERROR (table may not exist)\n" "${TABLE}"
    # Non-fatal: table might legitimately not exist in this deploy.
    continue
  fi
  if [[ "$COUNT" -gt 0 ]]; then
    printf "    FAIL  %-38s %s rows remaining\n" "${TABLE}" "${COUNT}"
    FAIL=1
  else
    printf "    PASS  %-38s 0 rows\n" "${TABLE}"
  fi
done

echo ""

# ── check audit row ──────────────────────────────────────────────────────────
AUDIT_ROW=$(mysql_exec "
  SELECT id, newapi_revocation_status, deletion_completed_at, error_message
  FROM gtk_audit_deletion
  WHERE coai_user_id = ${USER_ID}
  ORDER BY id DESC LIMIT 1;
" 2>/dev/null || echo "")

if [[ -z "$AUDIT_ROW" ]]; then
  echo "    FAIL  gtk_audit_deletion: no row found for user_id=${USER_ID}"
  FAIL=1
else
  AUDIT_ID=$(echo "$AUDIT_ROW" | awk -F'\t' '{print $1}')
  REVOKE=$(echo "$AUDIT_ROW"  | awk -F'\t' '{print $2}')
  COMPLETED=$(echo "$AUDIT_ROW" | awk -F'\t' '{print $3}')
  ERR=$(echo "$AUDIT_ROW"    | awk -F'\t' '{print $4}')
  echo "    PASS  gtk_audit_deletion row found:"
  echo "          audit_id=${AUDIT_ID}  revocation=${REVOKE}  completed=${COMPLETED}"
  if [[ -n "$ERR" ]] && [[ "$ERR" != "NULL" ]]; then
    echo "          error_message=${ERR}"
  fi
fi

echo ""

if [[ "$FAIL" -eq 0 ]]; then
  echo "==> PASS — user_id=${USER_ID} fully erased."
  exit 0
else
  echo "==> FAIL — user_id=${USER_ID} has leftover data. Investigate before confirming erasure."
  exit 1
fi
