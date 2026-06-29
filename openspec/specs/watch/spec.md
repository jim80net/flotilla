# watch Specification

## Purpose
TBD - created by archiving change watch-relay. Update Purpose after archive.
## Requirements
### Requirement: Gateway relay of operator messages into agent panes

The system SHALL provide `flotilla watch`, a long-lived process that streams the
Discord gateway and injects accepted operator messages into the target agent's
tmux pane via the `send` capability's delivery. The relay SHALL listen on a SET of
channels, each **bound** to exactly one XO; a message's **origin channel**
determines its routing (the channel's bound XO and that XO's member scope). A
single-channel roster (one `channel_id` + `xo_agent`) is the degenerate
one-binding case and SHALL behave exactly as before. Injection is the wake; no
polling loop and no agent kept alive are required. A relayed delivery SHALL be
CONFIRMED — reported successful (logged and mirrored) only when a turn is confirmed
to have started (the `Idle → Working` edge), never on the bare exit code of the
tmux keystrokes. A relayed message that cannot be confirmed delivered SHALL raise a
LOUD operator alert; it SHALL NOT be reported as delivered.

#### Scenario: An operator message reaches the bound channel's XO
- **WHEN** the operator posts a bare message in a bound channel and `flotilla watch` is running
- **THEN** the message is delivered (typed + submitted) into that channel's bound XO pane and the delivery is confirmed (a turn started) before it is logged/mirrored as delivered

#### Scenario: A message off any bound channel is ignored
- **WHEN** a message arrives on a channel that is not in the binding set
- **THEN** the relay ignores it (the gateway admits only bound channels)

#### Scenario: A relayed message that does not start a turn is never reported delivered
- **WHEN** a relayed submit does not produce a confirmed turn after the bounded retries
- **THEN** a LOUD operator alert is raised and no "delivered" log or mirror is emitted

### Requirement: Feedback-loop immunity

The relay SHALL drop any gateway message carrying a non-empty webhook identifier,
author-agnostically, before any other processing — so the `send`/`notify` audit
posts can never re-enter the relay. This guard SHALL hold on EVERY bound channel,
and SHALL hold even if the author authorization is later broadened.

#### Scenario: The audit mirror does not feed back on any channel
- **WHEN** an audit/notify webhook post lands in any bound channel (a webhook message)
- **THEN** `flotilla watch` ignores it (no self-injection storm), on that channel and every other

### Requirement: Operator-only authorization

The relay SHALL act only on messages authored by the configured operator user id,
on every bound channel. All other authors SHALL be ignored. There is no per-command
authorization; the operator's account (and its two-factor authentication) is the
security boundary. (A future federation transport that lets a parent meta-XO
deliver into a child fleet's channel is a SEPARATE, explicitly-gated change that
introduces a pinned parent allow-list; it is not part of this delta.)

#### Scenario: Non-operator message ignored on every channel
- **WHEN** a message from any author other than the operator arrives on any bound channel
- **THEN** it is ignored

### Requirement: Routing to the XO or a named agent

Within a bound channel, a bare operator message SHALL route to that channel's bound
XO. A message of the form `@<name> <body>` SHALL route `<body>` to `<name>` when it
resolves (case-insensitive) against **that channel's member scope** — for a project
channel, the XO's desks; for the fleet-command channel, the project-XO members. An
unresolved `@<name>` SHALL route the whole message to the channel's XO with a
one-line notice. A leading `@@` SHALL remain the literal-`@` escape hatch to the
channel's XO. Bodies SHALL be preserved verbatim, including newlines.

#### Scenario: A desk is addressed within its project channel
- **WHEN** the operator posts `@backend run the tests` in project-alpha's channel and `backend` is one of alpha's members
- **THEN** `run the tests` is delivered to alpha's `backend` desk pane

#### Scenario: A project-XO is addressed within the fleet-command channel
- **WHEN** the operator posts `@alpha-xo status across your desks` in the fleet-command channel and `alpha-xo` is a member of the fleet-command binding
- **THEN** the message routes to `alpha-xo` (the project chief), per the configured cross-tier transport

#### Scenario: An unknown @name falls back to the channel's XO
- **WHEN** the operator posts `@nope do X` in a bound channel where `nope` is not in that channel's member scope
- **THEN** the whole message routes to that channel's bound XO with a one-line notice naming the unknown token

### Requirement: Serialized injection

All injections (relayed messages and heartbeats) SHALL pass through a single
worker so two deliveries never interleave into a pane's composer. Serialization SHALL also
cover the change-detector's context rotate (the `/clear` injection): a per-pane mutex SHALL
be held across a confirmed-delivery's submit-confirm-retry sequence AND acquired by the
rotate, so the two in-daemon pane writers can never interleave keystrokes into the same
composer.

#### Scenario: Concurrent relay and heartbeat do not corrupt
- **WHEN** a relayed message and a heartbeat tick are ready at the same instant
- **THEN** they are delivered one fully after the other, never interleaved

#### Scenario: A context rotate cannot interleave with an in-flight confirmed delivery
- **WHEN** the change-detector rotates the XO context while a confirmed delivery to the same
  pane is mid-sequence (between its submit and its retry)
- **THEN** the rotate waits for the delivery sequence to complete (the per-pane mutex
  serializes them); the `/clear` and the message body never interleave in the composer

### Requirement: Idle-gated XO heartbeat (the clock)

The system SHALL inject a heartbeat tick into the XO pane after a configurable
inactivity interval, so the XO keeps moving with no operator input. The timer
SHALL reset on every real relayed delivery (an operator message is itself a
tick). The tick SHALL be SKIPPED when the XO pane appears busy (its title shows a
working glyph), so a heartbeat never interrupts in-flight work. The tick prompt
SHALL be idempotent and check-then-noop (advance one pending step or reply idle;
never invent work).

#### Scenario: Heartbeat skipped while the XO is busy
- **WHEN** the inactivity interval elapses but the XO pane title shows a working glyph
- **THEN** no tick is injected this cycle

#### Scenario: Heartbeat fires when idle
- **WHEN** the inactivity interval elapses with no real delivery and the XO appears idle
- **THEN** one idempotent tick is injected

### Requirement: The heartbeat drives autonomous continuation of authorized work

