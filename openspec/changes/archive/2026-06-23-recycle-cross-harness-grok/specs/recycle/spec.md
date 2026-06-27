# recycle Specification (delta)

## MODIFIED Requirements

### Requirement: Recycle is cross-harness-ready, per-phase-bounded, and previewable

The recycle relaunch SHALL target an arbitrary launch recipe (claude, grok, cursor, aider — not
hard-coded to one harness), and the handoff artifact it bridges SHALL be harness-agnostic markdown, so a
desk can be recycled onto a DIFFERENT harness behind the same per-driver bridge. **Claude Code and grok
both meet the recycle-capable bar** (grok added 2026-06-23, #158: it implements `RecycleBridge` with the
harness-agnostic `<cwd>/.flotilla/handoffs/recycle-<token>.md` convention and `ComposerStateProbe` with
a live-characterized cursor-indexed composer classifier; its `Close` returns `ErrNoGracefulClose`, so a
grok recycle closes via the handoff-gated respawn-kill fallback). A cross-harness MIGRATION of a running
desk (e.g. beta-xo Claude Code → Grok) is an ORCHESTRATED sequence over existing primitives — a
handoff turn on the FROM harness, a roster surface + launch-recipe flip, a `flotilla resume` relaunch on
the TO harness, and a takeover turn — NOT a single recycle call (one recycle resolves ONE driver for all
phases); flotilla provides the recycle-capable drivers, the migration is an XO-run runbook. The recycle
SHALL bound each phase with its own timeout (the handoff turn is multi-minute; the close, boot, and
takeover edges are seconds), not one timeout across gates of order-of-magnitude-different latency; the
per-phase timeouts are internal defaults (tuned from the live validation), not public flags. A
`--dry-run` SHALL print the resolved plan — the pane, the launch recipe, the designated handoff path, and
the handoff/takeover turns it would inject — without acting or acquiring the transaction lock (advisory;
the real run re-resolves under the lock).

#### Scenario: Dry-run previews without acting

- **WHEN** `flotilla recycle <desk> --dry-run` runs
- **THEN** it prints the resolved pane, recipe, designated handoff path, and the turns it would inject,
  and exits without injecting, closing, relaunching, or acquiring the pane transaction lock

#### Scenario: The relaunch is not hard-coded to one harness

- **WHEN** a desk's launch recipe names a non-claude harness
- **THEN** recycle relaunches via that recipe verbatim, and the handoff artifact bridged is the same
  harness-agnostic markdown a claude desk would have written

#### Scenario: A grok desk is recycled same-harness

- **WHEN** `flotilla recycle <desk>` runs on a desk whose surface is `grok`
- **THEN** the command does NOT refuse (grok implements `RecycleBridge` + `ComposerStateProbe`), gates on
  the handoff landing at `<cwd>/.flotilla/handoffs/recycle-<token>.md`, and closes via the handoff-gated
  respawn-kill (grok's `Close` returns `ErrNoGracefulClose`) before relaunching fresh

#### Scenario: A cross-harness migration sources the takeover path from the FROM harness

- **WHEN** a running desk is migrated Claude Code → Grok by orchestrating a claude handoff turn, a roster
  surface/launch flip, a `flotilla resume`, and a grok takeover turn
- **THEN** the takeover turn delivered to the relaunched grok session names the FROM-harness (claude)
  handoff path (`<cwd>/.claude/handoffs/recycle-<token>.md`), sourced from the claude recycle's status
  record (`~/.flotilla/<agent>/last-recycle.json` `handoff_path`), NOT grok's own `.flotilla/handoffs/`
  path — grok's `TakeoverTurn` is path-parametric, so it consumes the claude-authored handoff verbatim
