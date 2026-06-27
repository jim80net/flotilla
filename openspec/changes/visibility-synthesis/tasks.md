# Tasks — visibility-synthesis (B2, on the ratified TRANSCRIPT-FIRST substrate)

Bite-sized TDD tasks. Each writes the failing test first, then the minimal code. The
implement-phase gate is `/systems-review` + `/open-code-review` on the Go diff (per the standard
flow). The change MUST stay inert when the synthesis member is absent / no synthesis is owed
(`$0`-idle preserved). The substrate is TRANSCRIPT-FIRST: a synthesizing agent reads the LATEST
STATE of each subordinate via `claudestore.LatestTurnText` — NO `internal/synthledger`, NO new
write-path on the live Tier-1 mirror.

## 0. Verify-first (gates the implement phase)

- [x] 0.1 Confirm the surface-agnostic read seam: the synthesis read resolves each subordinate's pane
  via `deliver.ResolvePane(agentTitle(cfg, sub))` (`cmd/flotilla/watch.go:580,601`) then calls
  `rr.LatestResult(pane)` on the agent's `surface.ResultReader` (`watch.go:575-588`) — the EXACT path
  Tier 1 uses (claude → `claudestore.LatestTurnText`, `internal/surface/claude.go:39,106`; grok → the
  grok store). Confirm it needs NO change (read-only reuse) and that a surface without a `ResultReader`,
  or an unresolvable pane, is a clean SKIP (`watch.go:530,577`). The bind is to the SEAM, NOT to
  `claudestore` directly (which would exclude grok).
- [x] 0.2 Confirm the read is relay-disjoint (a read-only file read, never through
  `relay.Accept`/`relay.Route`, `internal/relay/relay.go:18-23`) and adds NO write-path to the Tier-1
  mirror.
- [x] 0.3 Confirm the Injector addresses an arbitrary agent (`watch.Job{Agent: ...}`) so a
  `WakeSynthesis` can enqueue to a non-primary synthesizing XO — read the Injector enqueue path. (The
  GAP is the detector wake SEAM, `detector.go:68` + `watch.go:259`, which is XO-hardcoded — §5.)

## 1. Routing — `OwnedChannels` + `AgentsBelow` + `AgentsAbove`, fleet-command-excluded (`internal/roster`) — DONE

- [x] 1.1 TEST + IMPL: a SINGLE fleet-command predicate `(Channel).IsFleetCommand()` (one source of
  truth — re-trio P3-1; not inline `== "fleet-command"` scattered across call sites). A fleet-command
  channel's members are command targets, NOT synthesis parents, so it contributes ZERO synthesis edges.
