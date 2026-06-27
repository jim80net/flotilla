#!/usr/bin/env bash
# check-private-boundary.sh — the public/private boundary guard.
#
# flotilla is a PUBLIC product (jim80net/flotilla) that is dogfooded on a private
# deployment. Public artifacts — code, tests, docs, issues, PRs — must describe
# GENERIC flotilla capabilities and NEVER carry deployment-specific identifiers
# (a private fleet's desk names, org/venture names, broker/vendor names, account
# ids). This guard greps a denylist of known-private tokens and FAILS on a hit,
# so a deployment specific can never silently re-enter the public surface. It is
# the executable form of docs/private-public-boundary.md.
#
# Usage:
#   scripts/check-private-boundary.sh            # scan the tracked repo TREE (CI default)
#   scripts/check-private-boundary.sh --issues   # ALSO scan open issues + PRs via `gh`
#
# Exit: 0 = clean, 1 = a private token was found (the offending lines are printed).
#
# Why a high-signal denylist (not every ambiguous word): the guard must never
# flap on a legitimate term, or it gets disabled and stops protecting anything.
# The tokens below have ZERO legitimate use in this codebase. Genuinely ambiguous
# deployment words (instrument tickers, common nouns that double as desk names)
# are handled by review + the doctrine, not by this guard — see the doc.

set -euo pipefail

# --- the denylist: deployment-specific identifiers with no generic meaning -----
# Desk/agent names, org/venture names, broker/data-vendor names, account ids.
# Extend this when a new private identifier is coined in the deployment.
DENYLIST='hydra-ops|family-office|tactical-head|empath-lead|empath-build|empath-research|grok-research|macro-desk-dev|crypto-trend-dev|world-model-dev|x-signal-dev|ib-rest-dev|openclaude-migration|\bmemex\b|General-ML|RamTank|Empath\.ai|Databento|Interactive Brokers|DUM[0-9]{4,}|\bSpark\b'

# Files that legitimately CONTAIN the denylist (this guard + the doctrine that
# documents it) are excluded, or the guard would trip on its own examples.
SELF_EXCLUDE=(':(exclude)scripts/check-private-boundary.sh' ':(exclude)docs/private-public-boundary.md')

fail=0

scan_tree() {
  echo "== boundary guard: scanning tracked tree =="
  # git grep over tracked files only; -I skips binaries (demo media is verified
  # by reading its tracked HTML source, not by byte-grepping the recording).
  if hits="$(git grep -nIE "$DENYLIST" -- . "${SELF_EXCLUDE[@]}" 2>/dev/null)"; then
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
  # Open issues+PRs are the live browsable surface; closed ones are lower-priority
  # (a one-off sweep handles history). Titles + bodies.
  local payload
  payload="$(gh issue list --state open --limit 300 --json number,title,body 2>/dev/null || echo '[]')"
  payload="$payload$(gh pr list --state open --limit 300 --json number,title,body 2>/dev/null || echo '[]')"
  if echo "$payload" | grep -nIE "$DENYLIST" >/dev/null 2>&1; then
    echo "PRIVATE TOKEN found in an open issue/PR:"
    echo "$payload" | grep -oIE "\"number\":[0-9]+|$DENYLIST" | grep -B1 -E "$DENYLIST" || true
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
