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

# History publication gate: a clean all-refs history passes.
HISTORY_REPO="$TMP/history-repo"
mkdir -p "$HISTORY_REPO/scripts"
cp "$ROOT/scripts/check-private-boundary.sh" "$HISTORY_REPO/scripts/"
chmod +x "$HISTORY_REPO/scripts/check-private-boundary.sh"
git -C "$HISTORY_REPO" init -q
git -C "$HISTORY_REPO" config user.email test@example.invalid
git -C "$HISTORY_REPO" config user.name boundary-test
git -C "$HISTORY_REPO" add scripts/check-private-boundary.sh
git -C "$HISTORY_REPO" commit -qm init

run_history_guard() {
  set +e
  OUTPUT="$(cd "$HISTORY_REPO" && env \
    FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
    FLOTILLA_PRIVATE_HISTORY_PATHS="${HISTORY_PATH_PATTERN:-}" \
    bash scripts/check-private-boundary.sh --history 2>&1)"
  RC=$?
  set -e
}

run_history_guard
[[ "$RC" -eq 0 ]]
require_contains 'history clean.'

# A shallow clone cannot prove full history and fails closed.
SHALLOW_REPO="$TMP/shallow-repo"
git clone -q --depth 1 "file://$HISTORY_REPO" "$SHALLOW_REPO"
set +e
OUTPUT="$(cd "$SHALLOW_REPO" && FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/check-private-boundary.sh --history 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]]
require_contains 'repository history is shallow or unreadable'

# A carrier reachable only from a non-current ref is still in publication scope.
HISTORY_BRANCH="$(git -C "$HISTORY_REPO" branch --show-current)"
git -C "$HISTORY_REPO" switch -qc retained-history
printf '%s\n' 'TEST_PRIVATE_SIDE_REF' >"$HISTORY_REPO/side.txt"
git -C "$HISTORY_REPO" add side.txt
git -C "$HISTORY_REPO" commit -qm 'add side history fixture'
SIDE_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
git -C "$HISTORY_REPO" switch -q "$HISTORY_BRANCH"
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$SIDE_COMMIT side.txt"
require_absent 'TEST_PRIVATE_SIDE_REF'

# A token committed and deleted from the tip is still refused. History output
# reports location only and never repeats the sensitive content it found.
printf '%s\n' 'TEST_PRIVATE_HISTORY' >"$HISTORY_REPO/notes.txt"
git -C "$HISTORY_REPO" add notes.txt
git -C "$HISTORY_REPO" commit -qm 'add historical fixture'
TOKEN_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
git -C "$HISTORY_REPO" rm -q notes.txt
git -C "$HISTORY_REPO" commit -qm 'remove historical fixture'
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$TOKEN_COMMIT notes.txt"
require_absent 'TEST_PRIVATE_HISTORY'

# Path-class carriers are caught after deletion from the tip.
mkdir -p "$HISTORY_REPO/.flotilla/handoffs"
printf '%s\n' 'generic handoff fixture' >"$HISTORY_REPO/.flotilla/handoffs/chapter.md"
git -C "$HISTORY_REPO" add .flotilla/handoffs/chapter.md
git -C "$HISTORY_REPO" commit -qm 'add handoff carrier'
PATH_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
git -C "$HISTORY_REPO" rm -q .flotilla/handoffs/chapter.md
git -C "$HISTORY_REPO" commit -qm 'remove handoff carrier'
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$PATH_COMMIT .flotilla/handoffs/chapter.md"

# Ignore coverage is not clearance: a tracked path remains in history.
printf '%s\n' '.flotilla/handoffs/' >"$HISTORY_REPO/.gitignore"
mkdir -p "$HISTORY_REPO/.flotilla/handoffs"
printf '%s\n' 'tracked despite ignore' >"$HISTORY_REPO/.flotilla/handoffs/tracked.md"
git -C "$HISTORY_REPO" add .gitignore
git -C "$HISTORY_REPO" add -f .flotilla/handoffs/tracked.md
git -C "$HISTORY_REPO" commit -qm 'track ignored carrier'
IGNORED_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$IGNORED_COMMIT .flotilla/handoffs/tracked.md"

# Deployment-specific path vocabulary stays configuration-only and extends the
# shipped generic classes.
mkdir -p "$HISTORY_REPO/private-state/exports"
printf '%s\n' 'generic state' >"$HISTORY_REPO/private-state/exports/item.json"
git -C "$HISTORY_REPO" add private-state/exports/item.json
git -C "$HISTORY_REPO" commit -qm 'add configured path carrier'
CONFIG_PATH_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
HISTORY_PATH_PATTERN='(^|/)private-state/exports(/|$)' run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$CONFIG_PATH_COMMIT private-state/exports/item.json"

# Tip scanning remains its existing surface and behavior.
printf '%s\n' 'TEST_PRIVATE_TIP' >"$HISTORY_REPO/tip.txt"
git -C "$HISTORY_REPO" add tip.txt
set +e
OUTPUT="$(cd "$HISTORY_REPO" && FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/check-private-boundary.sh 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]]
require_contains 'PRIVATE TOKEN found in the tracked tree:'

echo "check-private-boundary object attribution: PASS"
