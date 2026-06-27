# Design — desk-recycle

## Context

#157 asks for an XO-triggered "close the chapter, restart fresh" primitive that preserves context via
flotilla scaffolding, so a desk never has to run until it compacts. #158 (gated on #157) then moves
a federated XO claude→grok using the same primitive — so #157 is built cross-harness-**ready** (an
arbitrary launch recipe + a harness-agnostic handoff artifact + a per-driver bridge SPI), and #158
adds + exercises the grok bridge. #157 itself exercises ONLY claude→claude (fork 4).

The relaunch half is already built and harness-agnostic (`cmd/flotilla/resume.go` + `deliver`): it
drives `recipe.Launch` verbatim and `RespawnPane` reuses the pane id so the `@flotilla_agent` marker
survives. The new work is the **graceful close** and the **context-bridge orchestration** that wraps
the existing relaunch in a fail-closed lifecycle.

This design was parlayed with the primary XO (the remote XO) over a flotilla message on 2026-06-23 (the
four forks below), then the **design-trio (`/systems-review` + STORM) found a targeted NEEDS-REWORK**
and this revision folds the trio's prioritized findings in. The forks STAND; the rework changed the
*injection mechanism* and the *gate signals*, not the forks. Fork 3 (the cross-harness context
contract) is a product-pillar decision carried to the operator; #157 proceeds on the reversible
provisional default (portable markdown + a templated turn) so it is not blocked on the escalation.

## The root correction this revision encodes (the trio's spine finding)

