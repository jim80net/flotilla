# Tasks — desk-recycle (TDD)

Load-bearing invariants (assert these, every path):
- **(I1) at-most-once-context-loss** — the CLOSE is reached ONLY after the handoff is durably
  confirmed (fresh artifact ∧ Idle ∧ committed); any un-confirmation within the timeout ABORTS with the
  desk still running and nothing closed.
- **(I2) no relaunch on a live session** — the relaunch is reached ONLY after the close confirms the
  pane is a Shell; a close that does not confirm Shell ABORTS the relaunch.
- **(I3) marker survival** — the relaunch reuses the pane id so `@flotilla_agent` survives; the
  read-back confirms it (reuse resume's path).
- **(I4) takeover exactly once** — `InjectTakeover` is called once on the fresh Idle pane, never on a
  non-recycled path; status goes to a side channel, never the composer.
- **(I5) no silent degrade** — a surface without `RecycleBridge` REFUSES cleanly; a surface without a
  graceful close uses the handoff-gated kill fallback, logged.

## 1. SPI: `Close` on `Driver` + `ErrNoGracefulClose` (`internal/surface/surface.go`)

- [ ] 1.1 IMPL: add `Close(pane string) error` to the `Driver` interface; add the
  `ErrNoGracefulClose` sentinel (a surface with no clean in-session exit returns it).
- [ ] 1.2 IMPL: each driver implements `Close` via the slash-keys primitive (NOT bracketed-paste):
  claude `/exit`, grok `/exit` (confirm in the live grok slash menu), aider `/exit`, opencode its quit
  (or `ErrNoGracefulClose` if no clean exit is confirmed). Injectable close func per driver for tests.
- [ ] 1.3 TEST: each driver's `Close` issues the surface's documented exit keystrokes (the injected
  close func receives the expected command); a no-clean-exit surface returns `ErrNoGracefulClose`.

## 2. SPI: the optional `RecycleBridge` + the routing helper (`internal/surface`)

- [ ] 2.1 IMPL: `RecycleBridge` interface (`InjectHandoff(pane)`, `InjectTakeover(pane, handoffPath)`).
- [ ] 2.2 IMPL (claude reference): `InjectHandoff` → slash-keys `/handoff`; `InjectTakeover` →
  slash-keys `/takeover <handoffPath>`. Injectable seams for tests.
- [ ] 2.3 TEST: the claude bridge injects `/handoff`; `/takeover <path>` is built with the path
  exactly (no shell-quoting surprises on a path with spaces — quote/escape as the harness expects).
- [ ] 2.4 IMPL: a `surface.RecycleSupport(d) (RecycleBridge, bool)` type-assert helper so the command
  refuses cleanly when the bridge is absent.
- [ ] 2.5 TEST: `RecycleSupport` returns false for a driver without the bridge (e.g. a stub) → the
  command-layer refusal path (5.x) fires.

## 3. deliver: the completion-gate signal sources (`internal/deliver`)

- [ ] 3.1 IMPL: `HandoffFresh(handoffsDir string, since time.Time) (path string, ok bool, err error)`
  — the newest file under the dir with mtime strictly after `since`; ok=false if none. Injectable.
- [ ] 3.2 TEST: a file written after `since` → ok+path; only stale files → not-ok; missing dir → a
  clear err (fail-closed, not ok).
- [ ] 3.3 IMPL: `TreeClean(cwd string) (bool, err error)` — in a git work-tree: no uncommitted changes
  to TRACKED files (untracked ignored); a non-git cwd → (true, nil) [gate (c) skipped]; a git error →
  (false, err) [fail-closed].
- [ ] 3.4 TEST: a clean tree → true; a tracked-file edit → false; an untracked scratch file only →
  true; a git failure → false+err.

## 4. The fail-closed core: `runRecycle(ops, plan)` (`cmd/flotilla/recycle.go`)

- [ ] 4.1 TEST (resolve): `ResolveNone` → "nothing to recycle" error; `ResolveAmbiguous` → "mis-tagged"
  error; neither closes anything.
- [ ] 4.2 TEST (I1 — handoff gate ABORT): a gate where the fresh-handoff signal never appears (or Idle
  never returns, or tree never clean) within the attempt cap → `runRecycle` returns an ABORT error,
  and `closeSurface`/`respawn`/`injectTakeover` are NEVER called (desk untouched).
- [ ] 4.3 TEST (I1 — handoff gate PASS): once all three signals hold, the pipeline proceeds to close.
- [ ] 4.4 TEST (I2 — close gate ABORT): `Close` returns nil but the pane never reaches `Shell` within
  the cap → ABORT the relaunch (`respawn` NOT called); a clear error.
- [ ] 4.5 TEST (close fallback): `Close` returns `ErrNoGracefulClose` → the handoff-gated kill fallback
  runs (respawn-kill), logged; still gated on Phase-1 success.
- [ ] 4.6 TEST (I3 — relaunch + marker): after a confirmed Shell, `respawn` runs and the marker
  read-back must equal the key (mismatch → error, mirroring resume).
- [ ] 4.7 TEST (I4 — takeover once): on the success path, `injectTakeover` is called EXACTLY once with
  the handoff path detected in Phase 1, only after the fresh pane reads `Idle`; never on any abort path.
- [ ] 4.8 IMPL: `runRecycle` wiring the four phases with bounded-poll gates (attempt cap + injected
  sleep/now), fail-closed at each gate. Reuses resume's `respawn`/`readMarker`/`tag`/`resolve` ops.

## 5. The command: `flotilla recycle <desk>` (`cmd/flotilla/recycle.go` + `main.go`)

- [ ] 5.1 IMPL: arg parse (agent positional before/after flags, à la `parseResumeArgs`): `--roster`,
  `--launch`, `--timeout`, `--dry-run`. Pure → unit-tested ordering.
- [ ] 5.2 IMPL: load roster + recipe + driver (reuse resume's resolution); refuse cleanly if the driver
  lacks `RecycleBridge` (I5) — name the surface and that it is not recycle-capable.
- [ ] 5.3 IMPL: `--dry-run` prints the resolved plan (pane, recipe.Launch, the `/handoff` and
  `/takeover <path-template>` turns) and exits WITHOUT acting.
- [ ] 5.4 IMPL: wire `recycleOps` (real deliver/surface fns) → `runRecycle`; print the phase-by-phase
  result to stdout (side channel), never the desk composer.
- [ ] 5.5 TEST: arg-ordering + the no-bridge refusal + the dry-run-no-act path.
- [ ] 5.6 IMPL: register `recycle` in `main.go`'s command dispatch + usage.

## 6. Docs + live validation

- [ ] 6.1 Update `docs/watch-runbook.md`: the recycle procedure (when the XO triggers it, the
  fail-closed gates, the abort-leaves-running guarantee), the close→relaunch→takeover flow, and the
  **remote-parlay-via-message** protocol (never an in-pane interactive menu for a remote desk).
- [ ] 6.2 `openspec validate desk-recycle --strict`.
- [ ] 6.3 **LIVE claude→claude end-to-end validation on ONE real desk** (the acceptance gate): recycle
  a low-stakes desk — confirm a handoff was written + committed, the session closed gracefully (not a
  kill), relaunched fresh, took over from the handoff, and flotilla reachability (the marker / relay)
  is intact. Cold-test the live artifact, not unit stubs. Measure the gate latencies to tune the
  default `--timeout`. This is #157's "verified end-to-end on one real desk" acceptance.
- [ ] 6.4 `/systems-review` + STORM on the design (this change) and again on the impl diff — iterate
  until clean. PR → hydra-ops (no-self-merge). #158 (claude→grok) is gated on 6.3 passing.
