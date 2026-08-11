#!/usr/bin/env bash
# check-private-boundary.sh — the public/private boundary guard.
#
# flotilla is a PUBLIC, open-source product. You dogfood it on YOUR OWN private
# deployment (your fleet: your desk names, your org, your broker/data vendor,
# your accounts). The product is public; your deployment is not. This guard keeps
# your deployment's specifics from leaking into the public tree (and, with
# --issues, into open issues + PRs). It is the executable form of
# docs/private-public-boundary.md.
#
# Written is not readable, present is not reachable. Publication clearance is
# therefore a remote-reachability question: --history scans every commit
# fetchable from every ref advertised by origin, not merely the working tree,
# current tip, or refs already present in this clone.
#
# TWO layers of protection:
#   1. BUILT-IN, deployment-AGNOSTIC patterns (below) — leaks that are private for
#      ANYONE: absolute home paths revealing a username, chat webhook URLs, common
#      secret shapes. These run with no configuration.
#   2. YOUR deployment denylist — the names only YOUR fleet uses (desks, org,
#      broker, vendor). These are NOT shipped in this script (that would publish
#      your vocabulary). Provide them via either:
#         - a gitignored file:  .flotilla/private-denylist   (one regex/term per
#           line; blank lines and #-comments ignored), or
#         - an env var:         FLOTILLA_PRIVATE_DENYLIST="term1|term2|..."
#                               (a single regex alternation; used by CI from a
#                               repo secret so the list is never in the tree).
#      Copy .flotilla/private-denylist.example to get started.
#
#   3. YOUR deployment WARNLIST (advisory) — your domain VOCABULARY (jargon woven
#      into free text or example names that would deanonymize the fleet). A hit here
#      is ADVISORY: it prints a WARN section and EXITS 0 — it never fails. Same
#      gitignored loading as the denylist:
#         - a gitignored file:  .flotilla/private-warnlist, or
#         - an env var:         FLOTILLA_PRIVATE_WARNLIST="term1|term2|..."
#      Copy .flotilla/private-warnlist.example to get started. The runtime firewall
#      (internal/readermap) reads the SAME sources; a conformance test enforces that
#      the two engines agree.
#
# Usage:
#   scripts/check-private-boundary.sh            # scan the tracked repo TREE (CI default)
#   scripts/check-private-boundary.sh --issues   # ALSO scan open issues + PRs via `gh`
#   scripts/check-private-boundary.sh --history  # fetch + scan every commit reachable from ALL origin refs
#   scripts/check-private-boundary.sh --file F    # scan ONE file's contents (the git
#                                                  # pre-commit + pre-push hooks and the
#                                                  # conformance test use this; no `git
#                                                  # grep` over the tree)
#
# Exit: 0 = clean OR advisory-warn-only, 1 = a fail-closed private token was found.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
DENYLIST_FILE="${FLOTILLA_PRIVATE_DENYLIST_FILE:-$repo_root/.flotilla/private-denylist}"
WARNLIST_FILE="${FLOTILLA_PRIVATE_WARNLIST_FILE:-$repo_root/.flotilla/private-warnlist}"
HISTORY_PATHS_FILE="${FLOTILLA_PRIVATE_HISTORY_PATHS_FILE:-$repo_root/.flotilla/private-history-paths}"

# --- 1. built-in, deployment-AGNOSTIC patterns --------------------------------
# Private for ANY deployment, no configuration required. Kept high-signal so the
# guard never flaps. "/home/operator" and friends are the generic placeholders
# this project documents, so they are explicitly allowed.
GENERIC_PATTERNS=(
  '/home/(?!(?:operator|user|runner|youruser|you)(?![a-z0-9_-]))[a-z_][a-z0-9_-]*'
  '/Users/(?!(?:operator|user|you|youruser)(?![A-Za-z0-9_-]))[A-Za-z][A-Za-z0-9_-]*'
  'https?://(discord(app)?|slack)\.com/api/webhooks/[0-9]+/[A-Za-z0-9_-]{16,}'
  'ghp_[A-Za-z0-9]{20,}'
  'github_pat_[A-Za-z0-9_]{20,}'
  'xox[baprs]-[A-Za-z0-9-]{10,}'
  'xai-[A-Za-z0-9]{20,}'
  '(?<![A-Za-z0-9_-])sk-(ant-)?[A-Za-z0-9_-]{20,}(?![A-Za-z0-9_-])'
  'AKIA[0-9A-Z]{16}'
  '-----BEGIN [A-Z ]*PRIVATE KEY-----'
)

