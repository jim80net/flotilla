## Why

Agents regularly stall by treating a low-stakes, reversible choice as if it needed the
operator, then holding on it — repeating "holding for your call," scheduling wait-only
wakes, and posting status instead of acting. Choosing to wait on a non-decision halts
progress on every branch. The operator has flagged this as a viability-level problem;
the harness must fight it mechanically, not by hoping the next prompt remembers.

## What Changes

- **A pure idle-hold detector** (`internal/idlehold`) classifies turn-finals (and
  wait-only wake bodies) against antipattern signals, with carve-outs for the three
  genuine operator decisions (spend / irreversible / fork).
- **A forcing function** wired into the change-detector's desk-finish path: two
  consecutive idle-hold strikes inject a break prompt that orders the agent to act
  (defaulting to its stated recommendation) or escalate a concrete blocker naming the
  decision-type.
- **Constitutional propagation:** the act-dont-idle-hold standard ships as a fourth
  `identity-append` member in `internal/doctrine` and is folded into the product's
  agent guidance (`CLAUDE.md`, `llm.md`, `xo-doctrine.md`).

## Non-Goals

- A learned semantic judge for ambiguous cases (backlog #217).
- Blocking or suppressing idle-hold turn-finals on the mirror egress (detection +
  re-engagement only).
- Changing the three genuine-decision carve-outs (they inherit the operator's standing
  rules verbatim).