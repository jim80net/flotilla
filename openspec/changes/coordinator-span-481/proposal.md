# coordinator-span-481

## Problem

#464's `hasSpanOfControl` treated any non-self channel member as subordinate span. The
standard desk binding shape lists the supervisor coordinator as a member (observation/relay),
so execution desks were misclassified as coordinators and the delegation nudge misfired (#481).

## Fix (rank-aware span)

Span is conferred by either shape:
1. **Coordinator home** — `xo_agent=name` lists a non-XO member (classic subordinate listing).
2. **Desk home (supervisor-as-member)** — `name` appears on an execution desk's channel as observer.

Supervision-only members who are themselves XOs (cos, project-XOs) on a coordinator's home
channel do not confer span.

## Delegation nudge knob

`delegation_nudge: on|off` on roster; `FLOTILLA_DELEGATION_NUDGE` env overrides at watch
startup (mirrors `xo_rotate` / `FLOTILLA_XO_ROTATE`).