# Design — web as a first-class transport (the dash behind the SPI)

## Context

PR1 (#192) landed the Transport SPI: `transport.Transport`
(`internal/transport/transport.go:26-81`), the `RegisterFactory` / `Construct`
registry (`registry.go:67-122`), the opaque `Destination`
(`transport.go:89-93`), the 4-field medium-agnostic `MessageHandler`
(`transport.go:101`), and the optional `CatchUp` capability
(`catchup.go:31-48`). Discord is the one registered transport
(`discord.go`), wired into `flotilla watch` (`cmd/flotilla/watch.go:152-166`,
`595-647`).

PR1 deliberately left ONE coordination-bus seam un-migrated: the dashboard's
own control surface. `internal/dash/control/library.go:60-67` carries the
explicit `TODO(#188 Transport SPI, deferred to PR2)` marker — its `Notify` path
still calls `internal/discord` directly because re-pointing it would have
pre-decided the operator's Option-1-vs-Option-2 fork
(`transport-spi/design.md:262`, `375-397`).

The operator chose **Option 1**: the web transport IS the existing
`internal/dash`, refactored behind the SPI. This design grounds that choice in
the real code and resolves the four decisions the task demands.

## The two inbound models that exist today (the crux)

flotilla already has TWO inbound coordination paths, with DELIBERATELY different
resolution semantics. The web transport must honor the dash's model, not the
watch model — that is the load-bearing decision.

### Model A — the watch relay (channel-scoped, runs IN the watch process)

```
Discord gateway ─Subscribe→ rel.Handle(channelID, msgID, authorID, content)   internal/watch/relay.go:55
                              │  relay.Accept(authorID, operatorID)            relay/relay.go:25  (operator-only)
                              │  binding = cfg.BindingForChannel(channelID)    watch/relay.go:56
                              │  relay.Route(content, binding.XOAgent,
                              │              memberResolver(binding.Members))   relay/relay.go:45, watch/relay.go:92
                              └→ injector.Enqueue(Job{Kind:"relay",
                                                      OriginChannel: channelID}) watch/relay.go:99
                                   → the Injector drives the agent's pane
```

The watch relay is **channel-scoped by construction**: every inbound message
carries an `originChannel`; `BindingForChannel` resolves a per-channel XO + a
member scope, and `relay.Route`'s `@name` resolves ONLY within that channel's
members (`memberResolver`, `watch/relay.go:106-115`). An `@name` never crosses a
channel boundary. Delivery is asynchronous via the watch process's `Injector`
(an in-process queue).

### Model B — the dash control surface (roster-wide, runs IN the dash process)

```
POST /api/control/route ─→ handleControlRoute              dash/control_handlers.go:29
   requireWrite gate (custom header + Origin)              dash/tracker_handlers.go:188
   → control.Route(target, message)                        dash/control/library.go:134
        agentName = resolveTarget(target)  ── ROSTER-WIDE  dash/control/library.go:216  (NO channel context)
        pane = resolvePane(agent.Title())                  dash/control/library.go:155
        release = acquireTxn(pane)  ── CROSS-PROCESS lock   dash/control/library.go:159  (deliver.AcquirePaneTxn)
        Confirm.SubmitWithSelfHeal(drv, pane, message)     dash/control/library.go:171
        → drives the agent's pane DIRECTLY (synchronous)
```

The dash control surface is **roster-wide by intent** and DELIBERATELY NOT a
reuse of the watch relay. `resolveTarget` (`library.go:200-238`) carries the
explicit rationale: the dash is a host-local operator console with NO Discord
channel context, so the operator can address ANY desk in the roster; this
"differs from the Discord relay, which scopes @name to the typed-in channel's
members … for a federated roster the dash is intentionally boundary-transcending
(the operator owns the whole fleet). It is NOT a reuse of relay.Route."

The two paths also live in **different processes**: `flotilla watch` owns the
`Injector`; `flotilla dash` is a separate process and CANNOT call
`injector.Enqueue` — so it drives the pane directly, serialized cross-process by
the per-pane transaction lock `deliver.AcquirePaneTxn` (`library.go:159`, the
SAME lock `flotilla send` and the watch Injector/rotate take, so dash + watch
serialize on one pane).

## Decision 1 — inbound resolution: web KEEPS the dash's roster-wide resolver behind the SPI

**Decision:** the web transport does NOT reuse the watch `relay.Route`
channel-scoped path. It keeps the dash's EXISTING roster-wide resolver
(`resolveTarget` + `Route`, `library.go:200-238`) as its inbound, placed behind
the `Transport` seam. A web operator message enters the SAME confirmed-delivery
pipeline the dash already uses today — `resolveTarget → resolvePane →
AcquirePaneTxn → Confirm.Submit` — UNCHANGED.

This decision OVERTURNS the deferred plan inherited from the SPI design.
`transport-spi/design.md:423-424` aspired that the web transport "would reuse the
SAME `relay.Route` decision logic and the SAME `Job{Kind:"relay"}` enqueue for
inbound." That aspiration is rejected here in favor of the dash's roster-wide
resolver — which the SAME deferred note ALREADY anticipated: `:426-432` flagged
that the web inbound "does NOT route through a shared `relay.Route` in a way that
contradicts the SHIPPING dash, which deliberately resolves roster-wide," and
explicitly deferred "that reconciliation" to "the deferred operator fork." This
change IS that reconciliation, resolved in the direction `:426-432` foreshadowed
(roster-wide), not the direction `:423-424` first sketched (channel-scoped
`relay.Route`). Naming the overturn closes the inherited-thinking-silently-reversed
gap: a reader of the SPI design who saw `:423-424` will find here the explicit
reason it did not survive contact with the shipping dash.

### Why not reuse `relay.Route` / `Job{Kind:"relay"}`

1. **The watch `Job` pipeline is in the WRONG PROCESS — this is the unarguable,
   first-order reason.** `Job{Kind:"relay"}` is consumed by the watch process's
   `Injector` (`injector.Enqueue`, `watch/relay.go:99`; the `Injector` type lives
   in `internal/watch`). The dash is a SEPARATE OS process with no `Injector`
   instance; it CANNOT call `injector.Enqueue` — there is nothing in the dash
   process to enqueue onto. This forecloses `Job{Kind:"relay"}` reuse on a process
   boundary, independent of any semantics argument. The dash's direct,
   lock-serialized `Confirm.Submit` (`library.go:171`) IS its already-correct
   equivalent of "the Injector drives the pane," and it already serializes against
   watch via the shared cross-process flock.
2. **Even setting the process boundary aside, reusing `relay.Route` would CHANGE
   behavior, violating the no-behavior-change constraint.** `relay.Route` is
   channel-scoped (`memberResolver`); the dash is roster-wide by intent
   (`library.go:200-209`). Swapping the dash onto `relay.Route` would silently
   break the boundary-transcending model the operator relies on — an `@name` typed
   in the web console would stop resolving a desk outside the hub XO's channel
   members. The dash's resolver is a RATIFIED behavior, not an accident to
   "unify away."
3. **The SPI's `ResolveDestination` is the right seam for this, and it returns
   `(dest, agent, ok)` — exactly the (target, canonical-agent) shape the dash's
   resolver already produces.** The web transport's `ResolveDestination`
   delegates to the dash's roster-wide resolver; the `originChannel` argument is
   IGNORED (the dash has none) — honestly reflecting that web inbound is
   boundary-transcending. This is the medium-agnostic-interface, medium-specific-
   semantics pattern the SPI is built for (discord scopes per-channel; web
   resolves roster-wide; both satisfy the same `Transport.ResolveDestination`
   contract).

### Web inbound is STRUCTURED — no `@name`-body splitting, no `@@` escape

The web inbound carries a STRUCTURED payload: `POST /api/control/route` takes a
`{target, message}` JSON body (the target and the message are already separated by
the request shape). So web inbound needs NEITHER of `relay.Route`'s
text-parsing affordances: it does NOT split a leading `@name` token off the body
(`relay.go:49-63` — that exists because the Discord medium delivers one flat text
line where target and body are fused), and it does NOT need the `@@` escape hatch
(`relay.go:46-48` — which exists to let a Discord operator send a literal leading
`@` to the XO). This is an intentional MEDIUM difference, not a gap: a structured
HTTP body has no ambiguity to escape. The web transport's resolver therefore
resolves only the `target` field roster-wide (Decision 2); the `message` field is
delivered verbatim.

### How a web message "enters the relay pipeline" while preserving the boundary

The task asks: *how does a web message enter the relay→Job pipeline while
preserving the intentional boundary-transcending model?* The precise answer is:
it enters flotilla's **delivery** pipeline (the confirmed pane-delivery the relay
ALSO terminates in), not the watch process's `relay.Route → Job` glue. The
shared, medium-agnostic invariant both inbound paths honor is:

