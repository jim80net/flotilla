#!/usr/bin/env bash
# Smoke-test the mirror hook's mini-brief audit (executive-mini-brief doctrine).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/deploy/flotilla-xo-discord-mirror.sh"
grep -q 'MINI-BRIEF-AUDIT' "$SCRIPT"
grep -q 'has_action_status_close' "$SCRIPT"
grep -q 'executive-mini-brief' "$SCRIPT"
grep -q 'action-status close' "$SCRIPT"
grep -q 'lines\[-1\]' "$SCRIPT"
# #572: Stop hook drives mirror-self (session-mirror without Working→Idle).
grep -q 'mirror-self' "$SCRIPT"
grep -q 'cos_agent' "$SCRIPT"

# Behavioral regression: last-line-only check (mid-body mention must not pass).
python3 <<'PY'
import re

def has_action_status_close(text):
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    if not lines:
        return False
    last = lines[-1].lower()
    patterns = (
        r"^waiting on you\b",
        r"^nothing needs you\b",
        r"^no action on your side\b",
        r"^no action needed\b",
        r"^you're clear\b",
        r"^you are clear\b",
        r"^all handled\b",
        r"^all set\b",
        r"^you're good\b",
        r"^you are good\b",
        r"^nothing further needed\b",
        r"^nothing needed from you\b",
    )
    return any(re.match(p, last) for p in patterns)

ok = [
    ("closing waiting", "Bottom line.\n\nWaiting on you: merge when ready."),
    ("closing varied all-clear", "Summary.\nNo action on your side."),
    ("closing youre clear", "Done.\nYou're clear."),
    ("closing all handled", "Shipped the fix.\nAll handled."),
    ("legacy nothing needs you still ok", "Done.\nNothing needs you."),
]
for name, text in ok:
    assert has_action_status_close(text), f"expected pass: {name}"

fail = [
    ("mid-body only", "I was waiting on you to approve X.\n\nDetail footer here."),
    ("missing close", "Bottom line only.\nNo explicit close."),
    ("mention not last", "No action on your side mentioned mid-body.\nStill working."),
    ("false positive youre clearly", "Summary.\nYou're clearly still needed on the fork."),
    ("false positive all settings", "Done.\nAll settings saved for next deploy."),
]
for name, text in fail:
    assert not has_action_status_close(text), f"expected fail: {name}"
PY

echo "flotilla-xo-discord-mirror mini-brief audit: OK"