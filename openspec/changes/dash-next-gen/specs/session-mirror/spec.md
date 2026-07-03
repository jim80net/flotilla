# session-mirror Specification

## Purpose

Every agent session produces one canonical turn-final; operators consume that session on three
surfaces (tmux verbose, Discord info, dash info/debug) without per-message verbosity authoring.
This capability extends the shipped `deskMirror` / `readerModelInternal` pipeline — it does not
introduce a parallel publish transport.

## ADDED Requirements

### Requirement: Session mirror fanout derives all surface renderings from one turn-final

The system SHALL, on each mirrored turn completion (the existing `Working→Idle` finish edge for
non-XO desks and the coordinator finish hook for XO/coordinator agents), read exactly one canonical
turn-final via `readDeskTurnFinal` and derive all surface renderings through the existing
`readerModelInternal` pre-post pipeline (`cmd/flotilla/mirror.go`). The system SHALL NOT require
desks to author per-surface variants.

#### Scenario: One finish produces info and verbose renderings

- **WHEN** a non-XO desk completes a mirrored turn with an enveloped brief
- **THEN** the verbose rendering is the full turn-final text, the info rendering is
  `readermap.Render(envelope)`, and both derive from the same read without a second pane read

### Requirement: Per-surface verbosity is configuration, not authorship

Verbosity levels SHALL be fixed per surface: tmux at `verbose` (raw pane / full turn-final, no
filter), Discord at `info` (the existing mirror publish body), dash at `info` or `debug`
(selected by dash configuration). Desks SHALL NOT select a verbosity level at publish time.

#### Scenario: Dash debug shows diagnostics info omits

- **WHEN** dash mirror verbosity is `debug`
- **THEN** the dash renders envelope JSON and mirror decision notes in addition to the info body

#### Scenario: Dash info omits debug diagnostics

- **WHEN** dash mirror verbosity is `info` (default)
- **THEN** the dash renders only the info body, not raw verbose text or envelope JSON

### Requirement: Session mirror history is a watch-written append ledger

The system SHALL persist each non-suppressed mirror event to an append-only per-agent ledger under
the roster directory (`session-mirror/<agent>.jsonl`), written only by `flotilla watch`. Each
entry SHALL carry timestamp, agent, full `verbose` turn-final text (field name `verbose`, not a
hash), info body, debug record, and suppression flag. The dash SHALL read this ledger via pure
read-model builders (no pane probe).

#### Scenario: Dash conversation thread shows desk mirror history

- **WHEN** the operator selects an execution desk in the dash Conversations view
- **THEN** the thread includes session-mirror entries for that desk, not only CoS ledger lines

#### Scenario: Suppressed firewall refuses are not mirrored to dash

- **WHEN** `readerModelInternal` returns `suppress=true` for a private-firewall refuse
- **THEN** no session-mirror ledger entry is appended (consistent with Discord withhold)

### Requirement: Discord mirror behavior is unchanged at info level

The Discord post body for a mirrored turn SHALL remain the info-level body produced by
`readerModelInternal` (modeled render on envelope pass; raw turn-final on absent envelope).
Tri-surface mirroring SHALL NOT change Discord chunking or webhook identity.

#### Scenario: Discord post matches pre-tri-surface behavior

- **WHEN** a desk mirrors an enveloped turn-final
- **THEN** the Discord channel receives the same modeled body as before this capability

### Requirement: Session mirror retention is bounded

Per-agent session-mirror ledgers SHALL enforce a configurable maximum entry count (default 200) so
roster-dir storage does not grow without bound.

#### Scenario: Old entries roll off

- **WHEN** an agent exceeds the configured retention limit
- **THEN** the oldest entries are discarded on append