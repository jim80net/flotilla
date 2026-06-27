## Why

flotilla's stratified-visibility design has three tiers: awareness flows UP the federation
hierarchy, with depth inverse to altitude (a reader plumbs to any level). **Tier 1** — the
mechanical per-desk mirror — already ships (`desk-mirror-tier1`, pull request #135): when a boat
(a non-Executive-Officer desk) finishes a turn, its turn-final output is posted to that boat's own
Discord channel. What is missing is the SYNTHESIS that makes the higher channels legible:

- **Tier 2 — the Executive-Officer (XO) channel:** a domain XO synthesizes its boats' activity UP
  into its own channel — a curated, compressed domain rollup, not a raw firehose.
- **Tier 3 — the command-and-control channel (#c2):** the meta-XO synthesizes the XO channels UP
  into the fleet-command channel — a fleet headline plus the operator decisions plus drill-down
  pointers back down the hierarchy.

This synthesis is **large-language-model (LLM) curation**, not deterministic daemon code. It is
therefore a constitutional-set SKILL run on a daemon-emitted synthesis wake — the integrating half
of the substrate-or-integrator split the chief-of-staff ledger already embodies
(`internal/cos/ledger.go`: a deterministic append substrate that flotilla writes with no model
call, read by an LLM that writes its integrated view one level up). Tier 1 is the deterministic
mechanical mirror; Tiers 2 and 3 are the integrating LLM, one level up the hierarchy.

This change — **B2 of the ratified B1/B2 split** — ships that synthesis member. It is the SECOND
member of the installable constitutional set that B1 (`constitutional-skillset`, merged) shipped,
plugged into B1's deliberately member-count-agnostic registry via a NEW delivery `Mechanism` value
(`heartbeat-skill`) — the "the vocabulary extends with each new member kind" extensibility B1
promised and left open (`internal/doctrine/doctrine.go:26-36`).

## The ratified substrate decision: TRANSCRIPT-FIRST (read the latest STATE below, no new ledger)

The original B-change design specified synthesis reading "the Tier-1 mirror stream" from Discord
channel history. The design-gate trio (`/systems-review` + STORM, 2026-06-21) found a P1: **that
substrate does not exist as running code.** flotilla is SEND-ONLY to Discord — the gateway is
push-only `MESSAGE_CREATE` (`internal/discord/gateway.go:41-50`), there is no channel-history-fetch
primitive anywhere (grep-clean), and the inbound relay deliberately DROPS webhook posts
(`internal/relay/relay.go:18-23`) precisely so the bot never acts on its own posts. Reading Discord
history would have required a net-new `flotilla read <channel>` primitive, a fleet-wide bot
Read-Message-History permission expansion, and an unbounded-history cost/windowing landmine.

The first revision then proposed a LOCAL mirror-event LEDGER (a chief-of-staff-style append log of
boat-finish events). The re-trio split: STORM argued the ledger is an OVER-BUILD that inverts B1's
bounded-scope lesson (~8 net-new surfaces, including a NEW write-path bolted onto the LIVE shipped
Tier-1 mirror). **alpha-xo ratified (2026-06-21): the B2 substrate is TRANSCRIPT-FIRST, NOT a
ledger.**

**The ratified rationale — a rollup is a STATE view, not an event LOG.** A higher-tier synthesis
answers "where is each subordinate RIGHT NOW" ("the trade-desk is building X, macro is on Y"), not
"replay every finish since I last looked." Tier 2/3 needs the LATEST STATE of each subordinate, not
the finish-history across a burst. The latest state is already available, read-only, with no new
write-path: a synthesizing agent reads the latest turn of the tier BELOW it directly via
`internal/claudestore.LatestTurnText` (`internal/claudestore/claudestore.go:294`) — the SAME reader
the shipped Tier-1 mirror uses. This dissolves at once: the non-existent Discord substrate, the
relay collision, the cost/windowing landmine, the security expansion, AND the new-write-path risk to
the live mirror.

- **NO `internal/synthledger` package.** The ledger is not built.
- **NO new write-path on the live Tier-1 desk-mirror.** The shipped mirror is untouched.
- **The ledger (finish-history across a burst) is a documented FAST-FOLLOW**, filed as GitHub issue
  #138 (label `enhancement`): "visibility-synthesis ledger substrate (finish-history)
  — fast-follow iff burst-history proves needed." It is built ONLY if synthesis is later shown to
  need the history of every finish, not just the latest state per subordinate. See Out of scope.

The stratification (boats → XO channel → #c2) is unchanged; only the data source is the subordinates'
latest transcript state, read directly.

## What Changes

- **A transcript-first synthesis read (the ratified LOCAL source).** A synthesizing agent reads the
  LATEST STATE of each agent in the tier below it through the SAME surface-agnostic seam the shipped
  Tier-1 mirror uses: resolve the subordinate's pane (`deliver.ResolvePane(agentTitle(cfg, sub))`,
  `cmd/flotilla/watch.go:580`), then `rr.LatestResult(pane)` on the agent's `surface.ResultReader`
  (`watch.go:575-588` — for a claude desk this resolves to `claudestore.LatestTurnText`, for grok to
  the grok store; a surface without a `ResultReader`, or a pane that won't resolve, is a CLEAN SKIP).
  This is read-only reuse of the EXACT Tier-1 reader — NOT a direct bind to `claudestore` (which would
  silently exclude a grok subordinate Tier 1 mirrors fine). It is bounded (the LATEST turn per
  subordinate, N bounded reads, not an unbounded windowing pass), relay-disjoint by construction (a
  read-only file read, never a Discord read, never an inbound command), and adds NO new write-path. No
  ledger, no new package, no change to the live Tier-1 mirror. **Reachability invariant:** v1 requires
  each read-set subordinate's pane to be HOST-LOCAL to the synthesizer (the single-host dogfood
  fleet); a subordinate whose pane will not resolve is cleanly skipped, never a crashed wake.
  Cross-host federation is out of scope for v1 (pairs with the #138 ledger).

- **Routing — a down-traversal of the membership graph, no new schema, fleet-command-excluded.** The
  "tier below" an agent A is the set of AGENTS whose NON-fleet-command channels list A as a member:
  `{ ch.XOAgent : A ∈ ch.Members, ch.XOAgent != A, ch.Role != "fleet-command" }` over `Bindings()`
  (`internal/roster/roster.go:289`). A reads each of those agents' latest transcript; A POSTS its
  synthesis to the channel(s) A OWNS (`OwnedChannels(A)`, generalizing `ChannelForXO`, `roster.go:343`)
  via `secrets.Webhook(A)` — the post target INCLUDES the fleet-command channel (the meta posts Tier-3
  into #c2). The owed direction is the inverse `AgentsAbove(A)` (the members of the non-fleet-command
  channels A owns, minus self). Pure read-only derivations over the F#105 `members[]` graph — the SAME
  graph Tier 1 routes on — with NO new roster schema, respecting the read-only-slice contract.

- **Make the topology EXPLICIT (so it is never re-mis-read).** Each agent OWNS its home channel
  (`xo_agent == self`) and its PARENT is in that channel's `members[]` (verified live:
  `xo_agent=delta-xo members=[beta-xo]`; `xo_agent=beta-xo members=[alpha-xo]`).
  So "read the tier below me" = read the agents whose home channel lists ME as a member (a
  DOWN-traversal of the membership graph). The ONE channel where `members[]` runs the OTHER way — the
  fleet-command BROADCAST channel (`role="fleet-command"`, members = the meta-XO's full command list) —
  is EXCLUDED from synthesis-edge derivation (the implement-gate P0): its members are command targets,
  not synthesis parents. The proposal, design, and spec all state this topology plainly.

- **Membership-graph acyclicity, asserted at roster load, fail-closed — WITH self-edge AND
  fleet-command exclusion.** "Read below, post own level" gives acyclicity for free IFF the
  synthesis-edge graph is a DAG. The roster load SHALL assert it and REFUSE to start otherwise. The
  edge model `ch.XOAgent → m` (for `m ∈ ch.Members`) applies TWO exclusions: **(1) self-edges**
  (`m != ch.XOAgent` — the live home-channel self-membership and the legacy single-binding form,
  `roster.go:296-304`, are NOT cycles), and **(2) fleet-command channels** (`ch.Role != "fleet-command"`
  — the live broadcast channel lists all 12 agents; included, its `alpha-xo → {everyone}` edges close
  cycles with the per-XO channels, e.g. `alpha-xo → desk-j → desk-core → alpha-xo`, and
  refuse the live roster — empirically verified). A genuine cycle is a MUTUAL membership between two
  DISTINCT non-fleet-command channels (channel-A's XO is a member of channel-B and channel-B's XO is a
  member of channel-A).

- **Cadence — a daemon-emitted `WakeSynthesis` wake-kind, with an agent param on the wake seam.** A
  new `WakeKind` (a sibling of `WakeContinuation` / `WakeMaterial` / `WakeBacklog` / `WakePing` in
  `internal/watch/detector.go:30-45`), the trio's ROBUST successor to skill-self-scheduling.
  Self-scheduling breaks under the change-detector's idle-wake suppression (an idle fleet wakes
  nothing, so a skill that schedules itself "next tick" never runs) and under context rotation (the
  rotate wipes a self-set timer). Instead the daemon owns the cadence: a boat-finish event marks
  synthesis "owed" for the boat's synthesizing PARENT(s); the detector fires `WakeSynthesis` for that
  parent on a digest sub-cadence (debounce-up), so an idle fleet costs nothing and a busy one
  synthesizes at a bounded rate. The detector's `Wake` callback is `Wake func(kind WakeKind, reasons []string)`
  (`detector.go:68`) and production hardcodes `injector.Enqueue(watch.Job{Agent: xo, ...})`
  (`cmd/flotilla/watch.go:259`); both wake ONLY the daemon's primary XO. A Tier-2 synthesis wake
  targets a PROJECT XO and a Tier-3 wake targets the meta-XO, so the wake seam SHALL carry an AGENT
  parameter via a parallel `WakeAgent` (chosen over widening `Wake`, which would break every existing
  primary-XO call site; the parallel keeps the shipped path byte-identical), and the owed-state SHALL
  be keyed by synthesizing agent. The boat→parent owed resolution uses the `AgentsAbove(agent)` accessor
  (the inverse of `AgentsBelow` — the MEMBERS of the non-fleet-command channels the finishing agent
  OWNS, minus self), NOT `BindingForChannel` (which takes a channel id, not the agent name the detector
  holds, and cannot answer the many-channels-per-boat case). This is a real signature change, not "the Injector handles
  it."

- **A durable materiality/"owed" gate, daemon/disk-owned (NOT skill context — rotation wipes it).**
  Under transcript-first the read itself is stateless (the latest turn per subordinate, resolved
  fresh each wake — no watermark/offset to persist). But the MATERIALITY gate — synthesize only when
  a subordinate's state has CHANGED since the last synthesis, to preserve `$0`-idle and avoid
  re-posting an unchanged rollup — needs a per-synthesizing-agent durable "last-seen" snapshot (a hash
  of each subordinate's last-synthesized turn text). It SHALL be a DISK SIDECAR surviving BOTH context
  rotation (`/clear` wipes skill-context state) AND daemon restart (an in-memory-only snapshot would
  re-post every subordinate as "new" on the first post-restart wake — a restart-storm). An UNREADABLE
  subordinate (pane won't resolve) is EXCLUDED from the hash, never hashed as empty (which would flap
  the wake on a transient failure). The spec requires this durable last-seen state and reconciles it
  with transcript-first (no separate ledger watermark is needed — the latest-state read plus the
  durable last-seen hash IS the materiality mechanism).

- **The synthesis member ships via B1's installable surface as a NEW `heartbeat-skill` mechanism.**
  This change EXTENDS B1's `Mechanism` vocabulary (which today is `identity-append` ONLY,
  `doctrine.go:36`) with `MechanismHeartbeatSkill` plus the write/load behavior that value implies (a
  whole-file skill written into the agent's workspace, invoked when the daemon emits `WakeSynthesis`
  — NOT appended into the agent's standing identity, because synthesis is a tick-time discipline, not
  a structural identity rule). **`doctrine.Install` needs a SIGNATURE CHANGE.** Its current signature
  is `Install(identityPath string, members []Member)` (`internal/doctrine/install.go:40`) — an
  identity-file path ONLY. A whole-file member writes `<workspace>/skills/visibility-synthesis.md`,
  which an `identityPath`-only signature cannot resolve, so `Install` SHALL take a WORKSPACE-DIR param
  and DERIVE the identity path from `workspace.IdentityFileName` (Q-D resolved — one source of truth
  for the layout), changed at BOTH call sites (`cmd/flotilla/workspace.go:148`,
  `cmd/flotilla/doctrine.go:50`). The whole-file idempotency SHALL be STAT-based (kept-if-exists) —
  NOT the marker fence, because the identity-append marker guard `appendOnce` hard-errors on an empty
  `OpenMarker` (`internal/doctrine/install.go:85`) and a whole-file member carries no marker; the
  whole-file CREATE does its OWN `os.WriteFile`, disjoint from the identity-content write-back. The
  skill CONTENT is the curation prompt.

- **Generalize `ChannelForXO` → `OwnedChannels(agent)` for the multi-hub case.** `ChannelForXO`
  (`roster.go:343`) returns only the FIRST channel an XO owns; a multi-hub XO owns several. The
  owned-channel set (the post-target derivation AND the self-loop exclusion in routing) generalizes to
  an `OwnedChannels(agent)` accessor returning ALL channels where `ch.XOAgent == agent`.

- **Per-tier output contracts.** Tier 2 = a curated domain rollup (what the boats are doing,
  compressed, with what needs the operator's eye surfaced). Tier 3 = a fleet command-and-control
  headline + the open operator-decisions + drill-down pointers down the membership graph (the inverse
  chain #c2 → XO channel → boat channel → pane). A concrete rendered #c2 example is in `design.md`.

## Out of scope

- **Tier 1 (the mechanical per-desk mirror).** Already shipped (`desk-mirror-tier1`). This change
  CONSUMES its substrate (the boats' latest transcript state, via the SAME `claudestore` reader Tier
  1 uses) but does not re-spec it AND does NOT add any new write-path onto it. The live mirror is
  untouched by this change.
- **The local mirror-event LEDGER (finish-history substrate).** Explicitly DEFERRED as a fast-follow
  at ratification — filed as GitHub issue #138 (label `enhancement`). The ratified
  v1 substrate is transcript-first (latest STATE per subordinate). A ledger of finish-HISTORY is
  built ONLY iff synthesis is later shown to need the history of every finish across a burst, not just
  the latest state. Do NOT build it in this change.
- **The B1 installable surface itself.** Shipped (`constitutional-skillset`). This change adds a
  member and a mechanism to it; it does not re-spec the install/seed loop, the embed tree, or the
  identity-append marker-guard. (It DOES change the `Install` signature to plumb the workspace dir —
  see the heartbeat-skill bullet.)
- **A Discord channel-history read primitive.** Explicitly rejected at ratification — the substrate
  is LOCAL transcript state. No `flotilla read`, no bot Read-Message-History permission expansion.
- **The broader constitutional corpus.** WHICH further behaviors join the default set remains the
  operator's strategic lever; this change adds exactly one member (visibility-synthesis).
- **Channel/webhook provisioning.** Provisioning channels from the roster is the separate
  `flotilla provision` line; this change reuses existing per-XO webhooks (`secrets.Webhook`).

## Impact

- **New capability spec:** `visibility-synthesis`.
- **Affected code (implement phase, after the re-trio + on the ratified transcript-first substrate):**
  - `internal/roster/roster.go` — the down-traversal `AgentsBelow(agent)` (the XOs of the
    NON-fleet-command channels that list A as a member, minus self) and its exact inverse
    `AgentsAbove(agent)` (the members of the non-fleet-command channels A OWNS, minus self — the
    owed-marking resolver, replacing the wrong-typed `BindingForChannel`); generalize `ChannelForXO` →
    an `OwnedChannels(agent)` accessor (post target, fleet-command INCLUDED); a `role=="fleet-command"`
    predicate; a DAG acyclicity check in `Load` with BOTH self-edge AND fleet-command exclusion. All
    pure read-only derivations over `Bindings()`.
  - `internal/watch/detector.go` — net-new `WakeSynthesis` `WakeKind`; a parallel agent-targeted
    `WakeAgent` seam (leaving the shipped `Wake` byte-identical); the per-synthesizing-agent
    "synthesis owed" set (resolved via `AgentsAbove`) + the durable, restart-surviving disk-sidecar
    last-seen materiality state (unreadable subordinates excluded); the digest sub-cadence that fires
    it.
  - `internal/doctrine/doctrine.go` + `install.go` — net-new `MechanismHeartbeatSkill` value; a
    `TargetFile` (workspace-relative) field on `Member`; the whole-file STAT-based kept/created
    dispatch arm; the `Install` SIGNATURE change (a workspace-dir param) at both call sites
    (`cmd/flotilla/workspace.go:148`, `cmd/flotilla/doctrine.go:50`); the visibility-synthesis member
    registry entry.
  - `cmd/flotilla/watch.go` — a `WakeSynthesis` case in the `wake` prompt composer
    (`watch.go:245-260`), enqueuing the synthesis prompt to the SYNTHESIZING agent (the new agent
    param), not the hardcoded primary `xo`.
  - `internal/claudestore` — NO change; the transcript read is read-only reuse of
    `LatestTurnText` (`claudestore.go:294`).
  - `internal/doctrine/assets/skills/visibility-synthesis.md` — the embedded curation-prompt skill
    content (the embed tree is `internal/doctrine/assets/skills`, `doctrine.go:23`).
  - `docs/visibility.md` — the stratified-visibility doctrine doc (source of truth for the tiers).
- **Chief-of-staff orthogonality.** The CoS ledger is a HORIZONTAL who-knows-what view (operator↔XO
  exchanges across channels); visibility synthesis is a VERTICAL activity-rollup (boats up to their
  XO, XOs up to #c2). They are orthogonal axes and independent heartbeat steps; the spec asserts
  this. Under transcript-first they do not even share a substrate shape — CoS is an append ledger,
  synthesis is a direct transcript read.
- **Risk:** MODERATE-LOW (lower than the ledger revision). The transcript read is read-only reuse of a
  shipped reader (`claudestore.LatestTurnText`) with NO new write-path and ZERO risk to the live Tier-1
  mirror; the `ChannelsAwareOf` / `AgentsBelow` / `OwnedChannels` accessors are pure additive
  derivations; the `WakeSynthesis` cadence is a new detector branch with a real wake-seam signature
  change (covered by the detector's existing under-mutex/runTail discipline and unit-test harness);
  the heartbeat-skill mechanism is a new install dispatch arm plus the `Install` signature change (the
  registry was built for new mechanisms; the signature change touches two call sites). The DAG check
  is fail-closed (a cyclic roster refuses to start) with the self-edge exclusion that keeps the LIVE
  roster loading. No existing path changes behavior when the synthesis member is absent (inert by
  default).
