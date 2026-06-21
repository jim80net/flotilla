# visibility-synthesis Specification

## Purpose

flotilla's stratified visibility flows awareness UP the federation hierarchy, with depth inverse to
altitude. **Tier 1** (the mechanical per-desk mirror, `desk-mirror-tier1`) already posts each boat's
turn-final output to that boat's own channel. This capability covers **Tiers 2 and 3** — the
LLM-curated synthesis that rolls a level UP: an Executive Officer (XO) synthesizes its boats into its
own channel (Tier 2), and the meta-XO synthesizes the XO channels into the command-and-control
channel #c2 (Tier 3). Synthesis is a constitutional-set SKILL run on a daemon-emitted synthesis wake,
NOT daemon code — the integrating half of the substrate/integrator split (Tier 1 = the deterministic
mechanical mirror; Tiers 2/3 = the integrating LLM, one level up).

**The substrate is TRANSCRIPT-FIRST and LOCAL** — a synthesizing agent reads the LATEST STATE of each
subordinate directly from that subordinate's local session transcript (`internal/claudestore`), the
same reader the Tier-1 mirror uses. It does NOT read Discord channel history, and it does NOT depend
on any new write-path (no mirror-event ledger in v1). A rollup is a current-STATE view ("where is each
subordinate right now"), not an event log of every finish.

**The topology (stated explicitly so routing is never mis-read):** each agent OWNS its home channel
(`xo_agent == self`) and its PARENT is in that channel's `members[]` (e.g. `xo_agent=tactical-head
members=[family-office]`; `xo_agent=family-office members=[hydra-ops]`). Therefore "read the tier BELOW
me" = read the agents whose channels list ME as a member — a DOWN-traversal of the membership graph.
Command flows DOWN the graph; awareness flows UP; both are the SAME `members[]` graph traversed in
opposite directions.

This capability is the SECOND member of the installable constitutional set shipped by
`constitutional-skillset`, plugged into its member-count-agnostic registry via a new delivery
mechanism (`heartbeat-skill`), which extends the set's `Mechanism` vocabulary (today `identity-append`
only) per its extensibility seam.

## ADDED Requirements

### Requirement: Synthesis reads the subordinates' latest transcript STATE, never Discord channel history

The system SHALL source visibility synthesis from a LOCAL TRANSCRIPT-FIRST substrate: a synthesizing
agent SHALL read the LATEST turn-final state of each agent in the tier below it directly from that
agent's local session, through the SAME surface-agnostic reader the Tier-1 mirror uses — the agent's
`surface.ResultReader.LatestResult(pane)` (claude resolves it to `claudestore.LatestTurnText`, grok to
the grok store), NOT a direct bind to `claudestore` (which would exclude a non-claude subordinate the
mirror reads fine) — and SHALL NOT read Discord channel history. Reading a subordinate requires
resolving its tmux pane on the synthesizer's host; the system SHALL treat a subordinate whose pane
cannot be resolved (cross-host, or transiently gone) or whose surface exposes no `ResultReader` as a
CLEAN SKIP from the rollup, never a failed synthesis. Synthesis therefore carries a v1 PRECONDITION:
each read-set subordinate's session is HOST-LOCAL to the synthesizer (the single-host fleet);
cross-host synthesis is out of scope for v1 (it pairs with the deferred finish-history ledger). The
read SHALL be bounded — the latest turn per subordinate (N bounded reads for N subordinates), not an
unbounded windowing pass over transcript history. The synthesis read SHALL be architecturally disjoint
from the inbound relay (`relay.Accept`/`relay.Route`): it is a read-only local file read, never routed
through the inbound command path, so a subordinate's transcript is never consumed as a command and a
synthesis post is never re-injected as a command. The system SHALL NOT add any new write-path to the
shipped Tier-1 mirror to source synthesis (no mirror-event ledger in this capability).

#### Scenario: Synthesis reads subordinates' latest transcript state, not Discord

- **WHEN** an XO synthesizes its boats' activity
- **THEN** it reads each boat's latest turn-final state from that boat's local transcript, reads no Discord channel history, and adds no write-path to the Tier-1 mirror

#### Scenario: The synthesis read is disjoint from the command relay

- **WHEN** the synthesis substrate is read
- **THEN** the read is a read-only local transcript read that does not pass through the inbound relay, so a subordinate's transcript is never treated as an operator command and a synthesis post is never re-injected as a command

#### Scenario: The read is bounded to the latest state per subordinate

- **WHEN** a synthesizing agent reads its read set
- **THEN** it reads the latest turn-final state of each subordinate (N bounded reads), not an unbounded windowing pass over each subordinate's transcript history

#### Scenario: An unresolvable subordinate is cleanly skipped, not a failed synthesis

- **WHEN** a synthesizing agent's read set includes a subordinate whose pane cannot be resolved on this host (or whose surface exposes no result reader)
- **THEN** that subordinate is skipped from the rollup and the synthesis proceeds over the readable subordinates, rather than the wake failing

### Requirement: Synthesis routing is a down-traversal of the membership graph

The system SHALL derive synthesis routing purely from the federation `members[]` graph, with NO new
roster schema. For a synthesizing agent A, the tier BELOW A SHALL be the agents whose channels list A
as a member: the XO of each channel where `A ∈ ch.Members` and that XO is not A itself. The system
SHALL expose, as pure read-only derivations over `Bindings()` that do not mutate any binding: the
channels an agent is aware of (`ChannelsAwareOf` — every channel where the agent is a member OR the
channel's XO) and the channels an agent owns (`OwnedChannels` — every channel where the agent is the
XO, generalizing the single-channel `ChannelForXO` to the multi-hub case). The agent's synthesis READ
set SHALL be the channels it is aware of MINUS the channels it owns, and the read AGENTS SHALL be the
XOs of those read channels — so the agent reads strictly the tier below and never its own channel. The
synthesis POST target SHALL be the channel(s) the agent owns, delivered via that agent's webhook.

#### Scenario: An XO reads its boats and posts to its own channel

- **WHEN** an XO synthesizes
- **THEN** its read set is the agents below it (the XOs of the channels it is a member of, excluding itself), and it posts the synthesis to the channel it owns

#### Scenario: A multi-channel XO never reads its own post target (self-loop guard)

- **WHEN** an agent is both a member of a peer's channel AND the XO of its own channel
- **THEN** its synthesis read set excludes the channel it owns, so its read set can never equal its post target and it never synthesizes its own synthesis posts

### Requirement: The membership graph is asserted acyclic at roster load, excluding self-edges, fail-closed

The system SHALL assert that the channel-membership graph is a directed acyclic graph (DAG) at roster
load, and SHALL REFUSE to start when it is cyclic, so the "read below, post own level" routing cannot
form a synthesis feedback loop. The graph SHALL be built with an edge from each channel's XO to each
of that channel's members EXCEPT the channel's own XO — i.e. a SELF-edge (an agent that is a member of
its OWN channel) SHALL be EXCLUDED and is NOT a cycle. A cycle is ONLY a MUTUAL membership between two
DISTINCT channels (channel-X's XO is a member of channel-Y and channel-Y's XO is a member of
channel-X). The check SHALL run once at load (not on the synthesis hot path).

#### Scenario: A self-membership home channel loads (no false cycle)

- **WHEN** the roster contains a channel whose XO is also listed among its own members (the live/legacy home-channel shape, e.g. #c2 with `xo_agent=hydra-ops` and `hydra-ops` in its members, or the legacy single-binding form)
- **THEN** roster load succeeds (the self-edge is excluded; it is not treated as a cycle)

#### Scenario: A cyclic federation refuses to start

- **WHEN** the roster's channel-membership graph contains a mutual cycle between two DISTINCT channels (each lists the other's XO as a member)
- **THEN** roster load fails with a clear error and the daemon refuses to start

#### Scenario: An acyclic federation loads and routes

- **WHEN** the roster's channel-membership graph is acyclic (after excluding self-edges)
- **THEN** roster load succeeds and synthesis routing derives the read/post sets from it

### Requirement: A daemon-emitted WakeSynthesis wake-kind drives the cadence, targeting an arbitrary synthesizing agent

The system SHALL drive synthesis cadence from a daemon-emitted wake kind (`WakeSynthesis`), a sibling
of the existing change-detector wake kinds, NOT from skill-self-scheduling. Because a synthesizing
agent may be a project XO (Tier 2) or the meta-XO (Tier 3) and is generally NOT the daemon's single
primary clock XO, the detector's wake seam SHALL carry an AGENT parameter via a PARALLEL
agent-targeted wake (leaving the existing primary-XO `Wake` path byte-identical), and the wake SHALL
be enqueued to the SYNTHESIZING agent — the existing primary-XO-only wake path is insufficient. A
boat-finish event SHALL mark synthesis "owed" for the synthesizing XO(s) ABOVE the finishing agent —
resolved by the INVERSE membership traversal (the XOs of the channels that list the finishing agent as
a member, minus self), NOT by a channel-id lookup; a finishing agent that is a member of SEVERAL
channels marks EACH parent XO owed — tracked in a per-synthesizing-agent owed-set. The detector
SHALL fire `WakeSynthesis` for that synthesizing agent on a digest sub-cadence (debounce-up): a burst
of boat finishes SHALL coalesce into at most one synthesis wake per the digest cadence per synthesizing
agent, and a fleet with no synthesis owed SHALL fire no synthesis wake (an idle fleet costs nothing).
The `WakeSynthesis` side-effect SHALL be performed OUTSIDE the detector's state mutex (like every other
wake), so the synthesis enqueue can never stall the tick loop.

#### Scenario: WakeSynthesis targets the synthesizing agent, not only the primary XO

- **WHEN** a Tier-2 synthesis is owed for a project XO that is not the daemon's primary clock XO
- **THEN** the wake is enqueued to that project XO (via the agent-carrying wake seam), not to the daemon's primary XO

#### Scenario: A burst of boat finishes coalesces into one synthesis wake

- **WHEN** several boats finish turns within one digest sub-cadence window
- **THEN** the detector fires at most one WakeSynthesis for their XO that window (debounce-up), not one per finish

#### Scenario: An idle fleet fires no synthesis wake

- **WHEN** no boat-finish event has marked synthesis owed
- **THEN** no WakeSynthesis is fired (zero idle cost)

#### Scenario: Self-scheduling is not relied upon

- **WHEN** the fleet is idle and a prior synthesis "scheduled itself" for a later tick
- **THEN** cadence does not depend on that self-schedule (which the idle-wake suppression and context rotation would defeat); the daemon owns the cadence via WakeSynthesis

### Requirement: The synthesis materiality gate is durable, daemon/disk-owned, surviving context rotation

The system SHALL gate synthesis on MATERIALITY — it SHALL synthesize only when a subordinate's state
has CHANGED since the last synthesis, so an active-but-unchanged fleet does not re-post an identical
rollup and an idle fleet costs nothing. Because the transcript read is stateless (the latest turn per
subordinate, resolved fresh each wake), the materiality state SHALL be a per-synthesizing-agent
durable "last-seen" snapshot (a hash of each subordinate's last-synthesized turn text). This last-seen
state SHALL be a DISK SIDECAR (NOT skill-context state, NOT in-memory-only detector state), surviving
BOTH context rotation (`/clear` wipes skill-context state) AND daemon restart (an in-memory-only
snapshot would re-post every subordinate as "new" on the first post-restart wake — a restart-storm). A
missing or corrupt sidecar SHALL fail SAFE toward "all changed" (synthesize once), never toward
silent-never-fire. A subordinate that is UNREADABLE on a given wake (its pane will not resolve) SHALL
be EXCLUDED from the materiality computation for that wake (never hashed as empty), so a transient read
failure neither spams a re-post nor suppresses a later real change. No separate ledger watermark is
required (there is no append log under transcript-first); the durable last-seen snapshot plus the
latest-state read together form the materiality mechanism.

#### Scenario: Unchanged subordinate state yields no re-post

- **WHEN** a synthesis wake fires but no subordinate's latest state has changed since the last synthesis (the durable last-seen snapshot matches)
- **THEN** the agent does not re-post an identical rollup; it replies idle and the snapshot is unchanged

#### Scenario: The materiality state survives a context rotation

- **WHEN** the synthesizing agent's context is rotated (`/clear`) between synthesis wakes
- **THEN** the durable daemon/disk-owned last-seen snapshot persists across the rotation, so the next synthesis does not re-read from scratch and re-post an unchanged rollup

#### Scenario: The materiality state survives a daemon restart

- **WHEN** the daemon restarts and no subordinate's latest state has changed since the last synthesis
- **THEN** the disk-sidecar last-seen snapshot persists across the restart, so no WakeSynthesis fires (no restart-storm of re-posts)

#### Scenario: A transiently-unreadable subordinate does not flap the wake

- **WHEN** a subordinate's pane cannot be resolved on a given wake
- **THEN** that subordinate is excluded from the materiality hash for that wake (not recorded as changed-to-empty), so its transient unreadability neither triggers a re-post nor suppresses a later real change

### Requirement: The synthesis skill ships as a heartbeat-skill constitutional member

The system SHALL deliver the visibility-synthesis skill as a member of the installable constitutional
set via a NEW delivery `Mechanism` value (`heartbeat-skill`), extending the set's mechanism vocabulary
(today `identity-append` only) per its extensibility seam. A `heartbeat-skill` member SHALL be
delivered as a WHOLE-FILE skill written into the agent's WORKSPACE at a member-declared
workspace-relative path (NOT appended into the agent's standing identity file), because synthesis is a
tick-time discipline invoked when the daemon emits `WakeSynthesis`, not a structural identity rule. The
member registry entry SHALL carry that workspace-relative target path. Because writing a whole-file
member into the workspace requires the WORKSPACE directory (the install today receives only the
identity-file path), the install entry point SHALL take a WORKSPACE-DIRECTORY parameter and DERIVE the
identity-file path from it (one source of truth for the workspace layout), updated at every call site
in the same change. The whole-file member's install idempotency SHALL be STAT-based (a missing skill
file is CREATED via its OWN file write, disjoint from the identity-append content write-back; an
existing one is KEPT so operator edits survive), distinct from the identity-append marker guard (the
marker-fenced append path SHALL NOT be used for a whole-file member, which carries no marker). Adding this member SHALL require no change to the install/seed iteration loop
(which dispatches by mechanism); the new mechanism's whole-file write/load dispatch arm AND the install
signature change SHALL land in the same change as the member (the mechanism-coupling contract).

#### Scenario: The synthesis skill installs as a whole-file workspace member

- **WHEN** the constitutional set is installed for a synthesizing agent
- **THEN** the visibility-synthesis skill file is written into the agent's workspace at its declared relative path (created if absent, kept if present), and the identity-append members are unaffected

#### Scenario: Re-installing keeps an operator-edited synthesis skill

- **WHEN** the operator has edited the installed synthesis skill file and the set is re-installed
- **THEN** the existing skill file is kept unchanged (stat-based kept) and reported as kept

#### Scenario: A whole-file member does not route through the identity-append marker guard

- **WHEN** a heartbeat-skill (whole-file) member is installed
- **THEN** its idempotency is decided by a stat of the target file, not by an identity-file marker fence (which would error on an empty marker), and the identity-append members install via their own marker-guarded arm unchanged

### Requirement: Tier 2 produces a curated domain rollup; Tier 3 produces a command-and-control headline

The system SHALL produce, for a Tier-2 synthesis (an XO into its own channel), a curated, compressed
domain rollup of its boats' material STATE since the last synthesis — grouped by boat, surfacing what
needs the operator's attention, not a raw firehose. The system SHALL produce, for a Tier-3 synthesis
(the meta-XO into the command-and-control channel), a fleet headline PLUS the open operator decisions
PLUS drill-down pointers that follow the inverse of the membership graph (command channel → XO channel
→ boat channel → pane), so a reader can plumb to any depth. The operator-decisions SHALL be derived
from the subordinates' FULL latest turn text (transcript-first), not from a lossy gist; decision
extraction is best-effort over each subordinate's CURRENT state — a decision raised then superseded
WITHIN a burst can age out of the latest-turn window, and complete capture across a burst is the
deferred finish-history ledger's job (issue #138), not a guarantee of this substrate. Each synthesis
SHALL be governed by the narrow-answer discipline: when nothing material has changed since the last
synthesis, the agent SHALL reply idle rather than manufacture a synthesis.

#### Scenario: A Tier-2 synthesis is a curated rollup, not a firehose

- **WHEN** an XO runs a Tier-2 synthesis
- **THEN** it posts a compressed domain rollup grouped by boat with attention-worthy items surfaced, not every raw boat turn

#### Scenario: A Tier-3 synthesis carries headline, decisions, and drill-down pointers

- **WHEN** the meta-XO runs a Tier-3 synthesis
- **THEN** the #c2 post carries a fleet headline, the open operator decisions (derived from the project-XOs' full latest turn text), and drill-down pointers down the membership graph to each XO channel

#### Scenario: Nothing material yields no manufactured synthesis

- **WHEN** no subordinate's state has changed since the last synthesis
- **THEN** the agent replies idle rather than posting a manufactured synthesis

### Requirement: Vertical synthesis is orthogonal to the horizontal chief-of-staff ledger

The system SHALL keep visibility synthesis (a VERTICAL activity/state-rollup up the hierarchy)
orthogonal to the chief-of-staff who-knows-what ledger (a HORIZONTAL view of operator↔XO exchanges
across channels). The two SHALL be independent heartbeat steps and SHALL NOT share a substrate: the
chief-of-staff ledger records operator↔XO message exchanges in an append ledger; visibility synthesis
reads subordinates' latest transcript state directly and writes no ledger. Neither SHALL gate or depend
on the other.

#### Scenario: The two synthesis axes do not share substrate

- **WHEN** both the chief-of-staff mirror and visibility synthesis are active
- **THEN** the chief-of-staff ledger and visibility synthesis use separate mechanisms (an append ledger vs a direct transcript read) and run as independent heartbeat steps, neither gating the other
