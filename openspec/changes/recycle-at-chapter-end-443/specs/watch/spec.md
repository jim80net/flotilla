## ADDED Requirements

### Requirement: Chapter-end finish edges may recycle the desk session

When a monitored desk finishes a turn and chapter-end detection reports lane-done
(backlog unblocked empty + settled/PR-merged/coordinator-mark turn-final), the watch
daemon SHALL either auto-dispatch `flotilla recycle <desk>` (default ON via
`FLOTILLA_CHAPTER_END_RECYCLE`) or inject a suggest nudge. Stacked mid-lane finishes
(unblocked items remain) SHALL NOT recycle. Coordinators SHALL use `recycle --self`.
An adjutant for the owning layer SHALL receive a chapter-end notice when configured.

#### Scenario: Desk backlog fully done and turn-final settles

- **WHEN** a non-approval-sensitive desk finishes with zero unblocked backlog items and a
  settled turn-final
- **THEN** watch dispatches chapter-end recycle (or a suggest nudge if auto is disabled)

#### Scenario: Mid-stack PR merge does not recycle

- **WHEN** the turn-final names a merged PR but the desk backlog still has `[in-flight]` items
- **THEN** watch does not recycle that desk (suppress: unblocked-items-remain)
