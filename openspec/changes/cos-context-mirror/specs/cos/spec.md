# cos Specification (delta: new capability)

> Adds the `cos` (chief-of-staff) context-integration capability: a per-channel
> mirror of operator↔XO traffic to a configured `cos_agent`, and an automated
> who-knows-what context ledger. It builds on the `federation-channels` (#105) seams
> (`watch.Job.OriginChannel` + the reserved+validated `cos_agent`) and is observe-only
> — it changes no relay authorization rule.

## ADDED Requirements

### Requirement: Operator↔XO traffic is mirrored to the chief of staff

The system SHALL mirror operator↔XO traffic, across every per-XO channel, to a
configured chief-of-staff agent (`cos_agent`), so the CoS catches side-conversations
it was not a direct party to. Both directions SHALL be captured: an operator message
routed to an XO (inbound, via the relay's per-channel routing) and an XO's reply to
the operator (outbound, via `flotilla notify`). Each mirrored record SHALL carry the
origin channel, the from/to identities, and a timestamp, so the CoS can tell which
side-conversation each exchange belongs to.

#### Scenario: An inbound operator message is mirrored with its channel
- **WHEN** the operator posts in `#fleet-alpha` and the relay delivers it to `alpha-xo`
- **THEN** a CoS context record is written tagged `operator → alpha-xo` on `#fleet-alpha`

#### Scenario: An XO reply to the operator is mirrored
- **WHEN** `alpha-xo` replies to the operator via `flotilla notify`
- **THEN** a CoS context record is written tagged `alpha-xo → operator` on alpha's channel

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
- **WHEN** an operator↔XO exchange is mirrored
- **THEN** a structured entry (ts · channel · from → to · gist) is appended to the ledger with no LLM call

#### Scenario: CoS curation does not collide with flotilla's appends
- **WHEN** the CoS agent writes its integrated who-knows-what view
- **THEN** it writes a region distinct from flotilla's deterministic append region

### Requirement: The mirror is observe-only and grants no new authority

The mirror SHALL be strictly observe-only: it records traffic the relay and `notify`
already handle and SHALL NOT grant the chief of staff any delivery or command path to
desks, and SHALL NOT modify the relay's operator-only authorization or its
self-mirror webhook-drop guard. Reading mirrored context is not authority to act on
other agents' panes.

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