> **operator-authenticated → resolve to a canonical roster agent → acquire the
> per-pane cross-process transaction lock → confirmed `Surface.Submit` →
> CoS-mirror with provenance.**

The watch relay reaches that invariant channel-scoped through `relay.Route` +
the Injector; the web transport reaches it roster-wide through the dash's
`resolveTarget` + the direct lock-bracketed `Confirm.Submit`. Both terminate in
the SAME `deliver.AcquirePaneTxn` + `surface.Confirm` delivery, serialized on the
same pane key — which is exactly what makes them safe to run simultaneously
(Decision 4). The web transport is NOT made to impersonate a Discord channel; its
roster-wide resolution is preserved as the medium's own `ResolveDestination`
behavior.

### `ResolveDestination` direction asymmetry — discord's is OUTBOUND, web's is INBOUND

The two transports' `ResolveDestination` satisfy the same interface signature
(`transport.go:59-63`) but resolve in OPPOSITE directions, to DISJOINT downstream
consumers — and naming this prevents a future reader from conflating them:

- **discord's `ResolveDestination` returns an OUTBOUND post target.** It maps an
  origin channel to `discordDestination{channelID, webhookURL}` (`discord.go:161-173`)
  — a credential-bearing webhook. Its consumer is `Transport.Post` (the reply
  leg), which type-asserts the destination back and posts to its webhook
  (`discord.go:143-151`).
