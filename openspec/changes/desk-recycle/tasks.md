# Tasks — desk-recycle (TDD)

Load-bearing invariants (assert these, every path):
- **(I1) at-most-once handoff-ARTIFACT-loss** — the CLOSE is reached ONLY after the handoff is durably
  confirmed (the designated blob went ABSENT→COMMITTED at HEAD ∧ non-trivial ∧ `Idle ∧ ComposerCleared`,
  RE-VERIFIED under the lock); any un-confirmation ABORTS with the desk still running and nothing
  closed. (The gate guarantees the ARTIFACT lands, NOT its quality; quality is the desk's job. The gate's
  only quality proxy is `minHandoffBytes` — a floor that rejects an empty/error stub, NOT a truncation
  detector; the 6.3 cold-test validates substantive completeness. "Loss" ≠ availability: a Phase-2
  close-without-Shell may leave a dead OR live-uncertain desk — the artifact is durable; the state-aware
  abort copy names the recovery.)
- **(I2) no relaunch on a possibly-live session** — `RespawnPane` is ALWAYS `-k`, so the relaunch is
  reached ONLY after the close confirms `Shell` (RETRYING on a transient `Unknown` glitch). This gate is
  CORRECTNESS-CRITICAL; a close that does not confirm Shell ABORTS with a state-aware recovery copy.
