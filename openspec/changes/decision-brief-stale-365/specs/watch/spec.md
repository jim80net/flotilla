# watch Specification (delta)

## ADDED Requirements

### Requirement: Decision-brief dispatch is fresh and debounced across restarts

The decision-brief detector SHALL NOT fire a node-level brief request when any
`work_items[].brief` on that goal is non-empty. Before enqueueing a brief-authoring
dispatch, the watch daemon SHALL re-read the compiled goals file and confirm the gap
is still open. Confirmed-delivery gap keys SHALL persist to
`<roster-dir>/flotilla-decision-brief-claims.json` so a watch restart does not
re-dispatch for gaps already delivered. In-memory pending claims SHALL be released
when the injector busy-drops a detector job so a legitimate brief request is never
suppressed forever without confirmed delivery.

#### Scenario: Work-item brief satisfies a gated node
- **WHEN** a goal's roll-up is operator-gated but a work item already carries a `brief`
- **THEN** no decision-brief dispatch fires for that goal

#### Scenario: Brief lands between scan and dispatch
- **WHEN** the initial tick scan finds a gap but a fresh read before enqueue shows a brief
- **THEN** the dispatch is skipped with a stale-skip log line

#### Scenario: Claims survive watch restart
- **WHEN** watch restarts after confirming delivery for a gap that is still open
- **THEN** the gap is not re-dispatched until it clears and re-arms via Reconcile

#### Scenario: Busy drop does not durable-suppress
- **WHEN** a decision-brief detector job is busy-dropped before confirmed delivery
- **THEN** the in-memory claim is released and a later tick may dispatch again