# recycle Specification

## Purpose
TBD - created by archiving change desk-recycle. Update Purpose after archive.
## Requirements
### Requirement: An XO-triggered desk recycle preserves context across a fresh restart

The system SHALL provide `flotilla recycle <desk>` — a single operation that closes a desk's chapter
and restarts it with a fresh context window while preserving the chapter's context via the desk's own
handoff. The mechanism is flotilla's; the trigger is the XO's (the XO decides WHEN a chapter is
logically complete). A recycle SHALL run a linear, fail-closed pipeline: resolve the desk's pane
(marker-first), require a git work-tree and a recycle-capable surface, confirm the pane is idle before
injecting, drive the desk to emit a durable handoff, gate on that handoff landing durably, gracefully
close the session, relaunch fresh from the launch recipe with the control channel re-bound, and point
the fresh session at the handoff. The decision logic SHALL be separated from I/O so its abort behaviour
is unit-tested. The recycle SHALL REFUSE cleanly (naming the cause) when the surface is not
recycle-capable (no `RecycleBridge` or no composer-state probe), when the working directory is not a git
tree, when the pane is in a tmux copy/view mode (where composer state cannot be read), or when the
resolved target IDENTIFIES THE SAME PANE as the one the command runs in (self-recycle — closing it
would kill the running command before the relaunch, leaving an unrecoverable closed-but-not-relaunched
desk). The same-pane check SHALL compare canonical pane identities (the tmux `#{pane_id}` of the
resolved target against `$TMUX_PANE`), NOT a literal target-string equality (the resolved target is
`session:window.pane` while `$TMUX_PANE` is a `%N` id — they are never string-equal, so a literal
comparison would be a dead guard); an empty `$TMUX_PANE` (run from a non-pane context such as the watch
host) SHALL NOT trip the guard.

#### Scenario: A clean recycle preserves the chapter

- **WHEN** `flotilla recycle <desk>` runs on a recycle-capable desk in a git work-tree whose chapter is
  complete
- **THEN** a handoff is written and committed, the session is closed gracefully (not killed),
  relaunched fresh in the same pane via the launch recipe, the fresh session is pointed at the handoff
  and begins working, and flotilla reachability (the `@flotilla_agent` marker / relay) is intact

#### Scenario: Recycle refuses an unresolvable, ambiguous, self-targeted, non-git, copy-mode, or incapable desk

- **WHEN** no pane resolves, or more than one does, or the resolved target is the command's own pane, or
  the working directory is not a git tree, or the pane is in tmux copy-mode, or the surface lacks a
  recycle bridge / composer-state probe
- **THEN** recycle errors without injecting, closing, or launching anything, naming the cause and the
  remedy, never acting on ambiguity and never silently degrading

### Requirement: Recycle injects only into an idle pane and serializes the irreversible span against resume

Before injecting any turn, a recycle SHALL confirm the target pane is idle at its main composer (`Idle`
and the composer reads cleared), treating an undetermined composer reading (including copy-mode) as NOT
cleared (fail-closed). If the pane does not settle within the boot timeout, the recycle SHALL ABORT
without injecting (the desk is untouched). The cooperative handoff phase SHALL run WITHOUT a long-held
lock (to avoid starving operator delivery to the pane for minutes); the recycle SHALL then acquire a
single per-pane transaction lock and RE-VERIFY the handoff-completion gate under it before the
irreversible close, and SHALL hold that lock across the close→relaunch→takeover span. `flotilla resume`
SHALL take the SAME per-pane transaction lock around its in-place respawn, so a recycle and a resume (or
two recycles) cannot interleave across the close→relaunch window.

#### Scenario: A pane that never settles idle aborts before any injection

- **WHEN** the target pane is mid-turn, or its composer reads undetermined (e.g. copy-mode), and never
  settles to idle-and-cleared within the boot timeout
- **THEN** recycle ABORTS without injecting the handoff turn — the desk is untouched, still running

#### Scenario: A turn that starts in the unlocked window is caught by the under-lock re-verify

- **WHEN** the handoff-completion gate passes while unlocked, then the desk starts a turn before the
  recycle acquires the transaction lock
- **THEN** the under-lock re-verify reads the desk as working (not idle-and-cleared) and the recycle
  ABORTS rather than closing a mid-turn desk

