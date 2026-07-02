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

# Behavioral regression: last-line-only check (mid-body mention must not pass).
python3 <<'PY'
def has_action_status_close(text):
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    if not lines:
        return False
    last = lines[-1].lower()
    if last.startswith("waiting on you"):
        return True
    all_clear = (
        "nothing needs you", "no action on your side", "no action needed",
        "you're clear", "you are clear", "all handled", "all set",
        "you're good", "you are good", "nothing further needed",
        "nothing needed from you",
    )
    return any(last.startswith(p) for p in all_clear)

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
]
for name, text in fail:
    assert not has_action_status_close(text), f"expected fail: {name}"
PY

echo "flotilla-xo-discord-mirror mini-brief audit: OK"