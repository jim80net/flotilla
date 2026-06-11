# Tasks — watch-heartbeat-sidechannel (Phase 1: DESIGN ONLY)

> **GATE:** this is the build plan. It is unblocked only after (a) the XO ratifies
> the design at the checkpoint, (b) `/systems-review` + `/open-code-review` on the
> design are clean, and (c) **the OPERATOR reviews and greenlights** — this touches
> the **safety-critical heartbeat clock** (the liveness ack, the
> `flotilla-watch.service` unit, the missed-ack down-alert). DESIGN ONLY until then
> — no code.

## 0. Design gate (current phase)

- [x] 0.1 Forensic read of the running system: confirm prod runs LEGACY always-wake
      (live roster has no `change_detector`; no v2 artifacts in the state dir); v2
      detector exists but is dormant. (proposal.md §Why, design.md §Problem.)
- [x] 0.2 Draft proposal + design.md + watch spec delta + this plan.
- [x] 0.3 `openspec validate watch-heartbeat-sidechannel --strict` passes.
- [x] 0.4 `/systems-review` + `/open-code-review` in parallel on the design (round 1):
      P1-1 falsified activity-derived liveness (ping is `quietTicks`-driven, not
      `Age()`); P1-2 tracker-in-git-tree mtime spoof; P1-3 future-mtime blind-spot;
      P2-1 settle marker ephemeral; OCR H1 base-requirement contradiction. ALL folded:
      activity-derived liveness demoted to deferred F5 (recommend not-built); spec
      trimmed to the no-liveness-threshold core; H1 base requirement also MODIFIED.
- [ ] 0.5 RE-checkpoint the revised design → operator/XO review; if any fork (F1/F3/
      F4/F5) is re-opened, re-run the review pair.
- [ ] 0.6 **OPERATOR review + greenlight** of (a) the design and (b) the immediate
      production-enablement step (§1). BLOCKS everything below. The clock is
      safety-critical — no build, and no prod roster flip, without explicit sign-off.

## 1. Production enablement (config only — the immediate win; operator-gated)

> Not code in this repo — a change to the live roster
> `…/spark/state/flotilla.json`. Captures the bulk of the burn reduction by lighting
> up the already-shipped v2 detector. Listed here so it is not lost; do it only on
> operator sign-off, and verify the wedge/crash alert still fires after the flip.

- [ ] 1.1 Re-verify the live roster state at flip time (freshness — the operator may
      have enabled it already), then set `change_detector: true` +
      `liveness_ping_mode: "none"`; confirm the XO's standing instructions carry the
      v2 settle/awaiting marker discipline (docs/xo-doctrine.md §change-detector).
- [ ] 1.2 Restart `flotilla-watch`; verify from journald that the detector branch is
      taken (`change-detector running …`) and the snapshot/markers appear. NOTE: the
      pre-code flip will still exhibit the tracker self-trigger (extra wakes on the
      XO's own writes) until §2 lands — expected, harmless, not a regression.
- [ ] 1.3 Verify liveness end-to-end on the live clock: force a stale ack → confirm
      the down-alert still fires at the window; confirm a shell-crash alerts
      immediately. (No weakening — the precondition for trusting any later change.)

## 2. Tracker self-trigger fix + external signal file (Mechanism 1; the core code)

- [x] 2.1 TDD: `materiality_test.go` — the XO's single-writer tracker writes produce
      NO wake (`TestExternalMaterialTrackerWritesDoNotWake`); the detector no longer
      hashes the tracker (renamed `Snapshot.TrackerHash`→`SignalHash`; the wake source
      is the external signal file only).
- [x] 2.2 TDD: `--signal-file` (`$FLOTILLA_SIGNAL_FILE`) hash change → exactly one
      external-signal wake (`TestExternalMaterialSignalOnly`); unconfigured → inert
      (NewDetector defaults `SignalHash` to an always-false func); read error /
      absent / a directory → unchanged (`TestContentHasherFailSafe`,
      `TestContentHasherDirIsNotASignal`) — parity with the prior fail-safe.
- [x] 2.3 Wired `--signal-file` flag + env into `cmd/flotilla/watch.go` (renamed
      `trackerHasher`→`contentHasher`; the detector's `SignalHash` is the signal-file
      hasher when set, else nil→inert); the tracker path stays only as the
      continuation prompt's `{{tracker}}` target. Updated `watch-runbook.md`. (Deploy
      env template intentionally unchanged — the signal file is opt-in/advanced, not a
      standard host path the installer wires.)
- [x] 2.4 Existing detector liveness tests (wedge window, shell-immediate, snapshot
      fail-safe) pass UNCHANGED — `ack.go`/`evalLiveness`/the ping were not touched.

## 3. Named escalation-trigger set (Mechanism 2)

- [x] 3.1 The escalate-only-on-trigger contract is covered: a tick with none of the
      five triggers spends no XO turn (`TestDetectorColdStart…`/idle-quiet tests); each
      trigger has a wake test (desk transition, external signal, self-continuation,
      ping; operator message via `OperatorWake`). The signal wake names the trigger
      ("external signal changed").
- [ ] 3.2 Resolve F1 per the operator ruling (PR-awaiting-merge: XO duty vs an
      external signal-emitter that writes `--signal-file`). The `--signal-file` seam
      is the mechanism for option (b); no network/auth was added to `flotilla watch`.
      (Wiring an emitter is downstream/out-of-daemon — not part of this code PR.)

## 4. (DEFERRED — fork F5, only if the operator overrides the recommendation)

> Activity-derived liveness broadening. **Recommended NOT to build** (design.md §F5):
> review-falsified — it does not suppress the ping, and the variant that would is a
> liveness regression. Listed so the decision is explicit, not forgotten.

- [ ] 4.1 (ONLY if F5 approved) down-alert sources only (never the ping); ack-file
      canonical; tracker opt-in + proven single-writer; every source clamped to
      `≤ now`; settle/awaiting excluded. Full re-review before any such build.

## 5. Review + ship (build phase)

- [ ] 5.1 `gofmt` / `go vet` / `go build ./...` / `go test -race ./...` green.
- [ ] 5.2 `openspec validate watch-heartbeat-sidechannel --strict` green.
- [ ] 5.3 `/systems-review` + `/open-code-review` in parallel on the implementation
      diff; iterate to clean.
- [ ] 5.4 PR referencing this change; CI green; trace the runtime liveness path
      end-to-end on a staging clock before any prod flip.
- [ ] 5.5 Archive the change; update `docs/xo-doctrine.md` + `watch-runbook.md`.
