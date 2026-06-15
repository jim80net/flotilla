# Design: inter-harness execution — pull-only (the proven plumbing + the edge fixes)

## The increment in one line

The send → inject → Assess → wake plumbing is ALREADY per-agent-driver and was PROVEN
surface-agnostic LIVE this session (a $0 aider+opencode fleet with an opencode XO). This
change makes the inter-harness fleet model explicit + honest (pull-only; non-claude desks
are pull-participants) and closes the edge gaps the live trace found (G1 code; G2/G4 docs).
It is small and bounded — the foundation is done.

## Live evidence (the validation, already performed)

$0 mixed fleet, local ollama: roster `{xo_agent: ocx(opencode), desks: [ocx, aid(aider)]}`.
- `flotilla send aid …` → delivered via the AIDER driver's Submit (pane processed, idle).
- `flotilla send ocx …` → delivered via the OPENCODE driver's Submit (pane → Working).
- `flotilla watch --change-detector` → `change-detector running — XO=ocx`; then
  `detector delivered to "ocx" (330 bytes)` — the detector assessed each desk via its driver,
  found a material change, and woke the opencode XO via the opencode driver.
So a mixed-harness fleet — incl. a NON-CLAUDE XO — runs end-to-end on the existing code.
A regression test (this change) exercises a mixed-surface roster through the
resolve→Submit/Assess path so the surface-agnostic guarantee is locked.

## G1 — per-driver submit-newline method (the one code change)

`deliver.Send` (`tmux.go:279-316`) submits via bracketed paste: load-buffer → `paste-buffer
-p` (literal newlines) → settle → `Enter`. The literal-newline behavior holds ONLY when the
target enables bracketed-paste mode (`tmux.go:274-278`); confirmed live for claude-code,
aider (prompt_toolkit), and opencode. A harness without it would see each `\n` submit early,
breaking multi-line delivery.

**Fix — an alternate keystroke-newline primitive + a per-driver choice (no interface change):**

```go
// deliver.SendCtrlJ types text into the pane using Ctrl+J (C-j) for in-composer newlines
// (NOT bracketed paste), then submits with Enter. For TUIs whose tmux newline is Ctrl+J
// (e.g. cursor-agent's documented Shift+Enter→Ctrl+J caveat) or that lack bracketed-paste
// mode. Pure arg-builder split out for testing.
func sendCtrlJArgs(target string, lines []string) [][]string { … per-line `send-keys -l -- <line>`,
                                                                 `send-keys -- C-j` between, final `Enter` }
func SendCtrlJ(target, text string) error { … lock; type lines with C-j; settle; Enter }
```

The per-driver SEAM already exists: each driver wires a `send` field (currently
`deliver.Send`). A driver chooses its newline method by wiring `send` to `deliver.Send`
(bracketed paste) or `deliver.SendCtrlJ`. **claude/aider/opencode → `deliver.Send` (bracketed,
confirmed). grok and cursor → their choice is DEFERRED to their live-capture sessions (#58,
#61) and the gap is NOTED in their drivers + here — NOT silently assumed-confirmed.** So G1
ships the capability + the seam now; the per-harness selection for grok/cursor lands with
their captures.

(Why static per-driver, not a runtime fallback: bracketed-paste support cannot be reliably
detected at runtime from a tmux pane; the driver KNOWS its harness, so a static choice is
correct and testable.)

## The pull-only inter-harness model (G2 — docs)

A new `docs/inter-harness.md` documents, honestly:
- **The plumbing is surface-agnostic** (send/inject/Assess/wake resolve the per-agent
  driver; proven live) — a claude XO can drive aider/opencode/grok desks today (cursor once it ships).
- **Non-claude desks are PULL-PARTICIPANTS.** A claude desk has flotilla's skill set (it can
  `flotilla notify`/`send` to push reports + take flotilla-command delegation). A non-claude
  desk does not — it runs turns in its harness. So:
  - **Collect = pull**: the XO reads a non-claude desk's pane/files (capture) for the result;
    the driver's `Assess` (surfaced in the detector's material wake reason) tells the XO WHEN
    the turn finished, the pane content is the WHAT.
  - **Delegation = one-way**: the XO Submits; the desk reports via pane state + what it writes.
- **G2**: `docs/xo-doctrine.md` — the XO LEANS ON the driver-`Assess`'d desk state for
  monitoring (the detector already provides it), rather than eyeballing a non-claude render.
- **Smart desks (the follow-on, a Non-Goal here)**: injecting flotilla reporting conventions
  into a non-claude desk's `AGENTS.md` so it `flotilla notify`-pushes on completion — the
  first-class-peer design, sequenced after this increment.

## G4 — rotation/recovery harness-awareness (docs in-repo; skills out-of-repo)

In-repo docs (`xo-doctrine.md` / `watch-runbook.md` / `agent-launch-recipes-design.md`) note
that non-claude rotation = the driver's `Rotate` (`/new`, `/new-chat`), and recovery =
relaunch via the launch recipe — NOT claude `/clear`/resume. The `~/.claude` skills
(`fleet-session-rotation`, `flotilla-fleet-recovery`) are a skill-layer follow-on (cross-
project asset, updated via the skill-sync flow — not this repo PR; noted in tasks).

## G3 — dropped as a non-gap (the scope correction; surfaced at the checkpoint)

The doctor (`deploy/flotilla-doctor.sh:2-18`) is the gateway-health escalator for the
flotilla-watch DAEMON; on a sustained gateway-down it spawns `claude -p /recover-flotilla` to
diagnose+fix the DAEMON's Discord gateway. It NEVER drives the XO's harness — `claude` is the
recovery AGENT running the runbook, correct regardless of the fleet's harnesses. So G3 (as
the gap-trace framed it) is NOT a real inter-harness gap; "escalate via the XO's surface
driver" would be a wrong fix to a safety-critical path. G3 is dropped; the genuine recovery
harness-awareness is G4's dead-desk path.

## Test plan (TDD)

1. **`deliver.SendCtrlJ` arg-builder**: per-line `send-keys -l -- <line>`, `C-j` between lines,
   final `Enter` — pure-function test (no tmux), like `slashKeysArgs`.
2. **Per-driver newline method**: claude/aider/opencode resolve to bracketed-paste Submit
   (`deliver.Send`); a regression asserts they are unchanged. (grok/cursor: noted deferred.)
3. **Mixed-surface fleet regression**: a roster with claude-code + aider + opencode agents
   resolves each agent's driver and routes Submit/Assess per-driver (locking the
   surface-agnostic guarantee proven live).
4. Docs: `docs/inter-harness.md` is self-contained + honest about pull-participants.
5. `gofmt`/`go vet`/`go build`/`go test -race ./...` green; `openspec validate --strict`.
6. `/systems-review` + `/open-code-review` on the design AND the implementation.

## Non-Goals

- **(b) Smart desks** — flotilla reporting conventions in non-claude `AGENTS.md` (push-capable
  peers). The B-follow-on.
- **G3** — corrected non-gap (the doctor recovers the daemon, not the XO).
- **The `~/.claude` skill updates** (G4 skill-layer) — done via the skill-sync flow.
- **grok/cursor newline confirmation** — folded into #58/#61.
