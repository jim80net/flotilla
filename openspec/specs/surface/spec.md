# surface Specification

## Purpose
TBD - created by archiving change surface-driver. Update Purpose after archive.
## Requirements
### Requirement: Per-agent surface driver selects all surface-specific behavior

The system SHALL select a **surface driver** per agent (from `roster.Agent.surface`,
defaulting to `claude-code`) that encapsulates every surface-specific behavior:
prompt submission, rendered-state assessment, and context rotation. Surface
drivers SHALL be looked up from a registry by name; an unknown surface name SHALL
be a clear startup error, never a silent mis-drive. The driver DECIDES the
surface policy; the low-level tmux primitives EXECUTE it.

#### Scenario: Default surface is claude-code
- **WHEN** an agent has no `surface` set
- **THEN** it resolves to the `claude-code` driver and behaves exactly as before this change

#### Scenario: Unknown surface refuses startup
- **WHEN** an agent's `surface` names a driver not in the registry
- **THEN** the command exits at startup with a clear error rather than mis-driving the pane

### Requirement: Submission and assessment route through the agent's driver

`send` delivery and `watch` liveness/busy assessment SHALL route through the
agent's surface driver — `Submit` for a turn, `Assess` for rendered state
(working / idle / shell / awaiting / errored). For the `claude-code` surface the
results SHALL be byte-identical to the prior hard-coded behavior (bracketed-paste
submission; the `✻ …(Ns` working / `❯ … ⏵⏵ auto mode` idle / shell-command crash
classification).

#### Scenario: claude-code behavior is unchanged
- **WHEN** a message is delivered to, or the state assessed of, a `claude-code` agent
- **THEN** the tmux operations and the resulting state are identical to before the abstraction

### Requirement: Context rotation is strategy-typed and guards restart-only surfaces

Each driver SHALL declare a rotate strategy — `SlashCommand` (a reset is injected
into the pane, e.g. Claude Code `/clear`, Grok `/new`) or `RestartProcess` (no
in-session reset exists, e.g. `cursor-agent`). The system SHALL NEVER inject a
slash command (or any keystrokes) into a `RestartProcess` surface to rotate it —
doing so would land as literal composer text. A rotate request against a
`RestartProcess` surface SHALL instead signal that the caller must restart the
session.

#### Scenario: SlashCommand surface is injected
- **WHEN** a context rotate is requested for a `SlashCommand` surface (e.g. claude-code)
- **THEN** the surface's reset command is injected into the pane

#### Scenario: RestartProcess surface is never injected
- **WHEN** a context rotate is requested for a `RestartProcess` surface (e.g. cursor)
- **THEN** no keystrokes are injected and the caller is signaled to restart the session instead

### Requirement: A second surface driver drives a non-Claude harness through the same interface

The system SHALL provide a second registered surface driver, `aider`, that drives
the Aider CLI harness through the `Driver` interface — submitting a turn,
assessing rendered state, and rotating context — selectable per agent via
`roster.Agent.surface: "aider"`. The `aider` driver SHALL submit a turn by the
same bracketed-paste-then-Enter mechanism as `claude-code`, and SHALL declare its
context-rotate strategy as `SlashCommand` (its reset is the in-session `/clear`,
injected into the composer). Adding this driver SHALL NOT change `claude-code`
behavior, which remains byte-identical.

#### Scenario: An aider agent is driven through the aider driver
- **WHEN** an agent with `surface: "aider"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `aider` driver, and the command starts successfully (the surface resolves at startup)

#### Scenario: The aider reset is an injected slash command
- **WHEN** the aider driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/clear` is injected into the pane (no process restart)

#### Scenario: claude-code is unaffected
- **WHEN** an agent with no surface (or `surface: "claude-code"`) is driven
- **THEN** its submission, assessment, and rotation are byte-identical to before this change

### Requirement: The aider driver emits the full assessed-state set from the live pane tail

The `aider` driver's `Assess` SHALL classify the pane into the FULL state set —
including `AwaitingApproval` and `Errored`, which no prior driver emitted — in
addition to `Shell`, `Working`, and `Idle`. Classification of the rendered-text
markers SHALL be scoped to the live bottom region of the captured pane (the tail),
so a stale approval or error string scrolled up into history NEVER false-positives
the current state. Because aider's working indicator does not persist across its
streaming phase, `Idle` SHALL be the POSITIVELY-detected state (a recognized
prompt on the last line) and `Working` SHALL be the default — a readable pane not
at its prompt is presumed still working, so a mid-stream turn is never misread as
finished. State precedence SHALL be: a transient pane-command read error →
`Unknown` (not a crash); a shell foreground command → `Shell`; then, over the
tail, an open approval prompt → `AwaitingApproval`; else a recognized prompt on
the last non-empty line → `Idle`; else a known non-retryable error marker (with
no prompt below it) → `Errored`; else `Working` (the default — mid-stream,
streaming, or auto-retry). A pane capture error SHALL return `Unknown`
(non-material), never a false `Idle`.