- **(I3) marker survival** — the relaunch reuses the pane id so `@flotilla_agent` survives; the
  read-back confirms it (reuse resume's path); a mismatch ABORTS with the live-fresh-desk recovery copy.
- **(I4) takeover exactly once per relaunch generation** — `TakeoverTurn` is delivered once on the
  fresh `Idle ∧ ComposerCleared` pane, gated on `@flotilla_recycle_gen` still equalling this run's
  UNIQUE token, never on a non-recycled path; status goes to a side channel, never the composer.
- **(I5) no silent degrade** — a surface without `RecycleBridge` OR without `ComposerStateProbe`
  REFUSES cleanly; a surface returning `ErrNoGracefulClose` uses the handoff-gated kill fallback, logged.
- **(I6) only inject when idle** — Phase 0 confirms `Idle ∧ ComposerCleared` BEFORE the first injection;
  `ComposerUndetermined` (incl. tmux copy-mode) is NOT cleared (fail-closed); copy-mode is refused up front.
- **(I7) the irreversible span is serialized** — Phases 2→4 (close→relaunch→takeover) hold ONE
  `AcquirePaneTxn` lock; `resume` takes the same lock in its `ResolveUnique` branch; Phases 0–1 run
  lockless (the discrete handoff delivery self-locks), then the lock is acquired + the Phase-1 gate
  RE-VERIFIED under it before the close (closing the post-handoff TOCTOU).

## 1. SPI: `Close` on `Driver` + `ErrNoGracefulClose` (`internal/surface/surface.go`)

- [x] 1.0 LIVE-PREREQ (do FIRST, part of 6.3's keystroke characterization): characterize the FULL
  claude `/exit` INTERACTION, not just its outcome — (a) does post-`/exit` `pane_current_command` land
  in `knownShells` (`liveness.go:11`)? and (b) is `/exit` SINGLE-KEYSTROKE-TERMINAL or does it open a
  confirm sub-prompt ("Press Enter to exit")? If it needs a confirmation keystroke, `Close` (1.2) must
  issue it (else the single-injection `Close` would NEVER reach Shell and every recycle would abort).
  `/clear` resets context WITHIN a running session and does NOT prove process-termination-to-shell, so
  `/exit`→shell must be MEASURED, not assumed, before the claude `Close` keystroke (1.2) is trusted.
- [x] 1.1 IMPL: add `Close(pane string) error` to `Driver`; add the `ErrNoGracefulClose` sentinel
  (mirrors `ErrRestartRequired`). The doc comment states the caller ensures the pane is idle at the main
  composer first (InjectSlash's contract).
- [x] 1.2 IMPL: each driver's `Close` via slash-keys (NOT bracketed-paste): claude `/exit` (gated on 1.0);
  grok returns `ErrNoGracefulClose` EXPLICITLY (its `/exit` is unverified — #158); aider `/exit`;
  opencode/cursor `ErrNoGracefulClose` unless a clean quit is confirmed. Injectable close func per driver.
- [x] 1.3 TEST: claude `Close` issues `/exit` via the slash-keys seam; grok/cursor return `ErrNoGracefulClose`.

## 2. SPI: the optional `RecycleBridge` + the routing helper (`internal/surface`)

- [x] 2.1 IMPL: `RecycleBridge` interface — `HandoffPath(cwd, token string) string`,
  `HandoffTurn(designatedPath string) string`, `TakeoverTurn(designatedPath string) string`.
- [x] 2.2 IMPL (claude reference): `HandoffPath` → `<cwd>/.claude/handoffs/recycle-<token>.md` (the token
  leads with a sortable timestamp, so the file is dated + unique);
  `HandoffTurn` → the NON-INTERACTIVE self-committing instruction (write a handoff per the /handoff
  FORMAT to the path, `git add -f` + commit to the current branch, do NOT ask to confirm — remote-driven
  — then stop); `TakeoverTurn` → the IMPERATIVE instruction (read the path, take over, BEGIN WORK
  IMMEDIATELY, do not ask whether to start, you are remote-driven — surface clarifications via a flotilla
  message, never an in-pane interactive prompt). Neither invokes the interactive skill.
- [x] 2.3 TEST: `HandoffPath` embeds the token + `.claude/handoffs/`; `HandoffTurn` names the exact path,
  instructs `git add -f`+commit + "do not ask to confirm"/"remote-driven"; `TakeoverTurn` names the exact
  path + "begin work immediately"/"do not ask whether to start"/"surface clarifications via a flotilla
  message". Pure-text (no tmux); a path with spaces embeds verbatim.
- [x] 2.4 IMPL: `surface.RecycleSupport(d) (RecycleBridge, bool)` type-assert helper; the command also
  requires the driver to implement `ComposerStateProbe` (the `Idle ∧ ComposerCleared` gates need it).
- [x] 2.5 TEST: `RecycleSupport` returns false for a driver without the bridge; a driver with the bridge
  but no `ComposerStateProbe` also fails the recycle-capable check → the command refusal (5.x) fires.

## 3. deliver: the durability signal + the generation marker (`internal/deliver`)

- [x] 3.1 IMPL: `HandoffDurable(cwd, designatedPath string, minBytes int) (bool, error)` — resolve the
  git root via `git -C cwd rev-parse --show-toplevel` (exit≠0 → non-git → the caller REFUSES, see 5.2);
  committed-ness via `git -C cwd ls-tree HEAD -- <relpath>` (NON-EMPTY stdout = committed at HEAD; empty
  / unborn-HEAD / any error = not-yet-durable → keep polling). When committed, the blob size (`git -C cwd
  cat-file -s` of the ls-tree object id, or `git show | wc`) must be ≥ minBytes. NEVER false-pass.
  (Exit-code discrimination is impossible — `git show HEAD:<path>` returns 128 for BOTH unborn-HEAD and
  committed-absent; ls-tree discriminates by output PRESENCE. Verified empirically.) Injectable.
  `minHandoffBytes` default = a conservative interim floor (≈200 bytes — rejects an empty/error stub,
  never a real handoff); NEVER 0; tuned UP from the 6.3 measurement (the floor is fixed-safe, the tuned
  value is empirical).
- [x] 3.2 IMPL: `HandoffAbsentAtHead(cwd, designatedPath) (bool, error)` — baseline assertion (ls-tree
  empty) so the gate confirms an ABSENT→COMMITTED transition (no pre-existing-blob false-pass).
- [x] 3.3 TEST: in a temp git repo (token/path computed at test time — NO hardcoded dates): absent at
  baseline → AbsentAtHead true; after committing a non-trivial blob at the path → Durable true; a
  committed-but-trivial (< minBytes) blob → false; an uncommitted (worktree-only) handoff → false; an
  unborn HEAD → false (keep polling); a non-git cwd → the rev-parse failure surfaces so the caller refuses.
- [x] 3.4 IMPL: `StampRecycleGen(target, token)` (set `@flotilla_recycle_gen`, read-back-verified à la
  `TagPane`) and `ReadRecycleGen(target) (string, error)`. The token (built in `recycleToken`) is a
  colon-free high-resolution timestamp (`20060102T150405.000000000`, filesystem-safe) + a `crypto/rand`
  nonce (a timestamp alone is not collision-free; the nonce is the uniqueness guarantor for both the
  designated path and the gen marker).
- [x] 3.5 NOTE: `StampRecycleGen`/`ReadRecycleGen`/`PaneID` are thin tmux wrappers with no pure core —
  they are LIVE-EXERCISED in 6.3 (and via the self-recycle drill), exactly like `TagPane`/`ReadMarker`/
  `Send`/`RespawnPane`, which the codebase also does not unit-test (spinning a test session on the default
  tmux socket would collide with the live fleet on the host). The COMPARISON LOGIC they back
  (`samePaneAsSelf`) IS unit-tested as a pure helper at the command layer (4.1).
- [x] 3.6 IMPL: `PaneID(target string) (string, error)` — `tmux display-message -p -t <target>
  '#{pane_id}'`, trimmed (the stable `%N` identity, unlike `session:window.pane` which renumbers). No
  such helper exists today; it backs the canonical self-recycle comparison. Injectable.

## 4. The fail-closed core: `runRecycle(ops, plan)` (`cmd/flotilla/recycle.go`)

- [x] 4.1 TEST (resolve + self-recycle + git-tree + copy-mode): `ResolveNone` → "nothing to recycle";
  `ResolveAmbiguous` → "mis-tagged"; the target identifies the command's own pane → REFUSE
  (self-recycle would `/exit` the command's own pane). The self-recycle test MUST exercise the
  CANONICAL comparison: feed a resolved target in production `session:window.pane` shape whose
  `#{pane_id}` resolves to a `%N`, and a `$TMUX_PANE` `%N` — equal → refuse, different → proceed, empty
  `$TMUX_PANE` → proceed (so a `session:win.pane`-vs-`%N` string compare cannot green a dead guard).
  Also: a non-git cwd → REFUSE; a copy-mode pane → REFUSE; none inject/close anything.
- [x] 4.2 TEST (I6 — Phase 0 idle precondition): a pane that never settles `Idle ∧ ComposerCleared`
  (including `ComposerUndetermined` reads) within `bootTimeout` → ABORT; `deliver(handoffTurn)` NEVER called.
- [x] 4.3 TEST (I1 — handoff gate ABORT): the absent→committed durable signal never appears (or
  `Idle ∧ ComposerCleared` never holds) within `handoffTimeout` → ABORT; lock never acquired;
  `closeSurface`/`respawn`/`deliver(takeoverTurn)` NEVER called.
- [x] 4.4 TEST (I1+I7 — re-verify under lock): the handoff gate passes UNLOCKED, then a fresh under-lock
  read shows `Working` (a turn started in the unlocked window) or the blob regressed → ABORT (release
  lock), `closeSurface` NEVER called.
- [x] 4.5 TEST (I1 — gate PASS): the blob is committed-non-trivial AND `Idle ∧ ComposerCleared` holds at
  the gate AND re-verifies under the lock → the pipeline proceeds to the close.
- [x] 4.6 TEST (I2 — close gate ABORT + retry-on-Unknown): `Close` returns nil; the pane reads `Unknown`
  twice (transient) → the poll RETRIES (no early abort); never reaching `Shell` within `closeTimeout` →
  ABORT the relaunch (`respawn` NOT called) with the STATE-AWARE copy ("may still be live … flotilla
  resume <desk> --force").
- [x] 4.7 TEST (close fallback): `Close` returns `ErrNoGracefulClose` → handoff-gated kill fallback runs
  (respawn-kill), logged; still gated on the close→Shell confirmation before relaunch.
- [x] 4.8 TEST (I3 — relaunch + marker + gen stamp): after a confirmed Shell, `respawn` runs; the marker
  read-back must equal the key (mismatch → ABORT naming `flotilla send <desk> 'read <path> and take over'`);
  `StampRecycleGen` is called with the run's unique token.
- [x] 4.9 TEST (I4 — takeover once, gen-gated): on success, `deliver(takeoverTurn)` is called EXACTLY once
  with the designated path, only after the fresh pane reads `Idle ∧ ComposerCleared` AND
  `ReadRecycleGen == token`; a changed gen → ABORT without injecting; never on any abort path. A `Working`
  edge is polled best-effort (its absence logs, does not fail).
- [x] 4.10 IMPL: `runRecycle` wiring Phases 0–4: lockless Phase 0/1, acquire `AcquirePaneTxn` + re-verify,
  locked Phases 2→4, release + write the `last-recycle.json` outcome. Per-phase bounded polls from the
  injected `timeouts` struct + injected sleep/now; fail-closed at each gate; all turns via the injected
  confirmed-delivery `deliver`. Reuses resume's `respawn`/`readMarker`/`tag`/`resolve` ops.

## 5. The command: `flotilla recycle <desk>` (`cmd/flotilla/recycle.go` + `main.go`) + the resume lock

- [x] 5.1 IMPL: arg parse (agent positional before/after flags, à la `parseResumeArgs`): `--roster`,
  `--launch`, `--dry-run`. The per-phase timeouts are INTERNAL defaults (a `timeouts` struct, NOT public
  flags — re-expose post-6.3 only if operators must tune). Pure → unit-tested ordering.
- [x] 5.2 IMPL: load roster + recipe + driver (reuse resume's resolution); REFUSE cleanly (I5) if the
  driver lacks `RecycleBridge` OR `ComposerStateProbe` (name the surface), if the target pane is the
  command's OWN pane (self-recycle — would `/exit` the running command), if the cwd is non-git, or if
  the pane is in tmux copy-mode (named messages, not a confusing timeout). Self-recycle uses an
  injectable `samePaneAsSelf(target, tmuxPane string) (bool, error)` that canonicalizes via `tmux
  display-message -p -t <target> '#{pane_id}'` (a `%N`) and compares to `$TMUX_PANE` (also `%N`) — NOT
  a literal `target == $TMUX_PANE` (`session:window.pane` ≠ `%N`, a dead guard); empty `$TMUX_PANE` ⇒
  not-self. FAIL-CLOSED: a `PaneID` error (e.g. the target pane died between resolve and this check) is
  SURFACED, not swallowed as "not self" — the wiring must not drop `samePaneAsSelf`'s error.
- [x] 5.3 IMPL: `--dry-run` prints the resolved plan (pane, recipe.Launch, the designated handoff path,
  the `HandoffTurn`/`TakeoverTurn` texts) and exits WITHOUT acting or acquiring the lock (advisory — the
  real run re-resolves under the lock).
- [x] 5.4 IMPL: wire `recycleOps` (real fns: `surface.Confirm.Submit` bound to the driver as `deliver`;
  `HandoffDurable`/`HandoffAbsentAtHead`; `StampRecycleGen`/`ReadRecycleGen`; the driver's `ComposerState`;
  `selfHeal` when `SelfHealEnabled()`) → `runRecycle` (which acquires `AcquirePaneTxn` for Phases 2→4) →
  print the phase-by-phase result to stdout (side channel) + write `~/.flotilla/<desk>/last-recycle.json`
  ATOMICALLY (write-temp + rename, so a racing back-to-back recycle never reads a torn file).
- [x] 5.5 IMPL (I7): `cmdResume` acquires the SAME `AcquirePaneTxn(target)` lock around its
  resolve→respawn→tag transaction in the `ResolveUnique` branch (so recycle×resume cannot interleave on a
  pane). Keep `runResume` pure; take the lock in `cmdResume` after resolving the target.
- [x] 5.6 TEST: arg-ordering; the no-bridge / no-probe / non-git / copy-mode refusals; the
  dry-run-no-act-no-lock path.
- [x] 5.7 IMPL: register `recycle` in `main.go`'s command dispatch + usage.

## 6. Docs + live validation

- [x] 6.1 Update `docs/watch-runbook.md`: the recycle procedure (the XO runs it in a pane it controls and
  reads the stdout + the `last-recycle.json`), the fail-closed gates, the abort-leaves-running guarantee,
  the STATE-AWARE recovery commands (dead desk → `flotilla resume <desk>`; uncertain → investigate / `--force`;
  live-fresh-desk-marker-mismatch → `flotilla send <desk> 'read <path> and take over'`; wedged overlay →
  clear it or `FLOTILLA_SELF_HEAL=1` and re-run), `--dry-run` as the recommended first step, and the
  remote-parlay-via-message protocol.
- [x] 6.2 `openspec validate desk-recycle --strict`.
- [~] 6.3a (DONE 2026-06-23) `/exit` keystroke + fleet pane characterization: `/exit` is
  SINGLE-KEYSTROKE-TERMINAL (no confirm sub-prompt). FOUND: the live fleet runs claude as the pane's
  DIRECT process (parent = tmux server, no shell behind it) with `remain-on-exit` OFF, so `/exit` CLOSES
  the pane — it never becomes a `knownShells` shell. The first draft's close→`Assess==Shell` gate would
  ABORT on every real desk. FIXED (verified live): set `remain-on-exit on` → `/exit` leaves a DEAD pane
  (`pane_dead=1`, marker survives) → confirm via `pane_dead` OR Shell → `RespawnPane -k` revives it
  (marker survived) → restore `remain-on-exit off`. Landed in the close-confirm fast-follow.
- [ ] 6.3 **LIVE claude→claude end-to-end validation on ONE real desk** (the acceptance gate), a DRILL
  MATRIX, not happy-path only: (1) the clean recycle — designated handoff written + committed (absent→
  committed), graceful `/exit` reaching `knownShells` (the 1.0 characterization), relaunched fresh, took
  over and BEGAN WORKING, marker/relay intact; (2) handoff-uncooperative → ABORT leaves the desk running;
  (3) the dead-desk recovery copy executed verbatim; (4) crash-mid-pipeline re-run (idempotency — no
  double-takeover) — against a THROWAWAY desk, not a real one; (5) a partial-compliance handoff (good
  content, slightly-wrong path) → the timeout abort behaviour; (6) the self-recycle refusal (run
  `recycle` on the command's own desk → refused, not a killed pipeline). Cold-test the live artifact +
  the turns against a real agent. Measure the gate latencies to tune the internal `timeouts` defaults +
  set `minHandoffBytes` UP from the ≈200-byte interim floor using a real handoff size.
- [ ] 6.4 `/systems-review` + STORM on the design (this revision) and again on the impl diff — iterate
  until clean. PR → hydra-ops (no-self-merge). #158 (claude→grok) is gated on 6.3 passing.
