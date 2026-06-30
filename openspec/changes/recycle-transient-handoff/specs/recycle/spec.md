# recycle Specification (delta)

## ADDED Requirements

### Requirement: The transferred handoff is transient — the takeover removes it so it cannot leak on a public PR

The recycle takeover turn SHALL make the transferred handoff TRANSIENT: after the fresh session reads
the handoff (so it holds the content), its FIRST action SHALL be to remove the handoff from version
control (`git rm` the recycle-designated path + a path-scoped commit), so the handoff is committed ONLY
to durably transfer it across the recycle and is gone before the branch opens any feature PR. The
handoff is gitignored because it carries deployment-specific context (host paths, channel ids, internal
state); leaving it committed on a branch that later PRs to a public `main` is a partition leak the
harness itself injects (#212). The takeover instruction SHALL order the steps READ → REMOVE → WORK (the
removal MUST follow the read, never precede it, so the fresh session never deletes the handoff before
ingesting it). This SHALL NOT weaken the handoff DURABILITY gate: the handoff is still written and
committed by the handoff turn, and the close is still gated on the committed blob — only the takeover's
removal step is added, so the worst case remains a no-op recycle, never a lost handoff. Both
recycle-capable surfaces (claude and grok) SHALL carry the read-then-remove takeover instruction.

#### Scenario: The takeover removes the handoff after reading it

- **WHEN** a freshly-relaunched session runs its recycle takeover turn
- **THEN** it first reads the recycle-designated handoff, then as its first action removes that handoff
  from version control (a path-scoped `git rm` + commit), then begins the handoff's remaining work — so
  the handoff does not persist on the branch

#### Scenario: A squash-merged recycled branch carries no handoff

- **WHEN** a branch that was recycled later opens a PR to public `main` and is squash-merged (the
  project's merge policy)
- **THEN** the transferred handoff is not present in the PR's net diff and not in `main`'s history (the
  takeover removed it; the squash collapses the handoff-turn add and the takeover remove to nothing), so
  no deployment specific leaks through the recycle handoff. (NOTE: this closes the NET-DIFF leak; the
  blob still exists in the pre-squash branch commit list and would persist in history under a
  NON-squash merge — hence the squash-merge policy is load-bearing for full closure, and a desk that
  crashes mid-takeover before the removal leaves the handoff committed until the next cleanup.)

#### Scenario: The durability gate is unchanged

- **WHEN** a recycle runs after this change
- **THEN** the handoff is still written and committed before the close, the close is still gated on the
  committed blob at HEAD, and the only added behavior is the takeover's read-then-remove step — the
  worst case remains a no-op recycle, never a lost handoff