The heartbeat SHALL make the XO self-continuing: on each tick, when there is
clear, already-authorized work in flight — an open task in the active change, an
unanswered desk report, an approved plan step — the XO SHALL advance that work
without waiting for the operator to re-prompt. The XO SHALL NOT manufacture new,
unauthorized work; when nothing is laid out it SHALL acknowledge idle and stop.
This is the mechanism that turns a turn-based agent into a dynamic system that
keeps building while clear work remains — the operator does not have to nudge it
through laid-out, obvious steps.

#### Scenario: Laid-out work is advanced without the operator
- **WHEN** a heartbeat fires and an open, already-authorized task remains (e.g. an unchecked task in the active openspec change)
- **THEN** the XO advances that work itself rather than waiting for the operator to re-prompt

#### Scenario: Nothing laid out — idle, no make-work
- **WHEN** a heartbeat fires and no authorized work is in flight
- **THEN** the XO acknowledges idle and does nothing (it does not invent work)

### Requirement: Liveness watchdog via tick-and-acknowledge

The watchdog SHALL determine XO liveness from acknowledgements, not process
existence: each heartbeat tick asks the XO to emit a one-line ack, and the
watchdog SHALL alert only after K consecutive ticks produce no ack within a
window (covering the alive-but-context-exhausted case). A pane that has fallen
back to a shell SHALL alert immediately as a crash fast-path. Alerts SHALL fire
on the down-transition with a cool-down and clear on recovery; pane-resolution
failures SHALL be non-fatal to the daemon.

#### Scenario: Exhausted-but-alive XO is detected
- **WHEN** the XO process is still running but produces no ack for K consecutive ticks
- **THEN** the watchdog posts one down-alert (not one per cycle) and stops winding the clock until recovery

### Requirement: Validated configuration and resilient runtime

`flotilla watch` SHALL validate its configuration at load — `heartbeat_interval`
parses as a duration and `xo_agent` exists in the roster — refusing to start
otherwise. The gateway connection SHALL auto-reconnect; an authentication
failure SHALL be non-restartable (so a bad token does not hot-loop); SIGTERM
SHALL close the session gracefully.

#### Scenario: Misconfiguration refuses startup
- **WHEN** `xo_agent` names an agent absent from the roster, or `heartbeat_interval` is unparseable
- **THEN** the daemon exits at startup with a clear error rather than failing silently at runtime

### Requirement: Materiality-gated XO waking (the change detector)

When the change-detector is enabled, the system SHALL run a deterministic (no-LLM)
detector each tick that snapshots materiality signals — each monitored desk's
assessed `surface` state and the optional **external signal-file**'s content hash
(a file the XO does NOT write) — diffs them against a persisted prior snapshot, and
wakes the XO ONLY on a material change, with a prompt naming what changed. When
nothing material changed, the XO SHALL NOT be woken (an idle fleet costs nothing).
The detector SHALL reuse the surface `Driver.Assess` for state (never raw pane
bytes), so transient render flicker is not a change. The XO's own single-writer
state tracker (`.flotilla-state.md`) SHALL NOT be a wake signal (it is the XO's own
output; see "Material change …").

#### Scenario: Idle fleet does not wake the XO
- **WHEN** a detector tick finds no material change since the last snapshot
- **THEN** the XO is not woken and no LLM turn is spent

#### Scenario: A material change wakes the XO with a targeted prompt
- **WHEN** a monitored desk transitions into an actionable state (or the external signal-file hash changes)
- **THEN** the XO is woken with a prompt naming the specific change

### Requirement: Material change is a curated transition set, not a raw diff

A material change SHALL be any of: a desk transition INTO an actionable state —
`Shell`, `Errored`, `AwaitingApproval`, `AwaitingInput`, or `Working→Idle`
(finished a turn); a change in the optional **external signal-file** hash (a file
the XO does NOT write); or XO self-continuation (below). A transition INTO
`Working`, a no-change, and transitions into/out of an unknown/unassessable state
SHALL NOT be material. The set SHALL be extensible without changing the detector
loop.

The materiality predicate SHALL key only on states the configured driver actually
emits: for v1 (claude-code, which emits Shell/Working/Idle) the live signals are
`→Shell` (debounced) and `Working→Idle` plus the external signal-file hash; the
`Errored`/`AwaitingApproval`/`AwaitingInput` branches activate automatically when a
driver emits those states (no mandated dead branches). The XO's OWN transitions
SHALL feed only the self-continuation path, never the desk-finished wake (the XO
pane is excluded from the desk branch).

The XO's **single-writer state tracker** (`.flotilla-state.md`) SHALL NOT be a wake
signal: because the XO writes it itself (the heartbeat instructs it to keep the
tracker current), a tracker delta is the XO's own action and waking on it would
re-wake the XO on its own writes (a self-perpetuating loop until the XO settles).
Genuine external state changes the XO must react to SHALL be delivered via the
external signal-file, which the XO does not write. A deployment whose tracker is
genuinely multi-writer MAY point the external signal-file at that tracker to restore
delta-as-wake behavior; the change-detector itself SHALL NOT hash the XO's own
tracker as a wake signal.

#### Scenario: The XO's own finish is self-continuation, not a desk wake
- **WHEN** the XO pane transitions Working→Idle
- **THEN** it produces exactly one self-continuation wake and is NOT also treated as a desk-finished material change

#### Scenario: A desk resuming work is not material
- **WHEN** a desk transitions Idle→Working
- **THEN** it is not a material change and the XO is not woken

#### Scenario: A desk finishing or needing attention is material
- **WHEN** a desk transitions Working→Idle, or into Shell/Errored/AwaitingApproval/AwaitingInput
- **THEN** it is a material change and the XO is woken

#### Scenario: The XO updating its own single-writer tracker does not self-wake
- **WHEN** the XO writes `.flotilla-state.md` (its own single-writer tracker)
- **THEN** no material wake is produced by the tracker delta

#### Scenario: An external signal-file delta is a wake
- **WHEN** the configured external signal-file's content hash changes (a file the XO does not write)
- **THEN** it is a material change and the XO is woken with a prompt naming the external signal

### Requirement: XO self-continuation without a blind timer

