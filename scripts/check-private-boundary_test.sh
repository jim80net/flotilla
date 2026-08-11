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
HISTORY_REMOTE="$TMP/history-origin.git"
mkdir -p "$HISTORY_REPO/scripts"
cp "$ROOT/scripts/check-private-boundary.sh" "$HISTORY_REPO/scripts/"
cp "$ROOT/scripts/take-public.sh" "$HISTORY_REPO/scripts/"
chmod +x "$HISTORY_REPO/scripts/check-private-boundary.sh"
chmod +x "$HISTORY_REPO/scripts/take-public.sh"
git -C "$HISTORY_REPO" init -q
git -C "$HISTORY_REPO" config user.email test@example.invalid
git -C "$HISTORY_REPO" config user.name boundary-test
git -C "$HISTORY_REPO" add scripts/check-private-boundary.sh scripts/take-public.sh
git -C "$HISTORY_REPO" commit -qm init
git init --bare -q "$HISTORY_REMOTE"
git -C "$HISTORY_REPO" remote add origin "$HISTORY_REMOTE"

run_history_guard() {
	if [ "${HISTORY_SKIP_PUSH:-0}" != "1" ]; then
		git -C "$HISTORY_REPO" push --quiet --mirror origin
	fi
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

PUBLISH_CLEAN_MARK="$TMP/publish-clean-ran"
(cd "$HISTORY_REPO" && FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/take-public.sh -- sh -c "touch '$PUBLISH_CLEAN_MARK'") >/dev/null
[[ -e "$PUBLISH_CLEAN_MARK" ]]

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
require_contains "$SIDE_COMMIT location=side.txt"
require_contains 'reachable-ref: refs/heads/retained-history'
require_absent 'TEST_PRIVATE_SIDE_REF'

# A carrier advertised only by an unfetched, non-default remote ref is in scope.
REMOTE_ONLY_REPO="$TMP/remote-only-source"
git clone -q "$HISTORY_REMOTE" "$REMOTE_ONLY_REPO"
git -C "$REMOTE_ONLY_REPO" config user.email test@example.invalid
git -C "$REMOTE_ONLY_REPO" config user.name boundary-test
git -C "$REMOTE_ONLY_REPO" switch -qc remote-only
printf '%s\n' 'TEST_PRIVATE_REMOTE_ONLY' >"$REMOTE_ONLY_REPO/remote-only.txt"
git -C "$REMOTE_ONLY_REPO" add remote-only.txt
git -C "$REMOTE_ONLY_REPO" commit -qm 'remote-only carrier'
REMOTE_ONLY_COMMIT="$(git -C "$REMOTE_ONLY_REPO" rev-parse HEAD)"
git -C "$REMOTE_ONLY_REPO" push -q origin HEAD:refs/backup/private
HISTORY_SKIP_PUSH=1 run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$REMOTE_ONLY_COMMIT location=remote-only.txt"
require_contains 'reachability: ref_count=1 families=backup/*:1'
require_contains 'reachable-ref: refs/backup/private'
require_absent 'TEST_PRIVATE_REMOTE_ONLY'

# Whole-report redaction covers every rendered field, including a token-bearing
# top-level ref family summary rather than only the per-ref line.
git -C "$REMOTE_ONLY_REPO" push -q origin HEAD:refs/TEST_PRIVATE_FAMILY/item
HISTORY_SKIP_PUSH=1 run_history_guard
[[ "$RC" -eq 1 ]]
require_absent 'TEST_PRIVATE_FAMILY'
if printf '%s\n' "$OUTPUT" | grep -qP 'TEST_PRIVATE_[A-Z]+'; then
	echo "history report leaked a denylisted token" >&2
	exit 1
fi

# Subprocess stderr is part of the process egress. A failing fetch diagnostic
# carrying a denylisted value is sanitized before reaching combined output.
REAL_GIT="$(command -v git)"
DIAGNOSTIC_BIN="$TMP/diagnostic-bin"
mkdir -p "$DIAGNOSTIC_BIN"
for tool in bash grep paste jq base64; do
	ln -s "$(command -v "$tool")" "$DIAGNOSTIC_BIN/$tool"
done
printf '%s\n' '#!/usr/bin/env bash' \
	'for arg in "$@"; do' \
	'  if [ "$arg" = fetch ]; then' \
	'    echo "remote diagnostic TEST_PRIVATE_FETCH_STDERR" >&2' \
	'    exit 1' \
	'  fi' \
	'done' \
	'exec "'"$REAL_GIT"'" "$@"' >"$DIAGNOSTIC_BIN/git"
chmod +x "$DIAGNOSTIC_BIN/git"
set +e
OUTPUT="$(cd "$HISTORY_REPO" && env PATH="$DIAGNOSTIC_BIN:/usr/bin:/bin" \
  FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/check-private-boundary.sh --history 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]]
require_contains '<redacted-report-line>'
require_absent 'TEST_PRIVATE_FETCH_STDERR'

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
require_contains "$TOKEN_COMMIT location=notes.txt"
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
require_contains "$PATH_COMMIT location=.flotilla/handoffs/chapter.md"

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
require_contains "$IGNORED_COMMIT location=.flotilla/handoffs/tracked.md"

# Deployment-specific path vocabulary stays configuration-only and extends the
# shipped generic classes.
mkdir -p "$HISTORY_REPO/private-state/exports"
printf '%s\n' 'generic state' >"$HISTORY_REPO/private-state/exports/item.json"
git -C "$HISTORY_REPO" add private-state/exports/item.json
git -C "$HISTORY_REPO" commit -qm 'add configured path carrier'
CONFIG_PATH_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
HISTORY_PATH_PATTERN='(^|/)private-state/exports(/|$)' run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$CONFIG_PATH_COMMIT location=private-state/exports/item.json"

# Token-bearing filenames are locations with sensitive content and are redacted.
printf '%s\n' 'generic body' >"$HISTORY_REPO/TEST_PRIVATE_FILENAME.txt"
git -C "$HISTORY_REPO" add TEST_PRIVATE_FILENAME.txt
git -C "$HISTORY_REPO" commit -qm 'add filename fixture'
FILENAME_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$FILENAME_COMMIT location=<redacted-path> class=content-token-in-path"
require_absent 'TEST_PRIVATE_FILENAME'

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

# The same tip carrier, once committed, is location-only in history mode.
git -C "$HISTORY_REPO" commit -qm 'add tip fixture'
TIP_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
run_history_guard
[[ "$RC" -eq 1 ]]
require_contains "$TIP_COMMIT location=tip.txt"
require_absent 'TEST_PRIVATE_TIP'

# The executable human publication path gates before invoking its command.
PUBLISH_MARK="$TMP/publish-ran"
set +e
OUTPUT="$(cd "$HISTORY_REPO" && FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/take-public.sh -- sh -c "touch '$PUBLISH_MARK'" 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]]
[[ ! -e "$PUBLISH_MARK" ]]

# Mirror publication scans the local ref population the command would push,
# including refs that origin does not advertise yet.
LOCAL_ONLY_BRANCH=local-only-publication-carrier
git -C "$HISTORY_REPO" switch -qc "$LOCAL_ONLY_BRANCH"
printf '%s\n' 'TEST_PRIVATE_LOCAL_ONLY' >"$HISTORY_REPO/local-only.txt"
git -C "$HISTORY_REPO" add local-only.txt
git -C "$HISTORY_REPO" commit -qm 'local-only publication carrier'
LOCAL_ONLY_COMMIT="$(git -C "$HISTORY_REPO" rev-parse HEAD)"
git -C "$HISTORY_REPO" switch -q "$HISTORY_BRANCH"
MIRROR_DEST="$TMP/public-mirror.git"
git init --bare -q "$MIRROR_DEST"
set +e
OUTPUT="$(cd "$HISTORY_REPO" && FLOTILLA_PRIVATE_DENYLIST='TEST_PRIVATE_[A-Z]+' \
  bash scripts/take-public.sh -- git push --mirror "$MIRROR_DEST" 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]]
require_contains "$LOCAL_ONLY_COMMIT location=local-only.txt"
if git --git-dir="$MIRROR_DEST" show-ref --verify --quiet "refs/heads/$LOCAL_ONLY_BRANCH"; then
	echo "mirror command ran despite local-only history carrier" >&2
	exit 1
fi

echo "check-private-boundary object attribution: PASS"
