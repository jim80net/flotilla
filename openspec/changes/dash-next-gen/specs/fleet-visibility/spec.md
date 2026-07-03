# fleet-visibility Specification (delta)

## ADDED Requirements

### Requirement: Per-desk mirror fanout includes session-mirror ledger write

In addition to posting the info-level body to Discord, the per-desk turn-final mirror SHALL append
a session-mirror ledger entry for the same mirror event (unless suppressed by the firewall).
The ledger write SHALL occur in the same `deskMirror.run` invocation after `readerModelInternal`
and SHALL NOT require a second turn-final read.

#### Scenario: A finished desk mirrors to Discord and the session ledger

- **WHEN** a non-XO desk transitions Working→Idle with a substantive turn-final
- **THEN** the info body is posted to Discord and a session-mirror entry is appended under the roster dir

#### Scenario: Mirror fanout does not expand CoS ledger scope

- **WHEN** an execution desk session is mirrored
- **THEN** the CoS context ledger is not appended (desk session output remains distinct from operator↔XO traffic)