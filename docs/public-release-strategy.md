# flotilla public-release strategy — DRAFT for operator reaction

> **Status: DRAFT. This is a gap analysis + proposal, not a decision.** Product
> and marketing calls (the one-liner, what ships in v1, whether to build a
> landing site) are the operator's. Everything below is research + options +
> recommendations for him to react to. Authored overnight 2026-06-17 by the
> flotilla-dev XO; grounded in the code at `1fedd54`, every claim cited to
> file:line.

---

## 0. TL;DR

flotilla is **already a real product underneath** — the hard parts (confirmed
delivery, the surface-driver SPI, the change-detector + goal-driven loop, the
relay) are built, tested, and dogfooded daily. What stands between it and a
"drop-in chief of staff" public release is **not more plumbing — it's framing,
one obvious value-add, and two architectural stories the operator already named.**

The single highest-leverage move is the **README + a 15-second demo asset**:
the engine is shiny, the storefront isn't. After that, in priority order:

1. **REPORTING — ship `flotilla status`** (a whole-fleet snapshot command). This
   is the most concrete "value obvious in 30 seconds" feature and it has no
   abstraction risk. *(medium, ~1 PR)*
2. **MODES — make fleet behavior first-class** (a named, switchable Mode, not a
   pile of config flags). This is the operator's headline differentiator.
   *(medium-large, ~2-3 PRs)*
3. **INTERFACES — extract a Channel SPI seam** so "Discord is one interface" is
   true in the code, not just the pitch. *(medium-large, ~2 PRs for the seam +
   one second channel)*

The architecture is in great shape to do all three: the **surface-driver SPI is
the exact template** (a registry + a small interface + optional capabilities via
type assertion — `internal/surface/surface.go:58-130`), and the **voice
subsystem already proves the pattern twice more** (`Session` transport seam +
`SpeechProvider` SPI — `internal/voice/session.go:14-40`,
`internal/voice/provider.go:13-47`). Modes and interfaces are "do it the way
surfaces already work," not greenfield invention.

---

## 1. What flotilla IS (the 1-2 sentence statement)

The operator wants to state the product in a sentence. Here are three framings,
all true to the code, ordered by my recommendation. **This is his call** — I'm
offering options, not picking.

**Option A (recommended) — lead with "drop-in chief of staff":**
> **flotilla is a drop-in chief of staff for the AI coding agents you already
> run.** It turns separate Claude Code / Aider / Grok sessions into one
> coordinated fleet — a single hub agent routes the work and reports back — and
> you run the whole thing from a chat channel on your phone.

**Option B — lead with the problem it kills:**
> **Stop being the message bus for your AI agents.** flotilla wires your
> already-running coding agents into a coordinated fleet with one chief-of-staff
> agent in charge, every message mirrored to a chat channel you can read back
> from anywhere.

**Option C — lead with the architecture (the differentiator):**
> **flotilla is a pluggable coordination layer for AI agents** — drop it over
> the harnesses you already run (Claude Code, Aider, Grok…), pick a behavior
> mode (supervised, autonomous, standby), and drive the fleet from any interface
> (Discord today, more pluggable).

**My read:** Option A is the cleanest "what is it" for a cold visitor — it leads
with the *role* ("chief of staff") which is instantly graspable, and "drop-in"
+ "agents you already run" front-loads the no-lock-in promise that is flotilla's
real moat (see "Why these choices" in the README — substrate you already have).
Option C is the better *second* sentence / architecture tagline, and it's worth
saying because "pluggable everything" is the operator's stated headline. I'd use
**A as the one-liner and fold C in as the architecture line.**

---

## 2. The "30-second-shiny" bar — what it means and why we miss it today

The acceptance bar: a cold visitor lands on the repo and within ~30 seconds
**(a)** knows what it is, **(b)** sees it do something impressive, **(c)** knows
how to start. Cold-testing the current README against that bar:

