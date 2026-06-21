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
therefore a constitutional-set SKILL run on the XO's heartbeat — the integrating half of the
substrate-or-integrator split the chief-of-staff ledger already embodies (`internal/cos/ledger.go`:
a deterministic append substrate that flotilla writes with no model call, read by an LLM that
writes its integrated view one level up). Tier 1 is the deterministic substrate; Tiers 2 and 3 are
the integrating LLM, one level up the hierarchy.

This change — **B2 of the ratified B1/B2 split** — ships that synthesis member. It is the SECOND
member of the installable constitutional set that B1 (`constitutional-skillset`, merged) shipped,
plugged into B1's deliberately member-count-agnostic registry via a NEW delivery `Mechanism` value
(`heartbeat-skill`) — the "the vocabulary extends with each new member kind" extensibility B1
promised and left open.

## The ratified substrate decision: LOCAL, not Discord history

The original B-change design specified synthesis reading "the Tier-1 mirror stream" from Discord
channel history. The design-gate trio (`/systems-review` + STORM, 2026-06-21) found a P1: **that
substrate does not exist as running code.** flotilla is SEND-ONLY to Discord — the gateway is
push-only `MESSAGE_CREATE` (`internal/discord/gateway.go:41-50`), there is no channel-history-fetch
primitive anywhere (grep-clean), and the inbound relay deliberately DROPS webhook posts
(`internal/relay/relay.go:18-23`) precisely so the bot never acts on its own posts. Reading Discord
history would have required a net-new `flotilla read <channel>` primitive, a fleet-wide bot
Read-Message-History permission expansion (the relay guard exists to prevent exactly this), and an
unbounded-history cost/windowing landmine.

**hydra-ops ratified (2026-06-21): the B2 substrate is LOCAL** — the boats' own session transcripts
(via the `internal/claudestore` reader Tier 1 already uses) and/or a chief-of-staff-style
deterministic append-ledger of mirror events. NOT Discord history. This dissolves the
non-existent-substrate P1, the relay collision, the cost/windowing landmine, and the security
expansion at once. The stratification (boats → XO channel → #c2) is unchanged; only the data source
is local.

**This change's core design call — which local shape — and its recommendation are in `design.md`.**
The recommendation is **(b) a bounded local mirror-event ledger** (the `internal/cos/ledger.go`
substrate/integrator pattern), with **(a) direct `claudestore` transcript read** as the
enrichment-only fallback for drill-down. The justification (bounded read cost vs unbounded
transcript windowing; reuse of the proven atomic-append ledger; relay-disjointness for free) is in
`design.md`.

## What Changes

- **A local synthesis substrate (the ratified LOCAL source).** A deterministic, append-structured
  **mirror-event ledger** — modeled on `internal/cos/ledger.go` (atomic single-`O_APPEND`-write,
  rune-bounded lines, host-local-filesystem requirement) — to which the Tier-1 mirror appends one
  bounded event record per boat finish (timestamp · channel · agent · gist), IN ADDITION TO posting
  to Discord. The synthesis skill reads this ledger (the level below it in the hierarchy) as its
  primary substrate; it is bounded, cheap, relay-disjoint by construction (it is a local file, never
  a Discord read, never an inbound command), and reuses the proven atomic-append discipline rather
  than introducing an unbounded transcript-windowing read on the hot path. Direct `claudestore`
  transcript read remains available as drill-down enrichment.

- **Routing — one net-new roster accessor, `ChannelsAwareOf`, plus a self-loop guard.** Synthesis
  routing is the TRANSPOSE of the command graph, derived purely from the F#105 `members[]` graph
  (the same graph Tier 1 routes on) with NO new roster schema. An agent's READ set is the channels
  it is aware of — channels where the agent is a `member` OR the `xo_agent` — MINUS the channels it
  OWNS (`ChannelForXO`): **"read strictly below, never your own."** The owned-channel exclusion
  closes the multi-hub member-and-XO self-post loop (a multi-channel XO can be a member of a peer's
  channel AND the XO of its own; without the exclusion its read set could include its own post
  target). The POST target is `ChannelForXO(agent)` via `secrets.Webhook(agent)`.

- **Membership-graph acyclicity, asserted at roster load, fail-closed.** "Read below, post own
  level" gives acyclicity for free IFF the membership graph is a directed acyclic graph (DAG). The
  roster load SHALL assert the channel-membership graph is a DAG and REFUSE to start otherwise, so a
  misconfigured cyclic federation cannot produce a synthesis feedback loop.

- **Cadence — a daemon-emitted `WakeSynthesis` wake-kind.** A new `WakeKind` (a sibling of
  `WakeContinuation` / `WakeMaterial` / `WakeBacklog` / `WakePing` in `internal/watch/detector.go`),
  the trio's ROBUST successor to skill-self-scheduling. Self-scheduling breaks under the
  change-detector's idle-wake suppression (an idle fleet wakes nothing, so a skill that schedules
  itself "next tick" never runs) and under context rotation (the rotate wipes a self-set timer).
  Instead the daemon owns the cadence: a boat-finish event marks synthesis "owed" for the affected
  channel's XO; the detector fires `WakeSynthesis` for that XO on a digest sub-cadence (debounce-up),
  so an idle fleet costs nothing and a busy one synthesizes at a bounded rate.

