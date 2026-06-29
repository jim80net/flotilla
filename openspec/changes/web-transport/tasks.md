# Tasks — web as a first-class transport (the dash behind the SPI)

> Design-only change. These tasks are the IMPLEMENTATION plan the review trio
> evaluates; they are NOT executed in this change. Each is bite-sized TDD: a
> failing test first, then the minimal code. Byte-pin every preserved dash
> behavior (a green test on a half-migrated seam is the scaffold trap).

## 1. Outbound seam re-point (the `TODO(#188/#106)` closure)

> The notify's OUTBOUND post target is a Discord webhook
> (`library.go:106,113,117` — `secrets.Webhook(c.xo)`), so the `Transport`
> injected here is the **DISCORD** transport, NOT the web transport. The web
> transport owns only inbound resolution (section 3). This is the wiring-boundary
> pattern `watch.go:154,172-176` already uses (`Construct` + `NewWebhookDestination`
> + `tr.Post`).

- [ ] 1.1 Pin the CURRENT dash notify behavior as the baseline. Confirm the
      existing suites pass on `main` UNCHANGED:
      `internal/dash/control/control_test.go` (`TestNotify_*`,
      `control_test.go:124-227`) and `internal/dash/control_handlers_test.go`
      (`TestControlNotify_*`, `control_handlers_test.go:57-87`). These are the
      no-behavior-change oracle for the re-point. NOTE the "unchanged assertions vs
      unchanged source" caveat (design Test strategy): if `NewLibrary`'s signature
      gains the injected transport, the `control_test.go:46` helper's CONSTRUCTOR
      CALL updates, but its seam-override ASSERTIONS (`:48-55`) stay byte-pinned.
- [ ] 1.2 Write a FAILING test: the control library's `post` seam is satisfied by
      a `transport.Transport.Post` (not `discord.Post`) and its content cap comes
      from `transport.Transport.MaxContentRunes()` (not `discord.MaxContentRunes`).
      Inject a fake `Transport` and assert the notify routes through it. (In
      production this injected transport is the discord-backed one — task 6.2.)
- [ ] 1.3 Re-point `internal/dash/control/library.go`: replace the `post`
      seam default (`library.go:67`) and the over-length guard
      (`library.go:103`) to use an injected `Transport`. Remove the
      `internal/discord` import (`library.go:13`). Delete the
      `TODO(#188/#106)` marker (`library.go:60-66`). The discord dependency now
      enters as an injected `Transport` interface VALUE at the wiring boundary
      (task 6.2), not as a compile-time import in the control library.
- [ ] 1.4 Add `internal/dash/control/no_discord_import_test.go` mirroring
      `internal/watch/no_discord_import_test.go`: assert the control library no
      longer imports `internal/discord`.
- [ ] 1.5 Re-run 1.1's suites — they MUST pass UNCHANGED (the re-point is
      behavior-preserving: same XO-identity post, same CoS mirror, same
      over-length reject, same typed outcomes).

## 2. The `web` transport — registration

- [ ] 2.1 Write a FAILING test: `transport.Get("web")` resolves a constructed web
      transport after `Construct("web", …)`; an unknown-name `Construct` still
      errors (`registry.go:99-122`); an empty name still resolves discord
      (`DefaultTransport`, no default regression).
- [ ] 2.2 Add `internal/transport/web.go`: a `webTransport` with an `init()`
      calling `RegisterFactory("web", newWebTransport)` (mirroring
      `discord.go:17-19`), and a `Name()` returning `"web"`.
- [ ] 2.3 Define `webDestination` (the opaque `Destination`, `transport.go:89`)
      as a roster-wide INBOUND target: `{agentName, paneTarget}` + the unexported
      `isDestination()` marker — NO channel id, NO credential. This is the
      direction asymmetry (design Decision 1): discord's `ResolveDestination`
      returns an OUTBOUND post target (channel-id + webhook credential, consumed by
      `Post`); web's returns an INBOUND pane-delivery target (agent + pane, consumed
      by the delivery leg, NEVER by `Post`).
- [ ] 2.4 Write a test PINNING the direction: a `webDestination` carries no
      credential and flows to the delivery leg (`resolvePane → AcquirePaneTxn →
      Confirm.Submit`), and is NEVER handed to a `Post` — so a future reader cannot
      conflate web's inbound destination with an outbound post target.

## 3. The `web` transport — inbound resolution (Decision 1 + 2)

- [ ] 3.1 Write a FAILING test: `webTransport.ResolveDestination(originChannel,
      target)` resolves ROSTER-WIDE (empty → hub XO; `@name`/`name` → any roster
      agent, case-insensitive, exact-match-wins, ambiguity rejected) and IGNORES
      `originChannel`. Assert it matches the dash's existing `resolveTarget`
      semantics (`library.go:216-238`) exactly — including the case-collision
      exact-wins-else-ambiguous rule (pin against
      `control_test.go:301` `TestRoute_CaseCollisionExactWinsElseAmbiguous`).
