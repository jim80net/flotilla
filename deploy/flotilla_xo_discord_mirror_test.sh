#!/usr/bin/env bash
# Smoke-test the mirror hook's mini-brief audit (executive-mini-brief doctrine).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/deploy/flotilla-xo-discord-mirror.sh"
grep -q 'MINI-BRIEF-AUDIT' "$SCRIPT"
grep -q 'has_needs_you_line' "$SCRIPT"
grep -q 'executive-mini-brief' "$SCRIPT"
grep -q 'Nothing needs you' "$SCRIPT"
echo "flotilla-xo-discord-mirror mini-brief audit: OK"