- **web's `ResolveDestination` returns an INBOUND pane-delivery target.** It maps
  a roster-wide address to a `webDestination{agentName, paneTarget}` — an agent
  name + the resolved pane string, carrying **NO credential**. Its consumer is the
  dash's delivery leg (`resolvePane → AcquirePaneTxn → Confirm.Submit`), NOT
  `Transport.Post`. A `webDestination` must never be handed to a `Post`; the web
  transport has no meaningful `Post` (the only outbound the dash does is the
  Discord notify, posted by the DISCORD transport — see "The notify is a Discord
  post").

This asymmetry is honest, not a defect: a medium-agnostic interface admits
medium-specific *direction*. tasks 2.3–2.4 pin it — `webDestination` carries
`{agentName, paneTarget}` and no credential, and a test asserts a `webDestination`
flows to the delivery leg, never to `Post` — so a future reader cannot wire web's
inbound destination into an outbound post path.

### Partial unification, stated plainly — the Transport SPI does NOT carry the delivery leg

The Transport SPI unifies the **inbound-feed** seam (`Subscribe` /
`ResolveDestination`) and the **outbound-post** seam (`Post`) across media. It
does **NOT** carry **pane delivery**. Pane delivery is a SEPARATE seam —
`internal/deliver` (`ResolvePane`, `AcquirePaneTxn`) + `internal/surface`
(`Confirm.Submit`) — and BOTH transports' callers invoke it independently, OUTSIDE
the `Transport` interface. There is no `Transport.Deliver`; the interface
(`transport.go:26-81`) has no pane-delivery method.

Concretely: the watch relay resolves+routes via the discord transport, then the
**watch process's `Injector`** drives the pane (`watch/relay.go:99`,
`internal/watch`); the dash resolves roster-wide via the web transport, then the
**dash process** drives the pane directly (`library.go:155-187`). Neither pane
write flows THROUGH a `Transport` method.

So the cross-process convergence guarantee — "a watch relay and a dash route to
the same desk never corrupt the composer" — is provided NOT by the SPI but by the
per-pane `deliver.AcquirePaneTxn` **FLOCK** (`internal/deliver/lock.go:129`,
`syscall.Flock(LOCK_EX|LOCK_NB)`), keyed on the identical resolved pane target
every writer computes. The SPI gives a shared *resolution + post* vocabulary; the
flock gives the shared *delivery* serialization. This is honest **partial**
unification by design, and saying so is stronger than implying the delivery leg
flows through the transport — it does not. Where this design says a web message
"enters the same confirmed-delivery pipeline," that pipeline is the
`deliver`+`surface` delivery leg the dash ALREADY uses, reached by the web
transport's caller — NOT a leg inside the `Transport` SPI.

## Decision 2 — identity / originChannel: web is a roster-wide pseudo-origin, NOT a channel binding

