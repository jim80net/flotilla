## Why

Coordinators repeatedly ship operator-facing turn-finals as jargon-dense status dumps
(PR numbers, SHAs, gate vocabulary) that assume the operator has been watching move by
move. The Discord mirror posts turn-finals mechanically — so every turn-final IS an
operator communication and must read as an executive mini-brief in plain language.

## What Changes

- **Sixth constitutional member** `executive-mini-brief` (`identity-append`): the
  mechanical four-part format (bottom line → mini brief → detail footer → explicit
  needs-you line) installed into every agent's identity file via `doctrine install`.
- **Operating principles** concise constitution adds principle 12 cross-referencing
  operator turn-finals and the mirror egress path.
- **Mirror hook audit**: `deploy/flotilla-xo-discord-mirror.sh` logs when a posted
  turn-final lacks an explicit "Waiting on you" / "Nothing needs you" line (posts
  anyway — shape is doctrine-injected, not suppressed).
- **XO doctrine doc** section on operator communications and the mirror contract.

## Non-Goals

- Rewriting or reformatting mirror-posted text in the hook (doctrine shapes the
  coordinator's turn-final; the hook stays mechanical).
- Applying mini-brief format to desk-to-desk / XO-internal traffic (stays dense).
- Blocking posts that fail the audit (log-only v1).