On the XO's own `Working→Idle` transition the system SHALL wake the XO once with a
continuation prompt that instructs it to advance the next clear, already-authorized
step if one remains and otherwise reply idle WITHOUT manufacturing work. The XO's
context SHALL be rotated between continuation steps. When the XO replies idle, the
system SHALL record a settled state and stop self-continuation waking until an
external material change (a desk transition, a tracker change, or an operator
message) — EXCEPT that a settle SHALL be VETOED while the fleet backlog gate reports
unblocked items remain (see "Backlog-gated goal-driven continuation"). An
operator-message wake SHALL clear the settled state.

#### Scenario: Settled XO sleeps until an external change
- **WHEN** the XO replies idle to a continuation wake AND the backlog gate reports no unblocked items
- **THEN** it is not woken again for self-continuation until a desk/tracker change or an operator message arrives

#### Scenario: Operator input re-engages a settled XO
- **WHEN** an operator message is relayed to a settled XO
- **THEN** the settled state is cleared and the message is delivered immediately

The system SHALL bound self-continuation with a hard cap: after a configurable
number of CONSECUTIVE XO-initiated continuation cycles with no interleaved
external material change, the system SHALL force the settled state and stop
waking, regardless of the XO's reply (the prompt discipline is the soft guard;
the cap is the deterministic backstop — context rotation between steps erases the
XO's ability to self-throttle, so a code-level cap is required) — EXCEPT that the
cap-forced settle, like the idle-reply settle, SHALL be VETOED while the backlog
gate reports unblocked items remain. The counter SHALL reset on any external
material change or operator message.

#### Scenario: Runaway self-continuation is capped when the backlog is drained
- **WHEN** the XO keeps returning a "next step" on consecutive continuation wakes with no external change, beyond the cap, AND the backlog gate reports no unblocked items
- **THEN** the system forces the settled state and stops self-continuation waking until an external material change or operator message

### Requirement: Three-layer liveness without regressing the detection window

Liveness SHALL be detected by: (1) immediate alert when the XO pane is a shell
(crash), debounced to require two consecutive shell assessments so a transient
pane-read error does not false-alarm; (2) a **wall-clock ack-age** threshold — the
detector evaluates the ack file's age every tick and alerts when it exceeds
`K×interval` while the XO is not a shell (age-based and cadence-independent, since
v2 no longer prompts the XO every interval; this replaces the prior emergent
missed-ack-per-tick window); and (3) a max-quiet liveness ping that forces a
minimal ack-only wake when the XO has not been woken for `N` intervals. Liveness
state SHALL be independent of the change-detector snapshot (kept in-memory + the
ack file) so a snapshot outage cannot blind the watchdog. The relationship between
`N`, the round-trip budget, and the `K×interval` window is a deployment tradeoff
(strict-window vs $0-idle) resolved in configuration.

#### Scenario: A transient pane-read blip does not false-alarm a crash
- **WHEN** a single tick reads the XO pane as a shell due to a transient tmux error but the next tick reads it live
- **THEN** no crash alert fires (two consecutive shell assessments are required)

#### Scenario: Healthy idle XO is not falsely alerted
- **WHEN** the fleet is idle and the XO is not woken by any material change for `N` intervals
- **THEN** a liveness ping wakes it, it re-acks, and no down-alert fires

#### Scenario: Wedged XO is still detected within the current window
- **WHEN** the XO is alive (not a shell) but stops acking
- **THEN** a down-alert fires at the `K×interval` staleness threshold, no later than before

### Requirement: XO context rotation via the surface RotateContext guard

After the XO settles (returns to Idle) from handling a change, the system SHALL
rotate its context via `surface.RotateContext` — injecting the reset for a
SlashCommand surface and signaling restart for a RestartProcess surface (never
injecting a slash into a restart-only surface). The rotate SHALL be skipped while
the awaiting-operator veto marker is present. This change DEFINES that marker as
new work (it was proposed in the never-merged PR #18 and does not exist in the
tree): a `--awaiting-file` (env `$FLOTILLA_AWAITING_FILE`, default
`<roster-dir>/flotilla-xo-awaiting`) the XO sets when it poses an operator
question and clears when answered/recorded; read fail-safe (unreadable/stale →
skip the rotate, never a wrongful rotate).