#### Scenario: A concurrent resume cannot interleave with the close→relaunch span

- **WHEN** a recycle holds the pane transaction lock across close→relaunch and a `flotilla resume`
  targets the same pane
- **THEN** the resume waits for the lock (bounded) and does not interleave its respawn into the
  recycle's close→relaunch window

### Requirement: The close is gated behind a durably-confirmed handoff (at-most-once handoff-artifact-loss)

A recycle SHALL NOT close a desk's session until the handoff is durably confirmed by BOTH of: the
recycle-designated handoff artifact went from ABSENT at HEAD (at the start) to COMMITTED at HEAD (a
transition, so a pre-existing committed blob at the path cannot false-pass) AND is non-trivial (at least
a configured minimum size — the minimum-viability check, preventing an empty/trivial stub from
false-passing), AND the pane has returned to idle at its main composer (the handoff turn finished, not
paused inside a skill confirmation). Committed-ness SHALL be detected by the presence of the path in the
HEAD tree (not by a command exit code, which cannot distinguish "not yet committed" from an error). Any
git error or a not-yet-committed reading SHALL be treated as not-durable (fail-closed; keep polling). If
either condition is not confirmed within the handoff timeout — or fails the re-verify under the lock —
the recycle SHALL ABORT, leaving the desk RUNNING with its context intact and nothing closed. This makes
handoff-committed-before-close a code-enforced property: the worst case is a no-op recycle, never a lost
handoff. The recycle SHALL NOT require the desk's whole working tree to be committed (a chapter boundary
commonly has in-progress work, whose context the handoff prose captures); the durable signal is the
committed handoff blob, not a clean tree. The gate guarantees the artifact LANDS, not its quality;
handoff quality is the desk's responsibility.

#### Scenario: An unconfirmed handoff aborts with the desk still running

- **WHEN** the designated handoff artifact does not become committed-and-non-trivial, or the pane never
  returns to an idle cleared composer, within the handoff timeout
- **THEN** recycle ABORTS — it does NOT close the session; the desk keeps running with its context
  intact, and the abort is reported

#### Scenario: A pre-existing committed blob at the path does not false-pass

- **WHEN** a blob already exists at the designated handoff path at the start of the recycle
- **THEN** the gate (which requires an absent→committed transition) does NOT treat it as a fresh handoff;
  it waits for the desk to commit anew or aborts on timeout

#### Scenario: A confirmed handoff proceeds to the graceful close

- **WHEN** the designated handoff blob transitioned absent→committed and is non-trivial AND the pane is
  idle at a cleared composer AND this re-verifies under the lock
- **THEN** recycle proceeds to the graceful-close phase

### Requirement: The relaunch is gated behind a confirmed close and never duplicates a live session

The relaunch reuses a kill-respawn primitive that kills whatever is in the pane, so the recycle SHALL
treat confirming the close worked as correctness-critical, not defensive. Before issuing the close, a
recycle SHALL require the
composer to be cleared (not on a focus-stealing overlay), healing the overlay where self-heal is
available or ABORTING rather than firing the exit keystroke into an overlay. Because a harness may run as
the pane's DIRECT process (so a graceful exit CLOSES the pane rather than dropping to a shell), the
recycle SHALL set the pane's `remain-on-exit` ON before the close (so the exit leaves a DEAD-but-present
pane the relaunch can revive) and SHALL restore it OFF on every exit path including abort. After issuing
the close, a recycle SHALL confirm the old process has exited — by the pane being DEAD OR a shell verdict
— before relaunching, RETRYING on a transient uncertain (capture-glitch) reading rather than aborting
early on it. If the close does not confirm the process exited within the close timeout, the recycle SHALL ABORT the relaunch — it SHALL NOT relaunch
on top of a possibly-live session — and the abort copy SHALL be STATE-AWARE: a desk that may still be
live names investigation and `flotilla resume <desk> --force` (since `resume` refuses a non-shell pane
without `--force`); a confirmed-dead desk names `flotilla resume <desk>`. When a surface reports no
graceful close (`ErrNoGracefulClose`), the recycle MAY fall back to a hard respawn-kill — permitted ONLY
because the handoff is already durable. The relaunch SHALL reuse the existing pane (so `@flotilla_agent`
survives), SHALL confirm the marker reads back as the desk's key (a mismatch ABORTS, naming the
live-fresh-desk recovery: `flotilla send <desk> 'read <handoff-path> and take over'`), and SHALL stamp a
unique relaunch-generation marker on the pane.

