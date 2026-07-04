# cos Specification

## Purpose
Per-XO channels (the `federation` capability) fragment the operator's coordination
across N channels, so no single agent sees the union of who-was-told-what. The `cos`
capability gives one configured chief-of-staff agent (`cos_agent`) that union: a
deterministic, observe-only mirror of the fleet's coordination relay traffic into a
durable, append-structured "who-knows-what" context ledger the CoS reads and integrates.
The recorded scope is operator↔agent traffic (inbound relay + `notify`) AND inter-agent
`send` relays — every agent, XO and desk alike (the original operator↔XO-only scope was
widened by #349 so a desk's own coordination thread is populated, not just the XOs'). The
substrate is written with NO large-language-model call (reliable, auditable, cheap); the
CoS's integrated view is curation layered on top, kept in its own region. The capability
is inert unless `cos_agent` is set, and it grants the CoS no delivery path or relay-auth
change — it only records traffic the relay, `notify`, and `send` already handle.

## Requirements
### Requirement: Coordination relay traffic is mirrored to the chief of staff

The system SHALL mirror the fleet's coordination relay traffic, across every channel, to a
configured chief-of-staff agent (`cos_agent`), so the CoS catches side-conversations it was
not a direct party to AND each desk's own thread is populated. Three paths SHALL be
captured, for every agent (XO and desk alike): an operator message routed to an agent
(inbound, via the relay's per-channel routing); an agent's reply to the operator (outbound,
via `flotilla notify`); and an inter-agent relay (via `flotilla send`, recorded on confirmed
delivery). Each mirrored record SHALL carry a channel, the from/to identities, and a
timestamp, so the CoS — and the recipient's dash thread — can tell which side-conversation
each exchange belongs to. (The scope was operator↔XO-only before #349; it was widened so a
desk's coordination thread is no longer empty.)

#### Scenario: An inbound operator message is mirrored with its channel
- **WHEN** the operator posts in `#fleet-alpha` and the relay delivers it to `alpha-xo`
- **THEN** a CoS context record is written tagged `operator → alpha-xo` on `#fleet-alpha`

#### Scenario: An inbound operator message to a DESK is mirrored
- **WHEN** the operator's message is relayed to a desk (`alpha-be`)
- **THEN** a CoS context record is written tagged `operator → alpha-be`, so it appears in the desk's own thread

#### Scenario: An agent reply to the operator is mirrored
- **WHEN** any agent (XO or desk) replies to the operator via `flotilla notify`
- **THEN** a CoS context record is written tagged `<agent> → operator` on that agent's channel

#### Scenario: An inter-agent send is mirrored
- **WHEN** `flotilla send` delivers a confirmed relay from one agent to another (e.g. the CoS to a desk)
- **THEN** a CoS context record is written tagged `<from> → <to>` on the recipient's channel

### Requirement: The who-knows-what ledger is a deterministic substrate

The system SHALL maintain a durable, append-structured context ledger written
deterministically (NO large-language-model call in the write path) — one entry per
exchange with structured fields (timestamp, channel, from, to, gist). The ledger is
the productized form of the operationally hand-kept who-knows-what record. The CoS
agent's *integrated* view (summaries, the who-knows-what matrix) SHALL be kept
separate from this deterministic append region, so flotilla's appends never collide
with the CoS's curation and a context-hash wake signal never self-triggers on the
CoS's own writes.

#### Scenario: The ledger append is deterministic and structured
- **WHEN** any coordination exchange (operator↔agent or inter-agent `send`) is mirrored
- **THEN** a structured entry (ts · channel · from → to · gist) is appended to the ledger with no LLM call

#### Scenario: CoS curation does not collide with flotilla's appends
- **WHEN** the CoS agent writes its integrated who-knows-what view
- **THEN** it writes a region distinct from flotilla's deterministic append region

### Requirement: The mirror is observe-only and grants no new authority

The mirror SHALL be strictly observe-only: it records traffic the relay, `notify`, and
`send` already handle and SHALL NOT grant the chief of staff any delivery or command path
to desks, and SHALL NOT modify the relay's operator-only authorization or its
self-mirror webhook-drop guard. Reading mirrored context is not authority to act on
other agents' panes. (Recording a `send` relay does not grant the CoS a new send path —
`send` is an existing operator/agent capability; the mirror only observes its confirmed
deliveries.)

#### Scenario: Mirroring changes no relay security rule
- **WHEN** the CoS mirror is enabled
- **THEN** the relay still accepts only operator-authored messages, still drops self-mirror webhook posts, and the CoS gains no back-channel to inject into a desk

### Requirement: The chief of staff is a generalizable role, inert when unset

The chief of staff SHALL be a configurable role (`cos_agent`) naming an agent in the
roster, NOT any specific deployment's desk name, and the capability SHALL be inert
when `cos_agent` is unset (no mirror, no ledger — fully backward compatible). A
deployment's concrete CoS (e.g. an operations desk) and its operational ledger file
are instances of this role, not part of the product surface.

#### Scenario: Unset cos_agent is fully inert
- **WHEN** a roster does not set `cos_agent`
- **THEN** no mirroring occurs and no ledger is written, exactly as before this capability

#### Scenario: cos_agent names a roster agent
- **WHEN** `cos_agent` is set to a name not present in `agents[]`
- **THEN** roster load fails fail-closed (validation shared with the reserving change #105)

