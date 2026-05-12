#!/usr/bin/env bash
# install-hooks.sh — wire up .githooks/ as the git hooksPath for this clone.
# Run once per fresh clone. Idempotent.
#
# What this does:
#   git config core.hooksPath .githooks
# That tells git to look at .githooks/ instead of .git/hooks/ for hook
# scripts. .githooks/ IS committed to the repo so all developers share
# the same hooks. .git/hooks/ is per-clone and not committed.
#
# Existing per-clone hooks at .git/hooks/pre-commit (e.g. founder's
# gitleaks hook from PKG-C1 2026-05-07) are PRESERVED but no longer
# consulted by git. To restore them: `git config --unset core.hooksPath`.

set -e

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [ ! -d ".githooks" ]; then
  echo "❌ .githooks/ directory missing. This script expects to run from"
  echo "   a checkout of the greentokey CoAI fork with .githooks/ committed."
  exit 1
fi

# Make all hook scripts executable.
chmod +x .githooks/* 2>/dev/null || true

# Wire up.
current=$(git config --get core.hooksPath 2>/dev/null || echo "")
if [ "$current" = ".githooks" ]; then
  echo "✅ Hooks already wired: core.hooksPath = .githooks"
else
  git config core.hooksPath .githooks
  echo "✅ Hooks wired: core.hooksPath set to .githooks"
fi

# Show what's installed
echo ""
echo "Installed hooks:"
ls -l .githooks/ | grep -v '^total' | awk '{print "  " $NF}'

echo ""
echo "To bypass for one commit: git commit --no-verify"
echo "To uninstall: git config --unset core.hooksPath"
