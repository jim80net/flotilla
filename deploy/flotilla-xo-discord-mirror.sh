#!/usr/bin/env bash
# flotilla-xo-discord-mirror.sh — Claude Code Stop hook.
# MECHANICAL mirror: posts the XO pane's turn-final assistant text to Discord on
# every Stop (fail-safe exit 0). Self-gates to the flotilla XO pane only.
# Set FLOTILLA_MIRROR_DRYRUN=1 to print instead of posting (for testing).
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
#   4. Always end: "Waiting on you: …" OR "Nothing needs you."
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

# fast self-gate: only the XO tmux pane mirrors
[ -n "${TMUX_PANE:-}" ] || exit 0
marker="$(tmux show-options -pv -t "$TMUX_PANE" @flotilla_agent 2>/dev/null)" || exit 0
[ -n "$marker" ] || exit 0
xo="$(python3 -c "import json;print(json.load(open('$ROSTER')).get('xo_agent',''))" 2>/dev/null)" || exit 0
[ "$marker" = "$xo" ] || exit 0

python3 - "$payload" "$marker" "$SECRETS" "$FLOTILLA" <<'PY' 2>/dev/null || exit 0
import json, re, sys, subprocess, tempfile, os, time, datetime
payload, agent, secrets, flotilla = sys.argv[1:5]

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

def has_needs_you_line(text):
    """Doctrine mandates the needs-you line as the LAST line — not mid-body mention."""
    lines = [ln.strip() for ln in (text or "").splitlines() if ln.strip()]
    if not lines:
        return False
    last = lines[-1].lower()
    return last.startswith("waiting on you") or last.startswith("nothing needs you")

if not has_needs_you_line(resp):
    lg("MINI-BRIEF-AUDIT", "missing explicit Waiting-on-you / Nothing-needs-you line")

# BUG-5: mechanical post — trigger logged only, never suppresses.
residue = operator_residue(trig)
trig_note = (residue[:60] if residue else "(no-text-bearing-trigger)")

if os.environ.get("FLOTILLA_MIRROR_DRYRUN")=="1":
    lg("DRYRUN-POST", f"{len(resp)}ch waited={waited} trig={trig_note!r}")
    print("WOULD POST as %s (%d chars):\n%s" % (agent, len(resp), resp[:400])); sys.exit(0)

with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as tf:
    tf.write(resp); tmp=tf.name
try:
    r = subprocess.run([flotilla, "notify", "--chunk", "--from", agent, "--secrets", secrets, "--file", tmp],
                       check=False, timeout=60, capture_output=True, text=True)
    if r.returncode != 0:
        lg("POST-FAIL", f"{len(resp)}ch rc={r.returncode} err={(r.stderr or r.stdout or '')[:160]!r} waited={waited} trig={trig_note!r}")
    else:
        # notify --chunk posts N parts; we log aggregate (hook can't see chunk count without parsing).
        lg("POST", f"{len(resp)}ch waited={waited} trig={trig_note!r}")
except Exception as e:
    lg("POST-FAIL", f"{len(resp)}ch exc={type(e).__name__} waited={waited} trig={trig_note!r}")
finally:
    try: os.unlink(tmp)
    except Exception: pass
PY
exit 0