#### Scenario: Settled handling leaves the XO in fresh context
- **WHEN** the XO finishes handling a material change and is not awaiting an operator reply
- **THEN** its context is rotated (per the surface's strategy) before the next wake

#### Scenario: Rotate is skipped while awaiting the operator
- **WHEN** the XO has an outstanding operator question (the veto marker is present)
- **THEN** no context rotate is performed

### Requirement: Fail-safe, atomic snapshot persistence

The detector snapshot SHALL be written atomically (write-temp + rename) and read
fail-safe: a missing or corrupt snapshot SHALL degrade to treat-as-everything-
changed (wake once, conservatively), and a read/parse/write error SHALL NEVER
crash the detector or silently skip a tick.

#### Scenario: Corrupt snapshot does not break the detector
- **WHEN** the snapshot file is missing or unparseable
- **THEN** the tick treats all signals as changed (wakes once) and continues, persisting a fresh snapshot

### Requirement: Idle-gated relay with bounded busy-defer

The relay SHALL deliver an operator message to the XO only when the XO is idle. A message
arriving while the XO is busy SHALL be deferred (re-enqueued after a bounded delay) rather
than submitted into the active composer or blocking the single delivery worker — so delivery
to other desks proceeds meanwhile. A sustained-busy defer SHALL raise a LOUD operator alert
once (the message is queued behind a long turn), and the total deferral SHALL be bounded:
after the bound, the message SHALL be escalated and dropped rather than re-enqueued
indefinitely (a wedged XO must not produce an unbounded retry chain). A heartbeat or
change-detector wake (a time-relative tick) arriving while the XO is busy SHALL be dropped
(the next tick re-evaluates), not deferred.

#### Scenario: An operator message arriving mid-turn is deferred, then delivered when idle
- **WHEN** an operator message is enqueued while the XO assesses as `Working`
- **THEN** it is not submitted; it is re-enqueued after a bounded delay and delivered (and
  confirmed) once the XO is idle, while other desks' deliveries proceed in the meantime

#### Scenario: A sustained-busy operator message is escalated and bounded
- **WHEN** the XO stays busy past the sustained-busy threshold
- **THEN** a LOUD operator alert is raised once, and after the total deferral bound the
  message is escalated and dropped (not re-enqueued forever)

#### Scenario: A heartbeat tick arriving while busy is dropped, not deferred
- **WHEN** a heartbeat or change-detector wake is ready while the XO assesses as `Working`
- **THEN** it is dropped (the next tick re-evaluates from current state), not re-enqueued

### Requirement: A dropped operator message is never silent

The system SHALL raise a LOUD operator alert whenever a relay delivery is dropped for any
reason: a pane-lock-contention timeout, an exhausted busy-defer bound, or an unconfirmable
submit. The audit success log and channel mirror SHALL fire ONLY for a confirmed delivery.

#### Scenario: A pane-lock-contention drop of an operator message is escalated
- **WHEN** a relayed delivery is dropped because the per-pane lock was contended past its
  timeout
- **THEN** a LOUD operator alert is raised (the drop is not merely journal-logged)

### Requirement: Backlog-gated goal-driven continuation

When a fleet backlog is configured (opt-in via `--backlog-file`), the system SHALL NOT settle the
XO while the backlog reports UNBLOCKED items remain — overriding BOTH the XO's idle self-signal
AND the self-continuation cap. On the XO's `Working→Idle` transition with unblocked items present,
the system SHALL wake the XO to advance the top unblocked item, naming that item in the wake
prompt, paced at the heartbeat interval (never a tight loop). The system SHALL settle ONLY when the
backlog has no unblocked items (empty or all-operator-blocked) — or while the XO is awaiting an
operator answer (a legitimate operator-gated pause). When no backlog is configured, behavior SHALL
be unchanged.

#### Scenario: The loop does not settle while unblocked work remains
- **WHEN** the XO replies idle (or hits the self-continuation cap) but the backlog has unblocked items
- **THEN** the system does NOT settle; it wakes the XO to advance the top unblocked item

#### Scenario: The loop settles when the backlog is drained
- **WHEN** every backlog item is done or operator-blocked
- **THEN** the existing settle / self-continuation behavior applies (the gate is satisfied)

#### Scenario: An awaiting XO is not driven
- **WHEN** the XO is awaiting an operator answer (the awaiting marker is present)
- **THEN** the backlog drive is suppressed (the XO is not woken onto another task) until the operator responds

### Requirement: Per-item stuck handling, not whole-loop spin

While driving the backlog, the system SHALL track per-item drive counts and drive the
highest-priority unblocked item that has NOT exceeded a configurable stuck cap. When an item is
driven up to the stuck cap without leaving the unblocked set, the system SHALL raise a LOUD
operator alert naming that item ONCE and SHALL deprioritize it — driving other unblocked items
instead — so the loop drains the rest of the backlog rather than spinning on a non-progressing
item. An item that leaves the unblocked set (marked done/blocked, or advanced) SHALL have its
drive count cleared. An operator message SHALL clear all per-item drive counts.

#### Scenario: A stuck item is escalated and deprioritized
- **WHEN** the top unblocked item is driven up to the stuck cap without progressing while a lower-priority unblocked item remains
- **THEN** the stuck item is escalated once and the system drives the lower-priority item instead (it does not spin on the stuck one)

#### Scenario: Liveness is preserved when the XO never settles
- **WHEN** the backlog keeps the XO driving (never settling) and the XO stops acknowledging within the liveness window
- **THEN** the wedge alert still fires (the ack-age watchdog is independent of the settled state)

### Requirement: Deploy-surface enablement of the backlog gate

The anti-drift installer SHALL support an OPTIONAL `FLOTILLA_BACKLOG_FILE` key that enables the
backlog gate. The installer (`deploy/flotilla-watch-install.sh`) generates the systemd user unit
from `deploy/flotilla-watch.service.in` + a host-path `.env`; the key enables the backlog gate (see
"Backlog-gated goal-driven continuation") by appending ` --backlog-file <path>` to the generated
`ExecStart`. The key SHALL be
OPTIONAL: the installer SHALL NOT add it to the required-key check, so an `.env` without it still
generates a valid unit. When the key is UNSET (absent or commented), the generated unit SHALL be
byte-identical (at the functional-directive level) to a unit generated before this key existed — the
gate is OFF and there SHALL be no `--backlog-file` argument and no trailing space. When the key is
SET, the generated `ExecStart` SHALL contain exactly one ` --backlog-file <path>` argument using the
value taken from the `.env` file ONLY (an inherited/exported `FLOTILLA_BACKLOG_FILE` from the
process environment SHALL NOT leak into the generated unit). A backlog file that does not yet exist
at install time SHALL produce a non-fatal warning, not an error (the XO creates the file; the gate
is inert until it exists), in contrast to the roster/secrets hard-prerequisite errors. The
configuration SHALL be expressed as an `ExecStart` argument (NOT a systemd `Environment=` directive),
consistent with the existing roster/secrets/ack-file arguments.

#### Scenario: Backlog key set adds the argument
- **WHEN** the `.env` sets `FLOTILLA_BACKLOG_FILE=/srv/fleet/backlog.md`
- **THEN** the generated `ExecStart` ends with ` --backlog-file /srv/fleet/backlog.md` (exactly one argument, single-spaced)

#### Scenario: Backlog key unset is byte-identical to a no-backlog install
- **WHEN** the `.env` omits `FLOTILLA_BACKLOG_FILE` (absent or commented out)
- **THEN** the generated `ExecStart` contains no `--backlog-file` argument and no trailing space — identical to the unit generated before the key existed

#### Scenario: An inherited environment value does not leak
- **WHEN** `FLOTILLA_BACKLOG_FILE` is exported in the installer's process environment but the `.env` omits it
- **THEN** the generated `ExecStart` contains no `--backlog-file` argument (the value is read from the `.env` only)

#### Scenario: Existing five-key installs are unaffected
- **WHEN** an existing `.env` with only the five required keys is re-run through the installer
- **THEN** generation succeeds and the unit is unchanged (the optional key's absence is not a missing-required error)

### Requirement: At-least-once ingestion of operator messages (gateway-gap backstop)

The system SHALL guarantee that every operator message posted to a bound channel is **either relayed
to the routed agent at least once, or surfaced to the operator by a LOUD alert** — even when the live
Discord gateway websocket never delivers the message's `MESSAGE_CREATE` event (a reconnect /
resume-failure gap, or a daemon-restart window). A message accepted by Discord SHALL NOT be lost
without a trace.

To provide this, the system SHALL run a REST reconciliation, **independent of the gateway websocket's
health**, that fetches each bound channel's messages after a **durable per-channel cursor** and relays
(or, per the disposition rule below, alerts on) any operator message the live path has not already
relayed. The reconciliation SHALL run on a periodic interval AND SHALL additionally run immediately
when the gateway reconnects (the floor + accelerator). It SHALL be non-fatal: a failure to start or run
it SHALL degrade to live-gateway-only delivery with a warning and SHALL NOT affect the safety-critical
clock.

The durable cursor SHALL be advanced ONLY by the reconciler (after a contiguous after-cursor fetch),
never by the live gateway path. The live gateway path SHALL record relayed message ids for
deduplication but SHALL NOT advance the cursor — so a post-gap live message can never advance the
cursor past undelivered gap messages.

The reconciler SHALL enqueue recovered messages BEFORE it advances and persists the cursor, so that a
crash in that window causes a re-delivered duplicate on restart, never a silent drop. The reconciler
SHALL NOT advance the cursor past any message above the cursor that it has not fetched and processed
(a bounded backlog left by a page cap SHALL remain above the cursor for the next sweep, never
leapfrogged). Delivery is **at-least-once and NOT guaranteed in-order across the live/reconciler seam**
(a live message after a reconnect MAY precede an earlier gap message recovered on a later sweep); a
recovered batch SHALL itself be delivered in message-id order.

#### Scenario: A message the live gateway never delivered is recovered

- **WHEN** an operator message is posted to a bound channel but no `MESSAGE_CREATE` event is delivered
  to the live relay (e.g. it arrived during a gateway reconnect/resume-failure window)
- **THEN** the next REST reconciliation fetches it (its id is past the durable cursor), relays it to
  the routed agent (subject to the recovered-message disposition), and advances + persists the cursor

#### Scenario: A live-delivered message is not relayed twice

- **WHEN** the live gateway delivers a message that the reconciler subsequently also fetches
- **THEN** the reconciler recognizes the id as already relayed (the shared dedup set) and does NOT
  relay it again

#### Scenario: A post-gap live message does not orphan the gap messages

- **WHEN** messages arrive during a gateway gap (never delivered live), and a later message IS
  delivered live after the gateway recovers
- **THEN** the live message is relayed but does NOT advance the cursor, so the next reconciliation
  (fetching after the unchanged cursor) still recovers the earlier gap messages in id order

#### Scenario: First boot does not replay channel history

- **WHEN** the daemon starts with no persisted cursor for a channel
- **THEN** the cursor is initialized to the channel's latest message id WITHOUT relaying prior history,
  and reconciliation proceeds from there (no backlog flood)

#### Scenario: A corrupt or missing cursor store does not crash the daemon

- **WHEN** the persisted cursor file is missing or unreadable
- **THEN** the affected channels are treated as first-boot (tail-initialized), the daemon continues,
  and the failure is not fatal

### Requirement: Recovered-message disposition — auto-relay the few, alert the bulk or ancient

When the reconciler surfaces messages the live path missed, the system SHALL **auto-relay** those when
their count is at or under a bulk cap AND the batch's oldest message is within a (loose) stale ceiling —
the common reconnect-gap and routine-restart case — in message-id order, with a visible one-line trace
notice. The system SHALL NOT blind-inject a recovered batch whose count exceeds the bulk cap OR whose
oldest message is older than the stale ceiling; instead it SHALL raise a LOUD alert naming the count and
pointing the operator at the re-fetch command. The discriminator SHALL be count-primary (so a routine
deploy's few messages auto-deliver regardless of a few minutes' age), with the stale ceiling a loose
bound against replaying ancient directives after a very long outage. The cursor SHALL advance in both
cases (an alerted backlog has been surfaced; re-alerting it every sweep would be an alert storm).

#### Scenario: A small gap is auto-delivered

- **WHEN** the reconciler recovers a number of operator messages at or under the bulk cap whose oldest
  is within the stale ceiling
- **THEN** they are relayed in message-id order to the routed agent and a one-line catch-up notice is
  posted

#### Scenario: A large or ancient backlog is alerted, not flooded

- **WHEN** the reconciler recovers messages exceeding the bulk cap OR older than the stale ceiling
- **THEN** they are NOT auto-relayed; a LOUD alert names the count and directs the operator to the
  re-fetch command, and the cursor still advances (the backlog was surfaced, not re-alerted each sweep)

### Requirement: The catch-up reconciler's own liveness is never silent

A reconciler that itself stops working SHALL NOT fail silently — this backstop exists precisely to make
a silent ingestion failure loud. The system SHALL escalate ONCE to the operator when the reconciler
fails a threshold of consecutive sweeps (the at-least-once backstop is down while live gateway delivery
continues), and SHALL re-arm the escalation on recovery.

#### Scenario: A persistently failing reconciler is escalated

- **WHEN** the reconciler's REST sweep fails for a threshold of consecutive attempts
- **THEN** a LOUD operator alert is raised once stating the backstop is down (live delivery
  continues), and the alert re-arms after a subsequent successful sweep

### Requirement: Operator inbox re-fetch command

The system SHALL provide a `flotilla inbox <channel> [--limit N]` command that fetches and prints the
recent messages of a bound channel over the Discord REST API (resolving `<channel>` by its roster
`role` label or its raw channel id), so a dropped or missed message can be read and recovered without
hand-rolling a Discord API call. The command SHALL flag operator-authored messages and SHALL be
read-only.

#### Scenario: Recent channel messages are printed

- **WHEN** the operator runs `flotilla inbox <channel>` for a bound channel
- **THEN** the recent messages are printed in chronological order with timestamp, id, and content,
  operator-authored messages flagged

#### Scenario: An unknown channel is reported with valid options

- **WHEN** `<channel>` matches neither a binding role nor a bound channel id
- **THEN** the command errors and lists the valid channel roles/ids

### Requirement: The c2 hotline has a never-silent return leg

The system SHALL mechanically provide the RETURN leg of the operator↔XO hotline: a c2 channel is the
operator's hotline to its XO (an operator message in a bound channel routes to that channel's
`xo_agent` via `BindingForChannel→XOAgent`), and when such a message is confirmed-delivered to the XO,
the XO's resulting turn-final SHALL be routed back to that ORIGIN channel, attributed to the XO, for
EVERY such turn (not best-effort). The watcher SHALL be the UNIFIED return leg for EVERY channel's XO,
INCLUDING the PRIMARY XO (`cfg.XOAgent`): a relay delivery whose target is the origin channel's
`xo_agent` arms it regardless of whether that XO is the primary or a federated one. The primary XO's
prior host-local `Stop`-hook return leg is RETIRED — the watcher has the SAME replies-only semantics
(it arms ONLY on an operator relay delivery, never a heartbeat/detector tick), provided more robustly
(it knows the operator message at arm time and content-correlates from the store, with no
transcript-archaeology, no Stop-vs-flush race, and no per-pane host script).

