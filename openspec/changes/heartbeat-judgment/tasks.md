# Tasks — heartbeat-judgment (#189, refines #183, TDD)

Implementation is fresh-context-per-task-group (standard flow). Each group is self-contained + TDD; the
detector test pattern (`internal/watch/detector_heartbeat_g4_test.go` §9 matrix + the backlog gate fixture
`internal/watch/detector_backlog_test.go`) is the template. Load-bearing invariants (assert across paths):
the #184 approval-sensitive / XO HARD gate is NEVER overridden by the judgment; the judgment can only
SUPPRESS, never resurrect, a beat; byte-inert on BOTH axes (`HeartbeatEnabled` nil AND `HeartbeatWarranted`
nil ⇒ #183 exactly); a not-warranted idle tick is cap-neutral AND cadence-neutral (like a settled tick); the
judgment fails toward WARRANTED (absent/unreadable/malformed ⇒ keep beating); a wedge still escalates (a
wedged desk has live work, so it stays warranted).

## 1. The `[awaiting-auth]` status class (the authorizations ledger)
- [x] 1.1 TEST FIRST (`internal/backlog/backlog_test.go`): `classify("[awaiting-auth] flip the feed")`
  returns a new `clsAwaitingAuth`; `Parse` counts it in a new `Status.AwaitingAuth` field; an
  `[awaiting-auth]` item is NOT placed in `Unblocked` (not actionable) and NOT counted in `Blocked` (its
  own ledger). Case-insensitive marker match. Regression: a backlog with only `[in-flight]/[next]/[blocked]/
  [done]` items parses byte-identically (AwaitingAuth == 0, all other counts unchanged).
- [x] 1.2 Implement: add `clsAwaitingAuth` to the `cls` enum + the `"awaiting-auth"` case in `classify`
  (carved from the `blocked`/`needs-attention` case — match the EXACT token `awaiting-auth`, case-insensitive
  on the word), the `AwaitingAuth int` field on `Status`, and the `case clsAwaitingAuth: st.AwaitingAuth++`
  arm in `Parse`. Keep `Unblocked` unchanged (awaiting-auth is settle-neutral, like blocked). Update the
  package-doc item-line contract comment.
- [x] 1.3 Dash surfaceability (`internal/dash/readmodel.go`): TEST FIRST — `BuildHistory` projects the new
  `AwaitingAuth` count into `BacklogInfo` (a backlog with `[awaiting-auth]` items reports a non-zero
  `awaiting_auth` in the read-model, separate from `blocked`; a backlog with none reports zero — backward
  compatible). Implement: add `AwaitingAuth int json:"awaiting_auth"` to `BacklogInfo` (lines 256-265) and
  thread `st.AwaitingAuth` in `BuildHistory` (lines 279-286), so the authorizations ledger is visible in the
  dash (not collapsed into blocked) — the surfaceability rationale for splitting the class out. (The
  operator-RESURFACING of those items is the separate, out-of-scope backstop in issue #193.)

## 2. The roster judgment resolver (`HeartbeatWarranted`, I/O-free, composes the HARD gate)
- [x] 2.1 TEST FIRST (`internal/roster/heartbeat_test.go`): `Config.HeartbeatWarranted(name, st backlog.Status)
  bool`. Cases: HARD gate FIRST — an approval-sensitive desk / explicit `heartbeat:false` / the primary XO
  returns false EVEN with `st.Unblocked` non-empty (the judgment never overrides the HARD gate). An eligible
  desk: `Unblocked` non-empty ⇒ true; `Unblocked` empty AND `Found` true (all `[blocked]`/`[awaiting-auth]`/
  `[done]`) ⇒ false; `Unblocked` empty AND `Found` FALSE (a present-but-sectionless backlog) ⇒ TRUE (the
  `!Found` fail-safe arm — cannot prove no work); a malformed item (in `Unblocked` per the parser's
  fail-safe) ⇒ true. Assert `HeartbeatEnabled` is UNCHANGED by this addition (its existing tests still pass).
- [x] 2.2 Implement `HeartbeatWarranted(name, st)`: return false immediately if `!HeartbeatEnabled(name)`
  (the HARD gate), else `!st.Found || len(st.Unblocked) > 0` (the warrant predicate — the `!Found` arm keeps
  a present-but-sectionless backlog warranted; suppression requires a `Found` backlog with an empty
  actionable set). No I/O — the `Status` is injected. Document that the caller (cmd) supplies the
  per-recipient parsed `Status`.

## 3. The detector `HeartbeatWarranted` seam (last conjunct; default always-warranted)
- [x] 3.1 TEST FIRST (`internal/watch/detector_heartbeat_judgment_test.go`, extending the §9 matrix
  fixture): add a `HeartbeatWarranted func(agent) bool` to `DetectorConfig`. Cases on an Idle, eligible,
  cadence-elapsed, not-settled desk: warranted true ⇒ beat owed (as #183); warranted false ⇒ NO beat owed
  AND no cap accrual AND no cadence reset penalty (treated like a settled tick — assert `deskNoProgress` and
  the beat slice are both unchanged across the not-warranted tick). Seam nil ⇒ always-warranted ⇒ #183
  behavior byte-identical (the regression-lock: re-run a representative #183 case with the seam nil).
- [x] 3.2 TEST: a WEDGE is not masked — a desk with warranted==true that stays idle across capN beats still
  escalates once + stops (the judgment does not interfere with the cap path).
- [x] 3.3 Implement: in `deskHeartbeatLocked`, after the `HeartbeatEnabled(name)` HARD gate (line ~744) and
  the settled/stopped/cadence checks, add the `HeartbeatWarranted(name)` conjunct as the LAST gate before
  appending to `beats`. The conjunct is a PURE lookup against a per-recipient warrant computed OFF `d.mu`
  (the `HeartbeatWarranted func(agent) bool` seam returns an already-decided boolean — it does NOT read or
  parse a backlog file inside the locked decision; the read happens at the cmd wiring, off-lock, two-phase,
  mirroring `synthEligibleLocked`/`runSynthesis`). A not-warranted desk: `continue` BEFORE incrementing
  `deskSinceBeat`/`deskNoProgress` (cap- and cadence-neutral, exactly like the settled branch). Default the
  seam to `func(string) bool { return true }` in `NewDetector` (so an unwired judgment is #183-inert).
- [x] 3.4 TEST (off-mutex invariant — load-bearing): assert that the warrant seam invoked from the under-lock
  decision performs NO backlog file I/O (`os.ReadFile`/`backlog.Parse`) while `d.mu` is held — e.g. the seam
  wired in the test records that it was called with pre-computed data and never touches the filesystem under
  the lock. This locks the detector's off-mutex invariant against a regression that reads a backlog under
  `d.mu` (the invariant synthesis + the mirror honor).

## 4. cmd wiring: per-recipient backlog read into the judgment seam
- [x] 4.1 TEST FIRST (`cmd/flotilla/watch.go` helper test): a `deskWarrantedGate(cfg, rosterDir)` builds the
  `HeartbeatWarranted func(agent) bool` seam, performing the backlog read OFF the detector lock (the seam is
  consulted by the under-lock decision as a pure boolean; the I/O lives HERE). It resolves the recipient's
  OWN backlog (`<rosterDir>/flotilla-<agent>-backlog.md`); when that per-recipient file is ABSENT it returns
  WARRANTED (the missing-ledger fallback — #183 behavior) and SHALL NOT fall back to the shared
  `--backlog-file` (the shared queue is the XO's work, not this desk's; falling back to it would warrant
  every ledger-less desk on a busy fleet). When the per-recipient file is PRESENT it reads FRESH each call,
  `backlog.Parse`s it, and returns `cfg.HeartbeatWarranted(agent, st)`. Fail-safe: an UNREADABLE/torn
  per-recipient backlog ⇒ warranted (true); a present-but-sectionless file ⇒ warranted via the `!Found` arm
  AND alerts ONCE on the edge (mirroring `backlogStatusGate` lines 683-706, the alert-once latch). Cases:
  per-recipient file present with an `[in-flight]` item ⇒ true; present with all items `[blocked]`/
  `[awaiting-auth]`/`[done]` ⇒ false; per-recipient file absent (shared backlog non-empty) ⇒ true via the
  missing-ledger fallback, and the shared backlog is NOT consulted.
- [x] 4.2 Implement `deskWarrantedGate` + wire it as `HeartbeatWarranted` into the
  `NewDetectorWithSynthSidecar` config (alongside `HeartbeatEnabled` at line ~466), OFF the detector lock.
  Keep it ALWAYS wired (the judgment is universal, like the #183 default-ON); the per-recipient read
  self-defaults to warranted when no per-recipient ledger exists (the missing-ledger fallback), so a
  deployment that keeps no per-recipient backlogs is #183-equivalent.

## 5. The desk-continuation prompt: re-trigger-first + ledger-recording contract
- [x] 5.1 TEST FIRST (`cmd/flotilla/watch_prompt_test.go`): the refined `deskContinuationBuiltin` asserts the
  re-trigger-first wording (idle is usually a transient fault → resume the next authorized step), the
  never-sit-idle / opportunistic-work clause, the two-ledger recording instructions (`[blocked]`/
  `[needs-attention]` = open-questions; `[awaiting-auth]` = authorizations), the settle-when-no-actionable
  clause, and the preserved non-authorizing clause (#184 defense-in-depth). Assert the prompt QUOTES the
  EXACT literal `[awaiting-auth]` token the parser accepts (so a desk does not write `[awaiting-authorization]`
  / `[awaiting auth]` and silently break the judgment — the §4 brittleness fix). The `{{settle}}`
  substitution still resolves to the DESK's own marker; a `HEARTBEAT.md` still overrides.
- [x] 5.2 Implement the refined prompt string, quoting the literal `[awaiting-auth]` marker. Keep it within
  the established style (no deployment identifiers, generic roles only — #187 constitution).

## 6. Integration + spec close-out
- [x] 6.0 PREREQUISITE — archive `recursive-desk-heartbeat` (#183) FIRST. This change's `watch` delta
  MODIFIES the requirement "Recursive per-agent desk heartbeat", introduced by the still-unarchived
  `recursive-desk-heartbeat` change; that requirement is NOT yet in the base `openspec/specs/watch/spec.md`
  on main (the #183 code is merged in `origin/main` `f91882f`, but its spec delta is not archived).
  `openspec validate --strict` checks delta structure, not cross-change archive ordering, so it passes
  regardless — this hazard is invisible to the validator. `recursive-desk-heartbeat` MUST be archived into
  the base `watch` spec BEFORE this change validates against the base / merges, or the MODIFIED target
  requirement will not exist. Verify after archiving: the base `watch` spec contains the
  "Recursive per-agent desk heartbeat" requirement this delta modifies.
- [x] 6.1 Integration test (`internal/watch`): a desk with a per-recipient backlog drives the full loop —
  actionable item ⇒ beaten; recipient marks the last item `[awaiting-auth]` ⇒ next tick NOT beaten (idle,
  cap-neutral); operator re-arm (`AgentWake`) + a fresh `[next]` item ⇒ beaten again. Asserts the judgment is
  a live per-recipient decision across ticks.
- [x] 6.2 `go test ./... && go vet ./...` green; `gofmt` clean.
- [x] 6.3 `openspec validate heartbeat-judgment --strict` passes; `bash scripts/check-private-boundary.sh`
  PASS. Update `llm.md` / any deploy doc if the per-recipient backlog path convention needs surfacing.
- [ ] 6.4 Run the review trio (systems-review + open-code-review + STORM) on the implementation diff;
  iterate to clean; open the PR referencing this change and #189/#183.