- [ ] 3.2 Implement `webTransport.ResolveDestination` by extracting the dash's
      roster-wide resolver into a shared function both `control.LibraryController`
      and `webTransport` call — so there is ONE roster-wide resolver, not a
      reimplementation. This satisfies the SPEC REQUIREMENT "The roster-wide
      resolver is shared, not forked" (transport spec): write a test asserting
      BOTH call sites resolve through the one shared function (forking-and-drifting
      is a spec violation, not just a code-smell).
- [ ] 3.3 Write a test asserting the web transport does NOT implement `CatchUp`
      (type-assert returns `ok=false`) — its delivery is in-process and cannot
      gap, the clean demonstration that the optional capability is optional
      (`catchup.go:31-38`, `transport-spi/design.md:421-425`).
- [ ] 3.4 Write a test PINNING `webTransport.Subscribe` as a NO-OP: it opens no
      inbound feed (the only web ingress is the gated `POST /api/control/route` HTTP
      route — task 5.2). This forecloses a second, ungated `Subscribe` path that
      would bypass the reused `requireWrite` / Host / Origin defenses (transport
      spec "Web inbound is the ONE gated HTTP route; Subscribe is a no-op").

## 4. The `web` transport — delivery leg enters the confirmed-delivery pipeline (Decision 1)

- [ ] 4.1 Pin the CURRENT dash route delivery as the baseline. Confirm the
      existing route suite passes UNCHANGED:
      `internal/dash/control/control_test.go` (`TestRoute_*`,
      `control_test.go:229-380`) — the lock-on-resolved-pane-target keying
      (`:229`), the lock-brackets-submit ordering (`:255`), the typed-outcome
      mapping (`:358`), the busy-not-error contention (`:337`).
- [ ] 4.2 Confirm the web inbound delivery reuses the SAME confirmed-delivery
      path (`resolvePane → AcquirePaneTxn → Confirm.Submit`,
      `library.go:155-187`) — NOT a reimplementation. This delivery leg is the
      SEPARATE `internal/deliver` + `internal/surface` seam, NOT part of the
      `Transport` interface (the SPI carries inbound-feed + outbound-post only); the
      web transport's CALLER invokes it, exactly as the watch relay's caller invokes
      it via the Injector. The web transport's inbound delivery and the dash control
      `Route` share the one delivery leg; a test asserts the pane lock is keyed on
      the identical resolved target every writer uses (the cross-process
      serialization contract — the per-pane flock, the ONLY cross-process coupling,
      is the convergence guarantee, NOT the SPI).

## 5. Security model — REUSE (Decision 3)

- [ ] 5.1 Byte-pin the dash's security tests UNCHANGED — they are the web
      transport's security model: `internal/dash/server_test.go`
      `TestValidateBind` (`:214`, loopback fail-closed), `TestHostAllowlist`
      (`:187`, anti-rebinding), and the `requireWrite` CSRF tests in
      `internal/dash/control_handlers_test.go`
      (`TestControl_MissingCustomHeaderRejected` `:146`,
      `TestControl_GETOnControlRouteRejected` `:165`). The web transport adds NO
      new listener and NO new auth surface, so these suites cover its posture and
      MUST pass UNCHANGED.
- [ ] 5.2 Add a graft-level test: a web coordination instruction arrives via
      `POST /api/control/route` and is gated by the EXISTING `requireWrite` +
      Host allowlist (no new ungated ingress was introduced).

## 6. Wiring (Decision 4 — simultaneous discord + web)

- [ ] 6.1 Write a FAILING test for the dash command wiring: `cmd/flotilla/dash.go`
      constructs the DISCORD-backed transport for the notify and injects it into the
      control library's `post` / cap seams — mirroring how
      `cmd/flotilla/watch.go:154,172-176` constructs the discord transport and binds
      a webhook destination via `NewWebhookDestination`. (The web transport is the
      INBOUND resolver, registered + selected separately; it is not the notify's
      post medium.)
- [ ] 6.2 Implement the dash wiring: construct the discord-backed transport +
      webhook destination, thread it as a `dash.Config` field → `dash.NewServer`
      (`dash.go:54`) → `control.NewLibrary` (called at `server.go:107`, INSIDE
      `NewServer` — NOT in cmdDash; the seam extension is on `NewLibrary`'s
      signature). Existing dash flags + defaults unchanged.
- [ ] 6.3 Add a coexistence test: a `web`-constructed transport and a
      `discord`-constructed transport both resolve from the registry concurrently
      with no shared mutable state beyond the mutex-guarded maps
      (`registry.go:24`); a delivery to one pane from each serializes on the
      cross-process lock (assert the same pane key).

## 7. Spec + guard close-out

- [ ] 7.1 `openspec validate web-transport --strict` passes.
- [ ] 7.2 `bash scripts/check-private-boundary.sh` passes (no deployment
      identifiers introduced; generic roles only).
- [ ] 7.3 Full `go test ./...` green — every byte-pinned dash suite UNCHANGED,
      every new graft test passing.
- [ ] 7.4 Run the review trio (systems-review + open-code-review + STORM) on the
      implementation diff; iterate until clean.