| Requirement | Current state | Verdict |
|---|---|---|
| Know what it is in 10s | Title line is good but generic ("coordinate a fleet … durable auditable record"); no role hook, no "drop-in" | ⚠️ close, not sharp |
| *See* it do something | **No visual** — no GIF, no asciinema, no screenshot of the Discord channel + panes | ❌ the biggest miss |
| Know how to start | quickstart exists and is cold-tested (`docs/quickstart.md`, README:8) | ✅ good, but it's a click away |
| "v0, work in progress, expect rough edges" banner (README:5-6) | Honest, but it's the *first* thing a visitor reads — it deflates before it sells | ⚠️ reposition |

**The gap is presentation, not capability.** A visitor cannot *see* the wow (a
phone screenshot of a Discord channel where one XO agent is fanning work to four
desks and reporting back) — and that single image is the entire pitch.

---

## 3. Gap analysis by pillar

For each pillar: what exists (cited), what's the gap, rough effort. Effort is in
PRs / standard-flow units (brainstorm → design → review gates → openspec → TDD).

### Pillar 1 — MODES (distinct, first-class, switchable fleet behavior)

**What exists.** The *ingredients* of modes are all built, but only as scattered
config flags — there is **no `mode` concept in the code** (verified: `mode`
appears only as `liveness_ping_mode` and the legacy-vs-v2 heartbeat switch —
`internal/watch/detector.go:188-216`, `cmd/flotilla/watch.go:300`). Fleet
behavior is the *implicit sum* of:
- `change_detector: true/false` — wake every interval vs only on material change (`cmd/flotilla/watch.go:176`).
- `--backlog-file` present/absent — goal-driven drive vs passive (`internal/backlog/backlog.go:25-117`, `cmd/flotilla/watch.go:204-214`).
- `liveness_ping_mode` none/interval/consecutive — idle-cost vs wedge-detection tradeoff (`detector.go:188-216`).
- `--max-self-continuations`, `heartbeat_interval` — autonomy depth + cadence.

So today an operator configures a *behavior* by hand-assembling four+ knobs, and
**cannot switch it at runtime** — it's baked into the daemon's launch flags.

**The gap.** Make a **Mode** a first-class, named, switchable object:
- A small set of built-in modes that bundle the knobs into coherent presets:
  - **Standby** — fleet idle, liveness only, ~$0 (detector on, ping `none`, no self-continuation, no backlog drive).
  - **Supervised** (a.k.a. copilot/interactive) — operator-in-the-loop; XO routes + surfaces every material change, does *not* autonomously drive; verbose reporting.
  - **Autonomous** (a.k.a. goal-driven) — XO drives the backlog to completion, surfaces only decisions / blockers / completions (backlog gate + self-continuation on, quiet reporting).
  - **Sign-off** — a workflow mode (fan-out review to desks, collect go/no-go) — this is the README's already-promised release-sign-off workflow (README:86-88, 128).
- **Switchable at runtime, and over an interface** — `flotilla mode autonomous`, and "@xo go autonomous" from Discord. This is the part that *feels* like a product.
- **Pluggable** — a `Mode` registry that mirrors `surface.Register`/`surface.Get` exactly (`internal/surface/surface.go:118-130`), so a user can register a custom mode the same way they'd register a custom surface driver.

**Don't** bolt this on as another config flag (the handoff's explicit pitfall:
"the operator wants them first-class"). The right shape is a `Mode` interface
resolved from a registry, holding the policy the daemon currently reads from
loose flags — so the daemon asks `mode.HeartbeatPolicy()` / `mode.Autonomy()` /
`mode.ReportingCadence()` instead of reading four booleans.