The return leg SHALL detect the XO's reply from the harness SESSION STORE (the ground truth of
completed turns), NOT from pane-rendered state and NOT from the change-detector's heartbeat-cadence
sampling. It SHALL CORRELATE the reply to the specific operator message: it SHALL locate the operator
message as a recorded USER turn (the relay delivers it into the session, where the harness records it
verbatim) and return the text-bearing ASSISTANT turn that FOLLOWS it. Correlating to the user turn —
rather than a bare assistant-turn-count delta — is what makes the routed text the answer to THIS
message: it SHALL NOT mis-route a QUEUED message's prior, unrelated turn, nor an interleaved turn, as
the reply. The mechanism is timing-independent (whether the reply already completed or lands later, it
is found in the store) and uniform across harnesses whose stores record no per-entry timestamps. This
SHALL reliably capture a fast, queued, or sub-heartbeat-interval turn (which the detector-tick path
silently drops).

The reply SHALL be posted to the origin channel's webhook, resolved
`BindingForChannel(originChannel)→XOAgent→Webhook`, under the XO's identity, chunked to Discord's
limit. Because the reply is posted via a webhook, the relay's feedback-loop immunity (the `webhookID`
drop) SHALL prevent it from re-entering the relay.

Every NON-route outcome SHALL raise a LOUD operator alert (NOT a journald-only skip), extending the
"a dropped operator message is never silent" guarantee to the return leg: no reply within the bounded
window, an unresolved origin-channel webhook, or a failed post (which SHALL name the partial delivery
so the operator reads the pane for the remainder). The watcher SHALL bound its wait with a SOFT bound
(which escalates ONCE — "still working, will route when it lands" — but KEEPS watching, so a long XO
answer is routed rather than lost) and a HARD bound (the final give-up escalation). The watcher SHALL
be per-XO single — a newer hotline message supersedes and re-anchors the prior — and SHALL NOT emit a
stale reply to a superseded origin channel; in-flight watchers SHALL be cancelled on daemon shutdown.
The return leg SHALL be read-only with respect to the XO's pane (it acquires no pane transaction lock)
and SHALL NOT change the inbound relay, the detector tick, or the per-desk visibility mirror. Watchers
are IN-MEMORY: a daemon restart between an operator message and its reply loses that in-flight reply
(the operator re-asks) — v1 does not persist in-flight watchers.

