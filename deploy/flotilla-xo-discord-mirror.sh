#!/usr/bin/env bash
# flotilla-xo-discord-mirror.sh — Claude Code Stop hook.
# MECHANICAL mirror: records the coordinator turn-final in session-mirror on every
# Stop; current binaries keep Discord explicit-parade-only (fail-safe exit 0). Self-gates to xo_agent / cos_agent / *-xo seats
# (pane-less remote-control coordinators never get Working→Idle — #432/#572 use
# `flotilla mirror-self` so dash conversations still populate).
# Set FLOTILLA_MIRROR_DRYRUN=1 to print instead of recording (for testing).
#
# Decision log: ~/.claude/hooks/flotilla-xo-mirror.log (one line/invocation).
#
# 2026-07-01 P0 reliability pass (operator: mechanical posting — no discretionary
# trigger filtering). Prior bugs retained; NEW fixes:
#  BUG 5: task-notification / heartbeat trigger filters dropped substantive
#         turn-finals (249 COS turns in one session alone). Trigger is logged
#         only — never suppresses a non-empty turn-final.
#  BUG 6: no-transcript race — Stop can fire before transcript_path is flushed;
#         wait ≤5s for the file to appear.
#  BUG 7: stabilization keyed on turn-final TEXT stability (not tool-trailing
#         "clean" bit) so tool-heavy turns don't post mid-turn fragments.
#  BUG 8: forward-scan last text-bearing assistant (claudestore parity) + trigger
#         walk skips tool_result-only user entries.
# Chunking delegated to `flotilla notify --chunk` (BUG-4 belt-and-suspenders).
#
# TURN-FINAL SHAPE (doctrine-injected — the hook posts verbatim, never rewrites):
# Coordinators MUST write operator-facing turn-finals as executive mini-briefs:
#   1. Bottom line first (plain English, 1–2 sentences)
#   2. Mini brief (2–5 bullets; name streams by what they DO)
#   3. Detail footer optional (IDs/SHAs/paths last)
#   4. Always close with explicit action status on the LAST line (concrete ask OR
#      varied all-clear — not one fixed verbatim formula every turn).
# See internal/doctrine/assets/skills/executive-mini-brief.md (installed via
# `flotilla doctrine install`). The hook logs MINI-BRIEF-AUDIT when (4) is missing.
#
# REQUIRED host env (set by the operator's private fleet-ops install — no defaults
# in this public script): FLOTILLA_ROSTER, FLOTILLA_SECRETS. Optional: FLOTILLA_BIN
# (defaults to `flotilla` on PATH). Fail-safe exit 0 when unset or missing.
ROSTER="${FLOTILLA_ROSTER:-}"
SECRETS="${FLOTILLA_SECRETS:-}"
FLOTILLA="${FLOTILLA_BIN:-flotilla}"
if [[ -z "$ROSTER" || -z "$SECRETS" ]]; then exit 0; fi
if [[ ! -f "$ROSTER" || ! -f "$SECRETS" ]]; then exit 0; fi

payload="$(cat 2>/dev/null)" || exit 0

# Self-gate: coordinator seats only (xo_agent, cos_agent, or *-xo name). Desks use
# detector Working→Idle MirrorOnFinish — dual-firing would double-post Discord.
# TMUX_PANE is preferred for marker; if unset (remote-control / pane-less), fall back
# to FLOTILLA_SELF so a Stop hook without a pane still mirrors (#572).
marker=""
if [ -n "${TMUX_PANE:-}" ]; then
  marker="$(tmux show-options -pv -t "$TMUX_PANE" @flotilla_agent 2>/dev/null)" || true
fi
if [ -z "$marker" ]; then
  marker="${FLOTILLA_SELF:-}"
fi
[ -n "$marker" ] || exit 0
coord_ok="$(python3 -c "
import json,sys
r=json.load(open('$ROSTER'))
m=sys.argv[1]
xo=r.get('xo_agent') or ''
cos=r.get('cos_agent') or ''
if m and m in (xo, cos):
    print('1'); raise SystemExit
for a in r.get('agents') or []:
    if a.get('name')==m and a.get('coordinator') is True:
        print('1'); raise SystemExit
if m.endswith('-xo'):
    print('1'); raise SystemExit
print('0')
" "$marker" 2>/dev/null)" || exit 0
[ "$coord_ok" = "1" ] || exit 0

python3 - "$payload" "$marker" "$SECRETS" "$FLOTILLA" "$ROSTER" <<'PY' 2>/dev/null || exit 0
import json, re, sys, subprocess, tempfile, os, time, datetime
payload, agent, secrets, flotilla, roster = sys.argv[1:6]

_LOG = os.path.expanduser("~/.claude/hooks/flotilla-xo-mirror.log")
def lg(decision, detail=""):
    try:
        with open(_LOG, "a") as f:
            ts = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
            f.write(f"{ts} {decision} {detail}\n")
    except Exception:
        pass
def skip(reason):
    lg("SKIP", reason); sys.exit(0)

try: p = json.loads(payload)
except Exception: skip("bad-payload-json")
if p.get("stop_hook_active"): skip("stop_hook_active")

# BUG-6: wait for transcript_path / file (Stop-vs-path race).
tpath = p.get("transcript_path","")
deadline_path = time.time() + 5.0
while (not tpath or not os.path.isfile(tpath)) and time.time() < deadline_path:
    time.sleep(0.4)
    tpath = p.get("transcript_path","")