**Effort:** medium-large. ~2-3 PRs: (1) `internal/mode` package + registry +
the built-in modes wrapping today's knobs (pure refactor, no behavior change
when a mode maps to current config); (2) the `flotilla mode` command + Discord
control verb in the relay; (3) docs + a "modes" section in the README. The
*risk* is fragmenting the clean config path — keep modes as a thin policy layer
*over* the existing detector/heartbeat code, not a rewrite of it
(decouple-with-foundation-tradeoff applies).

### Pillar 2 — REPORTING (rich status, beyond the Discord push)

**What exists.** Reporting today is **alert-driven and liveness-focused**, not
state-driven. The daemon pushes to Discord only on *events*:
- Liveness down-alerts (crash / wedge / ack-age) — `internal/watch/watchdog.go:44-61` → `cmd/flotilla/watch.go:118-123`.
- Relay failure / gateway-down escalation — `internal/watch/inject.go:90-94`, `cmd/flotilla/relay.go:41-51`.
- Backlog format-error / stuck-item alerts — `cmd/flotilla/watch.go:461-479`.
- Confirmed-delivery audit mirror (relayed operator traffic only) — `inject.go:157-165`.
- Outbound: `flotilla notify` (agent → operator, one message) — `cmd/flotilla/main.go:289-352`.
- `flotilla result <agent>` reads **one** grok desk's last turn — `cmd/flotilla/result.go:17-61` (grok-only, via the `ResultReader` capability).

**The gap.** There is **no whole-fleet status surface** (verified: no
`status`/`report` subcommand exists — the command set is send / notify / speak /
voice / watch / register / resume / workspace / push-snippet / result / version
/ help — `cmd/flotilla/main.go:37-62`). An operator cannot ask "what is my whole
fleet doing right now?" and get one answer. Missing:
- A **fleet snapshot**: every desk's assessed state (the detector already
  computes this per-desk via `surface.Assess` — `cmd/flotilla/watch.go:249-266`),
  what each is working on (pane title), the backlog status, open blockers, recent
  completions.
- A **scheduled digest** — push that snapshot to Discord on a cadence (morning
  brief / end-of-run), not just on alerts.
- A **machine-readable export** (`--json`) so a dashboard / landing-site live
  widget / external monitor can consume fleet state.

**This is the highest-ROI "shiny" feature** because it's concrete, demoable
("`flotilla status` → a clean fleet table in your terminal AND your phone"), and
it has **zero abstraction risk** — it only *reads* state the daemon already
assesses. It's also the thing that makes the product feel alive vs. a pile of
plumbing.

