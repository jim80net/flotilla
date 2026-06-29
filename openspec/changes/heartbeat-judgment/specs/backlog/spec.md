# backlog Specification (delta)

## MODIFIED Requirements

### Requirement: The fleet backlog is a documented status-marker contract

The system SHALL define the fleet backlog as a markdown file with a documented item-line
contract, so the goal-driven loop and the per-recipient heartbeat judgment can deterministically
classify each item. A backlog item SHALL be a list line carrying a leading bracketed status marker
`- [<status>] <text>`, where `<status>` is one of `in-flight`, `next`, `blocked`, `needs-attention`,
`awaiting-auth`, or `done` (matched case-insensitively for the marker word). The authorizations-ledger
marker SHALL be the EXACT token `awaiting-auth` (case-insensitive on the word, but the spelling is
fixed): a desk writing a near-miss such as `[awaiting-authorization]` or `[awaiting auth]` produces an
UNRECOGNIZED marker, which — by the fail-safe contract below — is flagged `Malformed` AND treated as
actionable (the item warrants a heartbeat forever and never settles). To prevent that silent failure,
(a) the documented marker token here is authoritative and (b) the desk-continuation prompt SHALL QUOTE
the exact `[awaiting-auth]` string the parser accepts (see the watch spec's desk-continuation
contract). `in-flight` and `next`
items SHALL be classified UNBLOCKED (actionable). `blocked` and `needs-attention` items SHALL be
classified operator-blocked (the OPEN-QUESTIONS ledger: blocked-and-tracked work). `awaiting-auth`
items SHALL be classified awaiting-authorization (the AUTHORIZATIONS ledger: work pending an operator
go/no-go or spend decision) — a class DISTINCT from operator-blocked so that work waiting on an
authorization is not conflated with work blocked on a question or dependency. `done` items SHALL be
excluded. Both operator-blocked and awaiting-authorization items SHALL be settle-neutral: neither is
actionable, so neither enters the unblocked set (neither warrants a heartbeat). The convention SHALL be
documented both in the specification and in a header block of the backlog file itself.

#### Scenario: A status marker classifies an item
- **WHEN** the backlog contains `- [in-flight] ship the tactical PR` and `- [blocked] PR-E loss-cap values @operator`
- **THEN** the first is classified unblocked and the second operator-blocked

#### Scenario: An awaiting-authorization item is its own class, not operator-blocked
- **WHEN** the backlog contains `- [awaiting-auth] flip the metered feed on @operator`
- **THEN** it is classified awaiting-authorization (the authorizations ledger), counted separately from
  operator-blocked, and is NOT actionable (it does not enter the unblocked set)

#### Scenario: A done item is excluded
- **WHEN** a backlog item is marked `- [done] inbound-relay fix`
- **THEN** it is not counted as unblocked, operator-blocked, or awaiting-authorization

## ADDED Requirements

### Requirement: The parsed status exposes the awaiting-authorization count

The backlog parser's `Status` SHALL expose the count of awaiting-authorization (`[awaiting-auth]`)
items as a field distinct from the operator-blocked count, so the authorizations ledger is separately
observable (for surfacing, for the heartbeat judgment, and for tests) and a deployment can distinguish
"blocked on a question" from "awaiting an authorization". Adding the field SHALL be additive: a backlog
with no `[awaiting-auth]` items SHALL parse identically to before this change (the new count is zero and
no other classification changes).

#### Scenario: Awaiting-authorization items are counted separately from blocked items
- **WHEN** the backlog contains one `[blocked]` item and two `[awaiting-auth]` items
- **THEN** the parsed status reports an operator-blocked count of one and an awaiting-authorization count
  of two — the two ledgers are independently observable

#### Scenario: A backlog with no awaiting-auth items is unchanged
- **WHEN** a backlog contains only `[in-flight]`, `[next]`, `[blocked]`, and `[done]` items
- **THEN** the parsed status is identical to the pre-change classification and the awaiting-authorization
  count is zero
