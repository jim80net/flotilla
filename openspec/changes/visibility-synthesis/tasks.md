# Tasks — visibility-synthesis (B2)

Bite-sized TDD tasks. Each writes the failing test first, then the minimal code. The
implement-phase gate is `/systems-review` + `/open-code-review` on the Go diff (per the standard
flow). The change MUST stay inert when the synthesis member is absent / no synthesis is owed
(`$0`-idle preserved).

## 0. Verify-first (gates the implement phase)

- [ ] 0.1 Confirm the `internal/cos/ledger.go` atomic-append contract holds for a SECOND ledger
  written by the same `watch` daemon process (the Tier-1 mirror appends both a Discord post and a
  ledger line) — re-read `internal/cos/ledger.go` `Append`/`Line` and confirm the
  `O_APPEND`-under-`PIPE_BUF` reasoning + the host-local-filesystem requirement carry over verbatim.
- [ ] 0.2 Confirm `claudestore.LatestTurnText` (`internal/claudestore/claudestore.go:294`) is the
  enrichment drill-down path (no change needed — read-only reuse) and that resolving a boat's cwd for
  it goes through `deliver.PaneCWD`.
- [ ] 0.3 Confirm the Injector addresses an arbitrary agent (`watch.Job{Agent: ...}`) so a
  `WakeSynthesis` can enqueue to a non-primary synthesizing XO (Q-A) — read the Injector enqueue path.

## 1. Routing — `ChannelsAwareOf` accessor + the self-loop read set (`internal/roster`)

- [ ] 1.1 TEST: `ChannelsAwareOf(agent)` returns every channel id where the agent is a `member` OR
  the `xo_agent`, over `Bindings()` — for the federated multi-channel form AND the legacy
  single-binding form (where every agent is a member).
- [ ] 1.2 TEST: `ChannelsAwareOf` is a pure read-only derivation — it does not mutate any binding's
  `Members` slice (respect the read-only-slice contract).
- [ ] 1.3 IMPL: add `ChannelsAwareOf(agent string) []string` to `internal/roster/roster.go`.
- [ ] 1.4 TEST: the synthesis READ set = `ChannelsAwareOf(agent)` MINUS the channels the agent OWNS
  (all channels where `ch.XOAgent == agent`) — for a multi-channel XO that is BOTH a member of a
  peer's channel and the XO of its own, the read set excludes its own channel (the self-loop guard).
- [ ] 1.5 IMPL: expose the owned-channel set (generalize `ChannelForXO` to all owned channels, or a
  helper) and the read-set derivation (a `SynthesisReadSet(agent)` accessor, or document the
  minus-owned composition at the call site — pick one in review).

## 2. Membership-graph DAG assertion (`internal/roster`)