- **The synthesis member ships via B1's installable surface as a `heartbeat-skill` member.** This
  change EXTENDS B1's `Mechanism` vocabulary with `MechanismHeartbeatSkill` plus the write/load
  behavior that value implies (a whole-file skill written into the agent's workspace, invoked when
  the daemon emits `WakeSynthesis` — NOT appended into the agent's standing identity, because
  synthesis is a tick-time discipline, not a structural identity rule). The skill CONTENT is the
  curation prompt. This is exactly the registry extension B1 left a clean seam for.

- **Per-tier output contracts.** Tier 2 = a curated domain rollup (what the boats did, compressed,
  with what needs the operator's eye surfaced). Tier 3 = a fleet command-and-control headline + the
  open operator-decisions + drill-down pointers down the membership graph (the inverse chain
  #c2 → XO channel → boat channel → pane). A concrete rendered #c2 example is in `design.md`.

## Out of scope

- **Tier 1 (the mechanical per-desk mirror).** Already shipped (`desk-mirror-tier1`). This change
  CONSUMES it (the synthesis substrate is fed by the Tier-1 finish event) but does not re-spec it.
  The one Tier-1 touch is additive: the mirror ALSO appends a ledger event (a small, gated,
  best-effort write beside its existing Discord post).
- **The B1 installable surface itself.** Shipped (`constitutional-skillset`). This change adds a
  member and a mechanism to it; it does not re-spec the install/seed loop, the embed tree, or the
  marker-guard.
- **A Discord channel-history read primitive.** Explicitly rejected at ratification — the substrate
  is LOCAL. No `flotilla read`, no bot Read-Message-History permission expansion.
- **The broader constitutional corpus.** WHICH further behaviors join the default set remains the
  operator's strategic lever; this change adds exactly one member (visibility-synthesis).
- **Channel/webhook provisioning.** Provisioning channels from the roster is the separate
  `flotilla provision` line; this change reuses existing per-XO webhooks (`secrets.Webhook`).

## Impact

- **New capability spec:** `visibility-synthesis`.
- **Affected code (implement phase, after the trio + hydra-ops ratify):**
  - `internal/roster/roster.go` — net-new `ChannelsAwareOf(agent)` accessor (read-set derivation,
    owned-channel exclusion) + a DAG acyclicity check in `Load`.
  - `internal/watch/detector.go` — net-new `WakeSynthesis` `WakeKind`, the "synthesis owed" debounce
    state, and the digest sub-cadence that fires it.
  - `internal/doctrine/doctrine.go` + `install.go` — net-new `MechanismHeartbeatSkill` value, the
    whole-file skill write/load dispatch arm, and the visibility-synthesis member registry entry.
  - `internal/cos/ledger.go` pattern — a net-new mirror-event ledger (or a generalization of the CoS
    ledger; see `design.md` on whether they share substrate — recommendation: a SEPARATE ledger,
    they are orthogonal axes).
  - `cmd/flotilla/watch.go` — a `WakeSynthesis` case in the `wake` prompt composer, enqueuing the
    synthesis prompt to the synthesizing agent.
  - The Tier-1 mirror path — an additive best-effort ledger append beside the existing Discord post.
  - `assets/skills/visibility-synthesis.md` — the embedded curation-prompt skill content.
  - `docs/visibility.md` — the stratified-visibility doctrine doc (source of truth for the tiers).
- **Chief-of-staff orthogonality.** The CoS ledger is a HORIZONTAL who-knows-what view (operator↔XO
  exchanges across channels); visibility synthesis is a VERTICAL activity-rollup (boats up to their
  XO, XOs up to #c2). They are orthogonal axes and independent heartbeat steps; the spec asserts
  this and they do NOT share a ledger (see `design.md`).
- **Risk:** MODERATE. The substrate ledger and the `ChannelsAwareOf` accessor are pure additive
  derivations; the `WakeSynthesis` cadence is a new detector branch (covered by the detector's
  existing under-mutex/runTail discipline and unit-test harness); the heartbeat-skill mechanism is
  a new install dispatch arm (the registry was built for this). The DAG check is fail-closed
  (a cyclic roster refuses to start). No existing path changes behavior when the synthesis member
  is absent (inert by default).