The first draft templated the handoff/takeover injection from MEMORY of the `/handoff` and
`/takeover` skills, not from reading them — a direct violation of "read the source, every time."
Reading the skill bodies (the harness's `handoff`/`takeover` `SKILL.md` files) shows BOTH skills are **human-interactive and would deadlock a
remote-driven recycle**:

- `/handoff` step 8 (`SKILL.md:205-212`) is an INTERACTIVE *"Is anything missing?"* confirmation, and
  there is **NO git-commit step anywhere** in the skill (committing is `wrap-things-up` Phase 2, which
  *opens a PR, does not commit-to-branch*). ⇒ bare `/handoff` goes Idle **paused at a confirmation**
  nobody answers, and **never commits** — so a draft gate of "Idle ⇒ done" + "the handoff is
  committed" is dead-on-arrival on its own reference harness.
- `/takeover` step 5 (`SKILL.md:72`) is literally *"Shall I start with `<first item>`?"* before step 6
  "Begin work"; steps 1/4 ask which handoff and clarifying questions. ⇒ bare `/takeover <path>` lands
  the fresh session at a **"Shall I start?" wait, not working** — the design's own silent-stall
  anti-goal, realized.

**The fix is not new machinery — it is to inject RECYCLE-SPECIFIC NON-INTERACTIVE instructions, not
the bare interactive skills, and to make the gate signals match harness reality.** Every fix below
uses primitives that ALREADY EXIST (verified at file:line in this revision): the `ComposerStateProbe`
(`surface.go:127`, claude at `claude.go:143`), `AcquirePaneTxn` (`deliver/lock.go:173`), confirmed
delivery (`surface/confirm.go`), the committed-blob presence check (`git ls-tree HEAD -- <path>`, NOT
`git show` — exit codes can't discriminate; see the durability section), `selfHeal`
(`confirm.go:322`), and the `RotateContext`→`ErrRestartRequired` refusal pattern (`surface.go:164`).

## The recycle state machine

`flotilla recycle <desk>` is a linear pipeline whose ONLY irreversible step (the close) is gated
behind a durably-confirmed handoff. The decision core `runRecycle(ops, plan)` is separated from I/O
(à la `runResume`) so each gate's ABORT behaviour is unit-tested by injecting signals.

**Lock scoping (corrected from the first rework draft).** The pane-transaction lock
(`AcquirePaneTxn`) is held across the **seconds-to-~minute irreversible span (Phases 2→4:
close→relaunch→takeover)**, NOT across the multi-MINUTE cooperative handoff (Phase 1). Holding it for
the whole pipeline would starve every operator `send`/`voice`/`dash` and the heartbeat-clock tick to
that pane for minutes (they all bounded-wait `PaneTxnTimeout`≈12s then DROP — verified at
`watch.go:161`/`main.go:338`/`voice.go:132`/`dash` control). (Note: this is a delivery-STARVATION
cost, NOT an XO-down-alert trigger — the watchdog trips on a Shell pane or missed ack-FILE touches
`watchdog.go:32`/`watch.go:49`, which a held lock does not cause; the earlier draft's "trips the
XO-down alert" claim was an unverified mechanism and is dropped.) So: Phases 0–1 run lockless (the
discrete handoff-turn DELIVERY self-locks briefly via confirmed delivery; the polls are lockless
reads); then **acquire the txn lock and RE-VERIFY the Phase-1 gate under it** before the close.
`resume` is changed to take the SAME lock in its `ResolveUnique` branch, so a recycle and a
resume/recycle cannot interleave across the close→relaunch window (the duplicate-process race
`resume.go:176-183` admits, which recycle widens).

The re-verify-under-lock closes the primary post-handoff TOCTOU: if anything (the watch daemon, an
operator) woke the desk during the unlocked Phase 1, the under-lock re-read sees `Working` (or the
handoff blob regressed) and ABORTS rather than closing a mid-turn desk. **Two residuals are documented,
not silently ignored:** (1) a flotilla-driven wake that BOTH starts AND finishes a turn inside the
sub-second lock-acquire window; (2) a turn the AGENT starts AUTONOMOUSLY between the under-lock
re-verify and the `/exit` keystroke (the lock serializes flotilla WRITERS, it cannot freeze the agent
process itself). Both are bounded and recoverable — the XO must not wake/race a desk it is recycling,
and the handoff-of-record is already durable, so the worst case is a `-k` of a session that already
committed its handoff (recoverable via `flotilla resume`).

**Self-recycle is REFUSED.** If the resolved target pane is the SAME pane the `flotilla recycle`
command itself runs in, Phase-2 `Close` would `/exit` the command's own pane and kill the pipeline
before the relaunch — leaving a closed-but-not-relaunched desk with no process to recover it. The XO is
the likeliest first recycle target, so recycle REFUSES when the target is its own pane, naming the
remedy (run it from a different pane / the watch host). The comparison MUST be canonical, not a bare
string `==`: the resolved target is `session:window.pane` (`tmux.go:28` `paneListFormat`) while
`$TMUX_PANE` is a `%N` pane-id, so they are NEVER string-equal — a naive `target == $TMUX_PANE` is a
dead no-op that would let a real self-recycle through. The guard resolves the target's `#{pane_id}`
(`tmux display-message -p -t <target> '#{pane_id}'`, a `%N` — the stable, globally-unique pane identity,
unlike `session:window.pane` which renumbers when windows move) and compares THAT to `$TMUX_PANE`; an
empty `$TMUX_PANE` (recycle run from the watch host / cron — a genuinely different pane) correctly does
NOT trip. A `samePaneAsSelf(target, tmuxPane) (bool, error)` helper is injectable so the test exercises
real `%N`-vs-`%N` equality. `recycle` also REQUIRES a git work-tree (a non-git cwd is REFUSED cleanly —
its durability guarantee would be strictly weaker, with no atomic-commit immutability).

The recycle token (`<RFC3339nano>-<nonce>`, used for both the designated handoff path and the
relaunch-generation marker) draws its `<nonce>` from `crypto/rand` (a few bytes, hex) — RFC3339nano
alone is not collision-free, and the nonce is the uniqueness guarantor for both the path and the gen
marker.

```
resolve pane (marker-first)
  ├─ None       → error "no pane for <desk>; nothing to recycle"
  ├─ Ambiguous  → error "fleet mis-tagged; re-tag, then retry"   (never act on ambiguity)
  └─ Unique ↓
REFUSE if target is the command's OWN pane (self-recycle — would /exit the command's own pane).
  Compare CANONICALLY: resolve the target's `#{pane_id}` (`tmux display-message -p -t <target>
  '#{pane_id}'` → a %N id) and compare to $TMUX_PANE (also %N). A bare `target == $TMUX_PANE` is a
  DEAD guard — target is `session:window.pane`, $TMUX_PANE is `%N`, never string-equal. An empty
  $TMUX_PANE (run from the watch host / cron — a genuinely different pane) correctly does NOT trip.
REFUSE if the pane is in tmux copy/view mode (composer state unreadable → ComposerUndetermined)
require a git work-tree (else REFUSE) + a recycle-capable surface (RecycleBridge ∧ ComposerStateProbe)
record baseline:
   now=t0; recycle token=<t0 RFC3339nano>-<crypto/rand nonce>; the launch recipe; the desk key;
   designatedPath = driver.HandoffPath(cwd, token)
   ASSERT designatedPath is ABSENT at HEAD at t0 (ls-tree HEAD -- relpath empty) — the gate confirms a
     t0→now ABSENT→COMMITTED transition, so a pre-existing committed blob can never false-pass
  │
  ├─ PHASE 0 — IDLE PRECONDITION (honour InjectSlash's "only inject when idle" contract)   [lockless]
  │   poll until Idle ∧ ComposerCleared, within bootTimeout (the desk may be mid-turn when the
  │     XO triggers on chapter-complete) → never settled → ABORT (desk UNTOUCHED).
  │
  ├─ PHASE 1 — HANDOFF (cooperative: the desk writes its OWN durable bridge, non-interactively) [lockless]
  │   deliver HandoffTurn(designatedPath) via CONFIRMED delivery     # NOT bare /handoff; a templated,
  │       # non-interactive, self-committing instruction (write to designatedPath, git add -f &&
  │       # commit to the current branch, do NOT ask to confirm — remote-driven — then stop)
  │   poll until BOTH of, within handoffTimeout:
  │     (a) the designated handoff blob is DURABLE  — went ABSENT→COMMITTED at HEAD (git ls-tree
  │         presence, NOT exit-code), AND non-trivial (≥ minHandoffBytes — the minimum-viability check)
  │     (b) Idle ∧ ComposerCleared                  — the turn finished at the MAIN composer, not
  │                                                    paused inside a skill confirmation
  │   └─ timeout → ABORT: desk UNTOUCHED, still running, nothing closed.
  │        (at-most-once handoff-ARTIFACT-loss — the close NEVER happens on an unconfirmed handoff)
  │
  ├─ ACQUIRE pane-txn lock  (held across Phases 2→4; auto-released on completion or process death)
  ├─ RE-VERIFY the Phase-1 gate under the lock (Idle ∧ ComposerCleared ∧ durable, FRESH reads)
  │   └─ regressed (e.g. a turn started in the unlocked window) → ABORT (desk UNTOUCHED, lock released)
  │
  ├─ PHASE 2 — GRACEFUL CLOSE (the one irreversible step; the handoff is durable by here)   [locked]
  │   # CORRECTNESS-CRITICAL, not defense-in-depth: RespawnPane is ALWAYS `-k` (it kills whatever is in
  │   # the pane), so confirming the old process is GONE BEFORE relaunch is the ONLY thing preventing
  │   # a -k of a live session. (The Phase-1 re-verify already established Idle ∧ ComposerCleared.)
  │   SetRemainOnExit(pane, on)                  # so /exit leaves a DEAD pane (claude-direct fleet desk:
  │       #                                        no shell behind claude) instead of CLOSING the window
  │   defer SetRemainOnExit(pane, off)           # restore steady-state on EVERY exit (incl. abort)
  │   Close(pane)                                # claude: slash-keys /exit (single-keystroke-terminal,
  │       #                                        verified in 6.3)
  │     └─ ErrNoGracefulClose (surface has no clean exit, e.g. grok-unverified / cursor)
  │          → fall back to RespawnPane-kill      # safe: handoff already durable
  │   poll until pane_dead==1 (claude-direct) OR Assess==Shell (shell-backed), within closeTimeout:
  │     · a transient pane_dead error / Assess==Unknown (capture glitch, fail-open) → RETRY the poll
  │   └─ neither within the budget → ABORT (release lock + restore remain-on-exit): STATE-AWARE copy —
  │        "close did not confirm the process exited; the desk MAY STILL BE LIVE — investigate; if
  │         confirmed dead, recover with: flotilla resume <desk> --force"  (NEVER relaunch on a live pane)
  │
  ├─ PHASE 3 — RELAUNCH (reuse the hardened resume primitive)                                 [locked]
  │   RespawnPane(pane, cwd, recipe.Launch)      # reuses pane id → @flotilla_agent marker SURVIVES
  │   ReadMarker(pane) == desk.key  else ABORT   # confirm the marker landed (resume's read-back); a
  │       #   mismatch = a LIVE contextless fresh desk → recovery copy names:
  │       #   flotilla send <desk> 'read <designatedPath> and take over per it, begin immediately'
  │   stamp @flotilla_recycle_gen = token        # idempotency: this relaunch's UNIQUE generation
  │
  └─ PHASE 4 — TAKEOVER (point the fresh, clean-context session at the bridge — IMPERATIVELY)  [locked]
      poll until Idle ∧ ComposerCleared, within bootTimeout      # the fresh harness finished booting
          # (ComposerCleared — not bare Idle — is what gates against a premature boot-splash "Idle":
          #  a splash shows no ❯ prompt line → ComposerUndetermined, never Cleared)
      re-read @flotilla_recycle_gen == token  else ABORT         # another recycle superseded this pane
      deliver TakeoverTurn(designatedPath) via CONFIRMED delivery, EXACTLY ONCE
          # claude: an IMPERATIVE "read <path>, take over, BEGIN WORK IMMEDIATELY, do NOT ask whether
          # to start; you are remote-driven — surface clarifications via a flotilla MESSAGE, never an
          # in-pane interactive prompt"  (does NOT invoke the interactive /takeover skill)
      poll until Assess == Working, within takeoverTimeout       # success = the desk RESUMED, not just
          # that the turn was typed; a Working edge is the resumption-confidence signal (best-effort:
          # log if it never appears — the takeover WAS delivered-confirmed, the desk just hasn't shown
          # the spinner yet)
  RELEASE the lock ; write ~/.flotilla/<desk>/last-recycle.json (outcome + designatedPath + recovery)
```

### Why this ordering is the at-most-once handoff-ARTIFACT-loss property (renamed; honest scope)

The chapter's context lives in two places during a recycle: the running session (volatile, dies on
close) and the handoff artifact (durable). The ONLY way to lose the **artifact** is to close before it
is durable. Phase 1's gate is **fail-closed**: it ABORTS (leaving the desk running) on any
un-confirmation, so the close is reached ONLY after the handoff blob provably went absent→committed,
is non-trivial, and the turn finished at the main composer (re-verified under the lock). Worst case is
a *no-op recycle* (the desk keeps running with its context intact). A crash *between* Phase 1 and the
close loses no artifact — it is already durable.

**Renamed from "context-loss" → "handoff-ARTIFACT-loss" (the trio's honesty fix), because the gate
guarantees the ARTIFACT lands, NOT its QUALITY.** Handoff quality is the DESK's responsibility, not the
gate's — the gate's only quality proxy is `minHandoffBytes`, a floor that stops an empty/trivial stub
false-passing. It is NOT a truncation detector (a large-but-truncated handoff passes the floor); the
6.3 live cold-test against a real agent is what validates the handoff is substantively complete. We
deliberately do NOT require the whole working tree committed: a chapter boundary commonly has
in-progress WIP, whose *context* the handoff format captures in prose; forcing whole-tree-clean would
make recycle un-runnable exactly when wanted (and was an abort-forever footgun). The durable signal is
the committed **handoff blob**, not a clean tree.

`minHandoffBytes` ships with a **conservative interim default (≈200 bytes)** — high enough to reject an
empty/error stub, low enough never to reject a real handoff — and is tuned UP from the 6.3 measurement
of a real handoff size. It is NEVER 0 (a 0 floor would let an empty commit false-pass I1); the interim
default is named so the capability is never shippable with the floor effectively disabled, while the
*tuned* value is honestly empirical (not fabricated pre-measurement).

"Loss" here is artifact-loss, NOT availability: a Phase-2 close that never confirms a Shell can leave a
**closed-but-not-relaunched** or a **live-but-uncertain** desk — the artifact is durable either way; the
state-aware abort copy names the exact recovery (Phase 2 / Phase 3 below).

### Why the close confirmation matters (Phase 2 gate) — CORRECTNESS-CRITICAL

`RespawnPane` is **unconditionally `respawn-pane -k`** (`resume.go:108-117`): there is no kill-free
relaunch, so it kills *whatever* foreground process is in the pane. Confirming the old process is
PROVABLY GONE BEFORE relaunching is therefore the ONLY thing that prevents `-k`'ing a LIVE session that
the close did not actually end. This gate is **correctness-critical, not defense-in-depth.**

**The close-confirm mechanism (corrected by the 6.3 live validation, 2026-06-23):** the live fleet runs
`claude --remote-control <name>` as the pane's **DIRECT process** (parent = the tmux server, no shell
behind it), with the server's `remain-on-exit` **off**. So a graceful `/exit` *CLOSES the pane/window*
— it never drops to a `knownShells` shell. A naive close→`Assess==Shell` gate would therefore time out
and ABORT on **every** real desk (the pane vanishes → `Assess`→Unknown→never Shell). The fix
(verified live): Phase 2 sets **`remain-on-exit on`** before the close, so `/exit` leaves a **DEAD pane**
(`#{pane_dead}=1`, the pane + its `@flotilla_agent` marker preserved) instead of closing; the close is
confirmed by **`pane_dead==1`** (the claude-direct case) **OR** `Assess==Shell` (a shell-backed desk);
then `RespawnPane -k` revives the dead pane (reusing the id → marker survives, I3 holds); and
`remain-on-exit` is restored **off** on every exit path (incl. abort), so the desk's steady-state crash
behaviour is unchanged. A transient `pane_dead` read error / `Assess==Unknown` (the capture-glitch
fail-open value, `claude.go:82-83,90-92`) is RETRIED, not treated as "closed"; only a confirmed
dead-or-shell proceeds. `/exit` was confirmed **single-keystroke-terminal** (no confirm sub-prompt) in
6.3, so the claude `Close` issues `/exit` directly.

## The SPI additions

### `Close(pane string) error` — on the core `Driver` interface (fork 1)

```go
// Close gracefully exits the agent's session in the pane (the per-surface clean exit, e.g. claude
// "/exit"), flushing the harness's own session store and dropping the pane to a Shell. A surface with
// NO clean in-session exit (or whose exit keystroke is not yet live-verified, e.g. grok) returns
// ErrNoGracefulClose so the caller may fall back to a hard respawn-kill — safe ONLY because recycle
// has already made the handoff durable. Close MUST NOT blind-kill; the kill fallback is the caller's
// explicit, handoff-gated decision. The injection is the slash-keys primitive (literal keystrokes,
// like Rotate's /clear), NOT bracketed-paste Submit (a slash pasted as a bracketed block may not
// trigger the harness's command parser). Per InjectSlash's contract, the CALLER ensures the pane is
// idle at the main composer before Close (recycle Phase 2 gates ComposerCleared first).
Close(pane string) error
```

Every driver implements it (compile-forced — completeness):
- **claude** — slash-keys `/exit`. The EXACT exit keystroke is **verified in the live validation (6.3)**,
  not asserted from memory; the Phase-2 close→Shell gate is the structural safety net if it is wrong
  (it will not reach Shell → ABORT with the dead-desk recovery copy, never a force-kill of a live
  session). `/clear` slash-keys are verified (`deliver.go`); `/exit` is the analogous keystroke,
  pending 6.3.
- **grok** — returns `ErrNoGracefulClose` **explicitly** until #158 live-characterizes grok's `/exit`.
  The grok driver has a history of being written against the wrong product (`grok.go`); asserting an
  unverified `/exit` would be the same memory-not-source error this revision exists to correct. The
  handoff-gated kill fallback covers it for #158; #157 does not exercise grok.
- **aider** — slash-keys `/exit` (planned; verified when aider is exercised).
- **opencode / cursor** — `ErrNoGracefulClose` unless a clean quit is confirmed.

`ErrNoGracefulClose` mirrors the existing `ErrRestartRequired` refusal (`surface.go:157`): a
distinguished sentinel, never a guess.

### `RecycleBridge` — OPTIONAL capability (forks 2 + 3)

```go
// RecycleBridge is an OPTIONAL Driver capability: the per-harness context-preservation policy a
// recycle drives. A surface that implements it can be context-preservingly recycled; a surface
// WITHOUT it makes `flotilla recycle` REFUSE cleanly (never a silent degrade). Claude Code is the
// reference. The driver owns the per-harness CONVENTIONS (where handoffs live; the exact turn
// wording); the COMMAND owns the lifecycle + delivery. The two "turn" methods return TEXT (pure,
// unit-testable without tmux); the command delivers them via CONFIRMED delivery so it knows the turn
// actually started. CRUCIALLY, the turns are RECYCLE-SPECIFIC and NON-INTERACTIVE — they do NOT
// invoke the human-interactive /handoff or /takeover skills (which pause for confirmation / "shall I
// start?"); they instruct the desk to produce the same artifacts non-interactively.
type RecycleBridge interface {
    // HandoffPath returns the recycle-DESIGNATED handoff artifact path for this harness, given the
    // desk's cwd and a recycle-unique token. The driver owns the per-harness convention (claude:
    // <cwd>/.claude/handoffs/<YYYYMMDD>-recycle-<token>.md). Naming the path up front makes detection
    // EXACT (no mtime, no baseline set-difference, no stale-handoff false-pass) and hands Phase 4 the
    // precise path with zero ambiguity.
    HandoffPath(cwd, token string) string
    // HandoffTurn returns the NON-INTERACTIVE, self-committing handoff instruction to deliver. It must
    // tell the desk to write a handoff (per the /handoff FORMAT, not the interactive skill) to
    // designatedPath, force-commit it to the CURRENT branch (git add -f so a gitignored handoffs dir
    // does not block it), NOT ask for confirmation (it is remote-driven), then stop.
    HandoffTurn(designatedPath string) string
    // TakeoverTurn returns the IMPERATIVE takeover instruction to deliver to the freshly-relaunched
    // session. It must tell the desk to read designatedPath and take over, BEGIN WORK IMMEDIATELY (do
    // NOT ask whether to start), and that it is remote-driven — surface any clarification via a
    // flotilla MESSAGE, never an in-pane interactive prompt. It does NOT invoke the interactive
    // /takeover skill. The PATH is harness-agnostic (markdown); only the wording is per-harness.
    TakeoverTurn(designatedPath string) string
}
```

(`Close` is on the core `Driver` interface, NOT on `RecycleBridge` — a surface may have a graceful
close without a recycle bridge, and vice versa; recycle requires both, checked independently.)

Optional (not on the core interface) because not every surface has a handoff/takeover convention, and
the honest behaviour for one that doesn't is a clean refusal, not a degraded recycle. Keeping it
optional also keeps fork-3 low-commitment — the contract is thin per-driver templates + a path
convention, revisable to a structured schema if #158 demands it. **Minimum-harness-bar for
"recycle-capable":** a `RecycleBridge` (a durable handoff path convention + a non-interactive handoff
turn + an imperative takeover turn) + a graceful-exit keystroke (`Close` ≠ `ErrNoGracefulClose`, or the
handoff-gated kill fallback) + a `ComposerStateProbe` (required for the `Idle ∧ ComposerCleared` gates;
without it the gates are unsafe, so the surface refuses). Today this bar is met ONLY by Claude Code;
grok is a #158 deliverable. Keeping `RecycleBridge` as an idiomatic optional capability matches the
surface package's existing pattern (`ResultReader`, `ComposerStateProbe` are also optional per-driver
capabilities) — it is not speculative generality; what #157 does NOT do is assert a grok bridge EXISTS
(that normative claim moves to #158).

### Why handoff/takeover policy lives on the Driver (not in the command)

The handoffs-dir convention, the exact non-interactive wording, and the exit keystroke are all
intrinsically harness-specific. The command orchestrates the lifecycle (the phases, the gates, the
lock, confirmed delivery) and asks the driver for the per-harness pieces — exactly the per-surface
policy the SPI exists to encapsulate. Returning TEXT (not performing the injection) keeps the driver
pure and unit-testable and lets the command route every injection through the one hardened
confirmed-delivery path.

## The completion-gate signals (Phase 1) — provenance, not heuristics

- **(a) the designated handoff is DURABLE (an ABSENT→COMMITTED transition + non-trivial)** — replacing
  the first draft's mtime + tracked-tree-clean pair (mtime granularity / clock skew / NFS; whole-tree-
  clean was an abort-forever footgun) AND the second draft's "committed-at-HEAD-now" (which left a
  stale-handoff false-pass open — a pre-existing committed blob at the path would pass instantly).
  `recycle` requires a git work-tree (root resolved via `git -C cwd rev-parse --show-toplevel`, so the
  durability check inspects the SAME root where the handoff was written — fixing the git-root mismatch).
  Committed-ness is detected by `git -C cwd ls-tree HEAD -- <relpath>` (NON-EMPTY stdout = committed at
  HEAD; empty = not yet; verified empirically — `git show HEAD:<path>` returns exit 128 for BOTH an
  unborn HEAD and a committed-tree-absent path, so it CANNOT discriminate by exit code, whereas
  `ls-tree` discriminates by output PRESENCE, locale-independently). The gate confirms a **t0→now
  ABSENT→COMMITTED transition**: at baseline the designated path is asserted absent at HEAD (its token
  is `<RFC3339nano>-<nonce>`, so the path is unique and absent by construction — the assertion is the
  belt), and the gate requires it to become committed AND ≥ `minHandoffBytes`. Anything else
  (ls-tree empty, unborn HEAD, a git error) → not-yet-durable → keep polling → ABORT on timeout
  (fail-closed: it can never FALSE-PASS). Injectable as
  `HandoffDurable(cwd, designatedPath string, minBytes int) (bool, error)`. NOTE: the confirmed
  delivery of the handoff TURN proves only that the turn was ACCEPTED (the composer cleared / the
  spinner appeared), NOT that the commit happened — this durable-blob transition is the SOLE completion
  authority; the close never fires on "the instruction was accepted."
