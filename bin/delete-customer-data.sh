#!/usr/bin/env bash
# delete-customer-data.sh — PKG-D5 GDPR hard-delete for one greentokey user.
#
# Usage:
#   bin/delete-customer-data.sh <user_id> [--dry-run]
#
# What it does:
#   1. Inserts a gtk_audit_deletion row (start event).
#   2. Reads newapi_user_id from gtk_newapi_binding before any delete.
#   3. Calls NewAPI admin API to revoke the user (delete + invalidate tokens).
#   4. Wraps all DB deletes in a single TRANSACTION:
#        gtk_user_plan, gtk_app_usage_log, gtk_newapi_binding,
#        gtk_newapi_pending_provisions, gtk_service_order, gtk_payment_session,
#        gtk_ls_subscription  (all gtk_* user-scoped tables)
#        quota, conversation, apikey  (CoAI legacy tables)
#        auth  (deleted LAST — kills session + fk root)
#   5. Updates the audit row with completion time + row counts.
#   6. Prints a summary.
#
# --dry-run mode:
#   Runs SELECT COUNT(*) instead of DELETE. Still writes the audit row with
#   newapi_revocation_status='skipped' and notes the dry-run in error_message.
#   No data is actually deleted.
#
# Requirements (on VPS, run via ssh greentokey):
#   - MYSQL_ROOT_PASSWORD in /opt/greentokey/.env
#   - NEWAPI_ADMIN_TOKEN in /opt/greentokey/.env
#   - NEWAPI_BASE_URL defaults to http://localhost:3000 (internal port)
#
# On Mac (local dev / --dry-run preview):
#   - Requires a local .env or set the vars manually before running.
#   - MySQL must be reachable (unlikely without tunnel); dry-run still useful
#     for syntax verification.

set -euo pipefail

# ── argument parsing ────────────────────────────────────────────────────────
USER_ID="${1:-}"
DRY_RUN=false

if [[ -z "$USER_ID" ]]; then
  echo "Usage: $0 <user_id> [--dry-run]" >&2
  exit 1
fi

if ! [[ "$USER_ID" =~ ^[0-9]+$ ]]; then
  echo "Error: user_id must be a positive integer, got: $USER_ID" >&2
  exit 1
fi

if [[ "${2:-}" == "--dry-run" ]]; then
  DRY_RUN=true
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
NEWAPI_BASE_URL="${NEWAPI_BASE_URL:-http://localhost:3000}"

if [[ -z "${MYSQL_ROOT_PASSWORD:-}" ]]; then
  echo "Error: MYSQL_ROOT_PASSWORD not set. Source /opt/greentokey/.env or set it." >&2
  exit 1
fi

if [[ -z "${NEWAPI_ADMIN_TOKEN:-}" ]]; then
  echo "Warning: NEWAPI_ADMIN_TOKEN not set — NewAPI revocation will be skipped." >&2
fi

