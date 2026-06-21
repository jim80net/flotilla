# Design — visibility-synthesis (Tiers 2 and 3, on the ratified LOCAL substrate)

This is B2 of the ratified B1/B2 split. B1 (`constitutional-skillset`, merged) shipped the
installable constitutional-set surface plus its first member (the Rule of Three). B2 ships the
SECOND member — the visibility-synthesis skill — and the local substrate, routing, and cadence it
needs. The grounding is `.claude/handoffs/20260620-visibility-and-constitutional-skillset-design.md`
(the DESIGN-TRIO FINDINGS and the RATIFIED section).

## 1. The substrate (B2's core design call)

### The two local shapes considered

The ratification fixed the substrate as LOCAL (not Discord channel history). Two local shapes were
on the table:

- **(a) Direct transcript read via `internal/claudestore`.** The synthesizing XO reads its boats'
  LOCAL session transcripts directly. The XO knows its boats from the membership graph; for each
  boat it resolves the pane working directory (`deliver.PaneCWD`,
  `internal/deliver/panecwd.go:20`), encodes it, globs `~/.claude/projects/<enc-cwd>/*.jsonl`, takes
  the newest, and extracts the turn-final text (`claudestore.LatestTurnText`,
  `internal/claudestore/claudestore.go:294`). The meta-XO reads the XOs' state the same way.

- **(b) A bounded local mirror-event ledger (the CoS substrate/integrator pattern).** The Tier-1
  mirror, instead of/in addition to posting to Discord, appends ONE bounded event record per boat
  finish to a deterministic append-ledger — modeled exactly on `internal/cos/ledger.go`. The
  synthesis LLM reads that ledger as its substrate.

### Recommendation: (b) the bounded ledger as primary; (a) as enrichment-only drill-down

**Recommend (b).** Justification, against the cost/windowing landmine the Discord-read had:

1. **Bounded read cost vs unbounded transcript windowing.** A boat's session transcript is an
   unbounded JSONL that grows for the desk's whole life; reading "what my boats did since I last
   synthesized" by transcript means windowing N unbounded files by timestamp on every synthesis
   wake — exactly the cost/windowing landmine STORM flagged on the Discord-history read, just moved
   to local files. The ledger is bounded BY CONSTRUCTION: it is an append log of one short line per
   finish (`internal/cos/ledger.go` guarantees each line ≤ `maxLineBytes` = 4096, gist ≤
   `maxGistRunes` = 280), and the synthesis skill reads only the tail since its last synthesis
   watermark. Bounded read, bounded prompt, predictable cost.

2. **Reuse of a proven atomic-append substrate vs a new windowing read.** `internal/cos/ledger.go`
   is a shipped, reviewed, test-covered substrate with the hard property B2 needs: a SINGLE
   `O_APPEND` write of ≤ `PIPE_BUF` (4096) bytes is atomic with respect to other appenders on a
   local filesystem, so the Tier-1 mirror (in the `watch` daemon) appends concurrently-safely with
   no lock. B2 reuses that discipline (a separate ledger — see §6 on why not the SAME file). Option
   (a) would put an unbounded-transcript windowing read on the synthesis hot path with no such
   bounded-cost guarantee.

