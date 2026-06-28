## Why

flotilla's inbound/outbound coordination bus is hard-wired to Discord. The
seams are scattered across packages with no interface boundary: outbound posting
is `internal/discord.Post` (`internal/discord/discord.go:61`); inbound is
`internal/discord.NewGateway` + its `MessageHandler` typedef
(`internal/discord/gateway.go:16,38`); the at-least-once catch-up backstop is
`internal/discord.REST` (`internal/discord/rest.go`); destination resolution and
addressing live in `internal/roster` and `internal/relay`. Adding a SECOND
delivery channel today would mean threading a second concrete type through every
one of those call sites.

The fleet already proved the right pattern ONE layer down: the **surface Driver**
SPI (`internal/surface/surface.go:61` + the name-keyed registry at
`surface.go:164-176`) abstracts "drive an agent's terminal TUI" behind one
interface with a registry, optional capabilities probed by type-assertion
(`ResultReader`, `ComposerStateProbe`, `RecycleBridge`), and a default. Three
non-Claude harnesses (aider, opencode, grok) now register through it without the
callers knowing which harness they drive.

A **Channel SPI** applies that same proven shape to the coordination bus: make
"the medium the operator and agents talk over" pluggable, so Discord becomes one
registered channel and a second channel (a loopback web channel, EPIC #106) can
register alongside it without re-plumbing the relay, the catch-up backstop, the
reply leg, or the audit mirror.

## What Changes

Two PRs / two phases (Approach A — extract-in-place, then web):

- **PR1 — define the SPI + extract Discord, ZERO behavior change.** Add a
  `Channel` interface and a name-keyed registry mirroring `surface.Driver`
  (`Register` / `Get(name)` / a `DefaultChannel`, plus optional-capability
  type-asserts). Refactor the EXISTING Discord code (post, gateway/subscribe,
  REST catch-up, destination/address resolution) into a registered
  `discordChannel` that the existing call sites obtain via the registry. The
  extraction is **byte-pinned**: the existing relay / reply / mirror / catch-up
  test suites pass UNCHANGED, which is the proof obligation that the refactor
  moved code without changing behavior.
- **PR2 — add `webChannel` as the second registered channel.** A loopback web
  channel that reuses the SAME relay decision logic (`relay.Accept` / `Route`)
  and the SAME delivery `Job` enqueue path for inbound, and implements outbound
  post + destination resolution. It binds **loopback-only** and REFUSES a
  non-loopback bind (security-by-construction, the loopback-only-MCP posture),
  pinned by a test. Catch-up / cursor is an OPTIONAL capability the web channel
  need not implement (its loopback delivery is in-process; there is no gateway
  gap to reconcile).

The Discord behavior contract is preserved exactly; the operator-facing relay,
catch-up, reply, and audit-mirror semantics are unchanged. No deployment
identifiers enter the codebase — examples use only the generic roster roles
(`xo`, `backend`, `frontend`, `data`, …) from `flotilla.example.json`.

## Impact

- **Affected spec:** `channel` (NEW capability — the Channel SPI contract, the
  byte-pinned Discord-extraction requirement, the optional catch-up capability,
  the loopback-only web requirement).
- **Affected code (PR1):** NEW `internal/channel/` (the `Channel` interface +
  registry + optional capability interfaces); `internal/discord/*` refactored to
  back a registered `discordChannel`; the call sites in `cmd/flotilla/watch.go`
  (gateway/REST/relay wiring, `watch.go:531-557`), `cmd/flotilla/reply.go`
  (`replyDest` / `discord.Post`), `cmd/flotilla/mirror.go` (`deskMirror.run`),
  and `internal/watch/*` (the relay `Handle` / catch-up `route`) obtain the
  channel from the registry instead of calling `internal/discord` directly. The
  pure decision logic in `internal/relay` is UNCHANGED (it is already
  Discord-free).
- **Affected code (PR2):** NEW `webChannel` implementation behind the SPI;
  loopback-bind guard + its test; inbound reuses `relay.Accept`/`Route` +
  `Job{Kind:"relay"}`; outbound + destination resolution.
- **Risk:** PR1 is LOW — a pure extraction, gated by the existing test suites
  passing byte-for-byte (no test rewrites that could mask a regression). PR2 is
  additive and loopback-confined; the non-loopback-bind refusal is a
  fail-closed test.
- **Related:** #106 (web channel — PR2 is its delivery vehicle), #104
  (modularity — this is a concrete decoupling), #103 (tracker registry — the
  same name-keyed-registry pattern, a sibling SPI), #114 (federation transport —
  a pluggable channel is the transport seam a cross-roster transport later
  registers behind).
