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
#   scripts/check-private-boundary.sh --file F    # scan ONE file's contents (the git
#                                                  # pre-push hook + the conformance test
#                                                  # use this; no `git grep` over the tree)
#
# Exit: 0 = clean OR advisory-warn-only, 1 = a fail-closed private token was found.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
DENYLIST_FILE="${FLOTILLA_PRIVATE_DENYLIST_FILE:-$repo_root/.flotilla/private-denylist}"
WARNLIST_FILE="${FLOTILLA_PRIVATE_WARNLIST_FILE:-$repo_root/.flotilla/private-warnlist}"

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
  'sk-(ant-)?[A-Za-z0-9_-]{20,}'
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
# fail-closed + advisory-warn tiers. The git pre-push hook and the Go conformance
# test use this so neither depends on `git grep` over a committed tree.
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

scan_issues() {
  echo "== boundary guard: scanning open issues + PRs (gh) =="
  command -v gh >/dev/null || { echo "gh not available; skipping issues scan"; return; }
  local payload
  payload="$(gh issue list --state open --limit 300 --json number,title,body 2>/dev/null || echo '[]')"
  payload="$payload$(gh pr list --state open --limit 300 --json number,title,body 2>/dev/null || echo '[]')"
  if echo "$payload" | grep -nIP "$full_alternation" >/dev/null 2>&1; then
    echo "PRIVATE TOKEN found in an open issue/PR:"
    echo "$payload" | grep -oIP "\"number\":[0-9]+|$full_alternation" | grep -B1 -P "$full_alternation" || true
    fail=1
  else
    echo "open issues/PRs clean."
  fi
  if [ -n "$warn_alternation" ]; then
    warn_report "$(echo "$payload" | grep -nIP "$warn_alternation" 2>/dev/null || true)"
  fi
}

# Dispatch. `--file F` scans one file (hook + conformance test); otherwise the tree
# scan runs, with `--issues` adding the open-issue/PR scan.
if [ "${1:-}" = "--file" ]; then
  [ -n "${2:-}" ] || { echo "usage: $0 --file <path>"; exit 2; }
  scan_file "$2"
else
  scan_tree
  [ "${1:-}" = "--issues" ] && scan_issues
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