# --- 2. YOUR deployment denylist (from file or env; NEVER hard-coded here) ----
deployment_alternation=""
if [ -n "${FLOTILLA_PRIVATE_DENYLIST:-}" ]; then
  deployment_alternation="$FLOTILLA_PRIVATE_DENYLIST"
elif [ -f "$DENYLIST_FILE" ]; then
  # one term/regex per line; strip comments + blanks; join with '|'. The `|| true`
  # keeps a comment-only/empty file from aborting under `set -e` (grep exits 1 on no
  # match) — an empty result is then treated as "no deployment denylist" below, so a
  # freshly-copied template still runs the built-in generic checks.
  deployment_alternation="$(grep -vE '^[[:space:]]*(#|$)' "$DENYLIST_FILE" | paste -sd '|' - || true)"
fi

# Assemble the full FAIL-CLOSED pattern (generic always; deployment denylist only if
# configured). A hit here REFUSES (exit 1).
full_alternation="$(printf '%s|' "${GENERIC_PATTERNS[@]}")"
full_alternation="${full_alternation%|}"
[ -n "$deployment_alternation" ] && full_alternation="$full_alternation|$deployment_alternation"

# History path classes are independent of content tokens: deployment state can
# be private because of what the path represents even when the blob text happens
# to contain no configured token. Generic carrier classes ship here; private
# deployments extend them from a gitignored file or environment regex.
GENERIC_HISTORY_PATH_PATTERNS=(
  '(^|/)\.flotilla/handoffs(/|$)'
  '(^|/)\.flotilla/state(/|$)'
  '(^|/)\.flotilla/switch(/|$)'
)
deployment_history_paths=""
if [ -n "${FLOTILLA_PRIVATE_HISTORY_PATHS:-}" ]; then
  deployment_history_paths="$FLOTILLA_PRIVATE_HISTORY_PATHS"
elif [ -f "$HISTORY_PATHS_FILE" ]; then
  deployment_history_paths="$(grep -vE '^[[:space:]]*(#|$)' "$HISTORY_PATHS_FILE" | paste -sd '|' - || true)"
fi
history_path_alternation="$(printf '%s|' "${GENERIC_HISTORY_PATH_PATTERNS[@]}")"
history_path_alternation="${history_path_alternation%|}"
[ -n "$deployment_history_paths" ] && history_path_alternation="$history_path_alternation|$deployment_history_paths"

# --- 3. YOUR deployment WARNLIST (advisory; from file or env; NEVER hard-coded) ---
# Loaded EXACTLY like the denylist (the deployment vocabulary is never committed),
# but a hit is ADVISORY: a WARN section + exit 0, never a failure. The runtime
# firewall (internal/readermap) reads the same sources; a conformance test enforces
# that the two engines give identical Refuse/Warn/OK verdicts.
warn_alternation=""
if [ -n "${FLOTILLA_PRIVATE_WARNLIST:-}" ]; then
  warn_alternation="$FLOTILLA_PRIVATE_WARNLIST"
elif [ -f "$WARNLIST_FILE" ]; then
  warn_alternation="$(grep -vE '^[[:space:]]*(#|$)' "$WARNLIST_FILE" | paste -sd '|' - || true)"
fi

# Files that legitimately contain the patterns (this guard + the doctrine that
# documents the scheme + the example deny/warn lists) are excluded from the tree scan.
SELF_EXCLUDE=(
  ':(exclude)scripts/check-private-boundary.sh'
  ':(exclude)docs/private-public-boundary.md'
  ':(exclude).flotilla/private-denylist.example'
  ':(exclude).flotilla/private-warnlist.example'
  ':(exclude).flotilla/private-history-paths.example'
)

fail=0

# warn_report prints the advisory WARN section for any warnlist hits in $1 (the
# already-collected grep output, possibly empty). It NEVER sets fail — the WARN tier
# is advisory on both egresses (CI exits 0; the runtime publishes anyway). A human
# adjudicates each line. With no warnlist configured it is a silent no-op.
warn_report() {
  local hits="$1"
  [ -z "$warn_alternation" ] && return 0
  [ -z "$hits" ] && return 0
  echo ""
  echo "-- ADVISORY WARN (domain vocabulary — human-adjudicate; NOT a failure) --"
  echo "$hits"
  echo "(advisory only: review whether this deanonymizes the deployment; exit stays 0)"
}