if not tpath or not os.path.isfile(tpath): skip("no-transcript")

def text_of(o):
    m = o.get("message", o) or {}
    c = m.get("content")
    if isinstance(c, str): return c
    if isinstance(c, list):
        return "\n".join(b.get("text","") for b in c if isinstance(b,dict) and b.get("type")=="text")
    return ""

def role_of(o):
    return ((o.get("message",{}) or {}).get("role")) or o.get("type","")

def block_types(o):
    c = (o.get("message",{}) or {}).get("content")
    if isinstance(c, list):
        return [b.get("type") for b in c if isinstance(b, dict)]
    return []

def is_tool_result_only(o):
    bt = block_types(o)
    return bt and all(b == "tool_result" for b in bt)

# Strip harness-injected blocks for trigger logging (BUG-2).
_STRIP = re.compile(
    r"<command-name>.*?</command-name>"
    r"|<command-message>.*?</command-message>"
    r"|<command-args>.*?</command-args>"
    r"|<local-command-stdout>.*?</local-command-stdout>"
    r"|<local-command-caveat>.*?</local-command-caveat>"
    r"|<system-reminder>.*?</system-reminder>",
    re.DOTALL,
)
def operator_residue(t):
    return _STRIP.sub("", t or "").strip()

def load_msgs():
    msgs=[]
    try:
        with open(tpath) as f:
            for line in f:
                line=line.strip()
                if line:
                    try: msgs.append(json.loads(line))
                    except Exception: pass
    except Exception:
        return []
    return msgs

def extract(msgs):
    """Forward-scan last text-bearing assistant (claudestore parity) + trigger walk."""
    resp=None; trig=None; idx=None
    for i, m in enumerate(msgs):
        if role_of(m) == "assistant" and text_of(m).strip():
            resp = text_of(m); idx = i
    if idx is None:
        return None, None
    for j in range(idx - 1, -1, -1):
        if role_of(msgs[j]) != "user" or is_tool_result_only(msgs[j]):
            continue
        t = text_of(msgs[j]).strip()
        if t:
            trig = text_of(msgs[j]); break
    return resp, trig

# BUG-7: stabilize on turn-final TEXT + file size (not tool-trailing clean bit).
deadline = time.time() + 15.0
waited = 0
prev_resp = None
prev_size = -1
stable = 0
resp = trig = None
while True:
    try: size = os.path.getsize(tpath)
    except Exception: size = -2
    msgs = load_msgs()
    resp, trig = extract(msgs)
    if resp and resp == prev_resp and size == prev_size:
        stable += 1
        if stable >= 2:
            break
    else:
        stable = 0
    if time.time() > deadline:
        lg("TAIL-UNSTABLE", f"waited={waited} stable={stable} resplen={len(resp or '')}")
        break
    prev_resp = resp
    prev_size = size
    waited += 1
    time.sleep(0.8)

if not resp or not resp.strip(): skip("no-assistant-text")

def has_action_status_close(text):
    """Doctrine mandates explicit action status on the LAST line — varied phrasing OK."""
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    if not lines:
        return False
    last = lines[-1].lower()
    # Word-boundary match — avoids false positives ("you're clearly", "all settings").
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

if not has_action_status_close(resp):
    lg("MINI-BRIEF-AUDIT", "missing explicit action-status close on last line")

# BUG-5: mechanical post — trigger logged only, never suppresses.
residue = operator_residue(trig)
trig_note = (residue[:60] if residue else "(no-text-bearing-trigger)")

if os.environ.get("FLOTILLA_MIRROR_DRYRUN")=="1":
    lg("DRYRUN-POST", f"{len(resp)}ch waited={waited} trig={trig_note!r}")
    print("WOULD POST as %s (%d chars):\n%s" % (agent, len(resp), resp[:400])); sys.exit(0)

with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as tf:
    tf.write(resp); tmp=tf.name
try:
    # #572: mirror-self writes session-mirror (+ Discord when webhook present) without
    # requiring detector Working→Idle — the remote-control / pane-less coordinator path.
    # Legacy-binary compatibility only: falls back to notify --chunk when an older
    # binary does not provide mirror-self. Current mirror-self is best-effort and
    # keeps routine finals ledger-only.
    r = subprocess.run(
        [flotilla, "mirror-self", "--from", agent, "--secrets", secrets, "--roster", roster, "--file", tmp],
        check=False, timeout=60, capture_output=True, text=True,
    )
    if r.returncode != 0:
        # Fallback for pre-#572 binaries still on PATH.
        r2 = subprocess.run(
            [flotilla, "notify", "--chunk", "--from", agent, "--secrets", secrets, "--file", tmp],
            check=False, timeout=60, capture_output=True, text=True,
        )
        if r2.returncode != 0:
            lg("POST-FAIL", f"{len(resp)}ch mirror-self rc={r.returncode} notify rc={r2.returncode} err={(r.stderr or r2.stderr or '')[:160]!r} waited={waited} trig={trig_note!r}")
        else:
            lg("POST-NOTIFY-FALLBACK", f"{len(resp)}ch waited={waited} trig={trig_note!r}")
    else:
        lg("POST", f"{len(resp)}ch mirror-self waited={waited} trig={trig_note!r}")
except Exception as e:
    lg("POST-FAIL", f"{len(resp)}ch exc={type(e).__name__} waited={waited} trig={trig_note!r}")
finally:
    try: os.unlink(tmp)
    except Exception: pass
PY
exit 0