**Decision:** a web message does NOT acquire a Discord-style `originChannel` that
the roster's `BindingForChannel` resolves. The web transport has no channel; its
identity model is the dash's existing one — **the operator owns the whole
roster**, resolved by `resolveTarget` (empty target → the hub XO; `@name` /
`name` → any roster agent, case-insensitive with exact-match-wins,
`library.go:216-238`).

Rationale, grounded in code:
- The watch relay's `originChannel` is load-bearing precisely BECAUSE Discord
  has many channels with different XO + member scopes (`BindingForChannel`,
  federation). The dash has exactly one operator console and no channel
  multiplexing — inventing a synthetic "web pseudo-channel" to push through
  `BindingForChannel` would be cargo-culting the Discord shape onto a medium that
  doesn't have it, and would re-introduce the channel-scoping the dash
  deliberately rejects (`library.go:200-209`).
- For the CoS audit mirror, the dash already records provenance HONESTLY without
  a channel: `dashProvenance = "operator(dash)"` (`library.go:22`), and
  `mirrorRouteToLedger` tags the entry with the hub XO's channel via
  `ChannelForXO` when one exists, warning (not failing) when it doesn't
  (`library.go:240-257`). The web transport inherits this unchanged — its ledger
  provenance is `operator(dash)`, distinguishable from a Discord-originated
  action, with the channel left as the hub XO's binding (or empty, honestly).
- **Operator authentication** for the web medium is NOT Discord's
  `operator_user_id` author check (`relay.Accept`, `relay/relay.go:25`). It is the
  dash's transport-level gate: the request reached a LOOPBACK-bound listener
  (host-shell trust) AND passed `requireWrite` (custom header + Origin,
  `tracker_handlers.go:188-219`). That is the web medium's equivalent of "the
  author is the operator," and it is the dash's existing, tested posture — see
  Decision 3.

## Decision 3 — security model: REUSE the dash's loopback + anti-rebinding + CSRF (verbatim)

**Decision:** the web transport's security model IS the dash's existing one,
unchanged. No new listener, no new auth surface, no re-derived defenses. The SPI
graft is purely an internal call-path move; the network-facing posture is
identical to today's `flotilla dash`.

The reused, already-tested defenses (each cited to the shipping code):

| Defense | Code | What it stops |
|---|---|---|
| Loopback-only bind, fail-closed by construction | `internal/dash/server.go:339-357` `validateBind` | A non-loopback bind is REFUSED as a construction error — no flag opens the listener to the network |
| Anti-DNS-rebinding Host allowlist on EVERY request | `server.go:90` (build), `261-269` (`hostAllow` middleware), `303-316` (`buildHostAllowlist`) | A remote page that rebinds its hostname to 127.0.0.1 is rejected by Host-header mismatch |
| Origin allowlist for state-changing requests | `server.go:91` (build), `324-330` (`buildOriginAllowlist`) | A cross-origin browser forgery carries the attacker's Origin and is rejected |
| `requireWrite` browser-CSRF gate (custom header + Origin) | `tracker_handlers.go:188-219` | A forged "simple request" POST from a malicious page is rejected (no CORS preflight is ever approved); applies ON LOOPBACK too |

The web transport adds inbound coordination THROUGH these existing gates: a web
operator instruction is `POST /api/control/route` (already behind `requireWrite`,
`server.go:179`), so it inherits the full CSRF + Host + Origin defense with NO
new code. This is the precise reason Option 1 was the lean recommendation
(`transport-spi/design.md:415-419`): the dash "already got these right," and
re-implementing them (Option 2) risks a weaker re-derivation.

**Web inbound is ONE gated ingress; `Transport.Subscribe` is a no-op.** The web
medium's inbound is NOT a `Transport.Subscribe` feed (there is no live socket to
subscribe to, unlike the discord gateway). The ONLY web ingress is the existing
gated HTTP route `POST /api/control/route`. So the web transport's `Subscribe`
(`transport.go:42`) is implemented as a deliberate NO-OP — it opens no second
inbound path. This is load-bearing: if an implementer were to add a real
`Subscribe` feed for web, it would be a SECOND ingress that bypasses the reused
`requireWrite` / Host / Origin defenses. Pinning `Subscribe` as a no-op (tasks
3.x) forecloses that: the gated HTTP route is the one and only web inbound.

