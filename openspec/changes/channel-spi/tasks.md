# Tasks — the Channel SPI (Discord extract-in-place, then web)

Two PRs. PR1 defines the SPI and extracts Discord with ZERO behavior change
(byte-pinned by the existing suites). PR2 adds the web channel.

## PR1 — define the SPI + extract Discord (zero behavior change)

### 1. The SPI: interface + registry + optional capabilities

- [ ] 1.1 NEW `internal/channel/channel.go`: define the `Channel` interface
      (`Name` · `Subscribe(ctx, destinations, handler)` · `Post(dest, username, content)` ·
      `ResolveDestination(originChannel, bareOrMention)` · `Close`), the medium-agnostic
      `MessageHandler` projection (origin, id, sender, content), and the opaque
      `Destination` type. Doc each with the seam it replaces.
- [ ] 1.2 NEW `internal/channel/registry.go`: `const DefaultChannel = "discord"`,
      `var registry map[string]Channel`, `Register`, `Get(name)` (empty ⇒ default) —
      mirroring `internal/surface/surface.go:164-176` EXACTLY. Test (`registry_test.go`):
      a registered channel resolves by name; an empty name resolves to `discord`; an
      unknown name returns ok=false.
- [ ] 1.3 NEW `internal/channel/catchup.go`: the OPTIONAL `CatchUp` capability interface
      (`MessagesAfter(dest, afterID, pageLimit, maxPages)` + `Latest(dest)`), mirroring
      `surface.ResultReader` (type-asserted). Test: a channel implementing it type-asserts
      true; one not implementing it asserts false (the skip-cleanly contract).

### 2. Extract Discord into a registered discordChannel (TDD, byte-pinned)

- [ ] 2.1 NEW `internal/channel/discord.go`: `discordChannel` implementing `Channel`,
      self-registering in `init()` (mirroring `surface/grok.go:14`). `Post` wraps
      `internal/discord.Post` (`discord/discord.go:61`); `Subscribe` builds+opens the
      gateway (`discord/gateway.go:38,83`) and ADAPTS its 5-arg `MessageHandler`
      (`gateway.go:16`) to the 4-arg projection, folding the webhook-id into the
      self-mirror sentinel so `relay.Accept`'s feedback guard (`relay/relay.go:18-23`)
      is preserved; `ResolveDestination` is the existing `BindingForChannel`→`XOAgent`→
      `Webhook` chain (`reply.go:181` `replyDest`); `Close` closes the gateway session.
- [ ] 2.2 `discordChannel` implements `CatchUp` by delegating to `internal/discord.REST`
      (`rest.go:100` `MessagesAfterPaged` / `:123` `Latest`). Test: it type-asserts as a
      `CatchUp` and returns the same projection.
- [ ] 2.3 Re-point `internal/watch/catchup.go:29` `MessageReader` seam at the channel's
      `CatchUp` capability (instead of `*discord.REST` directly). The reconcile logic
      (`sweep`/`sweepChannel`/`disposition`) is UNTOUCHED. Existing
      `internal/watch/catchup_test.go` passes UNCHANGED.
- [ ] 2.4 Re-point the inbound wiring in `cmd/flotilla/watch.go:531-557` (gateway+REST+
      relay) to obtain the channel via `channel.Get(...)` and call `Subscribe`. The relay
      `Handle` (`internal/watch/relay.go:52`) and `Job{Kind:"relay"}` enqueue (`:96`) are
      UNCHANGED. Existing `internal/watch/relay_test.go` + `cmd/flotilla/relay_test.go`
      pass UNCHANGED.
- [ ] 2.5 Re-point the outbound paths — `cmd/flotilla/reply.go` (`replyDest`/`discord.Post`,
      `:181-194,234,240`) and `cmd/flotilla/mirror.go:39` `deskMirror.run` (the `post`
      collaborator, `:63`) — at `Channel.Post`/`ResolveDestination`. Existing
      `reply_test.go` + `mirror_test.go` pass UNCHANGED.

### 3. PR1 proof obligation + spec

- [ ] 3.1 BYTE-PINNED PROOF: run the FULL existing suites
      (`internal/relay`, `internal/discord`, `internal/watch` relay+catchup,
      `cmd/flotilla` relay/reply/mirror) WITHOUT editing any test. They MUST pass as-is.
      A test requiring an edit to stay green ⇒ the extraction changed behavior — fix the
      extraction, not the test.
- [ ] 3.2 `go vet ./... && gofmt -l` clean; `go test ./...` green.
- [ ] 3.3 ADD the `channel` spec requirements that PR1 satisfies: the SPI-routing
      requirement, the interface requirement, the optional-catch-up requirement, and the
      byte-for-byte-extraction requirement. `openspec validate channel-spi --strict`.
- [ ] 3.4 Design gate (systems-review + open-code-review + STORM) on the PR1 diff; iterate
      until clean. PR; CI green; merge on clean gates.

## PR2 — add the web channel (loopback-only)

### 4. webChannel behind the SPI (TDD)

- [ ] 4.1 NEW `internal/channel/web.go`: `webChannel` implementing `Channel`,
      self-registering in `init()`. Construction takes a bind address.
- [ ] 4.2 LOOPBACK-ONLY GUARD (write the test FIRST): constructing `webChannel` with a
      non-loopback bind (e.g. `0.0.0.0:PORT`, a LAN IP) returns a fail-closed error and
      opens NO listener; a loopback bind (`127.0.0.1` / `::1`) succeeds. Pin this as the
      security-by-construction invariant.
- [ ] 4.3 Inbound: `webChannel.Subscribe` delivers an operator message through the SAME
      `relay.Accept`/`Route` decision logic and the SAME `Job{Kind:"relay"}` enqueue as
      the Discord path. Test: a web operator message routes identically to a Discord one
      (shared relay logic), via the shared `route` seam.
- [ ] 4.4 Outbound + addressing: `webChannel.Post` and `ResolveDestination` deliver to the
      loopback medium. Test the round-trip (post → loopback receiver).
- [ ] 4.5 `webChannel` does NOT implement `CatchUp` (loopback cannot gap). Test: the
      type-assertion fails and the backstop is skipped cleanly — proving the optional
      capability is genuinely optional.

### 5. PR2 spec + gate

- [ ] 5.1 ADD the `channel` "second channel binds loopback-only by construction" requirement
      (web reuses the relay logic; non-loopback bind refused; no catch-up needed; discord
      unaffected). `openspec validate channel-spi --strict`.
- [ ] 5.2 `go vet ./... && gofmt -l` clean; `go test ./...` green (incl. the unchanged
      Discord suites — adding web must not perturb the default `discord` channel).
- [ ] 5.3 Design gate (systems-review + open-code-review + STORM) on the PR2 diff; iterate
      until clean. PR; CI green; merge on clean gates.

## 6. Close-out

- [ ] 6.1 Update docs (`llm.md` / `README.md` as relevant) to describe the pluggable
      channel layer and the loopback-only web channel, using only generic roster roles
      (`xo`, `backend`, `frontend`, `data`, …) — no deployment identifiers.
- [ ] 6.2 Archive the `channel-spi` change once both PRs are merged.