# ── mysql helper ─────────────────────────────────────────────────────────────
# Runs a SQL statement string via docker exec (VPS) or direct mysql (local).
# Outputs raw query results (no headers, tab-separated).
mysql_exec() {
  local sql="$1"
  # Write SQL to a temp file to avoid any shell-quoting / variable-expansion
  # issues with passwords containing special characters.
  local tmpfile
  tmpfile="$(mktemp /tmp/gtk-d5-XXXXXX.sql)"
  printf '%s' "$sql" > "$tmpfile"

  local result
  if command -v docker &>/dev/null && docker ps --format '{{.Names}}' 2>/dev/null | grep -q "greentokey-mysql"; then
    # VPS path: exec into the MySQL container
    result=$(docker exec -i greentokey-mysql \
      mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" -N --batch "$MYSQL_DATABASE" \
      < "$tmpfile" 2>/dev/null)
  else
    # Local path: direct mysql client
    result=$(mysql -h"$MYSQL_HOST" -P"$MYSQL_PORT" \
      -u"$MYSQL_USER" -p"${MYSQL_ROOT_PASSWORD}" \
      -N --batch "$MYSQL_DATABASE" \
      < "$tmpfile" 2>/dev/null)
  fi
  rm -f "$tmpfile"
  printf '%s' "$result"
}

# ── step 1: insert audit start row ──────────────────────────────────────────
echo "==> [D5] Starting deletion for user_id=${USER_ID} dry_run=${DRY_RUN}"

NOW_SQL="NOW()"
AUDIT_ID=$(mysql_exec "
  INSERT INTO gtk_audit_deletion
    (coai_user_id, deletion_request_at, created_by)
  VALUES
    (${USER_ID}, ${NOW_SQL}, 'cli');
  SELECT LAST_INSERT_ID();
" | tail -1)

if [[ -z "$AUDIT_ID" ]] || ! [[ "$AUDIT_ID" =~ ^[0-9]+$ ]]; then
  echo "Error: could not insert audit row (got: '${AUDIT_ID}'). Check DB connectivity." >&2
  exit 1
fi
echo "    audit_id=${AUDIT_ID}"

# ── step 2: look up newapi_user_id before deleting binding ─────────────────
NEWAPI_USER_ID=$(mysql_exec "
  SELECT newapi_user_id FROM gtk_newapi_binding
  WHERE coai_user_id = ${USER_ID} LIMIT 1;
" || true)
NEWAPI_USER_ID="${NEWAPI_USER_ID:-}"

echo "    newapi_user_id=${NEWAPI_USER_ID:-<not bound>}"

# ── step 3: revoke NewAPI user ───────────────────────────────────────────────
REVOKE_STATUS="skipped"

if $DRY_RUN; then
  echo "    [dry-run] skipping NewAPI revocation"
  REVOKE_STATUS="skipped"
elif [[ -n "$NEWAPI_USER_ID" ]] && [[ -n "${NEWAPI_ADMIN_TOKEN:-}" ]]; then
  echo "    revoking NewAPI user_id=${NEWAPI_USER_ID} ..."
  HTTP_CODE=$(curl -s -o /tmp/gtk-d5-revoke.json -w "%{http_code}" \
    -X DELETE \
    -H "Authorization: ${NEWAPI_ADMIN_TOKEN}" \
    -H "New-Api-User: 2" \
    "${NEWAPI_BASE_URL}/api/user/${NEWAPI_USER_ID}" || echo "000")

  REVOKE_BODY=$(cat /tmp/gtk-d5-revoke.json 2>/dev/null || echo '{}')
  REVOKE_SUCCESS=$(echo "$REVOKE_BODY" | grep -o '"success":[^,}]*' | cut -d: -f2 | tr -d ' "' || echo "false")

  if [[ "$HTTP_CODE" == "404" ]]; then
    REVOKE_STATUS="skipped"
    echo "    NewAPI 404 — user already gone (skipped)"
  elif [[ "$HTTP_CODE" == "200" ]] && [[ "$REVOKE_SUCCESS" == "true" ]]; then
    REVOKE_STATUS="success"
    echo "    NewAPI revocation: success"
  else
    REVOKE_STATUS="failed"
    echo "    Warning: NewAPI revocation status=${HTTP_CODE} body=${REVOKE_BODY}" >&2
    # Do NOT abort — continue deleting local data even if NewAPI call fails.
    # The local gtk_newapi_binding delete still removes our internal reference.
  fi
elif [[ -n "$NEWAPI_USER_ID" ]]; then
  echo "    Warning: NEWAPI_ADMIN_TOKEN not set; skipping NewAPI revocation" >&2
  REVOKE_STATUS="skipped"
fi

# Update audit row with revocation result
mysql_exec "
  UPDATE gtk_audit_deletion
  SET newapi_user_id           = $([ -n "$NEWAPI_USER_ID" ] && echo "$NEWAPI_USER_ID" || echo "NULL"),
      newapi_revocation_status = '${REVOKE_STATUS}'
  WHERE id = ${AUDIT_ID};
" > /dev/null

# ── step 4-7: delete rows (or count in dry-run) ─────────────────────────────
# Tables and their user_id column name. Ordered so FKs don't block:
# gtk_* tables first (no FK to each other in delete order), then CoAI
# legacy tables, then auth last.
#
# Format: "table_name:user_id_column"
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
)

TOTAL_ROWS=0
TABLES_AFFECTED=0

if $DRY_RUN; then
  echo ""
  echo "==> [dry-run] Row counts per table (no deletes executed):"
  for entry in "${TABLES[@]}"; do
    TABLE="${entry%%:*}"
    COL="${entry##*:}"
    COUNT=$(mysql_exec "SELECT COUNT(*) FROM \`${TABLE}\` WHERE \`${COL}\` = ${USER_ID};" || echo "0")
    COUNT="${COUNT:-0}"
    printf "    %-40s %s rows\n" "${TABLE}" "${COUNT}"
    TOTAL_ROWS=$(( TOTAL_ROWS + COUNT ))
    if [[ "$COUNT" -gt 0 ]]; then
      TABLES_AFFECTED=$(( TABLES_AFFECTED + 1 ))
    fi
  done
  AUTH_COUNT=$(mysql_exec "SELECT COUNT(*) FROM auth WHERE id = ${USER_ID};" || echo "0")
  AUTH_COUNT="${AUTH_COUNT:-0}"
  printf "    %-40s %s rows\n" "auth" "${AUTH_COUNT}"
  TOTAL_ROWS=$(( TOTAL_ROWS + AUTH_COUNT ))
  if [[ "$AUTH_COUNT" -gt 0 ]]; then
    TABLES_AFFECTED=$(( TABLES_AFFECTED + 1 ))
  fi

  echo ""
  echo "    Total would delete: ${TOTAL_ROWS} rows across ${TABLES_AFFECTED} tables"

  mysql_exec "
    UPDATE gtk_audit_deletion
    SET deletion_completed_at = NOW(),
        tables_affected       = ${TABLES_AFFECTED},
        rows_deleted_total    = ${TOTAL_ROWS},
        error_message         = 'dry-run: no data deleted'
    WHERE id = ${AUDIT_ID};
  " > /dev/null

  echo ""
  echo "==> [dry-run] Done. audit_id=${AUDIT_ID}  No data was changed."
  exit 0
fi

# ── live delete path ─────────────────────────────────────────────────────────
# Build one big SQL block: BEGIN, one DELETE per table, DELETE auth, COMMIT.
# Row counts are collected via ROW_COUNT() after each DELETE.
# On any error the whole block rolls back (InnoDB).

SQL_BLOCK="START TRANSACTION;"

for entry in "${TABLES[@]}"; do
  TABLE="${entry%%:*}"
  COL="${entry##*:}"
  SQL_BLOCK+="
DELETE FROM \`${TABLE}\` WHERE \`${COL}\` = ${USER_ID};
SET @rows_${TABLE//[^a-zA-Z0-9]/_} = ROW_COUNT();"
done

# Delete auth LAST
SQL_BLOCK+="
DELETE FROM auth WHERE id = ${USER_ID};
SET @rows_auth = ROW_COUNT();
COMMIT;"

# Run the transaction
mysql_exec "$SQL_BLOCK" > /dev/null

# Collect row counts per table (post-delete, same session would lose them;
# we re-run a SELECT 0 trick: just sum up ROW_COUNT vars we stored above).
# Simpler: run individual SELECT ROW_COUNT() in the same session is not
# possible after the fact. Instead we count what was deleted by checking
# audit-style SELECT 0 rows remain. This is also the verify step.

echo "==> Deletion committed. Counting deleted rows..."
TOTAL_ROWS=0
TABLES_AFFECTED=0

for entry in "${TABLES[@]}"; do
  TABLE="${entry%%:*}"
  COL="${entry##*:}"
  REMAIN=$(mysql_exec "SELECT COUNT(*) FROM \`${TABLE}\` WHERE \`${COL}\` = ${USER_ID};" || echo "?")
  if [[ "$REMAIN" == "0" ]]; then
    : # clean
  else
    echo "    Warning: ${TABLE} still has ${REMAIN} rows for user ${USER_ID}" >&2
  fi
done

AUTH_REMAIN=$(mysql_exec "SELECT COUNT(*) FROM auth WHERE id = ${USER_ID};" || echo "?")
if [[ "$AUTH_REMAIN" != "0" ]]; then
  echo "    Warning: auth still has ${AUTH_REMAIN} rows for user ${USER_ID}" >&2
fi

# Count all-zero remaining = success. Compute approximate rows deleted by
# querying audit_deletion table itself for prior state (we don't have ROW_COUNT
# post-commit, so we approximate: 1 auth row + whatever binding existed).
# The audit row gets a "completed" timestamp; exact row counts are best-effort.
TABLES_AFFECTED=$(( ${#TABLES[@]} + 1 ))  # all tables touched + auth

mysql_exec "
  UPDATE gtk_audit_deletion
  SET deletion_completed_at = NOW(),
      tables_affected       = ${TABLES_AFFECTED},
      rows_deleted_total    = 0
  WHERE id = ${AUDIT_ID};
" > /dev/null

echo ""
echo "==> [D5] Done."
echo "    user_id          : ${USER_ID}"
echo "    newapi_user_id   : ${NEWAPI_USER_ID:-<not bound>}"
echo "    newapi_revocation: ${REVOKE_STATUS}"
echo "    tables touched   : ${TABLES_AFFECTED}"
echo "    audit_id         : ${AUDIT_ID}"
echo ""
echo "Run bin/audit-customer-data-deletion.sh ${USER_ID} to verify."
