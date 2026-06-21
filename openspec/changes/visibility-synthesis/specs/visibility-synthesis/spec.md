# visibility-synthesis Specification

## Purpose

flotilla's stratified visibility flows awareness UP the federation hierarchy, with depth inverse to
altitude. **Tier 1** (the mechanical per-desk mirror, `desk-mirror-tier1`) already posts each boat's
turn-final output to that boat's own channel. This capability covers **Tiers 2 and 3** — the
LLM-curated synthesis that rolls a level UP: an Executive Officer (XO) synthesizes its boats into its
own channel (Tier 2), and the meta-XO synthesizes the XO channels into the command-and-control
channel #c2 (Tier 3). Synthesis is a constitutional-set SKILL run on the heartbeat, NOT daemon code —
the integrating half of the substrate/integrator split (Tier 1 = the deterministic substrate; Tiers
2/3 = the integrating LLM, one level up). The substrate is LOCAL (a deterministic mirror-event ledger
and the boats' local transcripts), NOT Discord channel history. This capability is the SECOND member
of the installable constitutional set shipped by `constitutional-skillset`, plugged into its
member-count-agnostic registry via a new delivery mechanism.

## ADDED Requirements

### Requirement: Synthesis reads a LOCAL substrate, never Discord channel history

The system SHALL source visibility synthesis from a LOCAL substrate — a deterministic mirror-event
ledger of boat-finish events and, as drill-down enrichment, the boats' local session transcripts —
and SHALL NOT read Discord channel history. The synthesis substrate read SHALL be architecturally
disjoint from the inbound relay (`relay.Accept`/`relay.Route`): it SHALL never be routed through the
inbound command path, so a mirror event is never consumed as a command and a synthesis post is never
re-injected as a command. The mirror-event ledger SHALL be the PRIMARY substrate (bounded, read by
its tail since a synthesis watermark); direct transcript read SHALL be enrichment-only drill-down for
the full text behind a ledger gist.

#### Scenario: Synthesis reads the local ledger, not Discord

- **WHEN** an XO synthesizes its boats' activity
- **THEN** it reads the local mirror-event ledger (and, for drill-down only, a boat's local transcript), and reads no Discord channel history

#### Scenario: The synthesis read is disjoint from the command relay

- **WHEN** the synthesis substrate is read
- **THEN** the read does not pass through the inbound relay, so a mirror event is never treated as an operator command and a synthesis post is never re-injected as a command

### Requirement: The mirror-event ledger is a bounded, atomic-append local substrate

The system SHALL append one bounded event record per boat-finish to a deterministic local ledger,
written by the Tier-1 mirror path IN ADDITION to its Discord post. Each ledger record SHALL be a
single physical line carrying at least the event time, the boat's channel, the boat's agent name, and
a clamped gist of the boat's turn-final output. Each line SHALL be bounded so a single append write is
atomic with respect to concurrent appenders on a local filesystem (the gist is rune-clamped and the
line is byte-clipped as a backstop, as the chief-of-staff ledger does). The ledger append SHALL be
best-effort and observe-only: a failure to append SHALL NEVER affect the Tier-1 Discord mirror, the
detector tick, message delivery, or any other behavior — it is logged and dropped. The ledger path
SHALL resolve to a LOCAL filesystem (the atomic-append guarantee relies on `O_APPEND`-under-`PIPE_BUF`
atomicity, which networked mounts may not honor). The synthesis ledger SHALL be SEPARATE from the
chief-of-staff who-knows-what ledger (the two are orthogonal axes — see the orthogonality
requirement).

#### Scenario: A boat finish appends one bounded ledger event

- **WHEN** a boat completes a unit of work (the Tier-1 work-finished edge)
- **THEN** one bounded single-line event (time, channel, agent, clamped gist) is appended to the local synthesis ledger, beside the Tier-1 Discord post

#### Scenario: A ledger-append failure never harms the fleet

- **WHEN** the ledger append fails (a read-only filesystem, a full disk)
- **THEN** the failure is logged and dropped; the Tier-1 Discord mirror, the detector tick, and delivery are all unaffected

### Requirement: Synthesis routing is the transpose of the command graph

The system SHALL derive synthesis routing purely from the federation `members[]` graph, with NO new
roster schema. For a synthesizing agent, the system SHALL expose the channels it is AWARE OF — every
channel where the agent is a member OR the channel's XO — via a roster accessor (`ChannelsAwareOf`),
as a pure read-only derivation that does not mutate any binding. The agent's synthesis READ set SHALL
be the channels it is aware of MINUS the channels it OWNS (the channels it is the XO of), so it reads
strictly the level below and never its own channel. The synthesis POST target SHALL be the channel
the agent owns (`ChannelForXO`), delivered via that XO's webhook.

#### Scenario: An XO reads its boats' channel and posts to its own

- **WHEN** an XO synthesizes
- **THEN** its read set is the channels it is aware of minus the channel it owns, and it posts the synthesis to the channel it owns

#### Scenario: A multi-channel XO never reads its own post target (self-loop guard)

- **WHEN** an agent is both a member of a peer's channel AND the XO of its own channel
- **THEN** its synthesis read set excludes the channel it owns, so its read set can never equal its post target and it never synthesizes its own synthesis posts

### Requirement: The membership graph is asserted acyclic at roster load, fail-closed

The system SHALL assert that the channel-membership graph (an edge from each channel's XO to each of
that channel's members) is a directed acyclic graph (DAG) at roster load, and SHALL REFUSE to start
when it is cyclic. This guarantees the "read below, post own level" routing cannot form a synthesis
feedback loop. The check SHALL run once at load (not on the synthesis hot path).

#### Scenario: A cyclic federation refuses to start

- **WHEN** the roster's channel-membership graph contains a cycle (two channels each listing the other's XO as a member)
- **THEN** roster load fails with a clear error and the daemon refuses to start

#### Scenario: An acyclic federation loads and routes

- **WHEN** the roster's channel-membership graph is acyclic
- **THEN** roster load succeeds and synthesis routing derives the read/post sets from it

### Requirement: A daemon-emitted WakeSynthesis wake-kind drives the cadence

The system SHALL drive synthesis cadence from a daemon-emitted wake kind (`WakeSynthesis`), a sibling
of the existing change-detector wake kinds, NOT from skill-self-scheduling. A boat-finish event SHALL
mark synthesis "owed" for the channel's synthesizing XO. The detector SHALL fire `WakeSynthesis` for
that synthesizing agent on a digest sub-cadence (debounce-up): a burst of boat finishes SHALL
coalesce into at most one synthesis wake per the digest cadence per synthesizing agent, and a fleet
with no synthesis owed SHALL fire no synthesis wake (an idle fleet costs nothing). The
`WakeSynthesis` side-effect SHALL be performed OUTSIDE the detector's state mutex (like every other
wake), so the synthesis enqueue can never stall the tick loop. The wake SHALL be enqueued to the
SYNTHESIZING agent (which may be a project XO for Tier 2 or the meta-XO for Tier 3), not necessarily
the daemon's primary clock XO.

#### Scenario: A burst of boat finishes coalesces into one synthesis wake

- **WHEN** several boats finish turns within one digest sub-cadence window
- **THEN** the detector fires at most one WakeSynthesis for their XO that window (debounce-up), not one per finish

#### Scenario: An idle fleet fires no synthesis wake

- **WHEN** no boat-finish event has marked synthesis owed
- **THEN** no WakeSynthesis is fired (zero idle cost)

#### Scenario: Self-scheduling is not relied upon

- **WHEN** the fleet is idle and a prior synthesis "scheduled itself" for a later tick
- **THEN** cadence does not depend on that self-schedule (which the idle-wake suppression and context rotation would defeat); the daemon owns the cadence via WakeSynthesis

### Requirement: The synthesis skill ships as a heartbeat-skill constitutional member

The system SHALL deliver the visibility-synthesis skill as a member of the installable constitutional
set via a NEW delivery `Mechanism` value (`heartbeat-skill`), extending the set's mechanism vocabulary
per its extensibility seam. A `heartbeat-skill` member SHALL be delivered as a WHOLE-FILE skill
written into the agent's workspace (NOT appended into the agent's standing identity file), because
synthesis is a tick-time discipline invoked when the daemon emits `WakeSynthesis`, not a structural
identity rule. Its install idempotency SHALL be the whole-file kept/created granularity (a missing
skill file is created, an existing one is KEPT so operator edits survive), distinct from the
identity-append marker guard. Adding this member SHALL require no change to the install/seed loop
(which iterates the registry and dispatches by mechanism); the new mechanism's write/load dispatch
arm SHALL be added alongside the member in the same change. The skill content SHALL be the curation
prompt.

