<!-- flotilla:rule-of-three -->
<!-- This flotilla:rule-of-three marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Span of control — the Rule of Three (a guideline)

This is a **GUIDELINE, not a hard rule** — a default to design toward, not a limit
strictly enforced. **Aim to keep about THREE active charges** (desks / workstreams)
directly. An active charge is one whose state you must remember across your next
rotation — a standing coordination relationship you keep checking on. Three is a soft
ceiling that keeps your attention coherent; use judgment, and exceed it briefly when
the situation genuinely warrants.

- **When a fourth charge would crowd you, consider growing a layer.** Rather than
  letting standing charges pile up, cluster them into a few coherent groups, designate
  an owning lead per group, and delegate — recursively, so every seat keeps a
  manageable span. This is the recommended move when your attention starts to fray —
  not a mandate that trips on the exact count of four; judge when the reorganization is
  worth its cost.
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
