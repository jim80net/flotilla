## Why

flotilla's inbound/outbound coordination bus is hard-wired to Discord. The
seams are scattered across packages with no interface boundary: outbound posting
is `internal/discord.Post` (`internal/discord/discord.go:61`); inbound is
`internal/discord.NewGateway` + its `MessageHandler` typedef
(`internal/discord/gateway.go:16,38`); the at-least-once catch-up backstop is
`internal/discord.REST` (`internal/discord/rest.go`); destination resolution and
addressing live in `internal/roster` and `internal/relay`. Adding a SECOND
delivery transport today would mean threading a second concrete type through every
one of those call sites.

The fleet already proved the right pattern ONE layer down: the **surface Driver**
SPI (`internal/surface/surface.go:61` + the name-keyed registry at
`surface.go:164-176`) abstracts "drive an agent's terminal TUI" behind one
interface with a registry, optional capabilities probed by type-assertion
(`ResultReader`, `ComposerStateProbe`, `RecycleBridge`), and a default. Three
non-Claude harnesses (aider, opencode, grok) now register through it without the
callers knowing which harness they drive.

A **Transport SPI** applies that same proven shape to the coordination bus: make
"the medium the operator and agents talk over" pluggable, so Discord becomes one
registered transport and a second transport (a loopback web transport, EPIC #106)
can register alongside it without re-plumbing the relay, the catch-up backstop,
the reply leg, or the audit mirror.

## What Changes

This change ships in TWO PRs. **Only PR1 is in scope now.** PR2 (the web
transport) is DEFERRED behind an explicit operator decision (see below) and is
NOT designed as buildable work in this change — its thinking is preserved but
de-scoped.

### PR1 — define the SPI + a COMPLETE Discord extraction, ZERO behavior change (IN SCOPE)

Add a `Transport` interface and a name-keyed registry mirroring `surface.Driver`
(`Register` / `Get(name)` / a `DefaultTransport`, plus optional-capability
type-asserts). Refactor the EXISTING Discord coordination-bus code (post,
gateway/subscribe, REST catch-up, destination/address resolution) into a
registered `discordTransport` that the runtime call sites obtain via the
registry. The extraction is COMPLETE for the coordination-bus seam: every
runtime `discord.` call site on that seam is either re-pointed in PR1 or
explicitly deferred with a tracking issue (the exhaustive inventory is in
`design.md` — a green byte-pinned test on a HALF-migrated seam is the
"tests-pass-on-a-scaffold" trap, so the inventory must show exactly what PR1
covers and what it does not).

The extraction is **behavior-pinned**: the existing relay / reply / mirror /
catch-up BEHAVIORS are preserved, and the ONLY intended signature change is
folding the Discord-shaped `webhookID` out of the inbound projection (it moves
into the transport adapter's self-mirror guard). That single change is
explicitly re-pinned by an UPDATED `relay_test.go` plus a NEW adversarial test;
every OTHER suite passes UNCHANGED. (See `design.md` — "the honest self-mirror
guard"; the earlier "all suites pass byte-for-byte unchanged" framing was
inaccurate, because folding `webhookID` necessarily changes `relay.Accept`'s
signature.)

### PR2 — add the web transport (DEFERRED, operator-gated — NON-GOAL of this change)

Adding a web coordination surface is **NOT designed as buildable here.** It is
an explicit NON-GOAL of `transport-spi` and is blocked on an operator decision,
because the web half collides with a ratified product decision that the
dashboard is a SEPARATE desk
(`openspec/specs/product-decisions/spec.md:141`). At PR2 time the operator
chooses between two genuinely divergent directions:

- **Option 1 (refactor the existing dash behind the SPI):** the web transport
  IS the existing `internal/dash` web surface, refactored to register behind the
  `Transport` interface and REUSING its already-proven anti-DNS-rebinding Host
  allowlist and CSRF/Origin defenses. No second web surface is introduced.
- **Option 2 (a separate second web surface):** a brand-new loopback web
  transport, distinct from the dash, that must re-implement those same defenses.

These foreclose one another, and Option 1 touches the ratified "dashboard =
separate desk" decision — so the choice is the operator's. PR2 is therefore
de-scoped from buildable work in this change; its design thinking lives in a
clearly-labelled "DEFERRED (PR2, operator-gated)" section of `design.md` /
`tasks.md` so it is not lost.

The Discord behavior contract is preserved (PR1); the operator-facing relay,
catch-up, reply, and audit-mirror semantics are unchanged. No deployment
identifiers enter the codebase — examples use only the generic roster roles
(`xo`, `backend`, `frontend`, `data`, …) from `flotilla.example.json`.

## Impact

- **Affected spec:** `transport` (NEW capability — the Transport SPI contract, the
  behavior-pinned Discord-extraction requirement, the optional catch-up
  capability). The deferred web transport's loopback-only requirement is recorded
  as a NON-GOAL / open-decision, not as buildable spec.
- **Affected code (PR1):** NEW `internal/transport/` (the `Transport` interface +
  registry + optional capability interfaces); `internal/discord/*` refactored to
  back a registered `discordTransport`; the runtime coordination-bus call sites
  in `cmd/flotilla/watch.go` (gateway/REST/relay wiring `watch.go:531-557`; the
  down-alert `post` hook `watch.go:128`; the desk-mirror `post` collaborator
  `watch.go:700`), `cmd/flotilla/reply.go` (`replyDest` / `discord.Post`
  `:234,240`), `cmd/flotilla/mirror.go` (`deskMirror.run` `:55`),
  `cmd/flotilla/main.go` (the `flotilla send` + c2 `discord.Post` `:444,604`),
  and `internal/watch/*` (the catch-up `MessageReader` seam) obtain the transport
  from the registry instead of calling `internal/discord` directly. The relay
  inbound projection narrows (`webhookID` folds into the adapter), changing
  `relay.Accept`'s signature — the single intended signature change. The
  remaining decision logic in `internal/relay` is otherwise UNCHANGED (already
  Discord-free). The COMPLETE per-site disposition (including the seams DEFERRED
  with tracking issues — `cmd/flotilla/inbox.go`'s `Recent`, the
  `internal/dash/control` notify path — and the seams legitimately OUT OF SCOPE —
  the `cmd/flotilla/channel.go` guild-provisioning CLI) is the inventory table in
  `design.md`.
- **Affected code (PR2 — DEFERRED, operator-gated):** the web transport behind
  the SPI; under Option 1 this is `internal/dash` refactored behind the
  interface (reusing its Host allowlist + CSRF defenses); under Option 2 a
  separate loopback web surface. NOT built in this change.
- **Risk:** PR1 is LOW — a behavior-pinned extraction, gated by the existing
  test suites passing unchanged EXCEPT the deliberately-updated `relay_test.go`
  (the `webhookID` fold) plus a new adversarial self-mirror test. The COMPLETE
  seam inventory is what keeps the gate honest (no half-migrated seam hiding
  behind a green test). PR2's risk is deferred with it.
- **Related:** #106 (web transport — the DEFERRED PR2, operator-gated against the
  ratified "dashboard = separate desk" decision, `product-decisions/spec.md:141`), #104
  (modularity — this is a concrete decoupling), #103 (tracker registry — the
  same name-keyed-registry pattern, a sibling SPI), #114 (federation transport —
  a pluggable transport is the seam a cross-roster transport later registers
  behind).
