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
- [ ] 4.1 TEST FIRST: per-agent in-memory state (quiet-counter, consecutive-cap, progressedSinceHeartbeat,
  stopped) keyed by agent; a `pendingDeskHeartbeats` slice decided under `d.mu`. Cases: Idle + cadence-
  elapsed + not-settled + not-stopped + enabled ⇒ owed; Working ⇒ not owed (+ resets cap); settled ⇒ not
  owed; opted-out / primary-XO ⇒ never owed; cold-start tick ⇒ owes nothing.
- [ ] 4.2 Implement the parallel `tickLocked` section (alongside the mirror/synthesis sections) + the
  `runDeskHeartbeats` off-mutex tail (like `runSynthesis`). progressedSinceHeartbeat latches on into-Working,
  clears on heartbeat-enqueue.

## 5. Delivery: extend the wakeAgent dispatcher + the desk-continuation prompt
- [ ] 5.1 TEST FIRST (`cmd/flotilla`): the `wakeAgent` dispatcher handles a `WakeDeskHeartbeat` kind
  (today it rejects non-synthesis kinds) → enqueues `Job{Agent, Message:<desk-prompt>, Kind:"detector"}`
  (audit-suppressed). Assert the desk prompt is NON-AUTHORIZING + distinct from the XO's + carries the
  agent's settle path.
- [ ] 5.2 Implement the dispatcher extension + a `deskContinuationBuiltin` (workspace `HEARTBEAT.md`-override).

## 6. Cap → escalate-to-owning-XO via the LOUD alert (leaf-desk parent resolution)
- [ ] 6.1 TEST FIRST: cap N=3 consecutive no-progress heartbeats → ONE loud alert (edge-trigger `==N`) +
  stop; the escalation target = the channel XO the desk is a MEMBER of (`BindingForChannel(...).XOAgent`),
  fallback primary XO (`AgentsAbove` is EMPTY for a leaf — assert the fallback); a Working edge or AgentWake
  clears stopped + cap; an input-blocked drop does NOT count toward the cap.
- [ ] 6.2 Implement the cap/escalate/reset matrix + the parent resolver.

## 7. Wire + integration
- [ ] 7.1 Wire the detector config (DeskHeartbeat enable, the cadence, the per-agent settle markers, the
  AgentWake) in `cmd/flotilla/watch.go`; the cadence = the heartbeat interval; default-ON gated by the
  roster resolver (§1).
- [ ] 7.2 Federation double-drive invariant: a sub-XO that is the primary XO of another daemon is opt-OUT
  of the parent's desk heartbeat.
- [ ] 7.3 Byte-inert-when-disabled regression test (the detector's existing behavior unchanged when no
  agent is heartbeat-enabled).

## 8. Ship
- [ ] 8.1 `go build ./...` + `go test -race ./...` green; `go vet` clean; `openspec validate --all --strict`.
- [ ] 8.2 Impl-trio (systems+OCR+STORM) on the diff; iterate clean.
- [ ] 8.3 PR to the reviewing XO's gate (reference #183 + the #184 approval-classifier unblock).
