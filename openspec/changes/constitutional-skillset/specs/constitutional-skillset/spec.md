# constitutional-skillset Specification

## Purpose

A newcomer who drops flotilla into their own project and Discord guild must get flotilla's default
fleet behaviors — not just its message plumbing. This capability ships flotilla's constitutional
doctrine as an INSTALLABLE set: a versioned, binary-embedded collection of members that a single
idempotent command drops into a fleet (and that `workspace init` seeds by default), so every
coordinating agent is born with the operating doctrine that makes a fleet legible. v1 ships exactly
two members — the Rule of Three (span of control) and the up-hierarchy visibility synthesis (Tiers
2 and 3, built on the Tier-1 mirror stream) — and leaves a clean seam for the operator to add more.

## ADDED Requirements

### Requirement: An installable, binary-embedded constitutional doctrine set

The system SHALL ship its default constitutional doctrine as a versioned set of members embedded in
the `flotilla` binary, installable into a per-agent workspace by a single command WITHOUT the
operator writing a hook, a script, or hand-copying prose. Each member SHALL declare its name, its
target file within the workspace, its delivery mechanism (a STRUCTURAL rule loaded once into the
agent's standing identity, or a TICK-TIME discipline delivered as a heartbeat skill), and its
content. The install SHALL be idempotent with kept/created semantics: a member already present in
the target workspace SHALL be KEPT (never overwritten — the operator may have edited it), a missing
member SHALL be CREATED, and each decision SHALL be reported. The set SHALL be embedded so the
binary remains self-contained (no external asset path to configure).

#### Scenario: Installing the constitutional set into a fresh workspace

- **WHEN** the operator runs the doctrine-install command against an agent whose workspace has none of the constitutional members
- **THEN** every member is written to its target file in that workspace and each is reported as created

#### Scenario: Re-installing never overwrites an operator's edits

- **WHEN** the doctrine-install command runs against a workspace whose members already exist (possibly operator-edited)
- **THEN** every existing member is kept unchanged and reported as kept, and only genuinely-missing members are created

### Requirement: workspace init seeds the constitutional set by default

The system SHALL seed the constitutional set into a workspace by default as part of scaffolding it,
so a freshly initialized workspace is born with the doctrine already in place rather than as a bare
identity placeholder. The seeding SHALL obey the same kept/created discipline as a direct install:
it SHALL NOT overwrite any file the base scaffold or a prior run created.

#### Scenario: A scaffolded workspace is born with doctrine

- **WHEN** the operator initializes a new agent workspace
- **THEN** the base scaffold files AND the constitutional members are present, and re-running the initialization keeps every file unchanged

### Requirement: The structural rule loads once into the agent's standing identity

The system SHALL deliver a STRUCTURAL member (one that defines the agent's standing organization,
such as the span-of-control rule) by writing its distilled instruction into the agent's native
identity file, so it loads once at launch into the agent's system prompt rather than being re-typed
on every heartbeat. The installer SHALL append the distilled rule to the identity file rather than
clobbering the agent's own identity content. Documentation-only distribution and re-typing a
structural rule into every heartbeat SHALL NOT be the primary home for such a rule; the canonical
doctrine document remains the source of truth the distilled rule is derived from.

#### Scenario: The span-of-control rule reaches the standing identity

- **WHEN** a structural member is installed into an agent's workspace
- **THEN** its distilled instruction is appended to that agent's identity file (without clobbering the agent's own identity), so it loads once at launch

### Requirement: The Rule of Three span-of-control doctrine ships as a member

The system SHALL include, as a constitutional member, the Rule of Three: no coordinating seat
manages more than three active charges, and the arrival of a fourth charge forces the creation of
an intermediate lead and a re-clustering, recursively, until every seat manages at most three. The
member SHALL also carry the upward-aggregation discipline (each lead rolls its charges' reports
into one summary upward) and the parallel-not-serial discipline (independent workstreams are
dispatched concurrently, never one-at-a-time). The full doctrine SHALL exist as a documentation
page (the source of truth), and the distilled standing-instruction form SHALL be the installed
member.