scan_tree() {
  echo "== boundary guard: scanning tracked tree =="
  if [ -z "$deployment_alternation" ]; then
    echo "note: no deployment denylist configured (.flotilla/private-denylist or"
    echo "      \$FLOTILLA_PRIVATE_DENYLIST) — running built-in generic patterns only."
  fi
  # git grep over tracked files only; -I skips binaries (demo media is verified
  # by reading its tracked HTML source, not by byte-grepping the recording).
  if hits="$(git -C "$repo_root" grep -nIP "$full_alternation" -- . "${SELF_EXCLUDE[@]}" 2>/dev/null)"; then
    echo "PRIVATE TOKEN found in the tracked tree:"
    echo "$hits"
    fail=1
  else
    echo "tree clean."
  fi
  if [ -n "$warn_alternation" ]; then
    local whits
    whits="$(git -C "$repo_root" grep -nIP "$warn_alternation" -- . "${SELF_EXCLUDE[@]}" 2>/dev/null || true)"
    warn_report "$whits"
  fi
}

# scan_file scans ONE file's contents (not the tracked tree) with the same
# fail-closed + advisory-warn tiers. The git pre-commit / pre-push hooks and the
# Go conformance test use this so neither depends on `git grep` over a committed tree.
scan_file() {
  local f="$1"
  echo "== boundary guard: scanning file $f =="
  [ -f "$f" ] || { echo "no such file: $f"; fail=1; return; }
  if hits="$(grep -nIP "$full_alternation" "$f" 2>/dev/null)"; then
    echo "PRIVATE TOKEN found in $f:"
    echo "$hits"
    fail=1
  else
    echo "file clean (fail-closed tier)."
  fi
  if [ -n "$warn_alternation" ]; then
    local whits
    whits="$(grep -nIP "$warn_alternation" "$f" 2>/dev/null || true)"
    warn_report "$whits"
  fi
}

scan_issue_payload() {
  local payload="$1"
  local source="$2"
  local encoded object number content
  if ! echo "$payload" | jq -e 'type == "array"' >/dev/null 2>&1; then
    echo "boundary scan error: $source payload is not a JSON array"
    fail=1
    return
  fi
  while IFS= read -r encoded; do
    [ -z "$encoded" ] && continue
    if ! object="$(printf '%s' "$encoded" | base64 --decode 2>/dev/null)"; then
      echo "boundary scan error: could not decode a $source object"
      fail=1
      continue
    fi
    if ! number="$(printf '%s' "$object" | jq -er '.number | numbers' 2>/dev/null)"; then
      echo "boundary scan error: $source object has no numeric number"
      fail=1
      continue
    fi
    if ! content="$(printf '%s' "$object" | jq -er '
      if ((.title | type) == "string" and (.body | type) == "string")
      then [.title, .body] | join("\n")
      else error("title/body must be strings")
      end
    ' 2>/dev/null)"; then
      echo "boundary scan error: $source #$number has invalid title/body fields"
      fail=1
      continue
    fi
    if printf '%s\n' "$content" | grep -qIP "$full_alternation" 2>/dev/null; then
      echo "$source #$number"
      fail=1
    fi
    if [ -n "$warn_alternation" ] && printf '%s\n' "$content" | grep -qIP "$warn_alternation" 2>/dev/null; then
      warn_report "$source #$number"
    fi
  done < <(echo "$payload" | jq -r '.[] | @base64' 2>/dev/null)
}

scan_issues() {
	echo "== boundary guard: scanning open issues + PRs (gh) =="
	command -v gh >/dev/null || { echo "gh not available; skipping issues scan"; return; }
	local issues prs before
	if ! issues="$(gh issue list --state open --limit 300 --json number,title,body 2>/dev/null)"; then
		echo "boundary scan error: could not list open issues"
		fail=1
		return
	fi
	if ! prs="$(gh pr list --state open --limit 300 --json number,title,body 2>/dev/null)"; then
		echo "boundary scan error: could not list open PRs"
		fail=1
		return
	fi
	before="$fail"
	scan_issue_payload "$issues" "issue"
	scan_issue_payload "$prs" "PR"
	if [ "$fail" -eq "$before" ]; then
		echo "open issues/PRs clean."
	else
		echo "PRIVATE TOKEN found in the open issue/PR objects listed above."
	fi
}

