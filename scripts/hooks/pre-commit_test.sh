#!/usr/bin/env bash
# Smoke test for scripts/hooks/pre-commit (private-boundary staged scan).
# Runs in an isolated temp git repo so the developer's index is never touched.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT/scripts/hooks/pre-commit"
GUARD="$ROOT/scripts/check-private-boundary.sh"

[ -f "$HOOK" ] || { echo "missing pre-commit hook" >&2; exit 1; }
[ -f "$GUARD" ] || { echo "missing boundary guard" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Isolated repo with the real hook + guard paths available via absolute paths
# in a thin wrapper (the hook resolves guard relative to show-toplevel).
git init -q "$tmp/repo"
cd "$tmp/repo"
git config user.email "operator@example.com"
git config user.name "operator"
mkdir -p scripts/hooks scripts
cp "$HOOK" scripts/hooks/pre-commit
cp "$GUARD" scripts/check-private-boundary.sh
chmod +x scripts/hooks/pre-commit scripts/check-private-boundary.sh
# Empty tree base commit so later stages are pure additions.
: > README
git add README
git commit -q -m "init"

# --- clean stage must exit 0 ---
printf 'hello from a generic fixture desk\n' > clean.txt
git add clean.txt
if ! scripts/hooks/pre-commit; then
  echo "FAIL: clean staged content was blocked" >&2
  exit 1
fi
git commit -q -m "clean"
echo "ok: clean stage exits 0"

# --- generic fail-closed leak must exit 1 ---
# Username-revealing home path (not in the allowlist operator/user/runner/youruser/you).
# Built dynamically so this test SCRIPT itself never embeds a continuous leak token
# that would trip the tree scan when the test is committed.
home_user=jim
printf 'path = /home/%s/workspace/secret-deploy\n' "$home_user" > leak.txt
git add leak.txt
set +e
scripts/hooks/pre-commit
rc=$?
set -e
if [ "$rc" -eq 0 ]; then
  echo "FAIL: staged username home path should exit 1" >&2
  exit 1
fi
echo "ok: leak stage exits $rc (non-zero)"

# Unstage so we leave the temp repo tidy (not required).
git reset -q HEAD leak.txt
rm -f leak.txt

echo "pre-commit_test.sh: all checks passed"
