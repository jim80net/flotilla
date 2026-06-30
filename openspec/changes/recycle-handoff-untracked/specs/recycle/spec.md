## MODIFIED Requirements

### Requirement: An XO-triggered desk recycle preserves context across a fresh restart

The system SHALL provide `flotilla recycle <desk>` — a single operation that closes a desk's chapter
and restarts it with a fresh context window while preserving the chapter's context via the desk's own
handoff. The mechanism is flotilla's; the trigger is the XO's (the XO decides WHEN a chapter is
logically complete). A recycle SHALL run a linear, fail-closed pipeline: resolve the desk's pane
(marker-first), require a recycle-capable surface, confirm the pane is idle before injecting, drive the
desk to emit a durable handoff, gate on that handoff landing durably, gracefully close the session,
relaunch fresh from the launch recipe with the control channel re-bound, and point the fresh session at
the handoff. The decision logic SHALL be separated from I/O so its abort behaviour is unit-tested. The
recycle SHALL REFUSE cleanly (naming the cause) when the surface is not recycle-capable (no
`RecycleBridge` or no composer-state probe), when the pane is in a tmux copy/view mode (where composer
state cannot be read), or when the resolved target IDENTIFIES THE SAME PANE as the one the command runs
in (self-recycle — closing it would kill the running command before the relaunch, leaving an
unrecoverable closed-but-not-relaunched desk). The same-pane check SHALL compare canonical pane
identities (the tmux `#{pane_id}` of the resolved target against `$TMUX_PANE`), NOT a literal
target-string equality (the resolved target is `session:window.pane` while `$TMUX_PANE` is a `%N` id —
they are never string-equal, so a literal comparison would be a dead guard); an empty `$TMUX_PANE` (run
from a non-pane context such as the watch host) SHALL NOT trip the guard.

#### Scenario: A clean recycle preserves the chapter

- **WHEN** `flotilla recycle <desk>` runs on a recycle-capable desk whose chapter is complete
- **THEN** a handoff is written as an untracked file on disk, the session is closed gracefully (not
  killed), relaunched fresh in the same pane via the launch recipe, the fresh session is pointed at the
  handoff and begins working, and flotilla reachability (the `@flotilla_agent` marker / relay) is intact

#### Scenario: Recycle refuses an unresolvable, ambiguous, self-targeted, copy-mode, or incapable desk

- **WHEN** no pane resolves, or more than one does, or the resolved target is the command's own pane, or
  the pane is in tmux copy-mode, or the surface lacks a recycle bridge / composer-state probe
- **THEN** recycle errors without injecting, closing, or launching anything, naming the cause and the
  remedy, never acting on ambiguity and never silently degrading

### Requirement: The close is gated behind a durably-confirmed handoff (at-most-once handoff-artifact-loss)

A recycle SHALL NOT close a desk's session until the handoff is durably confirmed by BOTH of: the
recycle-designated handoff artifact went from ABSENT on disk (at the start) to PRESENT on disk (a
transition, so a pre-existing file at the path cannot false-pass) AND is non-trivial (at least a
configured minimum size — the minimum-viability check, preventing an empty/trivial stub from
false-passing), AND the pane has returned to idle at its main composer (the handoff turn finished, not
paused inside a skill confirmation). Presence SHALL be detected by `os.Stat` on the designated path (a
regular file at or above the minimum size — not by a command exit code, which cannot distinguish "not
yet written" from an error). Any stat error or a not-yet-present reading SHALL be treated as not-durable
(fail-closed; keep polling). If either condition is not confirmed within the handoff timeout — or fails
the re-verify under the lock — the recycle SHALL ABORT, leaving the desk RUNNING with its context intact
and nothing closed. This makes handoff-present-before-close a code-enforced property: the worst case is a
no-op recycle, never a lost handoff. The recycle SHALL NOT require the desk's whole working tree to be
committed (a chapter boundary commonly has in-progress work, whose context the handoff prose captures);
the durable signal is the handoff file on disk, not version control. The gate guarantees the artifact
LANDS, not its quality; handoff quality is the desk's responsibility. The handoff SHALL NOT be committed
to git (`git add` / `git commit` are forbidden by the handoff turn).

#### Scenario: An unconfirmed handoff aborts with the desk still running

- **WHEN** the designated handoff artifact does not become present-and-non-trivial on disk, or the pane
  never returns to an idle cleared composer, within the handoff timeout
- **THEN** recycle ABORTS — it does NOT close the session; the desk keeps running with its context
  intact, and the abort is reported

#### Scenario: A pre-existing file at the path does not false-pass

- **WHEN** a file already exists at the designated handoff path at the start of the recycle
- **THEN** the gate (which requires an absent→present transition) does NOT treat it as a fresh handoff;
  it waits for the desk to write anew or aborts on timeout

#### Scenario: A confirmed handoff proceeds to the graceful close

- **WHEN** the designated handoff file transitioned absent→present and is non-trivial AND the pane is
  idle at a cleared composer AND this re-verifies under the lock
- **THEN** recycle proceeds to the graceful-close phase

## REMOVED Requirements

### Requirement: Recycle refuses when the working directory is not a git tree

**Reason**: The handoff durability gate is filesystem-based (#218); git HEAD is no longer consulted.
**Migration**: Remove git-work-tree refusal from recycle preconditions; operators no longer need a git
tree solely for recycle handoff detection.