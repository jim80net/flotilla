# recycle Specification (delta)

## ADDED Requirements

### Requirement: An XO-triggered desk recycle preserves context across a fresh restart

The system SHALL provide `flotilla recycle <desk>` — a single operation that closes a desk's chapter
and restarts it with a fresh context window while preserving the chapter's context via the desk's own
handoff. The mechanism is flotilla's; the trigger is the XO's (the XO decides WHEN a chapter is
logically complete). A recycle SHALL run a linear, fail-closed pipeline: resolve the desk's pane
(marker-first), drive the desk to emit a durable handoff, gate on that handoff landing durably,
gracefully close the session, relaunch fresh from the launch recipe with the control channel re-bound,
and point the fresh session at the handoff. The decision logic SHALL be separated from I/O so its
abort behaviour is unit-tested.

#### Scenario: A clean recycle preserves the chapter

- **WHEN** `flotilla recycle <desk>` runs on a desk whose chapter is complete
- **THEN** a handoff is written and committed, the session is closed gracefully (not killed),
  relaunched fresh in the same pane via the launch recipe, the fresh session is pointed at the handoff,
  and flotilla reachability (the `@flotilla_agent` marker / relay) is intact

#### Scenario: Recycle refuses an unresolvable or ambiguous desk

- **WHEN** no pane resolves for the desk, or more than one does (a mis-tagged fleet)
- **THEN** recycle errors without closing or launching anything, naming the remedy (nothing to recycle
  / re-tag the right pane), never acting on ambiguity

### Requirement: The close is gated behind a durably-confirmed handoff (at-most-once-context-loss)

A recycle SHALL NOT close a desk's session until the handoff is durably confirmed by ALL of: a fresh
handoff artifact exists (mtime after the handoff was triggered), the pane has returned to idle (the
handoff turn finished), AND the desk's durable outputs are committed (in a git work-tree: no
uncommitted changes to tracked files and the handoff committed; a non-git working directory skips only
the commit check). If any of these is not confirmed within the configured timeout, the recycle SHALL
ABORT — leaving the desk RUNNING with its context intact and nothing closed. This makes
handoff-committed-before-close a code-enforced property: the worst case of a recycle is a no-op, never
a lost chapter.

#### Scenario: An unconfirmed handoff aborts with the desk still running

- **WHEN** the fresh-handoff artifact never appears, the pane never returns to idle, or the tree is not
  clean within the timeout
- **THEN** recycle ABORTS — it does NOT close the session; the desk keeps running with its context
  intact, and the abort is reported

#### Scenario: A confirmed handoff proceeds to the graceful close

- **WHEN** the fresh handoff exists, the pane is idle, and the durable outputs are committed
- **THEN** recycle proceeds to gracefully close the session

### Requirement: The relaunch is gated behind a confirmed close and never duplicates a live session

After issuing the graceful close, a recycle SHALL confirm the pane has become a shell (the process
exited) before relaunching. If the close does not confirm a shell within the timeout, the recycle
SHALL ABORT the relaunch (it SHALL NOT relaunch on top of a possibly-live session, which would create
a duplicate process or a double-bound marker). When a surface reports it has no graceful close, the
recycle MAY fall back to a hard respawn-kill — permitted ONLY because the handoff is already durable by
that point. The relaunch SHALL reuse the existing pane (so the `@flotilla_agent` marker survives) and
SHALL confirm the marker reads back as the desk's key.

#### Scenario: A close that does not confirm a shell aborts the relaunch

- **WHEN** the graceful close is issued but the pane does not become a shell within the timeout
- **THEN** recycle aborts the relaunch and reports that the close did not confirm — it does not
  relaunch on top of a session that may still be live

#### Scenario: The relaunch re-binds the control channel for free

- **WHEN** the close confirmed a shell and the desk is relaunched from its recipe
- **THEN** the relaunch reuses the pane id so the `@flotilla_agent` marker survives, and the marker
  read-back confirms the desk is reachable again without re-tagging

### Requirement: The fresh session is handed the context bridge over a side channel; remote desks parlay via message

After the fresh session reaches idle, the recycle SHALL inject the takeover turn EXACTLY ONCE, pointing
it at the handoff artifact detected during the handoff phase. All recycle status SHALL go to the
command's own output (a side channel) and NEVER into the desk's composer. The takeover turn SHALL tell
a remote-driven session to surface any clarification via a flotilla message, never an in-pane
interactive prompt — because an in-pane interactive menu is unanswerable by a remote XO over the relay
(keystrokes navigate the menu rather than select an option).

#### Scenario: Takeover is injected once on the ready fresh session

- **WHEN** the relaunched session reaches idle
- **THEN** the takeover turn is injected exactly once, pointing at the handoff path, and recycle status
  is reported on the command's own output, never the desk's composer

#### Scenario: A recycled remote desk is told to parlay via message

- **WHEN** the fresh session is handed its takeover turn
- **THEN** it is instructed that it is remote-driven and must surface clarifications via a flotilla
  message, not an in-pane interactive menu (which a remote XO cannot answer)

### Requirement: Recycle is cross-harness-capable and previewable

The recycle relaunch SHALL target an arbitrary launch recipe (claude, grok, cursor, aider — not
hard-coded to one harness), and the handoff artifact it bridges SHALL be harness-agnostic, so a desk
can be recycled onto a DIFFERENT harness behind the same mechanism (the cross-harness exercise is a
separate, gated change). A `--dry-run` SHALL print the resolved plan — the pane, the launch recipe, and
the handoff/takeover turns it would inject — without acting.

#### Scenario: Dry-run previews without acting

- **WHEN** `flotilla recycle <desk> --dry-run` runs
- **THEN** it prints the resolved pane, recipe, and the turns it would inject, and exits without
  triggering a handoff, closing, or relaunching

#### Scenario: The relaunch is not hard-coded to one harness

- **WHEN** a desk's launch recipe names a non-claude harness
- **THEN** recycle relaunches via that recipe verbatim, and the handoff artifact bridged is the same
  harness-agnostic markdown a claude desk would have written
