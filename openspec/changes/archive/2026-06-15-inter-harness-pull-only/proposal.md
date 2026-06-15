## Why

The surface-driver roadmap shipped four drivers (claude-code/aider/opencode/grok)
behind one interface, with a cursor skeleton held for its live-capture (#62). This change delivers **pillar B — inter-harness execution**: a
flotilla fleet that COORDINATES across harnesses (a claude XO routing work to aider/
opencode/grok desks, collecting results). It is the direct, proven payoff of the
surface-driver work and the GOAL-#2 differentiator ("drop-in agentize ANY harness +
run an inter-harness fleet").

**The core is already built and PROVEN LIVE.** A $0 mixed-harness fleet (an aider desk
+ an opencode desk + an OPENCODE XO on local ollama) was driven end-to-end this session:
`send`/Submit, the watch detector's `Assess`, and the inject/wake path are ALL
per-agent-driver and surface-agnostic (`cmd/flotilla/main.go:235,247`;
`cmd/flotilla/watch.go:122,130,216,231`). The detector ran over the mixed roster and woke
the opencode XO via the opencode driver (`detector delivered to "ocx"`). So this increment
is NOT a from-scratch build — it is closing the EDGE GAPS the live trace surfaced + making
the inter-harness model explicit and honest.

## Scope: PULL-ONLY (option a) — the operator-ratified first increment

Non-claude desks (aider/opencode/grok, and cursor once it ships) are **pull-participants**: a claude desk has
flotilla's skill set (it can `flotilla notify`/`send` to PUSH reports + take flotilla-command
delegation); a non-claude desk just runs turns in its harness. So in this increment the XO
**collects** a non-claude desk's result by READING its pane/files (capture), state-cued by
the driver's `Assess` (the detector's material wake already names the desk's state, e.g.
"aid: entered awaiting-approval"). Delegation is one-way (XO Submits; the desk reports via
pane state + what it writes). This is the proven minimum-viable inter-harness fleet — it
works today + the edge fixes below. **The docs SHALL be explicit that non-claude desks are
pull-participants** (no silent assumption they can push).

## The edge gaps (from the live gap-trace)

- **G1 — multi-line Submit is bracketed-paste-dependent (CODE).** Every driver's `Submit`
  uses `deliver.Send` (bracketed paste + Enter), which yields literal newlines only if the
  target enables bracketed-paste mode (`tmux.go:274-278`). Confirmed for claude/aider/opencode.
  This change ADDS a per-driver alternate newline-submit primitive (`Ctrl+J` keystroke
  newlines) so a harness without bracketed paste can submit multi-line correctly, and makes
  the newline method a per-driver choice. claude/aider/opencode stay on bracketed paste
  (confirmed); **grok and cursor's choice is deferred to their live-capture sessions (#58,
  #61) and noted as an explicit gap — NOT silently assumed-confirmed.**
- **G2 — the XO's "eyeball each desk's pane" duty predates the per-driver `Assess` (DOCS).**
  `docs/xo-doctrine.md` SHALL instruct the XO to LEAN ON the detector's driver-`Assess`'d
  desk state for monitoring, rather than re-eyeballing a non-claude render it may misread.
- **G4 — rotation/recovery are claude-specific (DOCS + SKILLS).** In-repo docs
  (xo-doctrine / watch-runbook / launch-recipes) SHALL note that non-claude rotation is the
  driver's `Rotate` (`/new`, `/new-chat`) and recovery is relaunch via the launch recipe —
  not claude `/clear`/resume. The out-of-repo `~/.claude` skills (`fleet-session-rotation`,
  `flotilla-fleet-recovery`) are updated as a skill-layer follow-on (cross-project asset,
  via the skill-sync flow — not this repo PR).

## Scope correction — G3 is NOT a gap (fact-checked against the doctor source)

The gap-trace listed **G3 "harness-parameterize the doctor's escalation."** Reading the
actual escalator (`deploy/flotilla-doctor.sh:2-18`) shows this is WRONG: the doctor is the
gateway-health escalator for the **flotilla-watch DAEMON** — it checks `systemctl is-active
flotilla-watch` + the gateway socket and, on a sustained-down, spawns `claude -p
/recover-flotilla` to diagnose+fix the DAEMON's Discord gateway. **It never drives the XO's
harness.** `claude` is the *recovery agent* running the runbook — correct regardless of the
fleet's harnesses. "Escalate via the XO's surface driver" would be a wrong fix to a
safety-critical path. **G3 is dropped from scope as a non-gap**, surfaced at the design
checkpoint. (The genuine recovery harness-awareness is G4's fleet-recovery skill — reviving
non-claude DEAD DESKS via the launch recipe — already in scope.)

## What Changes

- **`internal/deliver`**: add the alternate newline-submit primitive (`Ctrl+J` keystroke
  path) + a per-driver seam; arg test. **`internal/surface`**: drivers select their
  newline method (claude/aider/opencode → bracketed paste, confirmed; grok/cursor → noted
  deferred).
- **Docs**: a new `docs/inter-harness.md` (the pull-only mixed-harness fleet model — the
  proven surface-agnostic plumbing + the pull-participant model + the smart-desk follow-on);
  `docs/xo-doctrine.md` (G2 lean-on-Assess); G4 notes in the rotation/recovery docs.
- **Spec**: `surface` capability — the inter-harness pull-only model + the per-driver
  submit-newline seam.

## Capabilities

### Modified Capabilities
- `surface`: the driver abstraction supports an inter-harness fleet (the XO drives mixed-
  harness desks via per-agent drivers; non-claude desks are pull-participants), and the
  submit-newline method is a per-driver concern (bracketed paste vs Ctrl+J keystrokes).

## Impact

- **Code**: small (`deliver` alternate-newline primitive + the per-driver seam). No
  interface change. **Docs**: the inter-harness model + G2/G4 notes. **No change** to the
  watch detector or the relay (already surface-agnostic + proven).
- **Validation**: the live $0 mixed-harness fleet (this session) is the proof; a regression
  test exercises a mixed-surface roster through the resolve→Submit/Assess path.
- **Out of scope (Non-Goals)**: (b) SMART DESKS — injecting flotilla reporting conventions
  into non-claude desks' `AGENTS.md` so they `flotilla notify`-push (the first-class-peer
  design, a B-follow-on); G3 (corrected non-gap); the `~/.claude` skill updates (G4
  skill-layer follow-on); grok/cursor newline confirmation (folded into #58/#61).