**One honest scope note on the non-loopback posture.** `validateBind`
(`server.go:339-357`) currently REFUSES any non-loopback bind; the bearer-token +
SSE-cookie auth gate that would make a non-loopback bind safe is a tracked
follow-on (`server.go:333-338`, `cmd/flotilla/dash.go:26-28`). This design does
NOT widen that posture — the web transport is loopback-only, exactly like the dash
today. Remote access stays via an SSH tunnel to the loopback bind. Widening to a
network bind remains a separately-reviewed change.

## Decision 4 — Discord + web run SIMULTANEOUSLY

**Decision:** yes. The two transports coexist with no interference, because they
serialize on the SAME cross-process per-pane transaction lock.

- `flotilla watch` constructs the discord transport (`watch.go:152-166`) and runs
  the inbound relay → Injector path. `flotilla dash` is a SEPARATE process that
  constructs the web transport and runs the dash control path. They are already
  separate processes today (the dash is "a pure reader … a SEPARATE process from
  `flotilla watch`", `control/control.go:13-21`).
- The ONE place they could collide is driving the SAME agent pane concurrently
  (a watch rotate/relay and a dash route racing the composer). That race is
  ALREADY closed by `deliver.AcquirePaneTxn` keyed on the resolved pane target —
  the dash's `Route` "holds that lock across the whole confirmed delivery, keyed
  on the SAME resolved pane target every writer uses" (`control/control.go:13-21`,
  `library.go:155-168`). Adding the web transport changes nothing here: it uses
  the identical lock-bracketed delivery, so web + discord coordination serialize
  per-pane by construction.
- Registry-wise, both transports register independently (`RegisterFactory` for
  each, `registry.go:89-93`) and are constructed independently
  (`Construct("discord", …)` in watch; `Construct("web", …)` in dash). The registry
  is a **package-global** map (`registry.go:30,37`) — so it is per-PROCESS, NOT
  shared across processes. WITHIN a process, the `registry.go:24` mutex (`var mu
  sync.Mutex`) guards the `registry` + `factories` maps against concurrent
  register/construct. ACROSS processes (watch vs dash) there is NO shared registry
  at all — each OS process holds its OWN copy of the package globals, so two
  processes' registries cannot collide. The ONLY cross-process coupling between the
  discord (watch) and web (dash) paths is the per-pane `deliver.AcquirePaneTxn`
  flock — exactly the convergence point that makes them safe to run simultaneously.
  A roster MAY name `web` for the dash while `watch` continues on the discord
  default.

## Refactor map — each dash piece → its Transport role (file:line)

The graft is a SEAM re-point, not a rewrite. The dash's behaviors stay; only the
call path behind them moves onto the SPI.

| Dash piece (shipping code) | Transport role | Behavior change |
|---|---|---|
| `dash/control/library.go:60-67` — `post: discord.Post` (the `TODO(#188/#106)` seam) | the **DISCORD** transport's `Post` (outbound) — re-point the `post` seam to a resolved `Transport.Post` whose backing transport is **discord** (the notify medium IS Discord — see "The notify is a Discord post" below) | NONE — same XO-identity post + CoS mirror; only the call path moves behind the SPI |
| `dash/control/library.go:103` — `discord.MaxContentRunes` over-length guard | the **DISCORD** transport's `MaxContentRunes()` (`transport.go:68`) — the cap read from the (discord-backed) notify transport, not the leaked discord const | NONE for discord-backed notify (same 2000); a future medium's cap is honored, not hard-coded |
| `dash/control/library.go:200-238` — `resolveTarget` (roster-wide) + `Route` | `web` transport's inbound resolver, surfaced via `ResolveDestination(originChannel="", target)` (Decision 1) | NONE — roster-wide resolution preserved verbatim, `originChannel` ignored |
| `dash/control/library.go:134-187` — `Route` → `resolvePane` → `acquireTxn` → `Confirm.Submit` | the web inbound's delivery leg (the relay-pipeline terminus, Decision 1) | NONE — same lock-bracketed confirmed delivery |
| `dash/control_handlers.go:29-70` — `handleControlRoute` / `handleControlNotify` behind `requireWrite` | the web transport's HTTP ingress (already CSRF/Host/Origin-gated) | NONE — same routes, same gates |
| `dash/server.go:339-357` `validateBind`; `:303-316` `buildHostAllowlist`; `:324-330` `buildOriginAllowlist`; `tracker_handlers.go:188-219` `requireWrite` | the web transport's security model (Decision 3) — REUSED unchanged | NONE |
| `cmd/flotilla/dash.go:54` — `dash.NewServer(Config{…})` wiring | construct the discord-backed transport for the notify (`transport.Construct(…)` + `NewWebhookDestination`) and thread it as a `dash.Config` field → `NewServer` → `control.NewLibrary` (`server.go:107`, NOT cmdDash — `NewLibrary` is called inside `NewServer`); the seam extension is on `NewLibrary`'s signature | NEW wiring only; existing flags + defaults unchanged |
| `dash/control/control.go:32-42` `Controller` interface (the seam HTTP handlers bind to) | unchanged — the SPI graft is BELOW this seam (inside `LibraryController`), so handlers + tests are untouched | NONE |
| `cmd/flotilla/resume.go` / `LibraryController.Resume` (`library.go:196-198`) | UNCHANGED — `Resume` is NOT a transport seam; it still returns `ErrResumeUnavailable` (its blocker is the un-extracted `runResume` orchestration, not the transport graft) | NONE |