#### Scenario: A coordinating agent receives the span-of-control discipline

- **WHEN** the constitutional set is installed for a coordinating agent
- **THEN** the agent's standing identity carries the ≤3-active-charges rule, the fourth-charge-forces-a-layer mechanic, upward aggregation, and parallel dispatch

### Requirement: The visibility-synthesis member curates the tier below up to the agent's own channel

The system SHALL include, as a constitutional member, a heartbeat-delivered visibility-synthesis
skill that an Executive Officer agent runs to synthesize the tier below it UP to its own channel: a
project-level XO curates its boats' activity up to its project channel (Tier 2), and the meta-XO
curates the project channels up to the fleet-command channel (Tier 3). The synthesis SHALL read the
Tier-1 mechanical mirror stream (the per-desk channel posts) as its primary substrate, so it
inherits Tier 1's extraction and keeps the tiers stratified. Its output contract SHALL be: a Tier-2
post is a curated domain view of the agent's boats; a Tier-3 post is a fleet headline plus the
operator-decision items plus drill-down pointers down the hierarchy (fleet-command → project
channel → boat channel → pane). The cadence SHALL be heartbeat-driven debounce-up — a new mirror
post marks a synthesis as owed, and the next quiet or continuation tick flushes one curated post —
so an idle fleet incurs no synthesis cost.

#### Scenario: An XO synthesizes its boats up to its channel on the heartbeat

- **WHEN** the agent's boats have produced new mirror posts since the last synthesis and a heartbeat tick arrives
- **THEN** the agent posts one curated rollup of the tier below to its own channel, debounced so an idle fleet produces nothing

#### Scenario: The meta-XO rollup carries drill-down pointers

- **WHEN** the meta-XO synthesizes the project channels up to the fleet-command channel
- **THEN** its post is a fleet headline plus operator-decision items plus pointers down the chain (fleet-command → project channel → boat channel → pane)

### Requirement: Synthesis routing is derived from the membership graph and is acyclic

The synthesis read/post routing SHALL be derived from the existing channel membership graph as the
transpose of the command graph — command flows down the graph, awareness flows up it — with NO new
roster schema. The READ set for an agent SHALL be the channels where that agent is a member or is
the channel's Executive Officer (the tier below it); the POST target SHALL be the channel that agent
owns. The membership graph SHALL be asserted to be a directed acyclic graph at roster load,
fail-closed with the cycle named otherwise; reads-below-and-posts-own-level over an acyclic graph
guarantees synthesis can never loop. Synthesis reading of the (webhook-authored) mirror stream SHALL
be a distinct, read-only intent that never treats a mirror post as an inbound command, so it does
not conflict with the relay's command-feedback guard.

#### Scenario: An agent reads the tier below and posts to its own channel

- **WHEN** an agent runs visibility synthesis
- **THEN** it reads the channels it is a member or Executive Officer of (the tier below) and posts the rollup to the channel it owns

#### Scenario: A cyclic membership graph is rejected at load

- **WHEN** the roster's membership graph contains a cycle
- **THEN** the roster fails to load with the cycle named, so synthesis can never loop

### Requirement: The constitutional set is extensible without enumerating its future contents

The constitutional set SHALL be a member registry such that adding a member is adding a registry
entry plus its embedded asset, with no change to the install or seed logic (the install/seed loop is
member-count-agnostic). v1 SHALL register exactly two members (the Rule of Three and the
visibility-synthesis skill). The set SHALL NOT pre-enumerate or hardcode a broader corpus; which
further behaviors join the default set is an operator decision applied incrementally through the
same seam.

#### Scenario: Adding a member requires no install-logic change

- **WHEN** a new member is added to the registry with its embedded asset
- **THEN** the install and seed paths distribute it with no change to their logic (they iterate the registry, not a fixed list)

#### Scenario: v1 ships exactly two members

- **WHEN** the constitutional set is enumerated in v1
- **THEN** it contains exactly the Rule of Three and the visibility-synthesis members, with the seam left open for the operator to add more
