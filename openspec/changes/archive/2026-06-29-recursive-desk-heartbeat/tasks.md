# Tasks — recursive-desk-heartbeat (#183, TDD)

Implementation is fresh-context-per-task-group (standard flow). Each group is self-contained + TDD; the
detector test pattern (`internal/watch/detector_synthesis_test.go` agent-wake fixture) is the template.
Load-bearing invariants (assert across paths): byte-inert when disabled; primary XO never desk-heartbeated;
a Working/busy agent never heartbeated; a settled desk suppressed until re-armed; cap→escalate-once→stop;
money desks opt-OUT by default; cold-start owes no heartbeat; off-mutex delivery; audit-suppressed Kind.

## 1. Roster opt-OUT flag + money-desk default-opt-out
- [x] 1.1 TEST FIRST (`internal/roster`): `roster.Agent.Heartbeat *bool` (pointer — ABSENT ⇒ ON). A
  `Config.HeartbeatEnabled(agent) bool` resolver: absent ⇒ true; `false` ⇒ off; the primary XO ⇒ false
  (excluded). Approval-sensitive/action desks marked via a per-agent `approval_sensitive` bool →
  default-off; assert an approval-sensitive / order-placing desk resolves OFF absent an explicit
  `heartbeat: true` (and that an explicit `heartbeat: true` overrides it). [`heartbeat_test.go`]
- [x] 1.2 Implement the flag + resolver. [`Agent.Heartbeat *bool` + `Agent.ApprovalSensitive bool` +
  `Config.HeartbeatEnabled`]

## 2. Per-agent settle marker (namespace + re-arm)
- [x] 2.1 TEST FIRST (`internal/watch`): a per-agent `SettledMarkerSet` keyed by agent → path
  `<roster-dir>/flotilla-<agent>-settled`; `Consume(agent)` is path-scoped per agent (no collision with
  the XO's marker or across desks); unreadable/absent/unconfigured ⇒ NOT settled. [`settled_test.go`]
- [x] 2.2 Implement the per-agent marker set + the per-agent settle prompt-path resolution
  (`SettledMarkerSet{Path,Consume}`, reusing `SettledMarker.Consume` for the fail-safe stat+remove).

## 3. AgentWake (the per-agent re-arm) wired for ALL relay targets
- [x] 3.1 TEST FIRST (`internal/watch/detector_heartbeat_test.go`): `Detector.AgentWake(agent)` clears
  that agent's settled+stopped state + resets its cadence(`deskSinceBeat`)/cap(`deskNoProgress`)
  counters, consumes its settle marker, touches ONLY that agent (isolation), no-ops on empty agent.
- [x] 3.2 Implemented `AgentWake` + the per-agent state maps (`deskSettled/deskSinceBeat/deskNoProgress/
  deskStopped/deskProgressed`) + `DetectorConfig.DeskSettleConsume`. (Wiring `cmd/flotilla/watch.go`
  `onAccepted` to call AgentWake for every desk target is folded into group 7's cmd wire-up.)

## 4. Detector per-agent heartbeat state + Idle-gated trigger (parallel tickLocked section)
- [x] 4.1 TEST FIRST: per-agent in-memory state (quiet-counter, consecutive-cap, progressedSinceHeartbeat,
  stopped) keyed by agent; a `pendingDeskHeartbeats` slice decided under `d.mu`. Cases: Idle + cadence-
  elapsed + not-settled + not-stopped + enabled ⇒ owed; Working ⇒ not owed (+ resets cap); settled ⇒ not
  owed; opted-out / primary-XO ⇒ never owed; cold-start tick ⇒ owes nothing.
  [`detector_heartbeat_g4_test.go` — the 11-case §9 matrix]
- [x] 4.2 Implement the parallel `tickLocked` section (`deskHeartbeatLocked`, alongside the mirror/synthesis
  sections) + the `runDeskHeartbeats` off-mutex tail (like `runSynthesis`) + the new DetectorConfig seams
  (all inert when `HeartbeatEnabled` is nil). progressedSinceHeartbeat (`deskProgressed`) latches on
  into-Working, clears on the owed beat.

## 5. Delivery: the WakeDeskHeartbeat dispatch + the desk-continuation prompt
- [x] 5.1 TEST FIRST (`cmd/flotilla`): the dispatch enqueues `Job{Agent, Message:<desk-prompt>,
  Kind:"detector"}` (audit-suppressed); the desk prompt is NON-AUTHORIZING + distinct from the XO's +
  carries the agent's settle path. [`watch_heartbeat_test.go`]
- [x] 5.2 Implement `newDeskHeartbeatDispatch` (the detector's `WakeDeskHeartbeat` seam) + a
  `deskContinuationBuiltin` (workspace `HEARTBEAT.md`-override) + a `WakeDeskHeartbeat` WakeKind constant.

## 6. Cap → escalate-to-owning-XO via the LOUD alert (leaf-desk parent resolution)
- [x] 6.1 TEST FIRST: cap N=3 consecutive no-progress heartbeats → ONE loud alert (edge-trigger `==N`) +
  stop (detector-level, `detector_heartbeat_g4_test.go` case 6); the escalation target = the owning XO
  (the channel the desk is a MEMBER of), fallback primary XO — `AgentsAbove` is EMPTY for a leaf, so the
  fallback is asserted (`TestOwningXO_LegacyStarFallsBackToChannelXO`,
  `TestDeskEscalateRoutesToOwningXOViaLoudAlert`); a Working edge or AgentWake clears stopped + cap (cases
  4/5/7).
- [x] 6.2 Implement the cap/escalate/reset matrix (in `deskHeartbeatLocked`) + the `roster.OwningXO`
  resolver + `newDeskEscalate` (the loud-alert seam).

## 7. Wire + integration
- [x] 7.1 Wire the detector config (HeartbeatEnabled→roster resolver, cadence=1 tick, the per-agent settle
  markers via `SettledMarkerSet` rooted at the roster dir, WakeDeskHeartbeat/DeskEscalate, and the
  `AgentWake` re-arm from `onAccepted` for every non-XO target) in `cmd/flotilla/watch.go`; default-ON
  gated by the roster resolver (§1).
- [x] 7.2 Federation double-drive invariant: a sub-XO that is the primary XO of another daemon is opt-OUT
  via the roster `heartbeat: false` flag (this daemon cannot introspect another).
  [`TestDeskHeartbeatFederationDoubleDriveOptOut`]
- [x] 7.3 Byte-inert-when-disabled regression: `TestDeskHeartbeat_ByteInertWhenUnwired` (detector-level) +
  the existing detector/watch/roster suites passing UNCHANGED (the path is inert until `HeartbeatEnabled`
  is wired).

## 8. Ship
- [x] 8.1 `go build ./...` + `go test -race ./...` green; `go vet` clean; `openspec validate --all --strict`.
- [x] 8.2 Impl-trio (systems+OCR+STORM) on the diff; iterate clean.
- [x] 8.3 PR to the reviewing XO's gate (reference #183 + the #184 approval-classifier unblock). (Merged as #191.)