- [x] 1.2 TEST: `OwnedChannels(agent)` returns ALL channels where `ch.XOAgent == agent` (generalizing
  `ChannelForXO`, which returns only the first), INCLUDING any fleet-command channel (it is a POST
  target — the meta posts Tier-3 into #c2) — single-home XO → one; multi-hub XO → all.
- [x] 1.3 IMPL: add `OwnedChannels(agent string) []string` to `internal/roster/roster.go` (keep
  `ChannelForXO` as the primary-channel convenience; `OwnedChannels` is the full set).
- [x] 1.4 TEST: `AgentsBelow(agent)` = `{ ch.XOAgent : agent ∈ ch.Members, ch.XOAgent != agent,
  ch.Role != "fleet-command" }` over `Bindings()`. On the LIVE federated shape: a LEAF desk →
  `{}` (it owns no channel listing it; it is only a member of the broadcast channel, excluded); a
  project-XO → its boats with NO meta-XO leak; the meta-XO → exactly the project-XOs. The self-loop
  guard (`!= agent`) AND the fleet-command exclusion are BOTH exercised (a member of the broadcast
  channel does NOT pull the broadcaster into its read set — this is the implement-gate P0).
- [x] 1.5 IMPL: add `AgentsBelow(agent string) []string` to `internal/roster/roster.go`.
- [x] 1.6 TEST: `AgentsAbove(agent)` = `{ m : ch.XOAgent == agent, m ∈ ch.Members, m != agent,
  ch.Role != "fleet-command" }` — the members of the NON-fleet-command channels the agent OWNS, minus
  self; the owed-marking resolver (re-trio P1-A), replacing the wrong-typed `BindingForChannel`. Assert
  it is the EXACT relational inverse of `AgentsBelow` (`C ∈ AgentsBelow(P) ⟺ P ∈ AgentsAbove(C)`) over
  a fixture roster; a boat whose owned channel lists two parents returns BOTH. The root (whose only
  owned channel is fleet-command) → `{}`.
- [x] 1.7 IMPL: add `AgentsAbove(agent string) []string` to `internal/roster/roster.go` (symmetric to
  `AgentsBelow`, opposite direction; same fleet-command + self exclusions).
- [x] 1.8 TEST: `AgentsBelow` / `AgentsAbove` / `OwnedChannels` are pure read-only derivations — none
  mutates any binding's `Members` slice (the read-only-slice contract).

## 2. Membership-graph DAG assertion WITH self-edge AND fleet-command exclusion (`internal/roster`) — DONE

- [x] 2.1 TEST: `Load` ACCEPTS the LIVE federated shape — a fleet-command broadcast channel
  (`role="fleet-command"`, members = ALL agents) PLUS per-XO home channels (members = parent) PLUS the
  two-tier project/meta structure. Without the fleet-command exclusion the broadcast channel's
  `meta → {everyone}` edges close a cycle (e.g. `meta → leaf-of-a-subtree → … → meta`) and Load would
  REFUSE — so this test is the regression guard for the implement-gate P0.
- [x] 2.2 TEST: `Load` ACCEPTS a roster whose home channel lists its own XO among its members (the
  live home-channel self-membership AND the legacy single-binding form) — the self-edge is EXCLUDED,
  not a false cycle.
- [x] 2.3 TEST: `Load` REFUSES a roster with a MUTUAL cycle between two DISTINCT NON-fleet-command
  channels (channel-X's XO is a member of channel-Y AND channel-Y's XO is a member of channel-X) with a
  clear error.
- [x] 2.5 TEST: `Load` ACCEPTS an acyclic federation (Tier-3 meta-XO + project-XOs + boats; each home
  channel self-membership excluded; the fleet-command broadcast excluded).
- [x] 2.6 IMPL: add a depth-first-search cycle check over the synthesis-edge graph to `roster.Load`,
  fail-closed. Build the edge set as `ch.XOAgent → m` for each `m ∈ ch.Members` with `m != ch.XOAgent`
  (DROP self-edges) AND `ch.Role != "fleet-command"` (DROP broadcast-channel edges — the implement-gate
  P0). Runs once at load, not on the hot path.

## 3. Transcript-first read of the subordinates' latest state (read-only reuse) — DONE (5b79e3c)

- [x] 3.1 TEST: for a synthesizing agent, the read path resolves `AgentsBelow(agent)`, resolves each
  subordinate's pane (`ResolvePane(agentTitle(...))`), and reads its latest turn-final text via the
  agent's `surface.ResultReader.LatestResult(pane)` (one bounded read per subordinate), NOT an
  unbounded windowing pass and NOT any ledger. A subordinate whose pane will not resolve, or whose
  surface has no `ResultReader`, is cleanly SKIPPED (never a crashed wake).
- [x] 3.2 TEST: the read is read-only — it never writes a ledger, never touches the live Tier-1 mirror
  path, and never routes through the relay.
- [x] 3.3 IMPL: wire the synthesis read (in the `wake` composer / the synthesis helper) to
  `AgentsBelow` + `deliver.ResolvePane(agentTitle(...))` + `surface.ResultReader.LatestResult(pane)`
  (the SAME seam Tier 1 uses), with the clean-skip on an unresolvable pane / no-`ResultReader` surface.
  No change to `internal/claudestore` or `internal/surface` (read-only reuse). NO `internal/synthledger`
  package. NO Tier-1 mirror change.

## 4. (REMOVED — the ledger is a fast-follow, GitHub issue #138)

The first revision's "Tier-1 mirror writes a ledger event" tasks are REMOVED. Under the ratified
transcript-first substrate there is NO mirror-event ledger and NO new write-path on the live mirror.
The finish-history ledger is deferred (issue #138, label `enhancement`), built ONLY iff
synthesis is later shown to need finish-history rather than latest-state.

## 5. The `WakeSynthesis` wake-kind + the agent-targeted wake seam + the digest cadence (`internal/watch/detector.go`) — DONE (5b79e3c)

- [x] 5.1 TEST: a boat-finish (Working→Idle, non-XO) marks synthesis "owed" for that channel's XO,
  keyed in a per-SYNTHESIZING-agent owed-set (alongside the existing `pendingMirrors`).
- [x] 5.2 TEST: the detector fires `WakeSynthesis` for a synthesizing agent AT MOST once per the digest
  sub-cadence per agent while it has synthesis owed (debounce-up — a burst coalesces to one).
- [x] 5.3 TEST: with no synthesis owed, no `WakeSynthesis` fires (idle `$0` cost; behavior
  byte-identical to before when the feature is inert).
- [x] 5.4 TEST: the wake seam carries an AGENT parameter — the `WakeSynthesis` side-effect is enqueued
  in `runTail`, OUTSIDE `d.mu`, and is enqueued to the SYNTHESIZING agent (which may differ from
  `d.cfg.XOAgent`), proving the XO-hardcoded path (`watch.go:259` `Agent: xo`) no longer constrains it.
- [x] 5.5 IMPL: add `WakeSynthesis WakeKind`; add a PARALLEL agent-targeted
  `WakeAgent func(agent string, kind WakeKind, reasons []string)` (NOT widening `Wake` — keep the
  shipped primary-XO path byte-identical, re-trio P2-1); add the per-agent owed-set + digest-cadence
  counter in the detector; emit it in `runTail` like the other wakes. The digest floor derives from
  `heartbeat_interval` (a small multiple), NOT a new roster knob (Q-B resolved); confirm the concrete
  multiple in review. Default cadence wired so an unconfigured deployment is inert.
- [x] 5.6 TEST + IMPL: the owed-set keying maps a finishing AGENT NAME → its synthesizing parent(s)
  via `AgentsAbove(agent)` (NOT `BindingForChannel`, which takes a channel id — re-trio P1-A) so the
  wake targets the correct agent(s); a boat that is a member of TWO channels marks BOTH parent XOs
  owed; a boat in a Tier-2 channel marks its project XO owed, and a project-XO finishing a turn marks
  the meta-XO owed (the Q-F recursion — Tier 3 reads Tier 2's latest STATE the same way Tier 2 reads
  its boats').

## 6. The DURABLE materiality (last-seen) state (`internal/watch` / a disk sidecar) — DONE (5b79e3c)

- [x] 6.1 TEST: the materiality gate is a per-synthesizing-agent durable last-seen snapshot (e.g. a
  hash of each subordinate's last-synthesized turn text); when no subordinate's latest state has
  changed, no `WakeSynthesis` fires (and a fired wake whose subordinates are unchanged yields an idle
  reply, no re-post).
- [x] 6.2 TEST: the last-seen snapshot is DAEMON/DISK-OWNED and survives a simulated context rotation
  (it is NOT skill-context state) — after a rotation the next synthesis does not re-read-from-scratch
  and re-post an unchanged rollup.
- [x] 6.3 IMPL: add the durable last-seen snapshot as a DISK SIDECAR (keyed by synthesizing agent,
  alongside the detector's existing snapshot); the detector either suppresses the wake on zero-change
  or passes "what changed since last fire" into the wake. The hash is the per-subordinate FULL latest
  turn text (Q-C resolved — a new-identical turn is a no-op, any change is material).
- [x] 6.4 TEST: the last-seen snapshot survives a DAEMON RESTART (it is a disk sidecar, not in-memory
  detector state) — after a restart with unchanged subordinates, NO `WakeSynthesis` fires (no
  restart-storm of re-posts). A missing/corrupt sidecar fails SAFE toward "all changed" (synthesize
  once), never silent-never-fire. (re-trio P2-4)
- [x] 6.5 TEST: an UNREADABLE subordinate (pane won't resolve) is EXCLUDED from the materiality hash
  for that wake — never hashed as empty — so a transient pane-resolve failure does not flap the wake
  (neither spams a re-post on "change to empty" nor suppresses a real change on recovery). (re-trio
  P2-4)

## 7. The `wake` prompt composer (`cmd/flotilla/watch.go`) — DONE (5b79e3c)

- [x] 7.1 TEST: a `WakeSynthesis` kind composes a prompt that points the agent at its read set (the
  agents below it) + its post target (`OwnedChannels`/`ChannelForXO` via `secrets.Webhook`) + the
  per-tier output contract + the narrow-answer discipline (curate what CHANGED, else reply idle), and
  enqueues to the SYNTHESIZING agent (the new agent param), NOT the hardcoded primary `xo`.
- [x] 7.2 IMPL: add the `WakeSynthesis` case to the `wake` switch (`watch.go:245-260`); reference the
  embedded skill (the prompt is the thin trigger; the skill file carries the detailed curation
  instructions); enqueue `watch.Job{Agent: <synthesizing agent>, ...}`.

## 8. The `heartbeat-skill` mechanism + the registry member + the Install signature change (`internal/doctrine`) — DONE (commit 798a5ea)

- [x] 8.1 TEST: `MechanismHeartbeatSkill` installs as a WHOLE-FILE member — a missing skill file in the
  workspace is CREATED, an existing one is KEPT (operator edits survive), reported created/kept —
  decided by a STAT of the target file, NOT a marker fence.
- [x] 8.2 TEST: a whole-file member does NOT route through `appendOnce` (which hard-errors on an empty
  `OpenMarker`, `install.go:85`); an identity-append member and a heartbeat-skill member install
  together via the SAME `doctrine.Install` loop with no LOOP change; the identity-append arm is
  unaffected.
- [x] 8.3 IMPL: add `MechanismHeartbeatSkill Mechanism = "heartbeat-skill"` to
  `internal/doctrine/doctrine.go`; add a `TargetFile` (workspace-relative) field to `Member` (empty for
  identity-append, set for heartbeat-skill); add its whole-file STAT-based kept/created dispatch arm to
  `internal/doctrine/install.go` (the `switch m.Mechanism` second case — landed in THIS change per the
  MECHANISM COUPLING contract, `install.go:51-57`).
- [x] 8.4 IMPL: CHANGE the `doctrine.Install` SIGNATURE to `Install(workspaceDir, identityFile string,
  members []Member)` — the CALLER (which holds the surface) resolves `identityFile` via
  `workspace.IdentityFileName(surface)` and passes it, keeping `internal/doctrine` dependency-free (a
  deliberate refinement of Q-D, which said "derive from workspace.IdentityFileName" — but that would
  force a workspace import; the caller already has the surface). Install joins `workspaceDir/identityFile`
  for the append and `workspaceDir/<TargetFile>` for the whole-file. The whole-file CREATE does its OWN
  `os.WriteFile` (+ `os.MkdirAll` of `skills/`), disjoint from the identity `anyAppended` write-back.
  Update BOTH call sites: `cmd/flotilla/workspace.go` and `cmd/flotilla/doctrine.go`.
- [x] 8.5 IMPL: register the visibility-synthesis member in the `members` slice
  (`Mechanism: MechanismHeartbeatSkill`, `TargetFile: "skills/visibility-synthesis.md"`, content from
  `internal/doctrine/assets/skills/visibility-synthesis.md`).
- [x] 8.6 TEST: `workspace init` seeds BOTH members (the Rule-of-Three identity-append AND the
  visibility-synthesis whole-file skill) via the same `doctrine.Install`; re-running init keeps both
  unchanged.

## 9. The skill content (`internal/doctrine/assets/skills/visibility-synthesis.md`) — DONE (§8/§9, commit 798a5ea)

- [x] 9.1 Author the curation prompt: how to read the subordinates' latest transcript STATE (the agents
  below, via the membership down-traversal), how to curate Tier 2 (domain rollup, grouped by boat,
  surface attention-worthy items, not a firehose), how to curate Tier 3 (#c2 headline + operator
  decisions [best-effort over latest-turn state] + drill-down pointers down the membership graph), and
  the narrow-answer discipline (no manufactured synthesis — reply idle when nothing material changed).
  Spell out acronyms. Embedded under `internal/doctrine/assets/skills` (the existing `go:embed` tree).

## 10. Docs

- [x] 10.0 Update `flotilla.example.json` AND `tools/landing-status/demo-roster.json` to the FEDERATED
  shape INCLUDING a `role="fleet-command"` broadcast channel + per-XO home channels (members=parent) +
  the two-tier project/meta structure (the primary XO's explicit ask). The legacy single-binding STAR shape
  these carry today is exactly why the design-trio missed the broadcast-channel P0 — the examples must
  exercise the REAL federated topology so the gap can never slip again, and so the roster tests can load
  a realistic fixture.
- [x] 10.1 `docs/visibility.md` — the stratified-visibility doctrine doc (the source of truth for the
  three tiers, the up-flow/inverse-drill-down, the TRANSCRIPT-FIRST LOCAL substrate, the topology
  [each agent owns its home channel; its parent is a member; the fleet-command broadcast channel is
  EXCLUDED from synthesis edges], the routing down-traversal). Cross-link from `docs/xo-doctrine.md` and
  the Tier-1 references.
- [x] 10.2 Update the constitutional-set member list reference (the README / doctrine docs) to note the
  second member (visibility-synthesis) and the `heartbeat-skill` mechanism — the "vocabulary extends
  with each new member kind" B1 promised, now realized.

## 11. Validation + review gates

- [x] 11.1 `openspec validate visibility-synthesis --strict` passes.
- [x] 11.2 Implementation-diff trio (systems-review + STORM, 2026-06-21) on the Go diff — clean after
  the fold. BOTH independently CONFIRMED the implement-gate P1 (the materiality `SynthRead` =
  blocking tmux + transcript I/O ran under `d.mu`, stalling the tick + blocking `OperatorWake`). FIXED:
  split `decideSynthesis` into a pure `synthEligibleLocked` (under `d.mu`) + an off-`d.mu`
  `runSynthesis` (read + materiality + short-relock commit + `WakeAgent`), sync-in-tail (not async —
  synthesis commits state the next tick reads). P2 (the off-mutex test guarded only DELIVERY) FIXED:
  added `TestSynthesisMaterialityReadRunsOutsideMutex` (parks the READ, asserts `OperatorWake` returns
  — would hang pre-fix). P3 folded: prune stale sidecar synthesizer keys at load; design notes for
  restart-re-derives-owed + enqueue-time-read-set. All else rated SOUND (byte-identical-absent,
  $0-idle, cadence, materiality, routing, doctrine). `go test ./... -race` green.
- [x] 11.3 Confirmed the resolved design decisions landed as specified (transcript-first substrate; DAG
  self-edge + fleet-command exclusion; the parallel `WakeAgent` seam; the durable disk-sidecar
  last-seen materiality surviving rotation+restart; heartbeat-skill whole-file STAT idempotency + the
  `Install(workspaceDir, identityFile, members)` signature).
- [x] 11.4 The re-trio (2026-06-21, systems-review + STORM) RESOLVED the open questions — confirm they
  land as decided: Q-B cadence = daemon floor derived from `heartbeat_interval`; Q-C = per-subordinate
  full-latest-turn-text hash with the unreadable subordinate excluded; Q-D = `Install(workspaceDir,
  members)` deriving the identity path; Q-E = post to the primary owned channel in v1, fan-out
  deferred; Q-F = Tier-3 reads Tier-2's latest transcript state (confirmed DAG-respecting).