- **(b) `Idle ∧ ComposerCleared`** — the handoff turn finished AT THE MAIN COMPOSER. `Idle` alone is
  insufficient (a desk paused inside an interactive skill confirmation also reads Idle); conjoining
  `ComposerState == ComposerCleared` (the cursor-located probe, `claude.go:143`) distinguishes "done"
  from "paused awaiting a yes." The non-interactive handoff turn (it ends with "then stop") makes
  `Idle ∧ ComposerCleared` reachable. **`ComposerStateProbe` is REQUIRED for recycle-capability** (it is
  in the minimum-harness-bar below) — a surface WITHOUT it REFUSES (consistent with I5 "no silent
  degrade"); the first-draft "degrade to Idle-alone" is REJECTED, because the design's own spine
  argument (Idle-alone can't tell done from paused) proves the degrade unsafe. **`ComposerUndetermined`
  is treated as NOT-cleared at every gate** (fail-closed — keep polling, abort on timeout), never as a
  pass. Claude's `ComposerState` returns `Undetermined` on a cursor/capture error AND in tmux copy/view
  mode (`claude.go:149-161`); recycle DETECTS copy-mode up front and REFUSES with a named message
  ("the pane is in tmux copy-mode; exit it, then retry") rather than letting every `ComposerCleared`
  gate silently degrade to a confusing timeout.

The handoff timeout is generous (a handoff + commit is a multi-minute turn); expiry is an ABORT, not a
force-close.

## Per-phase timeouts (internal defaults, not public flags)

The gates have order-of-magnitude-different latencies: the handoff turn is multi-minute; close→Shell
and fresh-boot→Idle are seconds; the takeover Working-edge is sub-minute. ONE timeout across all of
them would either abort a slow handoff or wait minutes for a close that already failed — so the gates
are bounded by a per-phase `timeouts` struct (`handoff`≈5m, `close`≈30s, `boot`≈60s used by Phase 0 +
Phase 4, `takeover`≈30s best-effort). The struct is injected into `runRecycle` for deterministic tests.
Per the trio (avoid premature public knobs), #157 does NOT expose the four as public flags — the
defaults are tuned from the 6.3 live-latency measurement and re-exposed as flags only if operators
need to tune them. Only `--dry-run` is public. The aggregate worst-case wall-clock is the sum
(`handoff`+`close`+2×`boot`+`takeover` ≈ 7.5m with the defaults) — the runbook states this ceiling so
an operator knows when the command itself (not a gate) is wedged.

## Idempotency across a crash / re-run (the relaunch generation marker)

A recycle that crashes after Phase-3 relaunch but before/during Phase-4 takeover must not, on re-run,
double-deliver the takeover (re-execution risk) nor re-close the fresh session. Phase 3 stamps
`@flotilla_recycle_gen=<token>` where the token is THIS run's UNIQUE `<RFC3339nano>-<nonce>` (the same
token that names the designated handoff path); Phase 4 re-reads it immediately before the single
takeover delivery and proceeds ONLY if it still equals this run's token. Because the token is unique
per run, a STALE gen left by a PRIOR completed recycle can never equal this run's token, so it cannot
false-match — and since Phases 2→4 run under one held lock that auto-releases on crash, no OTHER
recycle can re-stamp between this run's Phase 3 and Phase 4 (the gen check is the belt to the lock's
suspenders). The takeover is therefore delivered **at-most-once per relaunch generation**. Mid-pipeline
auto-resume is OUT of scope (a crashed recycle is recovered by the operator re-running, safe because
the lock auto-releases and the absent→committed handoff gate + the unique gen token prevent a
false-pass / double-takeover); `--dry-run` is the runbook's recommended first step.

