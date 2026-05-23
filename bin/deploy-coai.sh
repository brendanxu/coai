#!/usr/bin/env bash
# Build and deploy a CoAI image to the production VPS.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=bin/deploy-lib.sh
source "$SCRIPT_DIR/deploy-lib.sh"

VERSION_TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) GT_DEPLOY_DRY_RUN=1; shift ;;
    -h|--help)
      sed -n '2,100p' "$0"
      exit 0
      ;;
    -*)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
    *)
      if [[ -n "$VERSION_TAG" ]]; then
        echo "Too many positional args" >&2
        exit 1
      fi
      VERSION_TAG="$1"
      shift
      ;;
  esac
done

if [[ -z "$VERSION_TAG" ]]; then
  echo "Usage: bin/deploy-coai.sh <version-tag> [--dry-run]" >&2
  exit 1
fi

ROOT="$(gt_repo_root)"
IMAGE_REF="$GT_DEPLOY_IMAGE:$VERSION_TAG"
STAMP="$(date +%Y%m%d-%H%M%S)"

gt_script_header "deploy CoAI $IMAGE_REF"

if [[ -f "$ROOT/app/package.json" ]]; then
  echo "plan: build frontend with pnpm"
  gt_run pnpm --dir "$ROOT/app" build
else
  echo "WARN: app/package.json missing; skipping frontend build"
fi

echo "plan: back up current VPS source"
gt_remote "set -euo pipefail; if [ -d '$GT_DEPLOY_VPS_SOURCE' ]; then sudo cp -a '$GT_DEPLOY_VPS_SOURCE' '$GT_DEPLOY_VPS_SOURCE.bak.$STAMP'; fi"

echo "plan: rsync source to $GT_DEPLOY_VPS_HOST:$GT_DEPLOY_VPS_SOURCE"
gt_run rsync -az --delete \
  --exclude .git \
  --exclude node_modules \
  --exclude .playwright-mcp \
  "$ROOT/" "$GT_DEPLOY_VPS_HOST:$GT_DEPLOY_VPS_SOURCE/"

echo "plan: docker build $IMAGE_REF"
gt_remote "set -euo pipefail; cd '$GT_DEPLOY_VPS_SOURCE' && sudo docker build -f Dockerfile.split -t '$IMAGE_REF' ."
echo "plan: update docker-compose.yml image to $IMAGE_REF"
gt_remote "set -euo pipefail; sudo cp '$GT_DEPLOY_COMPOSE_FILE' '$GT_DEPLOY_COMPOSE_FILE.bak.$STAMP'; sudo sed -i 's|$GT_DEPLOY_IMAGE:[^[:space:]]*|$IMAGE_REF|g' '$GT_DEPLOY_COMPOSE_FILE'; grep '$GT_DEPLOY_IMAGE' '$GT_DEPLOY_COMPOSE_FILE'"
echo "plan: docker compose up -d $GT_DEPLOY_SERVICE"
gt_remote "set -euo pipefail; cd '$GT_DEPLOY_VPS_ROOT' && sudo docker compose -f '$GT_DEPLOY_COMPOSE_FILE' up -d '$GT_DEPLOY_SERVICE'"
echo "plan: tail service logs"
gt_remote "sudo docker compose -f '$GT_DEPLOY_COMPOSE_FILE' logs --tail 80 '$GT_DEPLOY_SERVICE' 2>&1 | tail -80"

echo "deploy CoAI complete: $IMAGE_REF"