- [ ] 2.1 TEST: `Load` REFUSES a roster whose channel-membership graph has a cycle (two channels each
  listing the other's XO as a member) with a clear error.
- [ ] 2.2 TEST: `Load` ACCEPTS an acyclic federation (Tier-3 meta-XO channel with project-XOs as
  members; each project channel with its boats as members).
- [ ] 2.3 TEST: the legacy single-binding form (no `channels[]`) loads (it is trivially acyclic).
- [ ] 2.4 IMPL: add a depth-first-search cycle check over the `Bindings()` edges (XO → each member)
  to `roster.Load`, fail-closed. Runs once at load, not on the hot path.

## 3. The mirror-event ledger (new `internal/synthledger`, modeled on `internal/cos`)

- [ ] 3.1 TEST: `Line(Event)` renders a single physical line (time · channel · agent · `%q` gist),
  rune-clamped gist + byte-clipped backstop, exactly like `cos.Line` (newline-flattened fields, ≤
  `maxLineBytes`).
- [ ] 3.2 TEST: `Append(path, e)` atomically appends one line; two concurrent appenders never
  interleave (the `O_APPEND`-under-`PIPE_BUF` contract); a missing file is created.
- [ ] 3.3 TEST: a tail-read helper returns events appended since a watermark (the synthesis read
  path reads only the tail since its last synthesis), filtered to a given channel set.
- [ ] 3.4 IMPL: `internal/synthledger/ledger.go` — `Event{Time, Channel, Agent, Gist}`,
  `Line`, `Append`, and a watermark-tail reader. Reuse the CoS clamp/flatten/clip discipline (factor
  a shared helper only if review prefers; default: a parallel package, orthogonal to CoS per §6).
- [ ] 3.5 IMPL: roster plumbing for the synthesis-ledger path (a `SynthLedger` field, defaulted at
  load to `<roster-dir>/synthesis-ledger.md` when synthesis is active, with the same LOCAL-filesystem
  documentation as `CosLedger`; inert/empty when synthesis is not configured).

## 4. Tier-1 mirror writes a ledger event (additive, best-effort)

- [ ] 4.1 TEST: the Tier-1 mirror path appends one ledger event per boat finish (beside its Discord
  post), carrying the boat's channel, agent, and clamped turn-final gist.
- [ ] 4.2 TEST: a ledger-append failure NEVER affects the Discord mirror, the detector tick, or
  delivery (logged + dropped, like the CoS best-effort contract and the Tier-1 observe-only contract).
- [ ] 4.3 IMPL: add the gated best-effort ledger append to the mirror closure (wherever the Tier-1
  `MirrorOnFinish` is wired in `cmd/flotilla/watch.go`), gated on the synthesis ledger being
  configured. No change to the detector trigger itself.

## 5. The `WakeSynthesis` wake-kind + the digest cadence (`internal/watch/detector.go`)

- [ ] 5.1 TEST: a boat-finish (Working→Idle, non-XO) marks synthesis "owed" for that channel's XO
  (alongside the existing `pendingMirrors`).
- [ ] 5.2 TEST: the detector fires `WakeSynthesis` for a synthesizing agent AT MOST once per the
  digest sub-cadence per agent while it has synthesis owed (debounce-up — a burst coalesces to one).
- [ ] 5.3 TEST: with no synthesis owed, no `WakeSynthesis` fires (idle `$0` cost; behavior
  byte-identical to before when the feature is inert).
- [ ] 5.4 TEST: the `WakeSynthesis` side-effect is enqueued in `runTail`, OUTSIDE `d.mu`, and is
  enqueued to the SYNTHESIZING agent (which may differ from `d.cfg.XOAgent`).
- [ ] 5.5 IMPL: add `WakeSynthesis WakeKind`; the per-agent owed-set + digest-cadence counter in the
  detector; emit it in `runTail` like the other wakes. Default cadence wired so an unconfigured
  deployment is inert. Resolve Q-A (one detector keyed by synthesizing agent) + Q-B (the daemon floor)
  in review.
- [ ] 5.6 TEST + IMPL: the owed-set keying maps a boat's channel → its synthesizing XO via the roster
  (`BindingForChannel(...).XOAgent`) so the wake targets the correct agent; a boat in a Tier-2 channel
  marks its project XO owed, and a project-XO synthesis post marks the meta-XO owed (the Q-E recursion
  — a synthesis post is itself a ledger event for the level above).

## 6. The `wake` prompt composer (`cmd/flotilla/watch.go`)

- [ ] 6.1 TEST: a `WakeSynthesis` kind composes a prompt that points the agent at its read-set ledger
  tail + its post target (`ChannelForXO` via `secrets.Webhook`) + the per-tier output contract +
  the narrow-answer discipline (curate what changed, else advance the watermark and reply idle).
- [ ] 6.2 IMPL: add the `WakeSynthesis` case to the `wake` switch; reference the embedded skill (the
  prompt is the thin trigger; the skill file carries the detailed curation instructions).

## 7. The `heartbeat-skill` mechanism + the registry member (`internal/doctrine`)

- [ ] 7.1 TEST: `MechanismHeartbeatSkill` installs as a WHOLE-FILE member — a missing skill file in
  the workspace is CREATED, an existing one is KEPT (operator edits survive), reported created/kept.
- [ ] 7.2 TEST: an identity-append member and a heartbeat-skill member install together via the SAME
  `doctrine.Install` loop with no loop change (the loop dispatches by mechanism); the identity-append
  arm is unaffected by the new arm.
- [ ] 7.3 IMPL: add `MechanismHeartbeatSkill Mechanism = "heartbeat-skill"` to
  `internal/doctrine/doctrine.go`; add its whole-file kept/created dispatch arm to
  `internal/doctrine/install.go` (the `switch m.Mechanism` second case — landed in THIS change per
  the MECHANISM COUPLING contract); add a `TargetFile` (workspace-relative) field to `Member` for the
  whole-file path (e.g. `skills/visibility-synthesis.md`).
- [ ] 7.4 IMPL: register the visibility-synthesis member in the `members` slice
  (`Mechanism: MechanismHeartbeatSkill`, content from `assets/skills/visibility-synthesis.md`).
- [ ] 7.5 TEST: `workspace init` seeds BOTH members (the Rule-of-Three identity-append AND the
  visibility-synthesis whole-file skill) via the same `doctrine.Install`; re-running init keeps both
  unchanged.

## 8. The skill content (`assets/skills/visibility-synthesis.md`)

- [ ] 8.1 Author the curation prompt: how to read the read-set ledger tail since the watermark, how to
  curate Tier 2 (domain rollup, grouped by boat, surface attention-worthy items, not a firehose), how
  to curate Tier 3 (#c2 headline + operator decisions + drill-down pointers down the membership
  graph), the narrow-answer discipline (no manufactured synthesis), and how to advance the watermark.
  Spell out acronyms.

## 9. Docs

- [ ] 9.1 `docs/visibility.md` — the stratified-visibility doctrine doc (the source of truth for the
  three tiers, the up-flow/inverse-drill-down, the LOCAL substrate, the routing transpose). Cross-link
  from `docs/xo-doctrine.md` and the Tier-1 references.
- [ ] 9.2 Update the constitutional-set member list reference (the README / doctrine docs) to note the
  second member (visibility-synthesis) and the `heartbeat-skill` mechanism — the "vocabulary extends
  with each new member kind" B1 promised, now realized.

## 10. Validation + review gates

- [ ] 10.1 `openspec validate visibility-synthesis --strict` passes (already passing at design time).
- [ ] 10.2 `/systems-review` + `/open-code-review` on the implementation diff — iterate until clean.
- [ ] 10.3 Resolve the design open questions (Q-A target generality, Q-B cadence value/ownership, Q-C
  materiality, Q-D separate-vs-shared ledger, Q-E Tier-3-reads-Tier-2-recursion) in the trio.
- [ ] 10.4 hydra-ops ratification fork: the substrate recommendation (local ledger (b) primary +
  transcript (a) enrichment) and the `heartbeat-skill` whole-file mechanism — surface at the design
  gate; hydra-ops merges on clean gates.
