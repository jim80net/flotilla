## Why

The Transport SPI (the `transport.Transport` interface + name-keyed registry,
`internal/transport/transport.go:26`, `registry.go:67-122`) made the coordination
bus pluggable and extracted Discord behind it as one registered transport. The
EPIC's second medium — a **web** coordination surface (#106) — was deferred behind
an operator fork (`openspec/changes/transport-spi/design.md:375-397`): either
refactor the EXISTING dashboard (`internal/dash`) behind the SPI (Option 1), or
build a brand-new second web surface (Option 2).

**The operator chose Option 1.** This change designs it: make the web a
first-class registered transport by sitting the EXISTING dashboard behind the
Transport SPI — ONE web surface, not two. The dash already IS a web coordination
surface: it accepts inbound operator instructions (`POST /api/control/route` →
`internal/dash/control` → a confirmed pane delivery) and posts outbound notes
(`POST /api/control/notify` → `discord.Post`). What it lacks is the SPI boundary:
its outbound notify calls `internal/discord` directly
(`internal/dash/control/library.go:60-67` — the `TODO(#188/#106)` seam deferred
from PR1), and its inbound has no `Transport` registration. This change grafts the
dash onto the SPI without building a second web app and without changing any
existing dash route's behavior.

Option 2 (a separate web surface) is rejected for the reason the SPI design
already named (`transport-spi/design.md:390-392`, `415-419`): it would
RE-IMPLEMENT the dash's already-proven loopback + anti-rebinding + CSRF defenses
from scratch — a weaker re-derivation of hardening the dash already got right —
and leave two web surfaces to maintain. One surface, hardened once, is the lean
design.

## What Changes

This is a DESIGN-ONLY change (proposal / design / spec deltas / tasks) for the
review trio. It does not implement.

### A registered `web` transport, backed by the existing dash

- Add a `web` transport that REGISTERS through the same `RegisterFactory` /
  `Construct` registry as discord (`internal/transport/registry.go:89-122`), so
  `transport.Get("web")` resolves it exactly as `Get("discord")` resolves the
  default. A roster naming `web` selects it; an unnamed roster still resolves to
  the discord default (`DefaultTransport`, `registry.go:14`) — **no behavior
  change for any existing deployment.**
- The dash notify's **outbound** `Post` is the **DISCORD** transport's `Post`
  (the operator-note destination is a Discord webhook, `library.go:106,113` —
  a web transport cannot post it): re-point
  `internal/dash/control/library.go`'s `post` / content-cap seams
  (`library.go:60-67,103`) from a direct `internal/discord` call to a resolved
  discord-backed `Transport` injected at the wiring boundary
  (`transport.NewWebhookDestination`, mirroring `watch.go:154,172-176`) —
  closing the `TODO(#188/#106)` deferred seam. The dash's notify behavior (post
  under the XO identity, CoS-mirror, over-length reject) is preserved exactly;
  only the call path moves behind the SPI.
- The **web** transport owns ONLY the **inbound** half — the dash's EXISTING
  roster-wide resolver
  (`internal/dash/control/library.go:200-238` `resolveTarget` + `Route`), placed
  BEHIND the transport seam without changing its model. The decisive
  inbound-resolution decision (web does NOT reuse the watch `relay.Route`
  channel-scoped path; it keeps the dash's intentional roster-wide,
  boundary-transcending resolver, and enters the SAME confirmed-delivery pipeline)
  is in `design.md`.

### Security model: REUSE, do not reinvent

The dash's hardening — loopback-only bind fail-closed by construction
(`internal/dash/server.go:339-357` `validateBind`), the anti-DNS-rebinding Host
allowlist (`server.go:90,261-269,303-316` `buildHostAllowlist` / `hostAllow`), the
Origin allowlist (`server.go:91,324-330` `buildOriginAllowlist`), and the
`requireWrite` browser-CSRF gate (`internal/dash/tracker_handlers.go:188-219`) —
is the web transport's security model UNCHANGED. The SPI graft introduces no new
network listener and no new auth posture; it reuses the one the dash already has.

### Dash-as-separate-desk reconciliation

The ratified decision "the landing site / dashboard is a separate dedicated
**desk**" (`openspec/specs/product-decisions/spec.md:141-146`) is about WORK
OWNERSHIP — which desk develops dashboard work — not about the dashboard being a
separate web APPLICATION from the coordination bus. Option 1 honors it: the
dashboard stays its own dedicated desk's work, and there is still exactly ONE web
surface. This change adds a clarifying scenario to the product-decisions spec so
the ownership decision is not later misread as forbidding the SPI graft.

## Impact

- **Affected specs:** `transport` (ADDED — the web transport is a registered
  transport; the dash refactored behind the SPI with no behavior change; web
  inbound keeps the roster-wide resolver; loopback + anti-rebinding + CSRF
  preserved); `product-decisions` (ADDED — a clarifying scenario that
  "separate desk" is ownership, not a separate web app).
- **Affected code (when built, NOT in this design change):** a new `web`
  transport registered in `internal/transport`; `internal/dash/control/library.go`
  re-pointed from `internal/discord` to the transport seam; `cmd/flotilla/dash.go`
  wiring to construct / select the transport. The dash read surface
  (`server.go` handlers, `readmodel.go`, SSE) is UNTOUCHED.
- **Behavior change:** none for existing routes. The discord default is
  unchanged; every existing dash test that pins a preserved behavior stays green
  (byte-pinned per `tasks.md`).
- **Related:** #188 (Transport SPI EPIC), #106 (web transport), #192 (the merged
  PR1). Builds on `openspec/changes/transport-spi/` (the SPI + the deferred-PR2
  thinking this change resolves).
