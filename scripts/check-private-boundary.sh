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
# Usage:
#   scripts/check-private-boundary.sh            # scan the tracked repo TREE (CI default)
#   scripts/check-private-boundary.sh --issues   # ALSO scan open issues + PRs via `gh`
#
# Exit: 0 = clean, 1 = a private token was found (offending lines printed).

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
DENYLIST_FILE="${FLOTILLA_PRIVATE_DENYLIST_FILE:-$repo_root/.flotilla/private-denylist}"

# --- 1. built-in, deployment-AGNOSTIC patterns --------------------------------
# Private for ANY deployment, no configuration required. Kept high-signal so the
# guard never flaps. "/home/operator" and friends are the generic placeholders
# this project documents, so they are explicitly allowed.
GENERIC_PATTERNS=(
  '/home/(?!operator\b|user\b|runner\b|youruser\b|you\b)[a-z_][a-z0-9_-]*'
  '/Users/(?!operator\b|user\b|you\b|youruser\b)[A-Za-z][A-Za-z0-9_-]*'
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
  # one term/regex per line; strip comments + blanks; join with '|'
  deployment_alternation="$(grep -vE '^\s*(#|$)' "$DENYLIST_FILE" | paste -sd '|' -)"
fi

# Assemble the full pattern (generic always; deployment only if configured).
full_alternation="$(printf '%s|' "${GENERIC_PATTERNS[@]}")"
full_alternation="${full_alternation%|}"
[ -n "$deployment_alternation" ] && full_alternation="$full_alternation|$deployment_alternation"

# Files that legitimately contain the patterns (this guard + the doctrine that
# documents the scheme + the example denylist) are excluded from the tree scan.
SELF_EXCLUDE=(
  ':(exclude)scripts/check-private-boundary.sh'
  ':(exclude)docs/private-public-boundary.md'
  ':(exclude).flotilla/private-denylist.example'
)

fail=0

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
}

scan_tree
[ "${1:-}" = "--issues" ] && scan_issues

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "BOUNDARY BREACH: a deployment-specific identifier reached the public surface."
  echo "Rewrite it to its GENERIC flotilla abstraction (a desk -> 'a desk'/'the XO';"
  echo "an org -> 'a private deployment'; a broker/vendor -> 'a broker'/'a data vendor')."
  echo "See docs/private-public-boundary.md."
  exit 1
fi
echo "boundary guard: PASS"
