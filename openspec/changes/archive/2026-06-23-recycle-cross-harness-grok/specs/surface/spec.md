# surface Specification (delta)

## RENAMED Requirements

- FROM: `### Requirement: A grok surface driver drives xAI's official grok CLI with a reduced state set`
- TO: `### Requirement: A grok surface driver drives xAI's official grok CLI, composer- and approval-aware`

## MODIFIED Requirements

### Requirement: A grok surface driver drives xAI's official grok CLI, composer- and approval-aware

The system SHALL provide a registered surface driver, `grok`, that drives **xAI's official grok
CLI** (`~/.grok/bin/grok` — the "Grok Composer 2.5 Fast" TUI) through the `Driver` interface —
submitting a turn, assessing rendered state, and rotating context — selectable via
`roster.Agent.surface: "grok"`. The driver SHALL submit a turn by the bracketed-paste-then-Enter
mechanism; **multi-line bodies are delivered intact** (grok supports bracketed-paste multi-line —
live-confirmed 2026-06-23: a multi-line paste lands as ONE composer body with no early submit, so the
recycle bridge's multi-line handoff/takeover turns deliver whole; no `SendCtrlJ` is needed). The
driver SHALL declare its context-rotate strategy as `SlashCommand` with the reset command **`/new`**
(confirmed in the official grok slash menu). Adding/replacing this driver SHALL NOT change any other
driver's behavior. (The driver's workspace identity-file mapping is intentionally NOT (re)specified
here — the previously-asserted `AGENTS.md` mapping is contradicted by a live finding that grok uses
`MEMORY.md`/`--rules`; correcting that mapping is a tracked follow-up, deliberately out of scope for
this recycle-capability change, so this delta drops the stale `AGENTS.md` clause rather than carrying
it forward.)

