## Why

Recycle handoffs carry deployment-specific content (host paths, channel ids, internal state).
The prior model force-committed them (`git add -f`) so the durability gate could detect the blob
at HEAD, then instructed the takeover turn to `git rm` them off the branch (#212 transient
handoff). That still left the handoff in **branch commit history** — a leak when the repo is
public and feature branches PR to `main`. With flotilla now public, this is urgent (#218).

## What Changes

- **Filesystem durability gate:** `HandoffDurable` / `HandoffAbsentAtHead` detect the handoff
  via `os.Stat` (regular file, minimum size) — not `git ls-tree` / HEAD.
- **Untracked handoff turns:** claude + grok `HandoffTurn` write to the designated gitignored
  path only; they explicitly forbid `git add` / `git commit`.
- **Disk cleanup on takeover:** `TakeoverTurn` instructs read → `rm -f` → work (not `git rm`).
- **Command copy:** recycle + switch error messages and runbook docs say absent→present on disk,
  not absent→committed at HEAD. Recycle no longer requires a git work-tree for the handoff gate.

## Non-Goals

- Scrubbing handoff blobs already in existing branch histories (handled per-branch at PR time).
- Command-side file removal after takeover (still desk-driven; same best-effort posture as #212).
- Changing handoff path conventions (`.claude/handoffs/` vs `.flotilla/handoffs/`).

## Supersedes

Partially supersedes `openspec/changes/recycle-transient-handoff` (the commit-then-rm model).
The transient-handoff change addressed net-diff leaks under squash-merge; #218 removes version
control from the transfer path entirely.