#### Scenario: A channel XO's reply routes back to the operator (federated OR primary)

- **WHEN** the operator sends a message in a bound channel whose `xo_agent` is that channel's XO —
  whether a federated c2-channel XO or the PRIMARY XO — and that XO produces a turn-final in response
- **THEN** the XO's verbatim turn-final is posted back to that channel (attributed to the XO), detected
  from the session store as the assistant turn following the operator message's user turn — including
  when the turn completes faster than the heartbeat interval

#### Scenario: The primary XO uses the same watcher, not a host-local Stop-hook

- **WHEN** the operator sends a message to the primary XO in its channel
- **THEN** the reply routes back via the flotilla-native watcher (the same mechanism as a federated XO),
  with no dependency on a per-pane host-local Stop-hook (which is retired)

#### Scenario: A reply that never arrives is escalated, never silently dropped

- **WHEN** an operator message is confirmed-delivered to a channel's XO but no new assistant turn
  appears within the bounded window (or the origin-channel webhook cannot be resolved, or the post
  fails)
- **THEN** a LOUD operator alert is raised naming the XO and channel (e.g. "read its pane"), rather than
  the reply being dropped silently

#### Scenario: The return-leg reply does not feed back into the relay

- **WHEN** the XO's reply is posted to the origin channel via the channel webhook
- **THEN** the relay drops it (the `webhookID` feedback-loop guard), so the reply is not re-ingested as
  a new operator message

#### Scenario: A superseding hotline message re-anchors the watcher

- **WHEN** a second operator message is delivered to the same XO before the first reply has routed
- **THEN** the watcher re-anchors to the second message's origin channel and does not emit a stale
  reply to the first channel

### Requirement: Per-agent continuation prompt and detector tracker from the workspace

`flotilla watch` SHALL source the change-detector's **continuation** prompt and the
detector's tracker file from the heartbeated/detected agent's workspace when present,
with the existing roster/flag values as fallback. The continuation prompt comes from
`~/.flotilla/<agent>/HEARTBEAT.md` (a template whose `{{tracker}}`/`{{settle}}`
placeholders are substituted and whose ack instruction is appended) when present **and
non-empty**, else the built-in continuation prompt; in legacy (non-detector) mode the
order is `HEARTBEAT.md` → roster `heartbeat_message` → `DefaultHeartbeatPrompt`. The
detector tracker resolves `~/.flotilla/<agent>/state.md` **(when non-empty)** →
`--tracker-file`/`$FLOTILLA_TRACKER_FILE` → `<roster-dir>/.flotilla-state.md`. The
non-empty guards are load-bearing: `flotilla workspace init` scaffolds EMPTY
`HEARTBEAT.md`/`state.md`, and an empty file must NOT hijack the detector (a blank
prompt, or a static empty-hash tracker that blinds the change signal). A deployment
with no workspace SHALL behave exactly as before (same prompt, same tracker) — the
change is additive on the no-workspace path.

#### Scenario: A workspace HEARTBEAT.md overrides the detector continuation prompt
- **WHEN** the heartbeated agent runs under the change-detector and has `~/.flotilla/<agent>/HEARTBEAT.md`
- **THEN** the detector's continuation wake uses that template (placeholders substituted, ack appended), not the built-in prompt — and NOT the legacy `heartbeat_message`, which the detector never reads

