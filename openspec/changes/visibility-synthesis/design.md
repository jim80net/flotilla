# Design — visibility-synthesis (Tiers 2 and 3, on the ratified TRANSCRIPT-FIRST substrate)

This is B2 of the ratified B1/B2 split. B1 (`constitutional-skillset`, merged) shipped the
installable constitutional-set surface plus its first member (the Rule of Three). B2 ships the
SECOND member — the visibility-synthesis skill — and the local substrate, routing, and cadence it
needs. The grounding is `.claude/handoffs/20260620-visibility-and-constitutional-skillset-design.md`
(the "B2 DESIGN-TRIO" and "RECONCILED B2 verdict" sections — the authoritative fix-list this
revision applies).

## 0. The topology — stated EXPLICITLY (so routing is never re-mis-read)

The design-trio's systems-review pass mis-modeled the federation topology and read the routing as
inverted (a FALSE finding — see the handoff). To make it impossible to re-mis-read, the actual
topology is stated plainly here, in the spec Purpose, and below in §2.

**Each agent OWNS its home channel and its PARENT sits in that channel's `members[]`.** Concretely,
verified against the LIVE roster:

- `xo_agent = tactical-head`, `members = [family-office]` — the trade-desk channel: tactical-head
  owns it; its parent family-office is a member.
- `xo_agent = family-office`, `members = [hydra-ops]` — the project-XO channel: family-office owns
  it; its parent (the meta-XO) hydra-ops is a member.

So the synthesis up-link of an agent C is **the members of the channel C OWNS** (its home channel);
inverted, "the tier below me" = the agents whose OWN channel lists me as a member. An XO is listed in
each of its desks' home channels, so the agents whose home channel lists the XO are exactly its desks
(Tier 2). The meta-XO is listed in each project-XO's home channel, so the agents whose home channel
lists the meta-XO are exactly the project-XOs (Tier 3). Command flows DOWN; awareness flows UP; the
same `members[]` graph, traversed in opposite directions.

### The fleet-command (broadcast) channel — EXCLUDED from synthesis edges (the implement-gate P0)

`members[]` is OVERLOADED, and grounding the implementation in the LIVE roster (per
verify-before-acting) caught it where the design-trio and the legacy-star example rosters did not. In a
per-XO home channel, `members` is the PARENT up-link (one agent). But the LIVE fleet-command channel —
`xo_agent = hydra-ops`, `role = "fleet-command"`, `members = [all 12 agents]` — uses `members` as the
meta-XO's command/broadcast DOWN-list (everyone it can address), the OPPOSITE direction. Read as a
synthesis up-link, that one channel is poison:

- `AgentsBelow(tactical-head)` would include `hydra-ops` (a leaf desk "synthesizing" the meta-XO),
  because tactical-head is a member of the broadcast channel whose XO is hydra-ops.
- `AgentsBelow(family-office)` would include `hydra-ops` (its own boss).
- The DAG check would find a CYCLE (`hydra-ops → … → hydra-ops`) and REFUSE TO START the live daemon.

**The fix (ratified, verified against the live roster): a `role == "fleet-command"` channel
contributes ZERO synthesis edges.** Its members are command targets, not synthesis parents. Synthesis
edges (AgentsBelow / AgentsAbove / the DAG) are derived ONLY from NON-fleet-command channels. `role`,
cosmetic until now, becomes LOAD-BEARING for synthesis. With the fleet-command channel excluded the
live topology is exactly two-tier and acyclic: `AgentsBelow(hydra-ops) = {the 5 project-XOs}`,
`AgentsBelow(family-office) = {its 5 boats}` (no hydra-ops leak), leaves empty, DAG cycle = NONE. The
meta-XO still POSTS its Tier-3 synthesis INTO the fleet-command channel it owns; only the READ
derivation excludes it. (A new schema field — `parent`/`reports_to` — was considered and REJECTED:
`role` already exists for exactly this, no new schema.)

## 1. The substrate (B2's core design call) — TRANSCRIPT-FIRST

### The two local shapes considered

The ratification fixed the substrate as LOCAL (not Discord channel history). Two local shapes were on
the table:

- **(a) Direct transcript read via the surface `ResultReader` seam — the LATEST STATE of each
  subordinate.** The synthesizing agent reads each subordinate's LOCAL latest turn-final state
  through the SAME polymorphic seam the shipped Tier-1 mirror uses: resolve the subordinate's pane
  (`deliver.ResolvePane(agentTitle(cfg, sub))`, `cmd/flotilla/watch.go:580,601`), get the agent's
  driver (`surface.Get`), and call `rr.LatestResult(pane)` on its `surface.ResultReader`
  (`watch.go:575-588`). For a claude desk that resolves internally to `claudestore.LatestTurnText`
  (`internal/surface/claude.go:39,106`); for a grok desk to the grok store
  (`internal/surface/grok.go`); a subordinate whose driver has no `ResultReader` (or whose pane will
  not resolve) is a CLEAN SKIP, exactly as Tier 1 skips an unreadable desk (`watch.go:530,577`). The
  meta-XO reads the project-XOs' latest state the same way. This is read-only reuse of the EXACT
  surface-agnostic reader the shipped Tier-1 mirror already uses — no new package, no new write-path —
  and NOT a direct bind to `claudestore.LatestTurnText`, which would silently exclude a grok or
  future-surface subordinate that Tier 1 mirrors fine.