declare -A history_seen=()
history_scan_namespace="refs/flotilla-boundary-scan"

clear_history_scan_refs() {
  local ref
  while IFS= read -r ref; do
    [ -z "$ref" ] || git -C "$repo_root" update-ref -d "$ref" >/dev/null 2>&1 || true
  done < <(git -C "$repo_root" for-each-ref --format='%(refname)' "$history_scan_namespace" 2>/dev/null || true)
}

safe_history_location() {
  local value="$1"
  if printf '%s\n' "$value" | grep -qP "$full_alternation" 2>/dev/null; then
    printf '<redacted-path>'
  else
    printf '%s' "$value"
  fi
}

safe_history_ref() {
  local value="$1"
  if printf '%s\n' "$value" | grep -qP "$full_alternation" 2>/dev/null; then
    printf '<redacted-ref>'
  else
    printf '%s' "$value"
  fi
}

history_reachability() {
  local commit="$1" scan_ref original family
  local -a refs=()
  declare -A families=()
  while IFS= read -r scan_ref; do
    [ -z "$scan_ref" ] && continue
    if git -C "$repo_root" merge-base --is-ancestor "$commit" "$scan_ref" 2>/dev/null; then
      original="refs/${scan_ref#"$history_scan_namespace"/}"
      refs+=("$original")
      family="${original#refs/}"
      family="${family%%/*}/*"
      families["$family"]=$(( ${families["$family"]:-0} + 1 ))
    fi
  done < <(git -C "$repo_root" for-each-ref --format='%(refname)' "$history_scan_namespace")

  printf '  reachability: ref_count=%d families=' "${#refs[@]}"
  local first=1
  for family in "${!families[@]}"; do
    [ "$first" -eq 1 ] || printf ','
    printf '%s:%d' "$family" "${families[$family]}"
    first=0
  done
  printf '\n'
  for original in "${refs[@]}"; do
    printf '  reachable-ref: %s\n' "$(safe_history_ref "$original")"
  done
}

history_hit() {
  local commit="$1"
  local path="$2"
	local class="${3:-content-token}"
	local key="$commit:$path:$class"
	[ -n "${history_seen[$key]:-}" ] && return
	history_seen[$key]=1
  printf '%s location=%s class=%s\n' "$commit" "$(safe_history_location "$path")" "$class"
  history_reachability "$commit"
  fail=1
}