3. **Relay-disjointness for free (the trio's P1/Q2).** The read MUST be architecturally disjoint
   from `relay.Accept`/`relay.Route` so synthesis never eats a mirror post as a command nor
   re-injects a synthesis post as a command. A LOCAL ledger read is disjoint by construction — it is
   a file read, never a Discord read, never an inbound message — so the relay's webhook-drop guard
   (`internal/relay/relay.go:18-23`, an INBOUND command filter, NOT a read filter) is irrelevant to
   it. This dissolves the P1/Q2 seam concern entirely.

4. **Stratification preserved.** The ledger carries the channel each event belongs to, so the
   synthesizing agent reads only the events for the channels in its read set (`ChannelsAwareOf` minus
   owned) — it reads strictly the level below, never its own posts. Tier 3 reads the SAME ledger but
   filtered to the XO channels it is aware of (an XO-channel synthesis event is itself a ledger
   append — see §3 on the recursion).

**(a) stays as enrichment-only drill-down.** When a synthesis needs the full text behind a gist
(the operator asks "what exactly did boat X conclude?"), the skill MAY fall back to
`claudestore.LatestTurnText` for that one boat. That is a bounded, on-demand, single-file read — not
the hot-path substrate. This flips the original (now-superseded) design's "channel stream primary,
transcript enrichment" to "ledger primary, transcript enrichment."

### The ledger shape (concrete)

A new package `internal/synthledger` (or a generalization — see §6), modeled on `internal/cos`:

```
- 2026-06-21T02:53:56Z · <channel-id> · <agent> · "<gist>"
```

- One line per boat finish, appended by the Tier-1 mirror (an ADDITIVE, best-effort, gated write
  beside its existing Discord post — a ledger append failure NEVER affects the mirror, the detector
  tick, or delivery, exactly like the CoS mirror's best-effort contract).
- `<gist>` is the boat's turn-final text, clamped (the CoS `clampGist`/`%q` single-physical-line
  discipline) so one finish is exactly one ledger line.
- The synthesis skill reads the tail since its watermark, filters to its read-set channels,
  and curates.
- Host-local-filesystem requirement inherited from CoS (the atomic-append reasoning relies on
  `O_APPEND`-under-`PIPE_BUF`, which networked mounts may not honor) — the same constraint the
  roster's `CosLedger` documents.

## 2. Routing — `ChannelsAwareOf` + the self-loop guard

Synthesis routing is the TRANSPOSE of the command graph, derived purely from the F#105 `members[]`
graph (`internal/roster/roster.go`), NO new schema.

- **READ set** = `ChannelsAwareOf(agent)` MINUS the channels the agent OWNS:
  - `ChannelsAwareOf(agent)` = the set of channel ids where `agent ∈ ch.Members` OR
    `agent == ch.XOAgent`, over `Bindings()` (respecting the read-only-slice contract — pure
    derivation, no mutation).
  - Owned channels = `{ ch.ChannelID : ch.XOAgent == agent }` (today exposed singularly as
    `ChannelForXO`; for the multi-hub case, ALL channels the agent is the XO of).
  - Read set = aware-of MINUS owned = **"read strictly below, never your own."**
- **POST target** = `ChannelForXO(agent)` (`roster.go:343`) via `secrets.Webhook(agent)`
  (`internal/roster/secrets.go:62`).

### Why the owned-channel exclusion is load-bearing (the trio's P2-a self-loop)

The F#105 multi-channel-XO model (`roster.go:240-246`) lets an agent be BOTH a `member` of a peer's
channel AND the `xo_agent` of its own. Without the exclusion, `ChannelsAwareOf` for such an agent
would include its OWN channel (it is the XOAgent of it), so its read set could equal its post target
— it would synthesize its own synthesis posts, a self-loop. Subtracting the owned channels closes
this: an agent reads the channels below it that it does not own, and posts to the one it does.

### The DAG assertion (the trio's loop-prevention requirement)

"Read below, post own level" gives acyclicity for free IFF the membership graph is a DAG. Model the
graph as: for each channel, an edge from the channel's XO (the synthesizer/poster) to each member
(the synthesized). Tier-3 over Tier-2 is the meta-XO's channel having the project-XOs as members; a
cycle would mean two channels each list the other's XO as a member, which would let A synthesize B's
channel while B synthesizes A's — an infinite mutual rollup. The roster `Load` asserts this graph is
acyclic and REFUSES to start otherwise (fail-closed, consistent with every other roster invariant —
duplicate channel id, unknown member, etc.). A standard depth-first-search cycle detection over the
`Bindings()` edges suffices; it runs once at load, not on the hot path.

## 3. Cadence — the daemon-emitted `WakeSynthesis` wake-kind

### Why not skill-self-scheduling (the trio's Q3)

The original design left "skill-judged cadence vs daemon-emitted wake" open. The trio's robust
answer is the daemon wake. Self-scheduling breaks twice:

1. **Idle-wake suppression.** The change-detector's whole point is that an idle fleet wakes nothing
   (`$0`-idle). A skill that says "synthesize again next tick" relies on there BEING a next wake —
   but on an idle fleet there is none, so the self-scheduled synthesis silently never runs.
2. **Context rotation.** `continueXO` rotates the XO context (`/clear`) between handlings
   (`detector.go` `continueXO` → `requestRotate`). A self-set "remind me to synthesize" timer lives
   in the XO's context and is wiped by the rotate. The daemon's state survives the rotate; the
   skill's does not.

### The mechanism: debounce-up, daemon-owned

- A new `WakeKind`, `WakeSynthesis`, sibling of `WakeContinuation`/`WakeMaterial`/`WakeBacklog`/
  `WakePing` (`detector.go:25-44`).
- **"Owed" marking.** A boat-finish event (the same confirmed Working→Idle transition Tier-1 mirrors
  on, `detector.go` §2b) marks synthesis "owed" for the channel that boat belongs to — i.e. for that
  channel's XO. The detector tracks an owed-set (per synthesizing agent), the way it tracks
  `pendingMirrors`.
- **Digest sub-cadence (debounce-up).** The detector does NOT fire `WakeSynthesis` on every boat
  finish (that would be a firehose, defeating the curation). It fires on a digest sub-cadence: at
  most once per N intervals per synthesizing agent while that agent has synthesis owed. So a burst of
  boat finishes coalesces into ONE curated synthesis wake, and an idle fleet (nothing owed) fires
  nothing — `$0`-idle preserved.
- **The wake is enqueued to the SYNTHESIZING agent**, not necessarily the daemon's primary XO. A
  federated daemon clocks the meta-XO, but a Tier-2 synthesis wake targets a PROJECT XO. This is a
  design point worth the trio's eye: the detector today wakes only `d.cfg.XOAgent`; `WakeSynthesis`
  must enqueue to an arbitrary roster agent (the channel's XO). The Injector already addresses any
  agent (`watch.Job{Agent: ...}`), so the enqueue is general; the detector's owed-set is keyed by
  synthesizing agent. (Open question Q-A below.)
- Runs in `runTail`, OUTSIDE `d.mu`, like every other wake — the synthesis prompt enqueue is a
  confirmed delivery that acquires the pane-txn lock, which must not be held under `d.mu`.

### The prompt (the `wake` composer, `cmd/flotilla/watch.go:245`)

A `WakeSynthesis` case composes a prompt that points the agent at its read-set ledger tail + its
post target + the per-tier output contract, and reminds it of the narrow-answer discipline (curate
what changed; if nothing material since the watermark, advance the watermark and reply idle — never
manufacture a synthesis). The skill CONTENT (the embedded `assets/skills/visibility-synthesis.md`)
carries the detailed curation instructions; the wake prompt is the thin trigger that references it.

## 4. The member — a `heartbeat-skill` constitutional member

B1 scoped the `Mechanism` vocabulary to `identity-append` only and explicitly left the seam for "a
future member of a new kind ... extends the vocabulary with its own value plus the write/load
behavior that value implies" (`internal/doctrine/doctrine.go:26-36`,
`constitutional-skillset/spec.md` extensibility requirement). B2 takes that seam:

- **New value `MechanismHeartbeatSkill`.** A tick-time discipline (a skill invoked when the daemon
  emits `WakeSynthesis`), NOT a structural identity rule — so it is delivered as a WHOLE-FILE skill
  written into the agent's workspace (e.g. `~/.flotilla/<agent>/skills/visibility-synthesis.md`),
  NOT appended into the identity file. This is why the structural-vs-tick-time distinction B1 drew
  matters: the Rule of Three is "who the agent IS" (loaded once into identity); the synthesis skill
  is "what the agent does on a synthesis tick" (a skill the wake prompt references).
- **Install dispatch arm.** `doctrine.Install` (`internal/doctrine/install.go`) gets a second
  `switch m.Mechanism` arm for `MechanismHeartbeatSkill`: WHOLE-FILE kept/created semantics (the
  member owns its own file under the workspace — a missing file is created, an existing one is KEPT
  so operator edits survive). This is the OTHER idempotency granularity B1's spec already described
  for "a future whole-file member" — B2 is that member. The identity-append arm is untouched.
- **The registry entry.** One new `Member` in `internal/doctrine/doctrine.go`'s `members` slice,
  `Mechanism: MechanismHeartbeatSkill`, content from `assets/skills/visibility-synthesis.md`. The
  install/seed loop is already member-count-agnostic (it iterates and dispatches by mechanism), so
  adding the member needs no loop change — exactly the seam B1 built.
- **Note on the `MECHANISM COUPLING` comment.** B1's `install.go` and `cmdWorkspaceInit` warn that a
  2nd mechanism "MUST be added to Install at the same time, or every caller ... starts erroring." B2
  honors this: the dispatch arm lands in the SAME change as the member.

## 5. Per-tier output contracts

### Tier 2 — the XO channel (a curated domain rollup)

The XO synthesizes its boats UP into its own channel. The contract: a compressed, curated view of
what the boats did since the last synthesis — grouped by boat, the material outcomes only (not the
firehose), with anything that needs the operator's eye surfaced. It is the domain-level "here is
where my desks are."

### Tier 3 — #c2, the command-and-control channel (the inverse of the membership graph)

The meta-XO synthesizes the XO channels UP into #c2. The contract has three parts:

1. **A fleet headline** — the one-paragraph "state of the fleet."
2. **Open operator-decisions** — the items waiting on the operator, surfaced explicitly (this is the
   one resource the operator is short on: attention).
3. **Drill-down pointers** — the inverse of the membership graph: a reader plumbs #c2 → the XO
   channel → the boat channel → the pane. Each headline item names the XO channel (and, for a
   specific item, the boat) to drill into.

### A concrete rendered #c2 example

```
[flotilla #c2 — fleet synthesis · 2026-06-21T03:10Z]

HEADLINE: 2 of 3 project fleets advancing. spark-fleet shipped the Tier-1 mirror
(live, first POST 02:53). research-fleet is mid-backtest. ops-fleet idle.

OPERATOR DECISIONS (2):
  • spark-fleet — B2 substrate ratification fork: local-ledger (b) vs direct-transcript (a).
    Recommendation: (b). → drill: #spark-xo
  • research-fleet — paid backtest budget top-up requested ($25). → drill: #research-xo

DRILL-DOWN:
  • #spark-xo  — desk-mirror-tier1 merged; constitutional-skillset (B1) merged; B2 in design.
  • #research-xo — entry-confirmation variants backtest running (3 desks).
  • #ops-xo    — idle, last activity 41m ago.
```

This is illustrative content, not measured fleet state.

## 6. Chief-of-staff orthogonality (the trio's Q4)

The CoS ledger (`internal/cos/ledger.go`) and visibility synthesis share the substrate/integrator
PATTERN but are ORTHOGONAL axes:

- **CoS = HORIZONTAL.** A who-knows-what view: operator↔XO exchanges across every channel (#108). Its
  axis is "what context has each party been told."
- **Visibility synthesis = VERTICAL.** An activity-rollup UP the hierarchy: boats up to their XO,
  XOs up to #c2. Its axis is "what is happening, summarized by altitude."

They are independent heartbeat steps and **do NOT share a ledger.** The CoS ledger records
operator↔XO message exchanges; the synthesis ledger records boat-finish activity events. Folding
them into one file would conflate two axes (a who-knows-what line and an activity line are different
records read by different integrators) and couple two independently-gated features. The spec asserts
the orthogonality and the separate substrate. (A shared GENERIC append-ledger primitive could host
both as a refactor, but that is a code-reuse decision for the implement phase, not a substrate
merge — see the impact note in `proposal.md`.)

## Open questions for the trio

- **Q-A — `WakeSynthesis` target generality.** The detector today wakes only `d.cfg.XOAgent`.
  `WakeSynthesis` must enqueue to an arbitrary synthesizing agent (a project XO for Tier 2, the
  meta-XO for Tier 3). Confirm the detector's owed-set keying by synthesizing agent + the wake
  enqueue to a non-primary agent is the right shape, vs a per-XO detector. Recommendation: keep ONE
  detector, key the owed-set by agent, enqueue by agent (the Injector already addresses any agent).
- **Q-B — digest sub-cadence value + ownership.** What is N (intervals per synthesis), and is it
  fixed, roster-configurable, or skill-judged-within-a-daemon-floor? Recommendation: a daemon floor
  (the wake cannot fire more than once per N intervals per agent), with the skill free to reply
  "nothing material, watermark advanced" — so the daemon bounds the rate and the skill bounds the
  content.
- **Q-C — materiality threshold ("enough changed to synthesize").** Who owns it — the daemon (only
  fire `WakeSynthesis` if ≥1 new ledger event since the watermark) or the skill (always wake on
  cadence, let the skill decide it's a no-op)? Recommendation: BOTH — the daemon suppresses a wake
  with zero owed events (no firehose, no empty wakes), and the skill still self-judges materiality
  on the events it does see.
- **Q-D — separate ledger vs generalized primitive.** §6 recommends a SEPARATE synthesis ledger
  (orthogonal to CoS). Confirm we are not over-coupling by reusing the CoS package directly vs
  factoring a shared generic append-ledger. Recommendation: separate ledger, optionally over a
  shared generic primitive extracted in implement.
- **Q-E — Tier-3 reads Tier-2 synthesis as ledger events.** For #c2 to synthesize the XO channels,
  an XO's Tier-2 synthesis POST must itself become a ledger event the meta-XO reads. Confirm the
  synthesis post path appends its own ledger event (the recursion that makes Tier 3 read Tier 2),
  and that this respects the DAG (it does — the XO posts to its own channel, the meta-XO is a
  reader-below of that channel, never the reverse).
