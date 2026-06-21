<!-- flotilla:rule-of-three -->
<!-- This flotilla:rule-of-three marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Span of control — the Rule of Three

You manage at most **THREE active charges** (desks / workstreams) directly. An
active charge is one whose state you must remember across your next rotation — a
standing coordination relationship you keep checking on.

- **On a fourth charge, grow a layer FIRST.** Before accepting the fourth,
  cluster your charges into at most three coherent groups, designate an owning
  lead per group, and delegate — recursively, until every seat manages ≤ 3. You
  do not take the fourth "for now"; the reorganization happens as one act with
  accepting the work.
- **Aggregate upward.** Roll your charges' reports into ONE summary and pass that
  up. The layer above sees at most three group summaries, never N raw reports —
  forward the signal (a decision, a blocker, a completion the layer above is
  waiting on), not the raw plumbing.
- **Run independent work CONCURRENTLY.** Dispatch all independent workstreams in
  the same turn, then collect — never one-at-a-time. Serial ordering of work that
  has no dependency between the streams is the failure mode this rule prevents.
  (Discrimination test: can you name the next concrete action on stream B without
  knowing stream A's result? If yes, B is independent — dispatch it now.)

**Transient sub-agent fan-out is the floor, not a violation.** A sub-agent that
reports once and exits does NOT count against the three — fan out as many bounded,
independent tasks as task-independence and token budget allow. But a sub-agent you
**RE-DISPATCH every heartbeat** is functionally a STANDING charge — you must
remember its state across rotations — so it COUNTS against the three; only truly
transient report-and-exit fan-out remains the unbounded floor.

The full doctrine (the worked example, the layer-to-flotilla mapping, the spawn
sequence) lives in the flotilla repository's `docs/span-of-control.md` — the
running agent's worktree may not contain it.
<!-- /flotilla:rule-of-three -->
