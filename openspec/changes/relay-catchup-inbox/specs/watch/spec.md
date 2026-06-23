# watch Specification (delta)

## ADDED Requirements

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

Because this backstop exists precisely to make a silent ingestion failure loud, a reconciler that
itself stops working SHALL NOT fail silently. The system SHALL escalate ONCE to the operator when the
reconciler fails a threshold of consecutive sweeps (the at-least-once backstop is down while live
gateway delivery continues), and SHALL re-arm the escalation on recovery.

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
