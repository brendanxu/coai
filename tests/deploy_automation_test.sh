#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

run_test() {
  local name="$1"
  shift
  echo "==> $name"
  "$@"
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "Expected output to contain: $needle" >&2
    echo "Actual output:" >&2
    echo "$haystack" >&2
    exit 1
  fi
}

test_orchestrator_dry_run_lists_ordered_steps() {
  local out
  out="$(GT_DEPLOY_ALLOW_DIRTY=1 GT_DEPLOY_ALLOW_OUTSIDE_WINDOW=1 bash "$ROOT/bin/deploy.sh" coai --coai-version v9.9.9 --dry-run)"
  assert_contains "$out" "pre-deploy-check.sh --dry-run --allow-dirty --allow-outside-window"
  assert_contains "$out" "pre-deploy-dump.sh --dry-run"
  assert_contains "$out" "deploy-coai.sh v9.9.9 --dry-run"
  assert_contains "$out" "post-deploy-smoke.sh --dry-run"
  assert_contains "$out" "canary.sh 60 --dry-run"
}

test_orchestrator_non_dry_run_reaches_window_guard() {
  local tmp out
  tmp="$(mktemp)"
  if GT_DEPLOY_NOW_HOUR=18 GT_DEPLOY_ALLOW_DIRTY=1 bash "$ROOT/bin/deploy.sh" coai --coai-version v9.9.9 >"$tmp" 2>&1; then
    echo "Expected non-dry-run orchestrator to fail outside the deploy window" >&2
    cat "$tmp" >&2
    rm -f "$tmp"
    exit 1
  fi
  out="$(cat "$tmp")"
  rm -f "$tmp"
  assert_contains "$out" "outside deploy window"
}

test_deploy_coai_dry_run_uses_version_tag() {
  local out
  out="$(bash "$ROOT/bin/deploy-coai.sh" v9.9.9 --dry-run)"
  assert_contains "$out" "greentokey-coai:v9.9.9"
  assert_contains "$out" "rsync"
  assert_contains "$out" "docker build"
  assert_contains "$out" "docker compose up -d coai"
}

test_pre_deploy_check_outside_window_blocks_without_override() {
  local tmp out
  tmp="$(mktemp)"
  if GT_DEPLOY_NOW_HOUR=18 GT_DEPLOY_ALLOW_DIRTY=1 bash "$ROOT/bin/pre-deploy-check.sh" --dry-run >"$tmp" 2>&1; then
    echo "Expected outside-window preflight to fail" >&2
    cat "$tmp" >&2
    rm -f "$tmp"
    exit 1
  fi
  out="$(cat "$tmp")"
  rm -f "$tmp"
  assert_contains "$out" "outside deploy window"
}

test_deploy_config_dry_run_backs_up_and_restarts_service() {
  local out
  out="$(bash "$ROOT/bin/deploy-config.sh" Caddyfile caddy --dry-run)"
  assert_contains "$out" "deploy config Caddyfile -> caddy"
  assert_contains "$out" "sudo cp"
  assert_contains "$out" "sudo mv"
  assert_contains "$out" "docker compose restart caddy"
}

run_test "orchestrator dry-run" test_orchestrator_dry_run_lists_ordered_steps
run_test "orchestrator non-dry-run window guard" test_orchestrator_non_dry_run_reaches_window_guard
run_test "deploy coai dry-run" test_deploy_coai_dry_run_uses_version_tag
run_test "deploy window guard" test_pre_deploy_check_outside_window_blocks_without_override
run_test "deploy config dry-run" test_deploy_config_dry_run_backs_up_and_restarts_service
echo "deploy automation tests passed"