The `grok` driver SHALL assess state Working-positive / Idle-default from LIVE-CAPTURED render markers.
The Working signal SHALL be a grok-chrome processing indicator — the live streamed-token arrow `⇣`
(U+21E3) OR a braille spinner frame (U+2801–U+28FF) — both present throughout a turn and absent when
idle/done (a finished turn renders an empty composer box `… Composer 2.5 Fast ─╯` with a `│ ❯` prompt
line). The driver SHALL NOT key Working on the leading gerund verb (it varies, and a bare
capitalized-word+ellipsis matches ordinary prose that can land in a finished turn's tail).

The `grok` driver SHALL emit `AwaitingApproval` when the official grok renders a **tool-approval
modal** (LIVE-CAPTURED 2026-06-23: a `┃`-bordered block `┃ Allow <Verb> \`<path>\`?` with numbered
options and the status line `N/M:select │ Ctrl+o:yolo │ Ctrl+c:cancel`). This detection SHALL run
BEFORE the `⇣`/spinner Working check, because the `⇣` arrow is co-present on the modal's `◆ Run …`
line and would otherwise mis-classify the blocked desk as Working (the live #58 gap). Emitting
`AwaitingApproval` activates XO escalation for a blocked grok desk (mirroring the aider precedent),
closing the prior gap where an approval-blocked grok desk was invisible to the XO-only wedge timer.
Auth/payment blocking gates remain a documented follow-up (not yet live-captured); their absence is a
known liveness gap, not a recycle-safety gap.

The `grok` driver SHALL implement the OPTIONAL `ComposerStateProbe` capability — a cursor-indexed
composer classifier (LIVE-CAPTURED 2026-06-23). grok's composer is a box (`╭─╮ │ ╰─╯`); the input
line at the terminal cursor is `│ ❯ <body> │` (the `❯` prompt is preceded by a `│` left border).
`ComposerState` SHALL read the line at the cursor row, strip the leading box border and the `❯`
prompt, and classify: an empty body ⇒ `Cleared`; a non-empty body ⇒ `Pending`; a cursor not on a
`❯` prompt line (including the tool-approval modal, where the cursor sits on the `◆ Run …` line, and
multi-line continuation rows, which carry no `❯`) ⇒ `Undetermined`; a tmux copy/view mode or a
cursor/capture read error ⇒ `Undetermined`. The classifier SHALL NOT mis-read the tool-approval modal
as `Cleared` — the load-bearing recycle-gate-safety property (so a recycle's `Idle ∧ ComposerCleared`
gate fails closed on a modal and never fires `/exit` into it).

The `grok` driver SHALL implement the OPTIONAL `RecycleBridge` capability, with a **harness-agnostic**
handoff convention `<cwd>/.flotilla/handoffs/recycle-<token>.md` (NOT the claude-branded
`.claude/handoffs/`), and grok-worded non-interactive turns that reference the handoff document FORMAT
only (grok has no `/handoff`,`/takeover` skills; it runs git/tools, so the handoff turn force-commits
via `git add -f`). The driver's markers SHALL be documented as live-captured against the official grok
CLI, NOT source-verified.

The driver's `Close` MAY return `ErrNoGracefulClose` (grok's `/exit` keystroke is not yet
live-characterized); a recycle tolerates this via the handoff-gated respawn-kill, so grok being
recycle-capable does NOT require a graceful close.

#### Scenario: A grok agent is driven through the grok driver
- **WHEN** an agent with `surface: "grok"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `grok` driver

#### Scenario: The grok reset is /new
- **WHEN** the grok driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/new` is injected into the pane

#### Scenario: A working grok turn assesses as Working
- **WHEN** the official grok pane shows a processing frame (the `⇣` streamed-token arrow or a braille spinner) and NO tool-approval modal
- **THEN** `Assess` returns `Working`

#### Scenario: A finished grok turn assesses as Idle
- **WHEN** the pane shows an empty composer box (no arrow, no spinner, no modal), even if the bottom tail contains an ordinary capitalized-word ellipsis like `Note…`
- **THEN** `Assess` returns `Idle`

#### Scenario: A tool-approval modal assesses as AwaitingApproval, not Working
- **WHEN** the grok pane renders the tool-approval modal (`┃ Allow …?` with `N/M:select`) even though the `⇣` arrow is co-present in the tail
- **THEN** `Assess` returns `AwaitingApproval` (the modal detection precedes the Working check)

#### Scenario: grok composer state is classified at the cursor
- **WHEN** `ComposerState` reads a grok pane whose cursor is on the `│ ❯` composer line
- **THEN** an empty composer returns `Cleared`, a composer with a pending body returns `Pending`

#### Scenario: A grok tool-approval modal is never classified as a cleared composer
- **WHEN** `ComposerState` reads a grok pane showing the tool-approval modal (the cursor on the `◆ Run …` line, no `❯`)
- **THEN** it returns a NON-`Cleared` disposition (`Undetermined`), so a recycle's idle-cleared gate fails closed and `/exit` is never injected into the modal

#### Scenario: The grok recycle bridge uses a harness-agnostic handoff path
- **WHEN** the grok `RecycleBridge` computes a handoff path for a desk's cwd and a recycle token
- **THEN** the path is `<cwd>/.flotilla/handoffs/recycle-<token>.md` (product-owned, not `.claude/handoffs/`), the handoff turn names that exact path and force-commits it, and neither turn references a claude harness-specific `/handoff`,`/takeover` skill

### Requirement: A surface driver MAY expose the context-preservation policy a recycle drives

The system SHALL define an OPTIONAL `RecycleBridge` capability that a surface driver MAY implement,
exposing the per-harness context-preservation policy a recycle drives as three pieces: `HandoffPath(cwd, token)` — the recycle-DESIGNATED handoff
artifact path for this harness (the driver owns the convention, e.g. claude
`<cwd>/.claude/handoffs/<date>-recycle-<token>.md`, grok `<cwd>/.flotilla/handoffs/recycle-<token>.md`);
`HandoffTurn(designatedPath)` — the
NON-INTERACTIVE, self-committing handoff instruction TEXT; and `TakeoverTurn(designatedPath)` — the
IMPERATIVE takeover instruction TEXT. The two turn methods SHALL return TEXT (the command delivers it
via confirmed delivery); they SHALL NOT themselves inject. The handoff turn SHALL instruct the desk to
write a handoff (per the handoff FORMAT, not the interactive handoff skill) to the designated path,
force-commit it to the current branch (so a gitignored handoffs directory does not block it), NOT ask
for confirmation (it is remote-driven), then stop. The takeover turn SHALL instruct a
freshly-relaunched session to read the designated path and take over, BEGIN WORK IMMEDIATELY (NOT ask
whether to start), and state that the session is remote-driven and must surface any clarification via a
flotilla message, never an in-pane interactive prompt. Neither turn SHALL invoke the human-interactive
handoff/takeover skills (which pause for a confirmation / a "shall I start?"). The handoff PATH SHALL be
harness-agnostic (a markdown file); only the per-harness convention and wording differ. A caller SHALL
type-assert the capability and, when it is ABSENT, REFUSE to recycle the desk cleanly (naming the
surface) rather than silently degrading to a context-losing restart. **Claude Code and grok both
implement the bridge** (grok added 2026-06-23, #158); a further harness's bridge remains a separate,
gated change.

#### Scenario: A recycle-capable surface supplies the designated path and the non-interactive turns

- **WHEN** a recycle drives a surface that implements `RecycleBridge`
- **THEN** `HandoffPath` yields the designated artifact path, `HandoffTurn` yields a non-interactive
  self-committing instruction naming that exact path (write, force-commit, do not ask to confirm, stop),
  and `TakeoverTurn` yields an imperative begin-work-immediately instruction naming that exact path and
  telling the remote-driven session to parlay via a flotilla message

#### Scenario: A surface without the bridge refuses to recycle

- **WHEN** a recycle targets a surface that does NOT implement `RecycleBridge`
- **THEN** the command refuses cleanly, naming the surface as not recycle-capable, rather than
  restarting the desk with its context lost

#### Scenario: The handoff artifact is harness-agnostic markdown

- **WHEN** the claude bridge produces a handoff artifact
- **THEN** the artifact is plain markdown with no claude-specific framing, so a second harness's
  bridge (e.g. grok) consumes the same artifact — only the per-harness path convention and turn wording
  differ

#### Scenario: A grok desk is recycle-capable

- **WHEN** a recycle targets a desk whose surface is `grok`
- **THEN** the command does NOT refuse for lack of a bridge/probe — grok implements both `RecycleBridge`
  and `ComposerStateProbe` — and drives the recycle pipeline (closing via the handoff-gated respawn-kill
  fallback, since grok's `Close` returns `ErrNoGracefulClose`)
