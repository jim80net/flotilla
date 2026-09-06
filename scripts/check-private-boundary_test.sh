#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

REPO="$TMP/repo"
FAKE_BIN="$TMP/bin"
mkdir -p "$REPO/scripts" "$FAKE_BIN"
cp "$ROOT/scripts/check-private-boundary.sh" "$REPO/scripts/"
chmod +x "$REPO/scripts/check-private-boundary.sh"
git -C "$REPO" init -q
git -C "$REPO" config user.email test@example.invalid
git -C "$REPO" config user.name boundary-test
git -C "$REPO" add scripts/check-private-boundary.sh
git -C "$REPO" commit -qm init

cat >"$FAKE_BIN/gh" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  issue) printf '%s\n' "${ISSUES_PAYLOAD:-[]}" ;;
  pr) printf '%s\n' "${PRS_PAYLOAD:-[]}" ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$FAKE_BIN/gh"

run_guard() {
  local issues="$1"
  local prs="${2:-[]}"
  set +e
  OUTPUT="$(cd "$REPO" && env \
    PATH="$FAKE_BIN:$PATH" \
    FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
    FLOTILLA_PRIVATE_WARNLIST="${WARN_PATTERN:-}" \
    ISSUES_PAYLOAD="$issues" PRS_PAYLOAD="$prs" \
    bash scripts/check-private-boundary.sh --issues 2>&1)"
  RC=$?
  set -e
}

require_contains() {
  [[ "$OUTPUT" == *"$1"* ]] || { echo "missing $1 in output:"; echo "$OUTPUT"; exit 1; }
}

require_absent() {
  [[ "$OUTPUT" != *"$1"* ]] || { echo "unexpected $1 in output:"; echo "$OUTPUT"; exit 1; }
}

# A hit in object N is attributed to N, not the preceding object in descending order.
run_guard '[{"body":"clean","number":42,"title":"clean"},{"body":"TEST_PRIVATE_ALPHA","number":41,"title":"hit"}]'
[[ "$RC" -eq 1 ]]
require_contains 'issue #41'
require_absent 'issue #42'

# The first object and title fields are independently scannable.
run_guard '[{"body":"TEST_PRIVATE_FIRST","number":99,"title":"clean"},{"body":"clean","number":98,"title":"clean"}]'
[[ "$RC" -eq 1 ]]
require_contains 'issue #99'
run_guard '[{"body":"clean","number":77,"title":"TEST_PRIVATE_TITLE"}]'
[[ "$RC" -eq 1 ]]
require_contains 'issue #77'

# Multiple issue and PR carriers retain their own identities.
run_guard '[{"body":"TEST_PRIVATE_ONE","number":9,"title":"one"},{"body":"TEST_PRIVATE_TWO","number":7,"title":"two"}]' \
  '[{"body":"TEST_PRIVATE_THREE","number":5,"title":"three"}]'
[[ "$RC" -eq 1 ]]
require_contains 'issue #9'
require_contains 'issue #7'
require_contains 'PR #5'

# A token-shaped substring embedded in a longer identifier is not a token start.
fake_secret="sk-$(printf 'A%.0s' {1..20})"
run_guard "[{\"body\":\"prefix${fake_secret}\",\"number\":6,\"title\":\"clean\"}]"
[[ "$RC" -eq 0 ]]
require_contains 'open issues/PRs clean.'

# Clean and empty payloads stay clean.
run_guard '[{"body":"clean","number":3,"title":"clean"}]'
[[ "$RC" -eq 0 ]]
require_contains 'open issues/PRs clean.'
run_guard '[]' '[]'
[[ "$RC" -eq 0 ]]
require_contains 'open issues/PRs clean.'

# Malformed object fields fail closed instead of disappearing from a string-only map.
run_guard '[{"body":{"nested":"clean"},"number":4,"title":"clean"}]'
[[ "$RC" -eq 1 ]]
require_contains 'boundary scan error: issue #4 has invalid title/body fields'
require_absent 'open issues/PRs clean.'

# Advisory matches use the same per-object attribution without failing the gate.
WARN_PATTERN='TEST_WARN_[A-Z]+' run_guard '[{"body":"TEST_WARN_ALPHA","number":12,"title":"clean"}]'
[[ "$RC" -eq 0 ]]
require_contains 'ADVISORY WARN'
require_contains 'issue #12'

# A host without gh retains the documented skip behavior.
NO_GH_BIN="$TMP/no-gh-bin"
mkdir -p "$NO_GH_BIN"
for tool in bash git grep paste jq base64; do
  ln -s "$(command -v "$tool")" "$NO_GH_BIN/$tool"
done
set +e
OUTPUT="$(cd "$REPO" && PATH="$NO_GH_BIN" /usr/bin/bash scripts/check-private-boundary.sh --issues 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 0 ]]
require_contains 'gh not available; skipping issues scan'

echo "check-private-boundary object attribution: PASS"