#### Scenario: An open approval prompt is AwaitingApproval
- **WHEN** the captured pane's last non-empty line shows aider's confirmation prompt (the `(Y)es/(N)o` token, e.g. `Run shell command? … (Y)es/(N)o [Yes]:`)
- **THEN** `Assess` returns `AwaitingApproval`

#### Scenario: A wrapped approval prompt is still AwaitingApproval
- **WHEN** the confirmation question is long enough to wrap so the `(Y)es/(N)o` token sits a line above the cursor and the last non-empty line is the cursor suffix (`[Yes]:` or `[No]:`)
- **THEN** `Assess` returns `AwaitingApproval` (the desk blocked on a wrapped prompt still wakes the XO; it is not misread as Working)

#### Scenario: A returned prompt is positively Idle
- **WHEN** the tail's last non-empty line is a recognized aider prompt (`> ` / `ask> ` / `architect> ` / `multi> `)
- **THEN** `Assess` returns `Idle`

#### Scenario: A stale approval/error in scrollback does not mislead
- **WHEN** a `(Y)es/(N)o` prompt or an error phrase appears earlier in the pane history but the live tail's last line is an idle prompt (`> `)
- **THEN** `Assess` returns `Idle` (the tail-scoped scan ignores the stale string)

#### Scenario: A mid-stream or auto-retry turn is Working (the default)
- **WHEN** the tail shows neither an approval prompt, nor a returned idle prompt, nor a live error marker (e.g. it is streaming the model's response, showing `Waiting for <model>`, or counting down `Retrying in N seconds...`)
- **THEN** `Assess` returns `Working` (the default — the desk is not finished until its prompt returns)

#### Scenario: A live non-retryable error is Errored
- **WHEN** the tail shows a known non-retryable error marker (e.g. an auth/`Check your API key` description, or the uncaught-exception banner), the last line is NOT a prompt, and there is no retry countdown
- **THEN** `Assess` returns `Errored`

#### Scenario: A crashed aider pane is Shell
- **WHEN** the pane's foreground command is a shell (the aider process exited)
- **THEN** `Assess` returns `Shell`

#### Scenario: A pane capture error is Unknown, not a false finish
- **WHEN** the pane's foreground command is readable (not a shell) but the pane capture fails
- **THEN** `Assess` returns `Unknown` (a non-material transient glitch), never `Idle`

### Requirement: Emitting AwaitingApproval and Errored activates XO escalation for aider desks

The system SHALL escalate `AwaitingApproval` and `Errored` transitions of an
aider desk to the XO with no change to the watch logic, because the
change-detector's materiality gate already routes those states as actionable
entries — a driver that merely emits them activates the dormant branch. A non-XO
desk ENTERING `AwaitingApproval` or `Errored` SHALL be reported as a material
change.

#### Scenario: An aider desk awaiting approval wakes the XO
- **WHEN** a non-XO aider desk transitions into `AwaitingApproval` (it is blocked on a confirmation prompt)
- **THEN** the change detector reports a material change ("entered awaiting-approval") and wakes the XO

#### Scenario: An aider desk in an error state wakes the XO
- **WHEN** a non-XO aider desk transitions into `Errored`
- **THEN** the change detector reports a material change ("entered errored") and wakes the XO

### Requirement: A third surface driver drives the OpenCode harness through the interface

The system SHALL provide a registered surface driver, `opencode`, that drives the
OpenCode CLI harness through the `Driver` interface — submitting a turn, assessing
rendered state, and rotating context — selectable per agent via
`roster.Agent.surface: "opencode"`. The `opencode` driver SHALL submit a turn by the
same bracketed-paste-then-Enter mechanism as the other drivers, and SHALL declare its
context-rotate strategy as `SlashCommand` (its reset is the in-session `/clear`, an
alias of OpenCode's new-session command, injected into the composer). Adding this
driver SHALL NOT change any existing driver's behavior.

#### Scenario: An opencode agent is driven through the opencode driver
- **WHEN** an agent with `surface: "opencode"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `opencode` driver, and the command starts successfully (the surface resolves at startup)

#### Scenario: The opencode reset is an injected slash command
- **WHEN** the opencode driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/clear` is injected into the pane (no process restart)

#### Scenario: The opencode identity file is AGENTS.md
- **WHEN** a workspace identity file is resolved for the `opencode` surface
- **THEN** it is `AGENTS.md` (OpenCode's native instruction file)

### Requirement: The opencode driver emits the full assessed-state set with Working-positive polarity

The `opencode` driver's `Assess` SHALL classify the pane into the full state set —
`Shell`, `Working`, `Idle`, `AwaitingApproval`, and `Errored` — scoped to the live
bottom region of the captured pane. Because OpenCode's working indicator persists for
the entire non-idle duration (it is bound to the session's `idle`/`busy`/`retry`
status), the driver SHALL use Working-POSITIVE polarity: `AwaitingApproval`, `Errored`,
and `Working` are positively detected and `Idle` is the default. State precedence SHALL
be: a transient pane-command read error → `Unknown`; a shell foreground command →
`Shell`; then, over the tail, the permission UI (`Permission required` / `Allow once` /
the footer permission counter) → `AwaitingApproval`; else the fatal-error boundary →
`Errored`; else a persistent working marker (the `esc interrupt` hint, the `[⋯]`
indicator, or a `[retrying` backoff line) → `Working`; else `Idle`. A pane capture
error SHALL return `Unknown` (non-material), never `Idle` — so a transient glitch on a
working desk cannot diff as `Working→Idle` ("finished a turn") and fire a spurious wake.

#### Scenario: A pending permission is AwaitingApproval
- **WHEN** the captured pane shows OpenCode's permission UI (the `Permission required` header, an `Allow once` button, or the footer permission counter), even while a working indicator co-renders
- **THEN** `Assess` returns `AwaitingApproval`

#### Scenario: A non-idle turn is Working
- **WHEN** the tail shows a persistent working marker (the `esc interrupt` hint, the `[⋯]` indicator, or a `[retrying` backoff line)
- **THEN** `Assess` returns `Working`

#### Scenario: An idle composer is Idle (the default)
- **WHEN** the tail shows no permission, no fatal error, and no working marker
- **THEN** `Assess` returns `Idle`

#### Scenario: A fatal error boundary is Errored
- **WHEN** the tail shows OpenCode's fatal error boundary (`A fatal error occurred!`)
- **THEN** `Assess` returns `Errored`

#### Scenario: A crashed opencode pane is Shell
- **WHEN** the pane's foreground command is a shell (the OpenCode process exited)
- **THEN** `Assess` returns `Shell`

### Requirement: The fleet drives mixed-harness desks; non-claude desks are pull-participants

The system SHALL support an inter-harness fleet: the XO and `watch` daemon SHALL drive every
desk through its per-agent surface driver, so a roster mixing harnesses (claude-code, aider,
opencode, grok; cursor when it ships) is delivered to, assessed, and woken correctly per-driver — submission
(`Submit`), state assessment (`Assess`), and the detector's wake injection SHALL all be
surface-agnostic. A non-claude desk SHALL be treated as a PULL-PARTICIPANT: because it does
not run flotilla's skill set, the XO collects its result by reading its pane/output (cued by
the driver's `Assess` state), and delegation is one-way (the XO submits; the desk reports via
its rendered state and what it writes). The documentation SHALL state the pull-participant
model explicitly — it SHALL NOT assume a non-claude desk can push reports.

#### Scenario: A mixed-harness roster routes per-driver
- **WHEN** a roster declares agents with different surfaces (e.g. claude-code XO, an aider desk, an opencode desk)
- **THEN** each agent's submission and state assessment route through that agent's surface driver, and the watch detector assesses and wakes each via the correct driver

#### Scenario: A non-claude desk is a pull-participant
- **WHEN** the XO coordinates a non-claude desk
- **THEN** it collects the desk's result by reading the desk's pane/output (state-cued by the driver's assessment), not by expecting the desk to push a report — and the documentation makes this pull-only model explicit

### Requirement: Submit's in-composer newline method is a per-driver choice

The system SHALL make the in-composer newline method used by a driver's `Submit` a per-driver
concern, so a harness that does not support bracketed-paste mode can still deliver multi-line
turns correctly. The system SHALL provide both a bracketed-paste submission (literal newlines
via paste) and a keystroke-newline submission (`Ctrl+J` between lines, then submit). A driver
SHALL select the method appropriate to its harness; `claude-code`, `aider`, and `opencode`
SHALL use bracketed paste (confirmed). For a harness whose newline behavior is not yet
confirmed, the driver SHALL NOT silently assume bracketed paste works — the gap SHALL be noted
pending that harness's live-capture.

#### Scenario: A driver selects bracketed-paste submission
- **WHEN** a claude-code / aider / opencode desk is sent a multi-line turn
- **THEN** the turn is delivered via bracketed paste with literal newlines and a single submit

#### Scenario: A keystroke-newline submission is available for harnesses that need it
- **WHEN** a harness does not enable bracketed-paste mode
- **THEN** a `Ctrl+J`-keystroke-newline submission method is available for its driver to select, so multi-line delivery does not submit each line early

### Requirement: A non-Claude desk may push reports to the XO without receiving any secret

The system SHALL allow a non-Claude desk to be provisioned as a push-capable peer that
proactively reports to the XO, turning the pull-only inter-harness model into a two-way
protocol. The push channel SHALL be `flotilla send` to the XO (pure tmux injection into
the XO's pane), which requires no secrets. The system SHALL NOT require, and a smart desk
SHALL NOT be provisioned with, the secrets file (the Discord bot token and per-agent
webhook URLs) — a desk SHALL NOT push to Discord directly. The XO, as the sole holder of
the secrets, SHALL decide what (if anything) to relay to the operator after receiving a
desk's pushed report. A desk without the smart-push convention SHALL remain a pure
pull-participant with no behavior change.

#### Scenario: A smart desk reports to the XO via send (no secrets)
- **WHEN** a provisioned smart desk finishes a delegated task or is blocked
- **THEN** it reports to the XO by `flotilla send --from <desk> <xo> "<pointer>"` (a tmux injection requiring no secrets), and the XO collects the desk's detail and relays to the operator only if warranted

#### Scenario: A desk is never given the fleet secrets (a provisioning contract)
- **WHEN** a smart desk is provisioned for push
- **THEN** it receives only the flotilla binary, the secret-free roster, and its own `--from` identity — never the secrets file, the bot token, or any webhook; the desk→Discord-direct push path is not provisioned. (This is a contract on PROVISIONING — the desk's launch environment must not include `$FLOTILLA_SECRETS` or a readable secrets path — not a guarantee the binary enforces; the docs SHALL state it as such.)

#### Scenario: The pull-participant default is unchanged
- **WHEN** a non-Claude desk has no smart-push convention
- **THEN** it remains a pull-participant (the XO collects by reading its pane), exactly as before this change

### Requirement: Confirmed turn delivery

The system SHALL provide a confirmed-delivery orchestration over a surface `Driver` that
delivers text to an agent's pane and CONFIRMS a turn started, rather than assuming success
from the exit code of the tmux keystrokes. Confirmation SHALL observe the agent's
`Idle → Working` state transition via the driver's `Assess`. A delivery SHALL be reported
as successful ONLY when a turn is confirmed to have started; an unverified submit SHALL NOT
be reported as delivered.

#### Scenario: An idle agent's submit is confirmed by the working edge
- **WHEN** the agent's pane is idle and text is submitted
- **THEN** the orchestration polls the driver's `Assess`, observes the `Idle → Working`
  transition, and reports the delivery confirmed (no retry)

#### Scenario: A submit that does not start a turn is retried, then escalated
- **WHEN** a submit does not produce a `Working` state within the bounded confirm window
- **THEN** the submitting Enter is re-sent (the body is NOT re-pasted) up to a bounded
  number of attempts, and if no turn is ever confirmed a LOUD operator alert is raised and
  the delivery is reported failed — never silently successful

### Requirement: Idle-gated delivery (deliver only when idle)

Confirmed delivery SHALL NOT submit into a busy composer. Before submitting, the
orchestration SHALL assess the pane state and act as follows: a `Working` pane SHALL signal
busy (the caller defers — a bounded delay is acceptable, a composer-eaten message is not); a
`Shell` pane SHALL escalate and report crashed (a crash is NOT deferred-forever — it will
not self-heal); an `Idle` pane SHALL proceed to submit; any other state (`Unknown`,
`AwaitingApproval`, `AwaitingInput`, `Errored`) SHALL signal a transient condition for a
bounded re-assess rather than a fire-into-uncertainty.

#### Scenario: A message arriving while the agent is working is not submitted
- **WHEN** confirmed delivery is invoked while the pane assesses as `Working`
- **THEN** no submit is attempted and the caller is signalled to defer (the message is not
  pasted into the active composer)

#### Scenario: A crashed agent is escalated, not deferred forever
- **WHEN** the pane assesses as `Shell` (the agent process is gone)
- **THEN** a LOUD operator alert is raised and the delivery is reported crashed — it is NOT
  re-enqueued indefinitely

### Requirement: Idempotent Enter-only retry

A confirmed-delivery retry SHALL re-send the submitting Enter ALONE and SHALL NEVER re-paste
the message body. The retry SHALL run only after the initial submit returned success (the
body is confirmed present in the composer; only the submitting keystroke is in question), so
a retry can never produce a second copy of the message. A submit that itself failed (e.g. a
paste that did not land, or a pane-lock timeout) SHALL be escalated and SHALL NOT enter the
Enter-only retry.

#### Scenario: A dropped Enter is recovered without double-submitting
- **WHEN** the body was pasted (submit returned success) but the turn did not start
- **THEN** a bare Enter is re-sent (not the body), the turn starts, and the message is
  delivered exactly once

#### Scenario: A failed paste is escalated, not Enter-retried
- **WHEN** the initial submit returns an error (the body never landed in the composer)
- **THEN** the failure is escalated and no Enter-only retry is attempted

### Requirement: A bare-Enter delivery primitive

The system SHALL provide a delivery primitive that submits a single Enter keystroke to a
pane under the per-pane cross-process lock, for use as the idempotent confirmed-delivery
retry. Its keystroke argument vector SHALL be testable as a pure function without a running
tmux server.

#### Scenario: The bare-Enter argv is exactly one submitting Enter
- **WHEN** the bare-Enter argv builder is invoked for a target pane
- **THEN** it produces exactly the single `send-keys … Enter` invocation, under the per-pane
  lock when executed

### Requirement: A grok surface driver drives xAI's official grok CLI with a reduced state set

The system SHALL provide a registered surface driver, `grok`, that drives **xAI's official grok
CLI** (`~/.grok/bin/grok` — the "Grok Composer 2.5 Fast" TUI) through the `Driver` interface —
submitting a turn, assessing rendered state, and rotating context — selectable via
`roster.Agent.surface: "grok"`. The driver SHALL submit a turn by the bracketed-paste-then-Enter
mechanism (single-line delivery confirmed; multi-line is a tracked follow-up), SHALL declare its
context-rotate strategy as `SlashCommand` with the reset command **`/new`** (confirmed in the
official grok slash menu), and its workspace identity file SHALL be `AGENTS.md`. Adding/replacing
this driver SHALL NOT change any other driver's behavior.

The `grok` driver SHALL emit a REDUCED assessed-state set — `Shell`, `Working`, `Idle` — and SHALL
classify state from LIVE-CAPTURED render markers (2026-06-16), Working-positive / Idle-default. The
Working signal SHALL be a grok-chrome processing indicator — the live streamed-token arrow `⇣`
(U+21E3) OR a braille spinner frame (U+2801–U+28FF) — both present throughout a turn and absent
when idle/done (a finished turn renders `Turn completed in …` and an empty composer). The driver
SHALL NOT key Working on the leading gerund verb (it varies, and a bare capitalized-word+ellipsis
matches ordinary prose that can land in a finished turn's tail). The driver SHALL NOT emit
`AwaitingApproval` until the official grok's blocking gates (auth / payment / tool approval) are
live-captured; this is a documented gap (an auth-blocked but process-alive grok desk reads Idle and
is not covered by the XO-only wedge timer; a crashed desk still alerts via the Shell path). The
driver's markers SHALL be documented as live-captured against the official grok CLI, NOT
source-verified against grok-dev.

#### Scenario: A grok agent is driven through the grok driver
- **WHEN** an agent with `surface: "grok"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `grok` driver

#### Scenario: The grok reset is /new
- **WHEN** the grok driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/new` is injected into the pane

#### Scenario: A working grok turn assesses as Working
- **WHEN** the official grok pane shows a processing frame (the `⇣` streamed-token arrow or a braille spinner)
- **THEN** `Assess` returns `Working`

#### Scenario: A finished grok turn assesses as Idle, even with prose ellipses in the tail
- **WHEN** the pane shows `Turn completed in …` with an empty composer (no arrow, no spinner), even if the bottom tail contains an ordinary capitalized-word ellipsis like `Note…`
- **THEN** `Assess` returns `Idle` (the finished-a-turn transition is detectable)