#### Scenario: The prompt's named tracker path equals the detector's hashed path
- **WHEN** the tracker is resolved from the workspace `state.md`
- **THEN** that one resolved path is BOTH the file the change-detector content-hashes AND the path substituted into the continuation prompt's `{{tracker}}` — they never diverge, so the XO updates the same file the detector watches

#### Scenario: Switching the tracker source is a restart-time, not live, change
- **WHEN** the operator relocates the tracker to the workspace `state.md`
- **THEN** the new source takes effect on a `flotilla-watch` restart, and the first post-switch tick may emit one expected, harmless spurious material wake (the snapshot was keyed to the prior file)

#### Scenario: No workspace leaves today's behavior unchanged
- **WHEN** no workspace exists for the heartbeated agent
- **THEN** the prompt and tracker resolve to the built-in/roster/flag defaults exactly as before

#### Scenario: An empty scaffolded HEARTBEAT.md/state.md does not hijack the detector
- **WHEN** the workspace exists but `HEARTBEAT.md` and/or `state.md` are empty (freshly scaffolded, not yet migrated)
- **THEN** the empty `HEARTBEAT.md` falls back to the built-in prompt (never a blank wake) and the empty `state.md` falls back to the `--tracker-file`/default (never a static empty-hash that blinds the change signal)

### Requirement: External gateway-health watchdog detects alive-but-disconnected

The system SHALL provide an external watchdog (`flotilla-doctor`), a deterministic
pure-shell health check fired periodically by a systemd timer, that detects the
"`flotilla-watch` process alive but Discord gateway down" state — which the daemon
itself cannot surface, because its relay-open failure is non-fatal and systemd's
`Restart=on-failure` never fires. The check SHALL be pure (NO large-language-model
call in the cheap path) and SHALL determine gateway health from observable state:
the `flotilla-watch` unit is active, its MainPID resolves to a non-zero value, and
that process owns at least one ESTABLISHED `:443` socket (flotilla connects only to
Discord, so any established `:443` socket from its process identifier means the
gateway is up). An error from the socket-inspection tool itself SHALL be treated as
indeterminate and SHALL NOT cause an escalation.

#### Scenario: Gateway up — no action
- **WHEN** flotilla-watch is active, its MainPID resolves, and it owns an ESTABLISHED :443 socket
- **THEN** the watchdog records the tick as healthy, clears any accumulated strikes, and takes no further action

#### Scenario: Process alive but no gateway socket — flagged
- **WHEN** flotilla-watch is active but owns no ESTABLISHED :443 socket
- **THEN** the watchdog treats the gateway as down and begins the confirmation sequence

#### Scenario: Socket-inspection tool error does not escalate
- **WHEN** the socket-inspection tool itself errors (not "no sockets", but a tool failure)
- **THEN** the watchdog treats the tick as indeterminate and does not escalate

### Requirement: Sustained-down confirmation before escalation

The watchdog SHALL require a sustained gateway-down before escalating, to avoid
acting on a momentary reconnect between ticks. A first down reading SHALL be
re-checked once after a short delay; if the recheck is healthy, the watchdog SHALL
clear its state and take no action. A still-down recheck SHALL increment a strike
counter persisted across ticks, and the watchdog SHALL escalate only once the strike
count reaches a configurable threshold. With the default cadence and threshold this
SHALL yield several minutes of confirmed-down before any escalation.

#### Scenario: Momentary blip clears on recheck
- **WHEN** a tick reads the gateway down but the single recheck reads it healthy
- **THEN** the watchdog clears its strikes and does not escalate

#### Scenario: Below-threshold strikes wait for more confirmation
- **WHEN** the gateway is still down after the recheck but the strike count is below the configured threshold
- **THEN** the watchdog records the strike and waits for subsequent ticks rather than escalating

#### Scenario: Threshold reached escalates
- **WHEN** the strike count reaches the configured threshold
- **THEN** the watchdog escalates

### Requirement: Escalation is notify-plus-diagnose, never restart

On a confirmed sustained gateway-down the watchdog SHALL escalate by (1) firing a
best-effort operator notify carrying a status payload, and (2) spawning a
time-bounded headless recovery agent that diagnoses the cause and applies the right
fix. The watchdog SHALL NEVER restart, stop, or otherwise control the
`flotilla-watch` process: whether a restart is warranted is the recovery agent's
decision after diagnosis, because the most common cause is a resolver failure that a
blind restart does not fix and restarting the safety-critical clock is the
operator's prerogative. The status payload SHALL include the gateway/process state,
a journal tail, the liveness ack-file age, and a per-resolver DNS probe so the
recovery agent can diagnose DNS first. The operator notify SHALL be best-effort: a
notify failure (for example, the same outage that downed the gateway also blocking
the notify) SHALL be logged and SHALL NOT prevent the recovery agent from running.
The recovery agent SHALL run under the host's permission gate (fail-closed) and SHALL
NOT be granted a permission bypass. A cooldown SHALL prevent re-spawning the recovery
agent on every subsequent tick while it works or while the operator acts.

#### Scenario: Escalation notifies and spawns the diagnosis agent
- **WHEN** the watchdog escalates
- **THEN** it fires a best-effort operator notify with the status payload and spawns the time-bounded recovery agent — and does not restart flotilla-watch

#### Scenario: Notify failure does not block diagnosis
- **WHEN** the operator notify fails (for example because the gateway-downing outage also blocks the notify)
- **THEN** the watchdog logs the failure and still spawns the recovery agent

#### Scenario: Cooldown prevents re-spawn storm
- **WHEN** a prior escalation occurred within the cooldown window and the gateway is still down
- **THEN** the watchdog does not spawn another recovery agent until the cooldown elapses

### Requirement: Watchdog runs are single-flight and observe-only

The watchdog SHALL prevent overlapping runs (a long run that spawns the recovery
agent must not collide with the next timer tick) by acquiring an exclusive lock and
exiting cleanly when the lock is already held. The watchdog SHALL be observe-only
with respect to the daemon: it reads the daemon's externally-visible state
(unit-active, process identifier, sockets, journal) and SHALL NOT import or mutate
daemon internals.

#### Scenario: Overlapping run exits cleanly
- **WHEN** a watchdog run starts while a prior run still holds the lock
- **THEN** the new run exits without performing a check or an escalation