- **(b) A bounded local mirror-event LEDGER (the chief-of-staff substrate/integrator pattern).** The
  Tier-1 mirror, in addition to posting to Discord, appends ONE bounded event record per boat finish
  to a deterministic append-ledger modeled on `internal/cos/ledger.go`. The synthesis LLM reads that
  ledger's tail since a watermark.

### Ratified: (a) transcript-first. (b) the ledger is a fast-follow.

**hydra-ops ratified (a) TRANSCRIPT-FIRST.** The deciding question, surfaced by STORM and put to
hydra-ops as the ratification fork, was: **does a higher-tier synthesis need the HISTORY of every
finish across a burst, or the LATEST STATE of each subordinate?** The answer is latest-state: a
rollup is inherently a STATE view — "the trade-desk is building X, macro is on Y" — not an event log
to replay. So:

1. **A rollup is current-STATE, not an event-LOG.** Tier 2/3 answers "where is each subordinate right
   now," which is exactly the latest turn of each subordinate's transcript. The latest-state read is N
   bounded reads (one latest-turn per subordinate), already what Tier 1 does per desk.

2. **Of the ledger's three justifications, only "bounded read" discriminated — and it does NOT, for
   latest-state.** Relay-disjointness is a WASH: a transcript read via `claudestore.LatestTurnText` is
   equally relay-disjoint (a read-only file read, never a Discord read, never through
   `relay.Accept`/`relay.Route`). Reuse applies to BOTH: `claudestore` is shipped, reviewed, and
   already used by Tier 1. The only thing the ledger buys is finish-HISTORY (every finish in a burst)
   — which a STATE rollup does not need.

