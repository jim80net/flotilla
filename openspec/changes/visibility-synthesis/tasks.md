# Tasks — visibility-synthesis (B2, on the ratified TRANSCRIPT-FIRST substrate)

Bite-sized TDD tasks. Each writes the failing test first, then the minimal code. The
implement-phase gate is `/systems-review` + `/open-code-review` on the Go diff (per the standard
flow). The change MUST stay inert when the synthesis member is absent / no synthesis is owed
(`$0`-idle preserved). The substrate is TRANSCRIPT-FIRST: a synthesizing agent reads the LATEST
STATE of each subordinate via `claudestore.LatestTurnText` — NO `internal/synthledger`, NO new
write-path on the live Tier-1 mirror.

## 0. Verify-first (gates the implement phase)

- [ ] 0.1 Confirm `claudestore.LatestTurnText` (`internal/claudestore/claudestore.go:294`) is the
  read-only latest-state reader and that resolving a subordinate's cwd for it goes through
  `deliver.PaneCWD` (`internal/deliver/panecwd.go:20`) → encode → glob
  `~/.claude/projects/<enc-cwd>/*.jsonl` → newest. This is the SAME path Tier 1 uses; confirm it needs
  NO change (read-only reuse).
- [ ] 0.2 Confirm the read is relay-disjoint (a read-only file read, never through
  `relay.Accept`/`relay.Route`, `internal/relay/relay.go:18-23`) and adds NO write-path to the Tier-1
  mirror.
- [ ] 0.3 Confirm the Injector addresses an arbitrary agent (`watch.Job{Agent: ...}`) so a
  `WakeSynthesis` can enqueue to a non-primary synthesizing XO — read the Injector enqueue path. (The
  GAP is the detector wake SEAM, `detector.go:68` + `watch.go:259`, which is XO-hardcoded — §5.)

## 1. Routing — `ChannelsAwareOf` + `OwnedChannels` + the down-traversal read set (`internal/roster`)

- [ ] 1.1 TEST: `ChannelsAwareOf(agent)` returns every channel id where the agent is a `member` OR the
  `xo_agent`, over `Bindings()` — for the federated multi-channel form AND the legacy single-binding
  form.
- [ ] 1.2 TEST: `ChannelsAwareOf` is a pure read-only derivation — it does not mutate any binding's
  `Members` slice (respect the read-only-slice contract).
- [ ] 1.3 IMPL: add `ChannelsAwareOf(agent string) []string` to `internal/roster/roster.go`.
- [ ] 1.4 TEST: `OwnedChannels(agent)` returns ALL channels where `ch.XOAgent == agent` (generalizing
  `ChannelForXO`, which returns only the first) — for a single-home XO it returns one; for a multi-hub
  XO it returns all.
- [ ] 1.5 IMPL: add `OwnedChannels(agent string) []string` to `internal/roster/roster.go` (keep
  `ChannelForXO` as the primary-channel convenience; `OwnedChannels` is the full set).
- [ ] 1.6 TEST: the synthesis READ AGENTS = the XOs of (`ChannelsAwareOf(agent)` MINUS
  `OwnedChannels(agent)`), excluding the agent itself — i.e. `AgentsBelow(agent)`. For a multi-channel
  XO that is BOTH a member of a peer's channel and the XO of its own, the read set excludes its own
  channel/itself (the self-loop guard).
- [ ] 1.7 IMPL: add the down-traversal read-set derivation (`AgentsBelow(agent string) []string`, the
  XO agents of the read channels, minus self) to `internal/roster/roster.go`.

## 2. Membership-graph DAG assertion WITH self-edge exclusion (`internal/roster`)

- [ ] 2.1 TEST: `Load` ACCEPTS a roster whose home channel lists its own XO among its members (the
  live #c2 `xo_agent=hydra-ops`/`members=[…,hydra-ops]` shape AND the legacy single-binding form where
  the XO is a member) — the self-edge is EXCLUDED, not a false cycle. (Without this the live/legacy
  roster would refuse to start.)
- [ ] 2.2 TEST: `Load` REFUSES a roster with a MUTUAL cycle between two DISTINCT channels (channel-X's
  XO is a member of channel-Y AND channel-Y's XO is a member of channel-X) with a clear error.
- [ ] 2.3 TEST: `Load` ACCEPTS an acyclic federation (Tier-3 meta-XO channel with project-XOs as
  members; each project channel with its boats as members; each home channel self-membership
  excluded).
- [ ] 2.4 IMPL: add a depth-first-search cycle check over the `Bindings()` edges to `roster.Load`,
  fail-closed. Build the edge set as `ch.XOAgent → m` for each `m ∈ ch.Members` with `m != ch.XOAgent`
  (DROP self-edges). Runs once at load, not on the hot path.

## 3. Transcript-first read of the subordinates' latest state (read-only reuse)

- [ ] 3.1 TEST: for a synthesizing agent, the read path resolves `AgentsBelow(agent)` and reads each
  subordinate's latest turn-final text via `claudestore.LatestTurnText` (one bounded read per
  subordinate), NOT an unbounded windowing pass and NOT any ledger.
