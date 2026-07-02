# Proposal — doctrine install refresh (#252)

## Problem

`flotilla doctrine install` keys idempotency on marker presence. When embedded
identity-append assets change in a later release, already-installed identity files
keep stale fenced blocks forever. Dogfood hit this on executive-mini-brief (#246).

## Change

- `flotilla doctrine install --refresh`: replace open→close fenced regions when
  content drifted (trailing-newline-tolerant compare; no-op when current).
- `flotilla doctrine install --all`: roster-wide refresh/install.
- `deploy/flotilla-doctrine-refresh.sh`: operator step 3 after binary rebuild +
  watch restart (never restarts the clock itself).
- `docs/watch-runbook.md`: documents the three-step deploy sequence.

Closes #252.