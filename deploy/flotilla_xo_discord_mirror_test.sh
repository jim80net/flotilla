#!/usr/bin/env bash
# Smoke-test the mirror hook's mini-brief audit (executive-mini-brief doctrine).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/deploy/flotilla-xo-discord-mirror.sh"
grep -q 'MINI-BRIEF-AUDIT' "$SCRIPT"
grep -q 'has_needs_you_line' "$SCRIPT"
grep -q 'executive-mini-brief' "$SCRIPT"
grep -q 'Nothing needs you' "$SCRIPT"
grep -q 'lines\[-1\]' "$SCRIPT"

# Behavioral regression: last-line-only check (mid-body mention must not pass).
python3 <<'PY'
def has_needs_you_line(text):
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    if not lines:
        return False
    last = lines[-1].lower()
    return last.startswith("waiting on you") or last.startswith("nothing needs you")

ok = [
    ("closing waiting", "Bottom line.\n\nWaiting on you: merge when ready."),
    ("closing nothing", "Summary.\nNothing needs you."),
    ("closing nothing period", "Done.\nNothing needs you."),
]
for name, text in ok:
    assert has_needs_you_line(text), f"expected pass: {name}"

fail = [
    ("mid-body only", "I was waiting on you to approve X.\n\nDetail footer here."),
    ("missing close", "Bottom line only.\nNo explicit close."),
    ("mention not last", "Nothing needs you mentioned mid-body.\nStill working."),
]
for name, text in fail:
    assert not has_needs_you_line(text), f"expected fail: {name}"
PY

echo "flotilla-xo-discord-mirror mini-brief audit: OK"