#### Scenario: A close that does not confirm a shell aborts with state-aware recovery copy

- **WHEN** the graceful close is issued but the pane does not become a shell within the close timeout
  (after retrying transient uncertain readings)
- **THEN** recycle aborts the relaunch and reports that the close did not confirm, naming investigation
  and `flotilla resume <desk> --force` — it does not relaunch on top of a session that may still be live

#### Scenario: The relaunch re-binds the control channel and stamps the generation

- **WHEN** the close confirmed a shell and the desk is relaunched from its recipe
- **THEN** the relaunch reuses the pane id so the `@flotilla_agent` marker survives, the marker
  read-back confirms reachability (a mismatch aborts with the live-fresh-desk recovery copy), and a
  unique relaunch-generation marker is stamped on the pane

### Requirement: The fresh session is handed the context bridge once over a side channel; remote desks parlay via message

After the fresh session reaches idle at a cleared composer, the recycle SHALL deliver the imperative
takeover turn EXACTLY ONCE, pointing it at the designated handoff artifact, and only while the pane's
relaunch-generation marker still equals this recycle's unique generation (a superseding recycle aborts
this one's takeover). The takeover turn SHALL instruct the session to begin work immediately and, being
remote-driven, to surface any clarification via a flotilla message rather than an in-pane interactive
prompt (which a remote XO cannot answer over the relay). All recycle status SHALL go to the command's
own output (a side channel) and NEVER into the desk's composer; the recycle SHALL also write a
host-local per-desk status record (outcome + the designated handoff path + the recovery command for an
abort) that survives the process. After delivering the takeover, the recycle SHOULD poll best-effort for
the desk to begin working (a resumption-confidence signal) and report it, without failing the recycle if
the signal is slow to appear.

#### Scenario: Takeover is delivered once on the ready fresh session

- **WHEN** the relaunched session reaches idle at a cleared composer and its generation marker matches
- **THEN** the imperative takeover turn is delivered exactly once via confirmed delivery, pointing at the
  designated handoff path; recycle status is reported on the command's own output and a host-local status
  record, never the desk's composer

#### Scenario: A recycled remote desk is told to parlay via message

- **WHEN** the fresh session is handed its takeover turn
- **THEN** it is instructed that it is remote-driven and must surface clarifications via a flotilla
  message, not an in-pane interactive menu (which a remote XO cannot answer)

### Requirement: Recycle is cross-harness-ready, per-phase-bounded, and previewable

The recycle relaunch SHALL target an arbitrary launch recipe (claude, grok, cursor, aider — not
hard-coded to one harness), and the handoff artifact it bridges SHALL be harness-agnostic markdown, so a
desk can be recycled onto a DIFFERENT harness behind the same per-driver bridge (the cross-harness
exercise is a separate, gated change; the only harness meeting the recycle-capable bar today is Claude
Code — the design is cross-harness-READY, not a shipped cross-harness capability). The recycle SHALL
bound each phase with its own timeout (the handoff turn is multi-minute; the close, boot, and takeover
edges are seconds), not one timeout across gates of order-of-magnitude-different latency; the per-phase
timeouts are internal defaults (tuned from the live validation), not public flags. A `--dry-run` SHALL
print the resolved plan — the pane, the launch recipe, the designated handoff path, and the
handoff/takeover turns it would inject — without acting or acquiring the transaction lock (advisory; the
real run re-resolves under the lock).

#### Scenario: Dry-run previews without acting

- **WHEN** `flotilla recycle <desk> --dry-run` runs
- **THEN** it prints the resolved pane, recipe, designated handoff path, and the turns it would inject,
  and exits without injecting, closing, relaunching, or acquiring the pane transaction lock

#### Scenario: The relaunch is not hard-coded to one harness

- **WHEN** a desk's launch recipe names a non-claude harness
- **THEN** recycle relaunches via that recipe verbatim, and the handoff artifact bridged is the same
  harness-agnostic markdown a claude desk would have written