3. **The ledger over-builds (it inverts B1's bounded-scope lesson).** The ledger revision bundled ~8
   net-new surfaces — a new `internal/synthledger` package, a NEW write-path bolted onto the LIVE
   shipped Tier-1 mirror, a dual-write ordering inside the live mirror closure, a watermark-tail
   reader with read-vs-append concurrency, a durable watermark — for a capability the STATE rollup
   does not require. Transcript-first ships the value with FAR less surface and ZERO risk to the live
   mirror.

**The ledger (finish-history) is a documented FAST-FOLLOW.** It is filed as GitHub issue
#138 (label `enhancement`) and built ONLY iff synthesis is later shown to need the
history of every finish across a burst (e.g. if "what did boat X conclude at 02:40 vs 02:55" turns
out to matter for a rollup, which latest-state cannot answer). It is NOT built in B2.

### What the transcript-first decision DISSOLVES

Several ledger-era under-specs and concerns simply go away under transcript-first:

- **The watermark-tail READER dissolves.** There is no append log to tail, so there is no
  read-vs-append concurrency, no "parse complete lines only / ignore a trailing partial." Each wake
  resolves the latest turn per subordinate fresh.
- **The dual-write ORDERING dissolves.** There is no second write inside the live-mirror closure, so
  there is no "ledger append strictly after the Discord post / never blocking it" ordering to specify.
  The live mirror is untouched.
- **The lossy-280-rune-gist concern (the trio's #c2 / P3) dissolves.** Synthesis reads the FULL latest
  turn text of each subordinate, not a 280-rune ledger gist. So the #c2 example's structured
  operator-decisions are derived from full turn text, not from a lossy gist — no "decisions are
  gist-derived (lossy) vs need enrichment" ambiguity.

### What transcript-first still needs (and §3/§4 specify)

- A bounded read contract: the LATEST turn per subordinate (one `ResultReader.LatestResult` per agent
  in the read set), not an unbounded windowing pass. N subordinates ⇒ N bounded reads per wake.
- **The agent→pane→transcript resolution hop, made EXPLICIT (the re-trio's P1-B).** `LatestResult`
  (and `LatestTurnText` beneath it) takes a tmux PANE, not an agent name — but the read set
  (`AgentsBelow(A)`) yields agent NAMES. Synthesis SHALL resolve each subordinate name → pane title →
  pane via `deliver.ResolvePane(agentTitle(cfg, sub))` (the SAME hop Tier 1 does, `watch.go:580`),
  then call `LatestResult(pane)`. It does NOT re-derive cwd itself — `PaneCWD`→encode→glob is the
  reader's INTERNALS, owned by `LatestResult`, not the caller's job.
- **The single-host reachability invariant + fail-soft (the re-trio's P1-B).** A subordinate's
  transcript is a local file resolvable only via its tmux pane on THIS host. v1 synthesis requires
  every read-set subordinate's pane to be HOST-LOCAL to the synthesizer (true on the single-host
  dogfood fleet). A subordinate whose pane will not resolve (cross-host, or transiently gone) is
  CLEANLY SKIPPED from the rollup — never a crashed wake — mirroring Tier 1 (`watch.go:577`).
  Cross-host federation (a meta-XO reading project-XOs on other hosts) is OUT OF SCOPE for v1 and
  pairs naturally with the #138 ledger (a host-portable substrate). The spec states this as a
  precondition, not an implicit assumption.
- A DURABLE materiality gate (§3): the read is stateless, but "did a subordinate's state CHANGE since
  I last synthesized" needs a daemon/disk-owned last-seen snapshot — surviving BOTH context rotation
  AND daemon restart — because the skill's own context is wiped by rotation.

## 2. Routing — a down-traversal of the membership graph (no new schema)

Synthesis routing is the TRANSPOSE of the command graph, derived purely from the F#105 `members[]`
graph (`internal/roster/roster.go:289` `Bindings()`), NO new schema — with fleet-command channels
excluded from the EDGE derivation (the implement-gate P0, §0).

- **READ set (the agents in the tier below A)** = `AgentsBelow(A)` =
  `{ ch.XOAgent : A ∈ ch.Members, ch.XOAgent != A, ch.Role != "fleet-command" }` over `Bindings()`.
  For every NON-fleet-command channel whose members list A, that channel's XO is a subordinate of A — A
  reads that XO's latest transcript. TWO exclusions, both load-bearing: `ch.XOAgent != A` (never your
  OWN channel — the self-loop guard) and `ch.Role != "fleet-command"` (never the BROADCAST channel —
  its members are command targets, not subordinates; without this a leaf desk would "synthesize" the
  meta-XO and the graph would cycle, §0).
- **OWED set (the synthesizing parents of agent C)** = `AgentsAbove(C)` =
  `{ m : ch.XOAgent == C, m ∈ ch.Members, m != C, ch.Role != "fleet-command" }` over `Bindings()` —
  the members of the NON-fleet-command channels C OWNS, minus self. This is the EXACT relational
  inverse of `AgentsBelow` (`C ∈ AgentsBelow(P) ⟺ P ∈ AgentsAbove(C)`), and it is the owed-marking
  resolver (§3): a finishing agent C marks each `P ∈ AgentsAbove(C)` owed. (Note: this is the MEMBERS
  of C's owned channels — NOT "the XOs of channels listing C", which would be `AgentsBelow` again.)
- **POST target** = the channel(s) A OWNS = `OwnedChannels(A)` (generalizing `ChannelForXO`,
  `roster.go:343`; A may hub several — it posts primary-first, fan-out deferred per Q-E) via
  `secrets.Webhook(A)`. The POST target **INCLUDES** the fleet-command channel: the meta-XO posts its
  Tier-3 synthesis INTO #c2 (the channel it owns). Only the READ/OWED edge derivation excludes
  fleet-command — the post does not.

`AgentsBelow`, `AgentsAbove`, and `OwnedChannels` are pure read-only derivations over `Bindings()` (no
mutation of any binding's `Members` slice — the read-only-slice contract).

### Why the two exclusions are load-bearing (self-loop guard + broadcast-channel guard)

- **Owned-channel / self exclusion (`ch.XOAgent != A`).** The F#105 multi-channel-XO model
  (`roster.go:44-61`) lets an agent be BOTH a `member` of a peer's channel AND the `xo_agent` of its
  own (and the legacy single-binding form lists the XO among its own members). Without the `!= A`
  clause an agent's read set would include its own channel, so it could synthesize its own synthesis
  posts — a self-loop. The clause closes it: read strictly below, never your own.
- **Fleet-command / broadcast exclusion (`ch.Role != "fleet-command"`).** The broadcast channel lists
  every agent as a member for COMMAND addressing (down), not as synthesis parents (up). Reading it as a
  synthesis edge inverts the hierarchy (leaves "above" the meta) and cycles the DAG. Excluding it is
  the P0 fix; it is what makes the live roster load and route correctly.

### The DAG assertion — WITH self-edge AND fleet-command exclusion (the trio's P1-2 + the implement-gate P0, CRITICAL)

"Read below, post own level" gives acyclicity for free IFF the synthesis-edge graph is a directed
acyclic graph (DAG). The roster `Load` asserts this and REFUSES to start otherwise (fail-closed,
consistent with every other roster invariant — duplicate channel id, unknown member, etc.).

**The edge model must apply BOTH exclusions, or it refuses the LIVE roster.** The synthesis-edge graph
is built with an edge `ch.XOAgent → m` for each `m ∈ ch.Members` such that:

- `m != ch.XOAgent` (DROP self-edges). An agent being a member of its OWN channel
  (`ch.XOAgent ∈ ch.Members` — the live home-channel self-membership and the legacy single-binding
  form, `Bindings()`, `roster.go:296-304`) is NOT a cycle; it is the normal home-channel shape. A naive
  model would flag it as `hydra-ops → hydra-ops` and refuse to start.
- `ch.Role != "fleet-command"` (DROP the broadcast channel's edges — the implement-gate P0). The
  fleet-command channel lists all 12 agents as members for command addressing; included, it adds
  `hydra-ops → {every agent}` edges that close cycles with the per-XO home channels (`hydra-ops →
  flotilla-dash → flotilla-dev → hydra-ops`, empirically verified) and REFUSE the live roster.

The cycle to genuinely catch is a MUTUAL membership between two DISTINCT non-fleet-command channels:
channel-X's XO is a member of channel-Y AND channel-Y's XO is a member of channel-X (X's XO would
synthesize Y while Y's XO synthesizes X — an infinite mutual rollup). Formally: build the directed
graph with the edges above (both exclusions applied), then standard depth-first-search cycle detection.
It runs once at load, not on the hot path.

## 3. Cadence — the daemon-emitted `WakeSynthesis` wake-kind (with an agent param on the seam)

### Why not skill-self-scheduling (the trio's Q3)

The original design left "skill-judged cadence vs daemon-emitted wake" open. The trio's robust answer
is the daemon wake. Self-scheduling breaks twice:

1. **Idle-wake suppression.** The change-detector's whole point is that an idle fleet wakes nothing
   (`$0`-idle). A skill that says "synthesize again next tick" relies on there BEING a next wake — but
   on an idle fleet there is none, so the self-scheduled synthesis silently never runs.
2. **Context rotation.** `continueXO` rotates the XO context (`/clear`) between handlings
   (`detector.go` `continueXO` → `requestRotate`). A self-set "remind me to synthesize" timer lives in
   the XO's context and is wiped by the rotate. The daemon's state survives the rotate; the skill's
   does not.

### The wake-seam signature change (the trio's P2-1 / Q-A, RESOLVED — not "the Injector handles it")

The detector's wake callback is `Wake func(kind WakeKind, reasons []string)` (`detector.go:68`) and
production hardcodes the target: `injector.Enqueue(watch.Job{Agent: xo, ...})` (`cmd/flotilla/watch.go:259`,
where `xo` is the daemon's single primary XO). Both wake ONLY `d.cfg.XOAgent`. But `WakeSynthesis` must
target an ARBITRARY synthesizing agent — a PROJECT XO for Tier 2, the meta-XO for Tier 3 — which is
generally NOT the daemon's primary clock XO. This is a real signature change, RESOLVED here (it is
normative, not an open question):

- The wake seam SHALL carry an AGENT parameter, via a PARALLEL
  `WakeAgent func(agent string, kind WakeKind, reasons []string)` for the synthesis path while the
  existing `Wake` keeps clocking the primary XO UNCHANGED (the re-trio's P2-1, RESOLVED). The two
  shapes are NOT equal-risk: WIDENING `Wake` to `Wake func(agent, kind, reasons)` is a breaking change
  to the shipped primary-XO wake path (every existing call site must pass an agent), whereas a
  parallel `WakeAgent` leaves the shipped path BYTE-IDENTICAL — which is the entire
  inert-when-the-feature-is-absent risk story this change rests on. So the parallel `WakeAgent` is the
  chosen, lower-blast-radius shape. The production composer (`cmd/flotilla/watch.go:245-260`) enqueues
  `watch.Job{Agent: <synthesizing agent>, ...}` — the Injector already addresses any agent, so the
  enqueue is general once the agent flows through the seam.
- The detector's "synthesis owed" state SHALL be keyed by SYNTHESIZING agent (a per-agent owed-set,
  the way it tracks `pendingMirrors`), NOT a single primary-XO flag.
- ONE detector, agent-keyed — not a per-XO detector (the recommendation, now resolved).

### The mechanism: debounce-up, daemon-owned, with a DURABLE materiality gate

- A new `WakeKind`, `WakeSynthesis`, sibling of `WakeContinuation`/`WakeMaterial`/`WakeBacklog`/
  `WakePing` (`detector.go:30-45`).
- **"Owed" marking (the re-trio's P1-A — a real desk→XO resolver, NOT `BindingForChannel`).** A
  boat-finish event (the same confirmed Working→Idle transition Tier-1 mirrors on) marks synthesis
  "owed" for the agent(s) ABOVE that boat. The detector resolves the finishing AGENT NAME → its
  synthesizing parent(s) via the `AgentsAbove(agent)` accessor (§2) — the members of the
  NON-fleet-command channels the agent OWNS, minus self; the exact relational inverse of `AgentsBelow`.
  (The earlier draft cited `BindingForChannel(...).XOAgent`; that is WRONG-TYPED — `BindingForChannel(channelID
  string)` (`roster.go:309`) takes a channel id, but the detector holds an agent NAME
  (`pendingMirrors []string`, `detector.go:431`), and a boat MAY be a member of SEVERAL channels
  (`roster.go:246`), so a single binding cannot answer "which XO is owed.") A boat whose owned channel
  lists two parents marks BOTH owed. A project-XO's synthesis POST in turn makes the meta-XO owed (the
  Q-E recursion — Tier 3 reads Tier 2's latest state the same way Tier 2 reads its boats').
- **Digest sub-cadence (debounce-up).** The detector does NOT fire `WakeSynthesis` on every boat
  finish (a firehose, defeating curation). It fires on a digest sub-cadence: at most once per N
  intervals per synthesizing agent while that agent has synthesis owed. A burst of boat finishes
  coalesces into ONE curated synthesis wake; an idle fleet (nothing owed) fires nothing — `$0`-idle
  preserved.
- **The DURABLE materiality gate (the trio's P1-b / P2-4, RESOLVED + re-trio hardened).** Under
  transcript-first the read is stateless — there is NO watermark/offset to persist (no append log to
  tail). But the MATERIALITY gate needs durable state: synthesize only when a subordinate's state has
  CHANGED since the last synthesis, so an idle-but-non-empty fleet does not re-post an unchanged
  rollup. This last-seen snapshot (a hash of each subordinate's last-synthesized turn text, keyed by
  synthesizing agent) MUST be DAEMON/DISK-OWNED, because context rotation (`/clear`) wipes any
  skill-context state — the exact bug `WakeSynthesis` exists to kill would return if the last-seen
  state lived in the skill.
  - **It SHALL survive BOTH context rotation AND daemon restart (re-trio P2-4).** A DISK SIDECAR, not
    in-memory detector state: an in-memory-only snapshot re-posts every subordinate's current state as
    "new" on the first wake after a restart (a synthesis restart-storm). Persisted on disk, a restart
    resumes against the last-seen state. The sidecar lives alongside the detector's existing snapshot,
    keyed by synthesizing agent; a missing/corrupt sidecar fails SAFE toward "all changed"
    (synthesize once), never toward silent-never-fire.
  - **An UNREADABLE subordinate is EXCLUDED from the materiality computation (re-trio P2-4), never
    hashed as empty.** A subordinate whose pane transiently won't resolve must NOT be recorded as
    "changed to empty," then "changed back" on recovery — that would flap the wake. It is dropped from
    the hash set for that wake (the same clean-skip as the read), so a transient pane-resolve failure
    neither spams a wake nor suppresses a real change.

  This is the transcript-first analogue of the ledger revision's watermark — but it is a CHANGE-detect
  hash, not a read offset, and it carries NO separate-ledger-watermark requirement.
- Runs OUTSIDE `d.mu`, like every other pane-touching side effect — the synthesis prompt enqueue is a
  confirmed delivery that acquires the pane-txn lock, which must not be held under `d.mu`.

### Implementation note — the materiality READ is off-mutex too (the implementation-gate P1)

The implementation trio caught a P1: under transcript-first the materiality gate's read is no longer a
cheap watermark compare — it is a BLOCKING `tmux`-resolve + transcript read (the same I/O the Tier-1
mirror runs off-mutex). So NOT JUST the wake delivery but the materiality READ + decision must run
outside `d.mu`, or a slow read stalls the tick loop and blocks `OperatorWake` (the relay goroutine).
The detector therefore splits the work: a PURE, cheap decision UNDER `d.mu` (`synthEligibleLocked` —
advance the cadence clock, pick the cadence-eligible owed agents, snapshot each one's read set +
last-seen hashes) and an OFF-`d.mu` read+commit pass (`runSynthesis` — the blocking `SynthRead`s, the
materiality compare, a SHORT re-lock to commit last-seen + reset the cadence + drain owed, then the
agent-targeted `WakeAgent` delivery). It runs SYNCHRONOUSLY in the tail — NOT async like the
observe-only mirror — because synthesis COMMITS last-seen state the next tick reads, so an async run
could interleave two ticks' decisions; sync-in-tail removes the mutex stall without that ordering
hazard.

Two minor durability notes (trio P3, by design): (a) the owed-set (`synthOwed`) and the cadence
counters are IN-MEMORY — a daemon restart re-derives owed-ness from the NEXT finish, not the last
(the durable materiality sidecar survives, so at worst one rollup is delayed to the next finish, never
silent data loss); (b) the wake prompt names the read set computed at ENQUEUE time — the skill treats
the wake prompt as the source of truth for what to read. Stale synthesizer keys (an agent removed from
the roster) are pruned from the sidecar at load.

### The prompt (the `wake` composer, `cmd/flotilla/watch.go:245-260`)

A `WakeSynthesis` case composes a prompt that points the agent at its read set (the agents below it,
via the membership down-traversal) + its post target (its owned channel(s) via `secrets.Webhook`) +
the per-tier output contract, and reminds it of the narrow-answer discipline (curate what CHANGED; if
nothing material changed since the last synthesis, reply idle — never manufacture a synthesis). The
skill CONTENT (the embedded `internal/doctrine/assets/skills/visibility-synthesis.md`) carries the
detailed curation instructions; the wake prompt is the thin trigger that references it. The wake is
enqueued to the SYNTHESIZING agent (the new agent param), not the hardcoded primary `xo`.

## 4. The member — a `heartbeat-skill` constitutional member (with the honest install cost)

B1 scoped the `Mechanism` vocabulary to `MechanismIdentityAppend` ONLY (`internal/doctrine/doctrine.go:36`;
there is no second value pre-baked — `doctrine.go:26-29` explicitly says "a future member kind extends
the vocabulary with its own value plus the write/load behavior that value implies, designed when that
member is"). B2 takes that seam:

- **New value `MechanismHeartbeatSkill`.** A tick-time discipline (a skill invoked when the daemon
  emits `WakeSynthesis`), NOT a structural identity rule — so it is delivered as a WHOLE-FILE skill
  written into the agent's WORKSPACE (e.g. `<workspace>/skills/visibility-synthesis.md`), NOT appended
  into the identity file. This is why the structural-vs-tick-time distinction B1 drew matters: the
  Rule of Three is "who the agent IS" (loaded once into identity); the synthesis skill is "what the
  agent does on a synthesis tick" (a skill the wake prompt references).
- **The `Install` SIGNATURE change (the trio's P2-2, HONEST).** `doctrine.Install`'s current signature
  is `Install(identityPath string, members []Member)` (`internal/doctrine/install.go:40`) — an
  IDENTITY-FILE path only. A whole-file member writes `<workspace>/skills/visibility-synthesis.md`,
  which an `identityPath`-only signature CANNOT resolve (the workspace dir is the identity file's
  parent, but Install never has the dir, only the file path). So `Install` SHALL take a WORKSPACE-DIR
  parameter (e.g. `Install(workspaceDir string, identityPath string, members []Member)` or derive the
  identity path from the workspace dir + `workspace.IdentityFileName`), changed at BOTH call sites:
  `cmd/flotilla/workspace.go:148` and `cmd/flotilla/doctrine.go:50`. This is NOT a "no loop change"
  add — the loop stays member-count-agnostic, but the function SIGNATURE and both callers change.
- **The whole-file idempotency is STAT-based, NOT marker-fenced (the trio's P2-2).** The identity-append
  arm keys idempotency on the OPENING marker's presence (`appendOnce`, `install.go:84-88`), and
  `appendOnce` HARD-ERRORS if `OpenMarker == ""` (`install.go:85`). A whole-file member carries no
  marker, so it MUST NOT route through `appendOnce`. Its dispatch arm STATs the target file: if it
  exists, KEEP it (report kept — operator edits survive); if absent, CREATE it (report created). On
  CREATE the arm performs its OWN `os.WriteFile(<workspaceDir>/<TargetFile>)` — a DIFFERENT file than
  the identity file — and does NOT ride the identity-content write-back, which fires only
  `if anyAppended` over the identity content (`install.go:72-76`). The whole-file write is independent
  of that accumulator (the re-trio's P2-2); the implementer must not try to thread a workspace-file
  write through the identity-content path. The identity-append arm is untouched.
- **A `TargetFile` field on `Member`.** The whole-file member needs its workspace-relative path (e.g.
  `skills/visibility-synthesis.md`). Add a `TargetFile` field to `Member` (`doctrine.go:42-54`), empty
  for identity-append members, set for heartbeat-skill members. The install resolves
  `<workspaceDir>/<TargetFile>`.
- **The registry entry.** One new `Member` in `internal/doctrine/doctrine.go`'s `members` slice,
  `Mechanism: MechanismHeartbeatSkill`, `TargetFile: "skills/visibility-synthesis.md"`, content from
  `internal/doctrine/assets/skills/visibility-synthesis.md`. The install/seed loop iterates and
  dispatches by mechanism, so adding the member needs no LOOP change — but the new dispatch arm + the
  signature change land in THIS change (the `MECHANISM COUPLING` contract, `install.go:51-57`: a 2nd
  mechanism MUST be added to Install at the same time as the member, or every caller passing the new
  kind errors).

## 5. Per-tier output contracts

### Tier 2 — the XO channel (a curated domain rollup)

The XO synthesizes its boats' LATEST STATE UP into its own channel. The contract: a compressed,
curated view of where the boats ARE (each boat's current state from its latest turn) — grouped by
boat, the material state only (not the firehose), with anything that needs the operator's eye
surfaced. It is the domain-level "here is where my desks are."

### Tier 3 — #c2, the command-and-control channel (the inverse of the membership graph)

The meta-XO synthesizes the project-XOs' latest state UP into #c2. The contract has three parts:

1. **A fleet headline** — the one-paragraph "state of the fleet."
2. **Open operator-decisions (best-effort over the latest turn — the re-trio's P2-3)** — the
   decisions waiting on the operator, surfaced explicitly (this is the one resource the operator is
   short on: attention). These are derived from the FULL latest turn text of each project-XO (NOT a
   lossy gist — transcript-first reads full turns), so a PRESENT decision is a first-class extraction,
   not a 280-rune approximation. HONEST LIMIT: latest-state is temporally lossy — a decision a project
   XO raised then moved PAST within a burst (so its latest turn is now unrelated work) can age out of
   the one-turn window. So Tier-3 surfaces decisions present in each subordinate's CURRENT state;
   complete capture of every decision raised-then-superseded across a burst is the #138 finish-history
   ledger's job, not a promise this substrate can structurally keep.
3. **Drill-down pointers** — the inverse of the membership graph: a reader plumbs #c2 → the XO channel
   → the boat channel → the pane. Each headline item names the XO channel (and, for a specific item,
   the boat) to drill into.

### A concrete rendered #c2 example

```
[flotilla #c2 — fleet synthesis · 2026-06-21T03:10Z]

HEADLINE: 2 of 3 project fleets advancing. spark-fleet shipped the Tier-1 mirror
(live, first POST 02:53). research-fleet is mid-backtest. ops-fleet idle.

OPERATOR DECISIONS (2):
  • spark-fleet — B2 substrate ratification: transcript-first (a) vs local-ledger (b).
    Recommendation: (a) transcript-first. → drill: #spark-xo
  • research-fleet — paid backtest budget top-up requested ($25). → drill: #research-xo

DRILL-DOWN:
  • #spark-xo  — desk-mirror-tier1 merged; constitutional-skillset (B1) merged; B2 in design.
  • #research-xo — entry-confirmation variants backtest running (3 desks).
  • #ops-xo    — idle, last activity 41m ago.
```

This is illustrative content, not measured fleet state. Each item is derived from the corresponding
project-XO's FULL latest turn (transcript-first), so "OPERATOR DECISIONS" is a real extraction, not a
lossy-gist approximation.

## 6. Chief-of-staff orthogonality (the trio's Q4)

The CoS ledger (`internal/cos/ledger.go`) and visibility synthesis are ORTHOGONAL axes:

- **CoS = HORIZONTAL.** A who-knows-what view: operator↔XO exchanges across every channel (#108). Its
  axis is "what context has each party been told." It IS an append ledger.
- **Visibility synthesis = VERTICAL.** An activity/state-rollup UP the hierarchy: boats up to their
  XO, XOs up to #c2. Its axis is "where is each subordinate, summarized by altitude." Under
  transcript-first it is a direct latest-state READ, not a ledger at all.

They are independent heartbeat steps and do NOT share a substrate. Under transcript-first they do not
even share a substrate SHAPE: CoS writes/reads an append ledger; synthesis reads transcripts directly
and writes nothing to a ledger. The spec asserts the orthogonality and that neither gates the other.

## Resolved trio questions

- **Q-A — `WakeSynthesis` target generality → RESOLVED (§3).** The wake seam carries an agent param;
  ONE detector, owed-set keyed by synthesizing agent; enqueue by agent. It is a real signature change
  (`detector.go:68` + `watch.go:259`), promoted from open question to normative.
- **Substrate → RATIFIED transcript-first (§1).** Latest-state-per-subordinate via
  `claudestore.LatestTurnText`; the finish-history ledger is a fast-follow (#138).
- **DAG self-edge → RESOLVED (§2).** Exclude self-edges; flag only a mutual membership between two
  DISTINCT channels as a cycle. The live/legacy roster loads.
- **Materiality/watermark → RESOLVED (§3).** Stateless read + a DURABLE, daemon/disk-owned last-seen
  CHANGE-detect snapshot (rotation-proof). No separate ledger watermark.
- **heartbeat-skill mechanism → RESOLVED (§4).** New `MechanismHeartbeatSkill` + a `TargetFile`
  field + a STAT-based whole-file kept/created arm + the `Install` workspace-dir SIGNATURE change at
  both call sites + the multi-hub `OwnedChannels` generalization.

## Re-trio resolutions (the design's second-pass trio — systems-review + STORM — folded)

The re-trio (2026-06-21, systems-review + STORM on the revised transcript-first design) confirmed NO
P0/blocker; the substrate, routing direction, DAG self-edge exclusion, wake-seam, materiality, and
install-signature decisions all verify SOUND against the live code, and the prior findings are
genuinely folded. The new findings, all folded above:

- **P1-A — owed-marking desk→XO resolver (§3).** Add `AgentsAbove(agent)` (inverse of `AgentsBelow`);
  key the owed-set off ALL parents (a boat in 2 channels marks both); the cited
  `BindingForChannel(...).XOAgent` was wrong-typed (channel id vs agent name). FOLDED.
- **P0 (caught at the IMPLEMENT gate, ratified by hydra-ops, §0/§2) — fleet-command channels contribute
  ZERO synthesis edges.** Grounding the implementation in the LIVE roster found that the design's
  `AgentsBelow` / DAG cycle on the live broadcast channel (`role="fleet-command"`, members=all 12):
  `AgentsBelow(tactical-head)` wrongly = `{hydra-ops}`, and the DAG refused to start. Fix: derive
  AgentsBelow / AgentsAbove / the DAG edges ONLY from NON-fleet-command channels (`role` becomes
  load-bearing; the `parent`/`reports_to` schema alternative was rejected). The meta still POSTS Tier-3
  into the fleet-command channel it owns. The example/demo rosters are updated to the federated
  broadcast shape so the gap can never slip again. FOLDED + independently re-verified by hydra-ops.
- **P1-B — agent→pane→transcript hop + host-local invariant (§1).** Read via the surface-agnostic
  `surface.ResultReader.LatestResult(pane)` seam (the EXACT Tier-1 reader — NOT a direct `claudestore`
  bind, which would exclude grok); resolve agent→pane via `ResolvePane(agentTitle(...))`; fail-soft
  skip an unresolvable subordinate; name the single-host v1 invariant, cross-host → #138. FOLDED.
- **P2-1 — the parallel `WakeAgent`, NOT widening `Wake` (§3).** Lower blast radius; keeps the shipped
  path byte-identical. FOLDED.
- **P2-2 — the whole-file arm does its OWN `os.WriteFile`, disjoint from the identity `anyAppended`
  write-back (§4).** FOLDED.
- **P2-3 — Tier-3 operator-decisions are best-effort over the latest turn; burst-completeness is #138
  (§5).** FOLDED.
- **P2-4 — materiality survives rotation AND restart (disk sidecar) and EXCLUDES an unreadable
  subordinate (§3).** FOLDED.

Open-question resolutions (both agents converged):

- **Q-B — digest sub-cadence value + ownership → a daemon floor DERIVED from `heartbeat_interval`** (a
  small multiple), NOT a new roster knob, with the skill free to reply idle. The daemon bounds the
  rate; the skill bounds the content. The concrete multiple is confirmed in the implement review.
- **Q-C — materiality hash granularity → a per-subordinate hash of the FULL latest turn text**, with
  the unreadable-subordinate exclusion (P2-4) as the nailed failure mode. A new-identical turn is a
  no-op; any change is material.
- **Q-D — `Install` signature → `Install(workspaceDir string, members []Member)`, deriving the
  identity path from `workspace.IdentityFileName`** (one source of truth for layout; `identityFilePath`,
  `cmd/flotilla/doctrine.go:63-73`, already centralizes that derivation). RESOLVED in the design, NOT
  deferred to the implement review.
- **Q-E — multi-hub post fan-out → post to the PRIMARY (first) owned channel in v1**; the live roster
  is single-home-per-XO. The `OwnedChannels` accessor still ships (the self-loop guard needs it); the
  FAN-OUT is deferred until a real multi-hub XO needs it. It is the mirror direction of P1-A's
  `AgentsAbove` — one graph, two directions — kept symmetric.
- **Q-F — Tier-3 reads Tier-2's latest TRANSCRIPT state → CONFIRMED correct + DAG-respecting** (the
  meta-XO is a reader-below of each project-XO's channel, never the reverse). Reading the transcript
  (not the posted Discord message) is the right substrate — relay-disjoint, needs no Discord-read
  primitive. The §5 prose reads "the project-XO's latest turn-final state (which, immediately after a
  Tier-2 synthesis, IS that synthesis; otherwise its most recent activity)" — not an unconditional
  "the latest turn IS the synthesis post."