scan_history() {
  echo "== boundary guard: scanning full remote all-refs reachability =="
  if ! git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
    echo "boundary history scan error: not a git repository"
    fail=1
    return
  fi
  if [ "$(git -C "$repo_root" rev-parse --is-shallow-repository 2>/dev/null || echo unknown)" != "false" ]; then
    echo "boundary history scan error: repository history is shallow or unreadable; remote reachability cannot be proven"
    fail=1
    return
  fi
  if ! git -C "$repo_root" remote get-url origin >/dev/null 2>&1; then
    echo "boundary history scan error: origin remote is unavailable; remote fetchability cannot be established"
    fail=1
    return
  fi
	local pattern_rc
	set +e
	printf '' | grep -qP "$full_alternation" 2>/dev/null
	pattern_rc=$?
	set -e
	if [ "$pattern_rc" -gt 1 ]; then
		echo "boundary history scan error: content token pattern is invalid"
		fail=1
		return
	fi
	set +e
	printf '' | grep -qP "$history_path_alternation" 2>/dev/null
	pattern_rc=$?
	set -e
	if [ "$pattern_rc" -gt 1 ]; then
		echo "boundary history scan error: history path pattern is invalid"
		fail=1
		return
	fi

  local advertised commits commit message paths path content_hits rc before
  if ! advertised="$(git -C "$repo_root" ls-remote --refs origin 2>/dev/null)" || [ -z "$advertised" ]; then
    echo "boundary history scan error: could not enumerate refs advertised by origin"
    fail=1
    return
  fi
  clear_history_scan_refs
  if ! git -C "$repo_root" fetch --quiet --no-tags origin "+refs/*:$history_scan_namespace/*"; then
    echo "boundary history scan error: could not fetch every ref advertised by origin"
    clear_history_scan_refs
    fail=1
    return
  fi
  if ! commits="$(git -C "$repo_root" for-each-ref --format='%(objectname)' "$history_scan_namespace" | git -C "$repo_root" rev-list --stdin 2>/dev/null | sort -u)"; then
    echo "boundary history scan error: could not enumerate commits reachable from fetched origin refs"
    clear_history_scan_refs
    fail=1
    return
  fi
  before="$fail"
  while IFS= read -r commit; do
    [ -z "$commit" ] && continue

    # Commit messages are public history too. They have no repository path, so
    # report the synthetic location only — never echo the matching text.
    if ! message="$(git -C "$repo_root" show -s --format='%B' "$commit" 2>/dev/null)"; then
      echo "boundary history scan error: could not read commit $commit"
      fail=1
      continue
    fi
    set +e
    printf '%s\n' "$message" | grep -qIP "$full_alternation" 2>/dev/null
    rc=$?
    set -e
    if [ "$rc" -eq 0 ]; then
      history_hit "$commit" "<commit-message>" "content-token"
    elif [ "$rc" -gt 1 ]; then
      echo "boundary history scan error: token pattern evaluation failed at commit $commit"
      fail=1
    fi

    # Changed paths supply the path-class axis and catch private tokens embedded
    # in filenames. This deliberately consults tracked history, never check-ignore.
    if ! paths="$(git -C "$repo_root" diff-tree -m --root --no-commit-id --name-only -r "$commit" 2>/dev/null | sort -u)"; then
      echo "boundary history scan error: could not enumerate paths for commit $commit"
      fail=1
      continue
    fi
    while IFS= read -r path; do
      [ -z "$path" ] && continue
      if printf '%s\n' "$path" | grep -qP "$full_alternation" 2>/dev/null; then
        history_hit "$commit" "$path" "content-token-in-path"
      elif printf '%s\n' "$path" | grep -qP "$history_path_alternation" 2>/dev/null; then
        history_hit "$commit" "$path" "state-carrier-path"
      fi
    done <<<"$paths"

    # Scan each reachable commit snapshot with the exact PCRE engine used by the
    # tip scanner. `-l` returns paths only, so the gate cannot leak matched text.
    set +e
    content_hits="$(git -C "$repo_root" grep -IlP "$full_alternation" "$commit" -- . "${SELF_EXCLUDE[@]}" 2>/dev/null)"
    rc=$?
    set -e
    if [ "$rc" -eq 0 ]; then
      while IFS= read -r path; do
        [ -z "$path" ] && continue
        path="${path#"$commit:"}"
        history_hit "$commit" "$path" "content-token"
      done <<<"$content_hits"
    elif [ "$rc" -gt 1 ]; then
      echo "boundary history scan error: could not scan content at commit $commit"
      fail=1
    fi
  done <<<"$commits"

  clear_history_scan_refs

  if [ "$fail" -eq "$before" ]; then
    echo "history clean."
  else
    echo "PRIVATE HISTORY CARRIER found at the commit/path locations listed above."
  fi
}

# Dispatch. `--file F` scans one file (hook + conformance test); otherwise the tree
# scan runs, with `--issues` adding open GitHub surfaces and `--history` adding
# every commit fetchable from every ref advertised by origin. A tip-only pass is
# snapshot evidence, never publication clearance.
if [ "${1:-}" = "--file" ]; then
  [ -n "${2:-}" ] || { echo "usage: $0 --file <path>"; exit 2; }
  scan_file "$2"
else
  case "${1:-}" in
    --issues) scan_tree; scan_issues ;;
    --history) scan_history ;;
    "") scan_tree ;;
    *) echo "usage: $0 [--issues|--history|--file <path>]"; exit 2 ;;
  esac
fi

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "BOUNDARY BREACH: a deployment-specific identifier reached the public surface."
  echo "Rewrite it to its GENERIC flotilla abstraction (a desk -> 'a desk'/'the XO';"
  echo "an org -> 'a private deployment'; a broker/vendor -> 'a broker'/'a data vendor')."
  echo "See docs/private-public-boundary.md."
  exit 1
fi
echo "boundary guard: PASS"