#### Scenario: The synthesis skill installs as a whole-file member

- **WHEN** the constitutional set is installed for a synthesizing agent
- **THEN** the visibility-synthesis skill file is written into the agent's workspace (created if absent, kept if present), and the identity-append members are unaffected

#### Scenario: Re-installing keeps an operator-edited synthesis skill

- **WHEN** the operator has edited the installed synthesis skill file and the set is re-installed
- **THEN** the existing skill file is kept unchanged and reported as kept

### Requirement: Tier 2 produces a curated domain rollup; Tier 3 produces a command-and-control headline

The system SHALL produce, for a Tier-2 synthesis (an XO into its own channel), a curated, compressed
domain rollup of its boats' material activity since the last synthesis — grouped by boat, surfacing
what needs the operator's attention, not a raw firehose. The system SHALL produce, for a Tier-3
synthesis (the meta-XO into the command-and-control channel), a fleet headline PLUS the open operator
decisions PLUS drill-down pointers that follow the inverse of the membership graph (command channel →
XO channel → boat channel → pane), so a reader can plumb to any depth. Each synthesis SHALL be
governed by the narrow-answer discipline: when nothing material has changed since the synthesis
watermark, the agent SHALL advance the watermark and reply idle rather than manufacture a synthesis.

#### Scenario: A Tier-2 synthesis is a curated rollup, not a firehose

- **WHEN** an XO runs a Tier-2 synthesis
- **THEN** it posts a compressed domain rollup grouped by boat with attention-worthy items surfaced, not every raw boat turn

#### Scenario: A Tier-3 synthesis carries headline, decisions, and drill-down pointers

- **WHEN** the meta-XO runs a Tier-3 synthesis
- **THEN** the #c2 post carries a fleet headline, the open operator decisions, and drill-down pointers down the membership graph to each XO channel

#### Scenario: Nothing material yields no manufactured synthesis

- **WHEN** no material activity occurred since the synthesis watermark
- **THEN** the agent advances the watermark and replies idle rather than posting a manufactured synthesis

### Requirement: Vertical synthesis is orthogonal to the horizontal chief-of-staff ledger

The system SHALL keep visibility synthesis (a VERTICAL activity-rollup up the hierarchy) orthogonal to
the chief-of-staff who-knows-what ledger (a HORIZONTAL view of operator↔XO exchanges across channels).
The two SHALL be independent heartbeat steps and SHALL NOT share a ledger: the synthesis ledger
records boat-finish activity events; the chief-of-staff ledger records operator↔XO message exchanges.
Neither SHALL gate or depend on the other.

#### Scenario: The two synthesis axes do not share substrate

- **WHEN** both the chief-of-staff mirror and visibility synthesis are active
- **THEN** they read and write separate ledgers and run as independent heartbeat steps, neither gating the other
