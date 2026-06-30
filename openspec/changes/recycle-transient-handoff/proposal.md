# Proposal — recycle leaves a transient handoff (no partition leak on a public PR)

## Why

`flotilla recycle` transfers a desk's context across a chapter close by having the desk write a handoff
and **force-commit it to the current branch** (`git add -f`, past the gitignored `.claude/handoffs/` —
`internal/surface/claude.go` `HandoffTurn`, grok's in `grok.go`). The commit exists for DURABILITY: the
recycle close is gated on the handoff blob landing at HEAD (`openspec/specs/recycle/spec.md`, the
"durably-confirmed handoff" requirement), so a crash between write and relaunch cannot lose it.

But the handoff is gitignored precisely because it carries **deployment specifics** (host paths, channel
ids, internal state). Left committed, it sits on the branch — and when that branch later opens a PR to
public `main`, the handoff **leaks** (#212). This is a mechanical leak vector in the harness itself: it
injects the leak regardless of how carefully the desk writes. It was caught when the reader-modeling
branch carried a recycle handoff that the boundary guard's denylist did not catch.

## What changes

The recycle **takeover** turn makes the handoff **transient**: after reading it (so the fresh session
has the content), the fresh session's FIRST action is to remove the handoff from version control —
`git rm "<path>" && git commit -m "chore(recycle): drop transferred handoff" -- "<path>"`. The handoff is
thus committed ONLY to durably transfer it across the recycle, and is gone before any feature PR (a
squash-merge — the project's merge policy — collapses the add+remove to nothing). Read → remove → work.

This preserves the durability gate (the handoff is still committed-before-close, still durably
transferred) while closing the leak. It applies to both recycle-capable drivers (claude + grok), whose
`TakeoverTurn` carry the same read-then-remove instruction.

## Impact

- **Affected specs:** `recycle` (ADDED) — a "transient handoff" requirement: the takeover removes the
  transferred handoff after reading it, so a recycle never leaves a gitignored, deployment-specific
  handoff committed on a branch that reaches a public PR.
- **Affected code:** `internal/surface/claude.go` + `internal/surface/grok.go` (`TakeoverTurn` gains the
  read-then-`git rm` step); `internal/surface/recycle.go` (the `RecycleBridge.TakeoverTurn` doc);
  `internal/surface/recycle_test.go` (assert the removal step + read-before-remove ordering).
- **No change** to the handoff DURABILITY model: `HandoffTurn` still writes + force-commits, and the
  close is still gated on the committed blob. The only addition is the takeover's removal step.

## Not in

- Changing the durability model to avoid committing at all (the "disk file persists in the same
  worktree" option in #212) — that touches the durability gate and is a larger change; the transient
  removal is the smaller fix that keeps the gate intact.
- Scrubbing handoff blobs from EXISTING branch histories (the reader-modeling branch already had its
  handoff untracked before its PR; other branches are handled at their own PR time).