- [ ] 3.2 TEST: the read is read-only — it never writes a ledger, never touches the live Tier-1 mirror
  path, and never routes through the relay.
- [ ] 3.3 IMPL: wire the synthesis read (in the `wake` composer / the synthesis helper) to
  `AgentsBelow` + `claudestore.LatestTurnText` + `deliver.PaneCWD` for cwd resolution. No change to
  `internal/claudestore` (read-only reuse). NO `internal/synthledger` package. NO Tier-1 mirror change.

## 4. (REMOVED — the ledger is a fast-follow, GitHub issue #138)

The first revision's "Tier-1 mirror writes a ledger event" tasks are REMOVED. Under the ratified
transcript-first substrate there is NO mirror-event ledger and NO new write-path on the live mirror.
The finish-history ledger is deferred (issue #138, label `enhancement`), built ONLY iff
synthesis is later shown to need finish-history rather than latest-state.

## 5. The `WakeSynthesis` wake-kind + the agent-targeted wake seam + the digest cadence (`internal/watch/detector.go`)

- [ ] 5.1 TEST: a boat-finish (Working→Idle, non-XO) marks synthesis "owed" for that channel's XO,
  keyed in a per-SYNTHESIZING-agent owed-set (alongside the existing `pendingMirrors`).
- [ ] 5.2 TEST: the detector fires `WakeSynthesis` for a synthesizing agent AT MOST once per the digest
  sub-cadence per agent while it has synthesis owed (debounce-up — a burst coalesces to one).
- [ ] 5.3 TEST: with no synthesis owed, no `WakeSynthesis` fires (idle `$0` cost; behavior
  byte-identical to before when the feature is inert).
- [ ] 5.4 TEST: the wake seam carries an AGENT parameter — the `WakeSynthesis` side-effect is enqueued
  in `runTail`, OUTSIDE `d.mu`, and is enqueued to the SYNTHESIZING agent (which may differ from
  `d.cfg.XOAgent`), proving the XO-hardcoded path (`watch.go:259` `Agent: xo`) no longer constrains it.
- [ ] 5.5 IMPL: add `WakeSynthesis WakeKind`; widen the `Wake` callback to carry an agent
  (`Wake func(agent string, kind WakeKind, reasons []string)`) OR add a parallel agent-targeted
  `WakeAgent`; add the per-agent owed-set + digest-cadence counter in the detector; emit it in
  `runTail` like the other wakes. Default cadence wired so an unconfigured deployment is inert. Resolve
  Q-B (the daemon floor) in review.
- [ ] 5.6 TEST + IMPL: the owed-set keying maps a boat's channel → its synthesizing XO via the roster
  (`BindingForChannel(...).XOAgent`) so the wake targets the correct agent; a boat in a Tier-2 channel
  marks its project XO owed, and a project-XO finishing a turn marks the meta-XO owed (the Q-F
  recursion — Tier 3 reads Tier 2's latest STATE the same way Tier 2 reads its boats').

## 6. The DURABLE materiality (last-seen) state (`internal/watch` / a disk sidecar)

- [ ] 6.1 TEST: the materiality gate is a per-synthesizing-agent durable last-seen snapshot (e.g. a
  hash of each subordinate's last-synthesized turn text); when no subordinate's latest state has
  changed, no `WakeSynthesis` fires (and a fired wake whose subordinates are unchanged yields an idle
  reply, no re-post).
- [ ] 6.2 TEST: the last-seen snapshot is DAEMON/DISK-OWNED and survives a simulated context rotation
  (it is NOT skill-context state) — after a rotation the next synthesis does not re-read-from-scratch
  and re-post an unchanged rollup.
- [ ] 6.3 IMPL: add the durable last-seen snapshot to the detector state (or a disk sidecar); the
  detector either suppresses the wake on zero-change or passes "what changed since last fire" into the
  wake. Resolve Q-C (hash granularity: full-turn-text hash vs new-turn signal) in review.

## 7. The `wake` prompt composer (`cmd/flotilla/watch.go`)

- [ ] 7.1 TEST: a `WakeSynthesis` kind composes a prompt that points the agent at its read set (the
  agents below it) + its post target (`OwnedChannels`/`ChannelForXO` via `secrets.Webhook`) + the
  per-tier output contract + the narrow-answer discipline (curate what CHANGED, else reply idle), and
  enqueues to the SYNTHESIZING agent (the new agent param), NOT the hardcoded primary `xo`.
- [ ] 7.2 IMPL: add the `WakeSynthesis` case to the `wake` switch (`watch.go:245-260`); reference the
  embedded skill (the prompt is the thin trigger; the skill file carries the detailed curation
  instructions); enqueue `watch.Job{Agent: <synthesizing agent>, ...}`.

## 8. The `heartbeat-skill` mechanism + the registry member + the Install signature change (`internal/doctrine`)

- [ ] 8.1 TEST: `MechanismHeartbeatSkill` installs as a WHOLE-FILE member — a missing skill file in the
  workspace is CREATED, an existing one is KEPT (operator edits survive), reported created/kept —
  decided by a STAT of the target file, NOT a marker fence.
- [ ] 8.2 TEST: a whole-file member does NOT route through `appendOnce` (which hard-errors on an empty
  `OpenMarker`, `install.go:85`); an identity-append member and a heartbeat-skill member install
  together via the SAME `doctrine.Install` loop with no LOOP change; the identity-append arm is
  unaffected.
- [ ] 8.3 IMPL: add `MechanismHeartbeatSkill Mechanism = "heartbeat-skill"` to
  `internal/doctrine/doctrine.go`; add a `TargetFile` (workspace-relative) field to `Member` (empty for
  identity-append, set for heartbeat-skill); add its whole-file STAT-based kept/created dispatch arm to
  `internal/doctrine/install.go` (the `switch m.Mechanism` second case — landed in THIS change per the
  MECHANISM COUPLING contract, `install.go:51-57`).
- [ ] 8.4 IMPL: CHANGE the `doctrine.Install` SIGNATURE to take a WORKSPACE-DIRECTORY parameter (the
  whole-file member writes `<workspaceDir>/<TargetFile>`, which the `identityPath`-only signature,
  `install.go:40`, cannot resolve). Update BOTH call sites: `cmd/flotilla/workspace.go:148` and
  `cmd/flotilla/doctrine.go:50`. Resolve Q-D (pass workspace dir + identity path, or derive identity
  path from workspace dir) in review.
- [ ] 8.5 IMPL: register the visibility-synthesis member in the `members` slice
  (`Mechanism: MechanismHeartbeatSkill`, `TargetFile: "skills/visibility-synthesis.md"`, content from
  `internal/doctrine/assets/skills/visibility-synthesis.md`).
- [ ] 8.6 TEST: `workspace init` seeds BOTH members (the Rule-of-Three identity-append AND the
  visibility-synthesis whole-file skill) via the same `doctrine.Install`; re-running init keeps both
  unchanged.

## 9. The skill content (`internal/doctrine/assets/skills/visibility-synthesis.md`)

- [ ] 9.1 Author the curation prompt: how to read the subordinates' latest transcript STATE (the agents
  below, via the membership down-traversal), how to curate Tier 2 (domain rollup, grouped by boat,
  surface attention-worthy items, not a firehose), how to curate Tier 3 (#c2 headline + operator
  decisions derived from full latest-turn text + drill-down pointers down the membership graph), and
  the narrow-answer discipline (no manufactured synthesis — reply idle when nothing material changed).
  Spell out acronyms. Embed under `internal/doctrine/assets/skills` (the existing `go:embed` tree,
  `doctrine.go:23`).

## 10. Docs

- [ ] 10.1 `docs/visibility.md` — the stratified-visibility doctrine doc (the source of truth for the
  three tiers, the up-flow/inverse-drill-down, the TRANSCRIPT-FIRST LOCAL substrate, the topology
  [each agent owns its home channel; its parent is a member], the routing down-traversal). Cross-link
  from `docs/xo-doctrine.md` and the Tier-1 references.
- [ ] 10.2 Update the constitutional-set member list reference (the README / doctrine docs) to note the
  second member (visibility-synthesis) and the `heartbeat-skill` mechanism — the "vocabulary extends
  with each new member kind" B1 promised, now realized.

## 11. Validation + review gates

- [ ] 11.1 `openspec validate visibility-synthesis --strict` passes (already passing at design time).
- [ ] 11.2 `/systems-review` + `/open-code-review` on the implementation diff — iterate until clean.
- [ ] 11.3 Confirm the resolved design decisions land as specified (transcript-first substrate; DAG
  self-edge exclusion; the agent-param wake seam; the durable daemon/disk last-seen materiality; the
  heartbeat-skill whole-file STAT idempotency + the `Install` workspace-dir signature change).
- [ ] 11.4 Resolve the remaining open questions in the re-trio (Q-B cadence value/ownership, Q-C
  materiality hash granularity, Q-D `Install` signature shape, Q-E multi-hub post fan-out, Q-F
  Tier-3-reads-Tier-2 latest-state recursion).