## Reuse, not reinvention

Phase 3 is literally `runResume`'s `ResolveUnique + StateShell → RespawnPane → ReadMarker` branch.
`runRecycle` calls the same injected ops (`respawn`, `readMarker`, `tag`, `resolve`) that `resume`
uses; `recycleOps` is a superset of `resumeOps` plus the close/handoff-turn/takeover-turn/durable/
composer/gen/deliver/selfHeal hooks. Both lifecycle commands share one hardened relaunch+marker path,
and both now take the same `AcquirePaneTxn` lock (a `cmdResume` change in this PR) so they cannot
interleave on a pane.

## Coordination protocol — remote desks parlay via message (a flotilla finding)

This parlay surfaced the rule the hard way: the primary XO (a REMOTE XO over the relay) could not answer an
in-pane `AskUserQuestion` — the relay delivers keystrokes that navigate the menu, not select an option
(the panel-block class, #156). Recycle bakes this in: the imperative takeover turn (Phase 4) TELLS the
fresh session it is remote-driven and to surface any clarification via a flotilla message
(`flotilla notify` / a channel message), NEVER an interactive in-pane prompt. The recycle command
itself emits all status to its own stdout/log (a side channel), never into the desk's composer
(`agent-control-notices-to-side-channel`). This is a generalizable flotilla coordination invariant,
not circumstantial deployment config.

**Operating model + outcome feedback (the trio's operability fix).** `flotilla recycle <desk>` is a
shell command the XO runs IN A PANE IT CONTROLS, so the XO reads the phase-by-phase stdout there — that
is the feedback path (status does NOT go into the recycled desk's composer). Because the pipeline can
run for minutes, the command ALSO writes a host-local `~/.flotilla/<desk>/last-recycle.json` on
completion/abort (outcome + the designated handoff path + the exact recovery command for an abort), so
the outcome survives the process and a relay outage and a cold-pickup XO can read it. The write is
ATOMIC (write-temp + rename) so a back-to-back recycle that races the write never reads a torn file. It
does not push into the relay (no new outbound coupling); the runbook documents that the XO triggers
recycle from its own pane and reads the result there + the status file.

## Cross-harness READINESS for #158 (built-in, not exercised)

The spec wording is **cross-harness-READY**, not "cross-harness-CAPABLE": #157 BUILDS the seams (an
arbitrary launch recipe, a harness-agnostic markdown artifact, the per-driver `RecycleBridge`) but the
only harness that meets the recycle-capable bar today is Claude Code. The strategist's caution is
honored: "the markdown bridge already works — this session is proof" is a SAME-HARNESS claim only and
is NOT evidence for the cross-harness pillar; the spec does not stand it as such.

- The relaunch already targets an arbitrary recipe — no claude hard-coding (verified in `resume.go`).
- The handoff ARTIFACT is markdown — harness-agnostic by construction.
- `RecycleBridge` is per-driver, so a grok bridge (its handoffs convention + a plain takeover turn) is
  a #158 addition behind the same interface, not a retrofit.
- **Capability-parity is a #158 gate, surfaced not silently degraded:** before the federated XO's
  cutover, #158 confirms Grok's harness supports subagents / parallel-review / git-PR / MCP (the
  federated XO runs multi-agent reviews and owns a high-consequence system's approval-sensitive order path). If it genuinely
  cannot, that is an operator-facing finding, not a quiet downgrade.

## Alternatives considered (and rejected)

- **Invoke the bare `/handoff` / `/takeover` skills** — REJECTED (the root finding): both are
  human-interactive and would deadlock a remote-driven recycle (confirmation pause; "shall I start?").
  Recycle injects non-interactive recycle-specific turns that produce the same artifacts.
- **SIGTERM / RespawnPane-kill as the close** (fork 1 alts) — a TUI may not flush its session store on
  a signal/kill; not the graceful exit #157 asks for. Per-driver `Close` is the surface's own clean
  path; the kill is the handoff-gated fallback only.
- **Verify-only or two-command recycle** (fork 2 alts) — both put the safety-critical
  handoff-before-close ordering in a human's hands; the whole value of a primitive is code-enforcing it.
- **A structured handoff schema now** (fork 3 alt B) — premature for #157; the markdown bridge works
  same-harness. Revisit if #158 shows freeform doesn't transfer cross-harness.
- **mtime + whole-tree-clean as the durability gate** (first-draft) — REJECTED: mtime is clock/NFS-
  fragile and whole-tree-clean was an abort-forever footgun. **And "committed-at-HEAD-now"** (second
  draft) — REJECTED: a pre-existing committed blob at the path would false-pass. The
  designated-path + ABSENT→COMMITTED-transition + ls-tree-presence check is exact, clock/NFS-
  independent, gitignore-proof (force-add), footgun-free, AND stale-proof.
- **Hold the pane-txn lock across the WHOLE pipeline** (this rework's own first draft) — REJECTED: the
  multi-minute handoff phase under the lock would starve operator `send`/`voice`/`dash` and the
  heartbeat-clock tick to the pane for minutes (a real delivery-starvation cost; NOT an XO-down-alert
  trigger — that earlier justification was an unverified mechanism, dropped). The lock is scoped to the
  seconds-scale Phases 2→4, with a re-verify-under-lock before the close to close the post-handoff TOCTOU.
- **A non-git durability path** — REJECTED for #157: REQUIRE a git work-tree (the on-disk-only path has
  no atomic-commit immutability, so a mid-write partial file ≥ minBytes could false-pass). A non-git cwd
  is refused cleanly.
- **Recycle decides WHEN to recycle** — out of scope; that is the XO's judgment (#157 is the mechanism).

## Risks

- **HIGH — recycle ends a live session.** Mitigated by the fail-closed Phase-1 gate (no close without a
  durable absent→committed handoff), the under-lock re-verify of that gate before the close, the
  Phase-2 `Idle ∧ ComposerCleared`-before-close guard + the correctness-critical close→Shell
  confirmation (retry-on-Unknown; `RespawnPane` is always `-k`, so this gate is the ONLY thing
  preventing a kill of a live session), the Phases-2→4 pane-txn lock (no recycle×resume interleave; the
  relaunch atomic), the unique relaunch-generation marker (at-most-once takeover), reuse of the hardened
  resume relaunch/marker path, and a mandatory live claude→claude end-to-end validation on one real desk
  before use in anger.
- **The exit keystroke is wrong on an unverified harness** — the close→Shell gate catches it (ABORT
  with the state-aware recovery copy, never a force-kill of a live session); the claude `/exit`→shell
  behaviour is VERIFIED in 6.3 BEFORE the claude `Close` is trusted (task ordering); grok returns
  `ErrNoGracefulClose` explicitly until #158 verifies its `/exit`.
- **The desk does not cooperate with the handoff turn** (ignores it, errors) — the Phase-1 gate times
  out and ABORTS (desk keeps running). The XO sees the abort and intervenes; no loss.
- **A long handoff turn vs the timeout** — the per-phase handoff timeout is generous and configurable;
  an abort is recoverable (re-run, or `flotilla resume` for a dead desk), a premature force-close would
  not be — so the gates err toward abort.