What this map deliberately does NOT touch: the dash READ surface
(`server.go` read handlers, `readmodel.go`, SSE `sse.go`, the tracker
`tracker_handlers.go` reads) — it is a pure reader over watch's artifacts and has
no coordination-bus seam.

### The notify is a Discord post — disambiguating the OUTBOUND seam

The dash notify is NOT "the web transport's `Post`." A web transport cannot post
the operator note, because the note's destination is a **Discord webhook**:
`Notify` resolves `hook := secrets.Webhook(c.xo)` and calls
`c.post(hook, dashProvenance, message)` (`library.go:106,113,117`). The post
target is fundamentally a Discord channel/webhook credential. So the seam being
re-pointed (`post` + `MaxContentRunes`) is satisfied by the **DISCORD transport's
`Post`**, obtained from the registry and bound to a webhook destination via
`transport.NewWebhookDestination(hook)` (`discord.go:269-277`) — the EXACT
wiring-boundary mechanism `flotilla watch` already uses for its down-alert post
(`watch.go:154,172-176`: `Construct("", …)` → `NewWebhookDestination(alertHook)`
→ `tr.Post(alertDest, …)`). The implementer wires the dash notify the same way:
construct the discord transport at the dash wiring boundary, build the webhook
`Destination`, inject `transport.Post` + `transport.MaxContentRunes()` into the
control library's `post`/cap seams.

The **WEB** transport, by contrast, owns ONLY the INBOUND half
(`ResolveDestination` — Decision 1); it has no meaningful outbound `Post`, because
the only thing the dash posts outbound is the Discord operator-note. (The web
medium's "outbound" to a desk is the pane delivery leg, which is a SEPARATE seam,
not `Transport.Post` — see "Partial unification" below.) This is the direction
asymmetry made precise in Decision 1's "ResolveDestination direction" note.

### The `discord` import after the graft

`internal/dash/control/library.go` imports `internal/discord` today
(`library.go:13`) only for `discord.Post` + `discord.MaxContentRunes`. After the
re-point those two uses move to the injected `Transport` (a **discord-backed**
`Transport` *value*, constructed at the wiring boundary), so the dash control
library no longer imports `internal/discord` — the same "package no longer imports
internal/discord" property PR1 established for the watch/relay packages
(`internal/watch/no_discord_import_test.go` is the pattern to mirror). The
decoupling is real: the library depends on `internal/transport` + a
`Transport` interface VALUE, not on the concrete `internal/discord` package. The
`internal/dash/control`-free-of-`internal/discord` invariant is reached precisely
because the discord dependency now enters as an injected interface value at the
`cmd/flotilla/dash.go` wiring boundary (the one place permitted to resolve the
concrete transport + the webhook credential), not as a compile-time import in the
control library.

## Dash-as-separate-desk reconciliation

The ratified decision (`openspec/specs/product-decisions/spec.md:141-146`): *"The
landing-site / dashboard ('flotilla-dash') SHALL be owned by a separate dedicated
**desk**, not the core-flotilla XO."* Its scenario (`:148-151`) is about WORK
ROUTING — dashboard work goes to the dedicated desk so the core XO stays on core
work.

Option 1 HONORS this, and refines it:
- **"Separate desk" is an OWNERSHIP decision (which agent develops the work), not
  an ARCHITECTURE decision (a separate web app).** Refactoring the dashboard
  behind the Transport SPI does not change who owns dashboard work — it stays the
  dedicated flotilla-dash desk's work. The core XO is not pulled onto it.