### Requirement: The cheap side-channel checker escalates to a full XO turn only on a real trigger

The change-detector tick SHALL act as a cheap (pure-Go, no-LLM) side-channel
checker that examines durable state — each monitored desk's assessed `surface`
state and the external signal-file hash — and escalates to a full XO turn ONLY on
one of a named, extensible trigger set: (1) an operator message (delivered
immediately by the relay, which also clears the XO settled state); (2) a desk
needing attention (a material desk transition); (3) an external signal-file delta;
(4) XO self-continuation (the XO's own `Working→Idle`, bounded by the
self-continuation cap); or (5) the max-quiet liveness ping (a clock-driven
wedge-probe, independent of the other signals). Every other observed condition — a
desk resuming work, render flicker, the XO advancing its own single-writer tracker,
a steady idle state — SHALL NOT escalate (no XO turn). Adding a new trigger SHALL
NOT require changing the detector loop shape.

#### Scenario: Nothing actionable — no XO turn
- **WHEN** a tick finds no operator message, no material desk transition, no external signal delta, the XO not self-continuing, and the ping cadence not reached
- **THEN** the XO is not woken and no LLM turn is spent

#### Scenario: A real trigger escalates to a full XO turn
- **WHEN** any of the named triggers fires (operator message, desk needs attention, external signal delta, self-continuation, or the liveness ping)
- **THEN** the XO is woken with a prompt naming the specific trigger

### Requirement: External signal-file for non-XO wake deltas

The system SHALL support an optional external signal file (`--signal-file`, env
`$FLOTILLA_SIGNAL_FILE`) whose content-hash change is a material wake trigger. The
signal file is for state the XO must react to but does NOT itself write (a desk or
tool dropping a signal); keeping it distinct from the XO's own single-writer
tracker is what prevents the XO's writes from self-waking it. When unconfigured,
there SHALL be no external-signal trigger (absent → no signal), and a read error on
the signal file SHALL be treated as unchanged (no wake-storm), the same fail-safe
direction as the prior tracker-hash signal.

#### Scenario: Unconfigured signal file is inert
- **WHEN** no `--signal-file` is configured
- **THEN** no external-signal wake is ever produced and the detector behaves as if the trigger is absent

#### Scenario: A signal-file read error does not wake-storm
- **WHEN** the configured signal file is momentarily unreadable
- **THEN** the tick treats it as unchanged and produces no wake

### Requirement: A per-desk visibility mirror posts each non-XO desk's turn-final to its own channel

The system SHALL provide a per-desk VISIBILITY mirror: when a NON-XO desk finishes a turn, the daemon
SHALL post that desk's substantive turn-final to the DESK's OWN channel webhook
(`secrets.Webhook(<desk>)`), under the desk's identity, chunked below Discord's hard content limit (a
per-chunk budget held under 2000 runes for headroom) — so the operator/XO can see what a desk has been
doing in its own channel. This is DISTINCT from the operator↔XO hotline
RETURN leg (which routes a reply to the OPERATOR's origin channel): the visibility mirror fires for
EVERY non-XO desk turn and posts to the DESK's channel, not in response to an operator message.

The visibility mirror SHALL be OBSERVE-ONLY and BEST-EFFORT: it SHALL NEVER affect the desk or
propagate a failure, and it SHALL emit exactly one decision log line per finished desk — a clean SKIP
(no webhook configured, no session-store reader for the surface, or no substantive turn-final), a POST
(the turn-final was mirrored, with its chunk count), or a MIRROR-FAIL (a chunk post failed,
redaction-safe). The turn-final SHALL be read from the harness session store via the surface
`ResultReader` — the SAME extraction `flotilla result` uses, so the CLI and the auto-mirror never
diverge. That extraction SHALL resolve the desk's OWN session by its working directory and SKIP a
colliding desk's session (the lossy project-dir-encoding guard), so a desk's channel never carries
another desk's turn-final. Because it posts via a webhook, the relay's feedback-loop immunity (the
`webhookID` drop) SHALL prevent the mirrored post from re-entering the relay.

The visibility mirror SHALL be TRIGGERED by the change-detector's sampled `Working→Idle` edge at the
heartbeat-interval cadence. It is therefore explicitly BEST-EFFORT and LOSSY: a turn that starts AND
finishes entirely within one detector-tick window is NOT observed and is NOT mirrored. A desk's channel
is consequently a best-effort VIEW of its activity, NOT a reliable or complete record — and this
property SHALL be documented so the channel is not mistaken for a complete log. (Making per-desk
mirroring reliable — per-turn store-completion detection independent of the tick — is a separate,
scoped change, NOT part of this requirement.)

The daemon SHALL emit a startup coverage line naming which non-XO desks WILL mirror (a webhook
resolves) and which will NOT (no webhook ⇒ a per-desk SKIP at runtime), so a mis-provisioned desk is
visible at boot rather than at the first dropped mirror.

#### Scenario: A non-XO desk's turn-final is mirrored to its own channel

- **WHEN** a non-XO desk with a configured webhook finishes a turn that the detector observes (its
  `Working→Idle` edge lands within a tick), and the turn has substantive turn-final text
- **THEN** the desk's turn-final is posted to the desk's own channel webhook, under the desk's identity,
  chunked, and exactly one POST decision line is logged

#### Scenario: A desk with no webhook / no reader / no substantive turn is a clean skip

- **WHEN** the mirror runs for a desk that has no configured webhook, OR whose surface has no
  session-store reader, OR whose finished turn has no substantive turn-final
- **THEN** the mirror posts nothing and logs exactly one SKIP decision line, and the desk is never
  affected

#### Scenario: A sub-tick turn is not mirrored (the documented best-effort lossiness)

- **WHEN** a desk's turn starts and finishes entirely within one change-detector tick window (so the
  detector samples Idle before and Idle after, never observing the `Working→Idle` edge)
- **THEN** that turn is NOT mirrored — the desk channel is a best-effort view, not a complete record
  (this is the documented property, not a silent defect the spec hides)

#### Scenario: The mirrored post does not feed back into the relay

- **WHEN** a desk's turn-final is posted to its channel via the webhook
- **THEN** the relay drops it (the `webhookID` feedback-loop guard), so it is not re-ingested as an
  operator message