**Effort:** medium, ~1 PR. A `flotilla status` command that loads the roster,
runs `surface.Assess` over every desk (reusing the detector's exact code path),
reads the backlog file, and renders a table (+ `--json`). A scheduled-digest
mode in `watch` is a small follow-on (the detector already ticks on a clock).

### Pillar 3 — INTERFACES (pluggable, beyond Discord)

**What exists.** Discord is **the one interface, and it's hardcoded** — but the
*seam to abstract it is small and partly already there:*
- Outbound posts: `discord.Post(webhookURL, username, content)` called directly from `cmd/flotilla/main.go:280-347` and `cmd/flotilla/watch.go:118`.
- Inbound: `discord.NewGateway(...)` constructed in `cmd/flotilla/watch.go:395`; but the **routing logic is already pure and channel-agnostic** — `internal/relay/relay.go:18-62` (`Accept`/`Route`) takes plain strings, no Discord types, and `cmd/flotilla/relay.go:21-31` already defines a `gatewayController` interface seam for testing.
- The **voice subsystem already proves the abstraction**: `Session` is a transport interface with a discordgo adapter isolated behind a build tag (`internal/voice/session.go:14-40`, `internal/voice/discord_session.go`), and `SpeechProvider` is a clean pluggable SPI (`internal/voice/provider.go:13-47`).

**The gap.** There is no `Channel` / `Interface` SPI: no single interface that
says "an interface can *post* an outbound message, *mirror* an audit copy, and
*stream* inbound operator messages," with Discord as one implementation. To make
"Discord is one interface" honest (the evaluate-oss-at-code-level rule applies to
*our own* marketing — don't claim pluggable interfaces if only Discord exists and
there's no seam), we need:
- A `Channel` interface abstracting **outbound post** + **inbound stream** (the relay's `Accept`/`Route` already consume the inbound side generically — that half is done).
- Discord refactored to implement it (mechanical — wrap `discord.Post` + `discord.NewGateway`).
- At least **one second channel** shipped to prove pluggability. Cheapest-but-real options, in order: **a terminal/TUI channel** (read-back + send from a local pane, $0, no external account — great for the quickstart "no Discord needed" path), then **Telegram or Slack** (one webhook + one bot — Slack/Telegram bot APIs are close enough to the Discord shape that the adapter is small).

**Effort:** medium-large. ~2 PRs for the seam + Discord-as-impl (mechanical,
well-covered by existing tests), + ~1 PR per additional channel. For the *pitch*
to be honest you need the SPI to exist + ideally one second channel; you do **not**
need five.

### Pillar 4 — PITCH / ON-RAMP (README + landing)

**What exists.** A solid, accurate README (139 lines) and a genuinely
cold-tested quickstart (`docs/quickstart.md`). The content is *good*; the
*packaging* isn't 30-sec-shiny (see §2).

**The gap.** (1) the one-liner (§1), (2) a visual demo asset, (3) reordering so
the wow is above the fold and the "v0 rough edges" banner doesn't lead, (4)
optionally a landing site (§6). Full README proposal in §5.

**Effort:** small-medium. README rewrite is ~half a day. The **demo asset
(asciinema or GIF)** is the long pole — it needs a clean, generic (non-Spark)
demo fleet to record (separate-circumstantial-from-generalizable: the demo must
use `infra`/`research`, never `hydra-ops`/the trading desks).

---

## 4. Recommended release sequencing

Grouped by what they buy us. **Order within each tier is the build order.**

**Tier 0 — Storefront (do first; unblocks everything; mostly non-code):**
1. README rewrite — one-liner + on-ramp restructure (§5). *(small)*
2. Demo asset — asciinema/GIF of a generic fleet + Discord. *(medium; needs a clean demo rig)*

**Tier 1 — One obvious value-add (the "shiny in 30s" feature):**
3. `flotilla status` — whole-fleet snapshot + `--json`. *(medium, 1 PR, no abstraction risk)*

**Tier 2 — The architecture stories (the differentiator):**
4. MODES first-class — `internal/mode` registry + built-in modes + `flotilla mode` + Discord control verb. *(medium-large, 2-3 PRs)*
5. INTERFACES — `Channel` SPI seam + Discord-as-impl + one second channel (recommend the $0 terminal channel first). *(medium-large, 2 PRs + 1/channel)*

**Tier 3 — Polish / promised-but-unbuilt:**
6. Scheduled reporting digest (small follow-on to #3).
7. Release-sign-off workflow (README:128 — currently the only unchecked roadmap workflow; pairs naturally with the "Sign-off" mode from #4).
8. Landing site (§6) — *after* the README nails the message, since the site reuses it.

**In-flight changes to land/decide alongside this** (from the openspec backlog —
these are adjacent, not blockers): `agent-workspace` PR-1 is near-merge (per-desk
customization — supports per-mode/per-desk config); `discord-voice` is
build-complete and operator-gated (a *voice interface* — it's actually pillar-3
evidence already); `watch-gateway-doctor` + `watch-heartbeat-sidechannel` are
built and operator-gated. None blocks the release framing; all strengthen it.

---

## 5. README proposal (structure + skeleton)

The rewrite keeps all the current accurate content but **reorders for the
30-second arc** and adds the missing hook + visual. Proposed above-the-fold
structure:

```
# flotilla
> [ONE-LINER — Option A from §1: "a drop-in chief of staff for the AI
>  coding agents you already run."]

[VISUAL: a single image/GIF — phone screenshot of the Discord channel where
 the XO fans work to infra/research/data desks and reports back. THIS IS THE
 PITCH. ~15s asciinema of `flotilla status` + a send + the Discord mirror.]

**What you get** (3 bullets, scannable in 10s):
- Coordinate Claude Code / Aider / Grok agents as one fleet — drop-in, no new
  daemon, runs on tmux + a chat channel you already have.
- One chief-of-staff agent routes the work and reports back; you manage it all
  from your phone.
- Pick a behavior mode (supervised / autonomous / standby); every message is
  mirrored to a durable, auditable channel.

**30-second start** (the condensed quickstart — install, one send, see it land):
   go install ./cmd/flotilla
   flotilla send --from me infra "run the tests"
   flotilla status            # ← the new value-add: see the whole fleet
→ full walkthrough: docs/quickstart.md

[THEN the existing "problem / how it works / why these choices" sections —
 they're good, they just belong below the wow, not above it.]
```

Changes from today:
- **Add** the one-liner hook + the visual (the two biggest misses).
- **Add** a "What you get" 3-bullet scan and a "30-second start" code block
  above the fold (the current README makes you click into quickstart to see any
  command).
- **Move** the "v0, work in progress" banner from the very top (README:5-6) to a
  small honest note lower down — keep it (honesty matters: the grok caveats and
  "source-verified not live-captured" notes are *features* of our credibility),
  just don't let it deflate before the pitch lands.
- **Add** modes + `flotilla status` to the feature story once built (Tier 1-2).

I have **not** rewritten the README in place — the one-liner is a marketing
decision (§1) and the visual needs the demo rig. On greenlight I'll produce the
full rewrite as a PR with the chosen one-liner.

---

## 6. Landing-site recommendation

**Recommendation: yes, but as Tier-3 (after the README message is locked), and
scoped to a single static page.** Rationale:

- A landing site's whole job is the §1 one-liner + the §2 visual + a "get
  started" button. It is the *same message* as the rewritten README, in a
  prettier frame — so **building it before the README message is decided is
  premature** (you'd redo it).
- Keep it **dead simple**: one static page, GitHub Pages off the repo (`/docs`
  or a `gh-pages` branch), no framework, no backend. Sections: hero (one-liner +
  demo GIF + install one-liner), "what it does" (the 3 bullets), "how it works"
  (the hub-and-spoke diagram), "modes" (the differentiator), a `flotilla status`
  screenshot, link to quickstart + GitHub. The `frontend-design` skill can make
  it genuinely sharp.
- **A live fleet-status widget** on the site is a great stretch goal — and it's
  exactly what the `flotilla status --json` export (Tier 1) enables. That's a
  reason to build `status` with `--json` from day one.
- **Scope/cost:** ~1 sub-desk, ~1-2 days for a polished single page. The
  operator floated "start another boat to create a landing site" — I'd spawn that
  sub-desk **once the README one-liner + demo asset are locked**, give it the
  message + the GIF + the `frontend-design` skill, and have it produce the static
  page. Not before — it would build on an undecided message.

---

## 7. Honesty / positioning risks (flag before we market)

The evaluate-oss-at-code-level rule cuts both ways — **don't let our own
marketing outrun the code:**
- **"Pluggable interfaces"** is *aspirational* until the `Channel` SPI exists +
  a second channel ships (§3.3). Until then, market "Discord today, interface
  layer designed to be pluggable" — not "pluggable interfaces" flat.
- **"Modes"** is not in the code yet (§3.1). Don't put a modes diagram on the
  landing page until #4 ships.
- **Grok caveat** (README:66-72): grok auto-executes shell/edits without
  approval, and its render markers are source-verified not live-captured. This is
  an honest hazard note and should *stay* — it's credibility, not a wart to hide.
- **The release-sign-off workflow** is promised (README:86-88) but unbuilt
  (README:128, unchecked). Either build it (#7) or soften the "Example workflows"
  framing to "target" (it's already labeled "target" — keep that honest).
- **Separate circumstantial from generalizable:** the entire public surface
  (README, demo, landing) must use generic examples (`infra`/`research`/`data`)
  — never the Spark trading desks (`hydra-ops`, the daemon). The current
  README/quickstart already do this correctly; keep it that way in the demo.

---

## 8. Open decisions for the operator (the genuine forks)

These are *his* calls — I'm not finalizing them (the directive: surface product/
marketing decisions, don't decide them):

1. **The one-liner** — Option A / B / C from §1 (I recommend A + C's
   architecture line). *(marketing)*
2. **v1 scope** — is the release "Tier 0 + Tier 1" (storefront + `status`),
   or does it wait for "Tier 2" (modes + interfaces)? I'd ship **Tier 0+1 as the
   first public cut** (it's honest, shiny, and real) and land Tier 2 as fast
   followers — but the operator may want modes+interfaces in the headline launch
   since they're *his* differentiator. *(product scope)*
3. **Modes taxonomy** — are Standby / Supervised / Autonomous / Sign-off the
   right named modes, with those semantics (§3.1)? *(product design)*
4. **Second interface choice** — terminal (recommended, $0) vs Slack vs Telegram
   first (§3.3). *(product)*
5. **Landing site** — greenlight to spawn the sub-desk after the README message
   is locked? *(go/no-go + spend if it needs anything metered)*

Everything else in §4 is already-authorized build work that flows on clean gates
once the operator picks scope (#2) — no per-step permission needed.

---

## Appendix — evidence index (every claim above, cited)

- Command set: `cmd/flotilla/main.go:37-62` (12 subcommands; no `status`/`report`/`mode`).
- No fleet-mode concept: `mode` in code = `liveness_ping_mode` (`internal/watch/detector.go:188-216`) + legacy/v2 heartbeat switch (`cmd/flotilla/watch.go:300`) only.
- Surface-driver SPI (the template): interface `internal/surface/surface.go:58-73`; registry `surface.go:118-130`; optional capabilities via type assertion `surface.go:80-84` (ResultReader), `surface.go:105-113` (ComposerProbe); drivers `claude.go`/`aider.go`/`opencode.go`/`grok.go`.
- Voice proves the pattern twice: `Session` transport seam `internal/voice/session.go:14-40` + discordgo adapter `internal/voice/discord_session.go`; `SpeechProvider` SPI `internal/voice/provider.go:13-47`.
- Discord coupling (outbound) `discord.Post` callers: `cmd/flotilla/main.go:280-347`, `cmd/flotilla/watch.go:118`; (inbound) `discord.NewGateway` `cmd/flotilla/watch.go:395`; pure routing already channel-agnostic `internal/relay/relay.go:18-62`; testing seam `cmd/flotilla/relay.go:21-31`.
- Reporting is alert/event-driven: watchdog `internal/watch/watchdog.go:44-61`; escalations `internal/watch/inject.go:90-94`, `cmd/flotilla/relay.go:41-51`; backlog alerts `cmd/flotilla/watch.go:461-479`; audit mirror `inject.go:157-165`; `result` (grok-only, one desk) `cmd/flotilla/result.go:17-61`.
- Behavior knobs (today's implicit "modes"): `change_detector` `cmd/flotilla/watch.go:176`; backlog gate `internal/backlog/backlog.go:25-117`, `watch.go:204-214`; detector assess-all-desks `watch.go:249-266`.
- README current state: `README.md:3` (title line), `:5-6` (v0 banner), `:86-88`,`:128` (sign-off workflow promised/unchecked), `:66-72` (grok caveat).
- In-flight changes: `openspec/changes/{agent-workspace,discord-voice,watch-gateway-doctor,watch-heartbeat-sidechannel}/`.