- **A CONSEQUENCE of the chosen Option 1 is that there remains exactly ONE web
  surface** (the dashboard, placed behind the SPI). This is stated as a consequence
  of the choice the operator already made — NOT as a retroactive re-ratification of
  Option 1 by the product-decisions spec. Option 2 (a second web app) would have
  created a genuinely separate surface to own and harden twice; Option 1 does not.
  So Option 1 is consistent with the spirit of the "separate desk" decision (which
  is about ownership, not surface count) rather than in tension with it.
- This change adds a clarifying scenario to the `product-decisions` spec
  (delta below) so a future reader cannot misread "separate desk" as "the
  dashboard must be a separate web application from the coordination bus." The
  ratified ownership decision is untouched; the clarification only forecloses the
  misreading — it does NOT make the product-decisions spec the place that ratifies
  the Option-1-vs-Option-2 architecture choice (that choice is the operator's,
  recorded in this change's proposal/design, not in the ownership decision).

## Alternatives considered (rejected)

- **Option 2 — a separate second web surface.** Rejected: re-implements the
  dash's loopback + anti-rebinding + CSRF from scratch (a weaker re-derivation),
  and leaves two web surfaces to maintain (`transport-spi/design.md:390-392`).
  The operator chose Option 1.
- **Unify the dash onto `relay.Route` (channel-scoped).** Rejected: it would
  silently break the dash's intentional roster-wide, boundary-transcending
  resolution (`library.go:200-209`) — a behavior change, and a regression of a
  ratified model. Decision 1. This OVERTURNS the inherited deferred plan
  `transport-spi/design.md:423-424` ("reuse the SAME `relay.Route` … and the SAME
  `Job{Kind:"relay"}` enqueue"); the same SPI note already anticipated the
  reversal at `:426-432` (web inbound "does NOT route through a shared
  `relay.Route`" that contradicts the roster-wide dash; "that reconciliation" was
  deferred to the operator fork — which this change resolves).
- **Synthetic web pseudo-channel through `BindingForChannel`.** Rejected: cargo-
  cults the Discord channel shape onto a medium with no channels, re-introducing
  the very scoping the dash rejects. Decision 2.
- **Promote `web` to a network bind in this change.** Rejected: out of scope; the
  bearer-token / SSE-cookie auth gate that makes a non-loopback bind safe is a
  separate tracked follow-on (`server.go:333-338`). The web transport is
  loopback-only, exactly like the dash today. Decision 3.

## Test strategy (design intent; details in tasks.md)

- **Byte-pin every preserved dash behavior.** The existing dash + control test
  suites (`internal/dash/control_handlers_test.go`,
  `internal/dash/control/control_test.go`, `internal/dash/server_test.go`) pin
  notify, route, the typed outcomes, `requireWrite`, the Host/Origin allowlists,
  and `validateBind`. After the seam re-point their ASSERTIONS MUST pass UNCHANGED
  — that is the no-behavior-change proof, the same discipline PR1 used (a green
  test on a half-migrated seam is the scaffold trap,
  `transport-spi/proposal.md:40-44`).
- **"Unchanged assertions" vs "unchanged source" — the `NewLibrary` signature
  caveat.** The seam extension is on `control.NewLibrary`'s signature (it gains the
  injected transport). `control_test.go`'s helper constructs via
  `NewLibrary(rc, xo, secretsPath)` (`control_test.go:46`) and then OVERRIDES the
  seam fields with fakes (`c.post = cap.post`, etc., `:48-55`). So the test
  ASSERTIONS (notify routes through the injected `post`; route keys the lock on the
  resolved pane target; the typed outcomes) are byte-pinned UNCHANGED, but the
  constructor CALL site in the helper updates to pass the new param. Either keep
  `NewLibrary`'s old signature and add a separate injector (so even the call site
  is untouched), OR update the `NewLibrary` callers in the SAME task — never leave
  a half-updated signature. The pin is on the asserted BEHAVIOR, not on every line
  of test source.
- **New adversarial tests for the graft:** the `web` transport registers + is
  `Get`-resolvable by name; an unnamed roster still resolves discord (no default
  regression); the web `ResolveDestination` is roster-wide and ignores
  `originChannel`; the dash control library no longer imports `internal/discord`
  (mirror `no_discord_import_test.go`); web + discord coexist and serialize on the
  pane lock.
