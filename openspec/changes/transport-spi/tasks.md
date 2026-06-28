# Tasks — the Transport SPI (Discord extract-in-place; web deferred)

This change ships in two PRs, but **only PR1 is in scope now**. PR1 defines the
SPI and extracts Discord with the behavior preserved (the ONLY intended signature
change is `relay.Accept`'s `webhookID` fold, re-pinned by an updated + a new
test). PR2 (the web transport) is DEFERRED behind an operator decision — its tasks
are recorded in a clearly-labelled "DEFERRED (PR2, operator-gated)" section and
are NOT buildable work in this change.

## PR1 — define the SPI + extract Discord (IN SCOPE; behavior preserved)

### 1. The SPI: interface + registry + optional capabilities

- [ ] 1.1 NEW `internal/transport/transport.go`: define the `Transport` interface
      (`Name` · `Subscribe(ctx, destinations, handler, onReconnect)` ·
      `Post(dest, username, content)` · `ResolveDestination(originChannel, bareOrMention)` ·
      `MaxContentRunes()` · `Chunk(text)` · `Close`), the medium-agnostic
      `MessageHandler` 4-field projection (origin, id, sender, content — `webhookID`
      is NOT carried; see 2.1), and the opaque `Destination` type. Doc each with the
      seam it replaces. `Subscribe` takes `onReconnect func()` so the #161
      reconnect-gap→catchup-kick coupling survives the seam.
- [ ] 1.2 NEW `internal/transport/registry.go`: `const DefaultTransport = "discord"`,
      `var registry map[string]Transport`, `Register`, `Get(name)` (empty ⇒ default) —
      mirroring `internal/surface/surface.go:164-176` EXACTLY. Test (`registry_test.go`):
      a registered transport resolves by name; an empty name resolves to `discord`; an
      unknown name returns ok=false.
- [ ] 1.3 NEW `internal/transport/catchup.go`: the OPTIONAL `CatchUp` capability interface
      (`MessagesAfter(dest, afterID, pageLimit, maxPages)` + `Latest(dest)`) and the
      `transport.Message` projection, mirroring `surface.ResultReader` (type-asserted).
      Test: a transport implementing it type-asserts true; one not implementing it asserts
      false (the skip-cleanly contract).

### 2. Extract Discord into a registered discordTransport (TDD, behavior-pinned)

- [ ] 2.1 NEW `internal/transport/discord.go`: `discordTransport` implementing `Transport`.
      REGISTRATION vs CONSTRUCTION are separate (stateful-transport lifecycle): `init()`
      registers a factory/zero-value keyed `discord` (the bot token is NOT available at
      init); a separate construct step (called from `runWatch`) takes the bot token +
      channel ids + cursor path and wires the live gateway/REST/catchup. `Post` wraps
      `internal/discord.Post` (`discord/discord.go:61`); `MaxContentRunes`/`Chunk` return
      `discord.MaxContentRunes` / wrap `discord.ChunkContent`; `Subscribe` builds+opens
      the gateway (`discord/gateway.go:38,83`), forwards `onReconnect`, and ADAPTS the
      5-arg `MessageHandler` (`gateway.go:16`) to the 4-field projection — DROPPING a
      message with non-empty `webhookID` INSIDE the adapter (the self-mirror guard moves
      here, AUTHOR-AGNOSTIC); `ResolveDestination` is the existing
      `BindingForChannel`→`XOAgent`→`Webhook` chain (`reply.go:181` `replyDest`); `Close`
      tears down the gateway session AFTER the caller's ctx-owned catchup goroutine drains.
- [ ] 2.2 THE SELF-MIRROR GUARD MOVE (the one intended signature change): fold `webhookID`
      out of `relay.Accept` (`internal/relay/relay.go:18`) — it becomes
      `Accept(authorID, operatorID)`. UPDATE `internal/relay/relay_test.go`: migrate the
      self-mirror-drop case to assert the ADAPTER drops a webhook-flagged inbound before
      `handler`. ADD a NEW adversarial test: a transport self-post is dropped EVEN WHEN
      its sender id EQUALS the operator id (the author-agnostic case the existing test
      misses). Update `internal/watch/relay.go:52` `Handle` to the 4-field signature
      (drop the `webhookID` param); its `route`/`Enqueue` are unchanged.
- [ ] 2.3 `discordTransport` implements `CatchUp` by delegating to `internal/discord.REST`
      (`rest.go:100` `MessagesAfterPaged` / `:123` `Latest`), returning the
      `transport.Message` projection. Test: it type-asserts as a `CatchUp` and returns the
      same projection.
- [ ] 2.4 Re-point `internal/watch/catchup.go:29` `MessageReader` seam at the transport's
      `CatchUp` capability (instead of `*discord.REST`), re-typing `discord.Message` →
      `transport.Message` in `catchup.go`/`dedup.go` (incl. `MaxSnowflake`, `classify`).
      Move `ParseSnowflake` (`internal/watch/relay.go:77`) to a `transport`-package helper
      so NO `internal/discord` import remains in `internal/watch`. The reconcile logic
      (`sweep`/`sweepChannel`/`disposition`) is UNTOUCHED; `catchup_test.go` passes
      UNCHANGED.
- [ ] 2.5 Re-point the inbound wiring in `cmd/flotilla/watch.go:531-557` (gateway+REST+
      relay) to obtain the transport via `transport.Get(...)`, construct it, and call
      `Subscribe(…, catchupKick)`. PRESERVE the NON-FATAL degrade (`watch.go:505-512`): a
      transport construct/Subscribe failure degrades to clock-only / live-only and NEVER
      crashes the clock. `internal/watch/relay_test.go` + `cmd/flotilla/relay_test.go`
      pass UNCHANGED.
- [ ] 2.6 Re-point the outbound paths at `Transport.Post`/`ResolveDestination`/`Chunk`:
      `cmd/flotilla/reply.go` (`replyDest`/`discord.Post`, `:100,181,234,240`);
      `cmd/flotilla/mirror.go` (`deskMirror.run`/`post`, `:55,63`); `cmd/flotilla/watch.go`
      (the down-alert `post` closure `:128`; the desk-mirror `post` collaborator `:700`);
      `cmd/flotilla/main.go` (`flotilla send` `:444` + c2 send `:604`; the
      `MaxContentRunes` length guards `:376,429` → `transport.MaxContentRunes()`). Existing
      `reply_test.go` + `mirror_test.go` pass UNCHANGED.

### 3. PR1 proof obligation + spec

- [ ] 3.1 BEHAVIOR-PINNED PROOF: run the FULL existing suites
      (`internal/relay`, `internal/discord`, `internal/watch` relay+catchup,
      `cmd/flotilla` relay/reply/mirror). They MUST pass UNCHANGED **except**
      `internal/relay/relay_test.go`, which is the deliberately-updated suite for the
      `webhookID` fold + the new adversarial self-mirror test (task 2.2). Any OTHER suite
      requiring an edit to stay green ⇒ the extraction changed behavior — fix the
      extraction, not the test.
- [ ] 3.2 EXHAUSTIVE-SEAM CHECK: re-run `grep -rn 'internal/discord' --include=*.go cmd/
      internal/ | grep -v _test` and confirm every remaining site is either re-pointed,
      or a DEFERRED site with a tracking issue (inbox `Recent`; the dash control notify),
      or the OUT-OF-SCOPE guild-provisioning CLI (`cmd/flotilla/channel.go`). No
      half-migrated bus seam.
- [ ] 3.3 FILE the deferred-site tracking issues: (a) `flotilla inbox` (`inbox.go`'s
      `Recent` is not in `CatchUp` — decide add-`Recent`-to-CatchUp vs a separate
      `HistoryReader` capability vs leave-on-discord); (b) the `internal/dash/control`
      notify path (moves with the PR2 web/dash fork).
- [ ] 3.4 `go vet ./... && gofmt -l` clean; `go test ./...` green.
- [ ] 3.5 ADD the `transport` spec requirements that PR1 satisfies: the SPI-routing
      requirement, the interface requirement (incl. `onReconnect` + the content-cap
      methods), the optional-catch-up requirement, the stateful-lifecycle + non-fatal-
      degrade requirement, and the behavior-preserving-extraction requirement (with the
      honest `relay.Accept` signature-change framing). `openspec validate transport-spi
      --strict`.
- [ ] 3.6 Design gate (systems-review + open-code-review + STORM) on the PR1 diff; iterate
      until clean. PR; CI green; merge on clean gates.

## DEFERRED (PR2, operator-gated) — add the web transport

**NOT buildable in this change.** Adding a web coordination surface collides with
the ratified "dashboard = separate desk" decision
(`openspec/specs/product-decisions/spec.md:141`) and is blocked on an OPERATOR
FORK: Option 1 = the web transport IS `internal/dash` refactored behind the SPI
(reusing its Host-allowlist + CSRF defenses, `server.go:90-91`); Option 2 = a
separate second web surface. The tasks below are recorded so the thinking is not
lost; they execute only AFTER the operator resolves the fork.

### 4. webTransport behind the SPI (TDD) — deferred

- [ ] 4.1 (DEFERRED) Resolve the operator fork (Option 1 dash-refactor vs Option 2 second
      surface) BEFORE any web code. The lean is Option 1 (reuse the dash's proven
      defenses); it touches `product-decisions/spec.md:141`, so it is the operator's call.
- [ ] 4.2 (DEFERRED) Web transport behind the SPI, self-registering. Construction takes a
      bind address. LOOPBACK-ONLY GUARD (test FIRST): a non-loopback bind returns a
      fail-closed error and opens NO listener; a loopback bind succeeds.
- [ ] 4.3 (DEFERRED) Anti-DNS-rebinding Host allowlist + CSRF/Origin on state-changing
      routes + an operator auth posture — REUSING `internal/dash`'s proven defenses
      (`server.go:90-91`, `requireWrite`) under Option 1.
- [ ] 4.4 (DEFERRED) Inbound reuses `relay.Route` + the `Job{Kind:"relay"}` enqueue;
      outbound + `ResolveDestination` deliver to the loopback medium. (No "shared relay
      route" spec claim — the shipping dash resolves roster-wide; the reconciliation is
      part of this fork.)
- [ ] 4.5 (DEFERRED) The web transport does NOT implement `CatchUp` (loopback cannot gap);
      the type-assertion fails and the backstop is skipped cleanly.

### 5. PR2 spec + gate — deferred

- [ ] 5.1 (DEFERRED) ADD the `transport` web-transport spec requirement once the fork is
      resolved (loopback-only by construction; reuses the relay logic; discord
      unaffected). `openspec validate transport-spi --strict`.
- [ ] 5.2 (DEFERRED) `go vet` / `gofmt` / `go test ./...` green (incl. the unchanged Discord
      suites — adding web must not perturb the default `discord` transport).
- [ ] 5.3 (DEFERRED) Design gate (systems-review + open-code-review + STORM) on the PR2
      diff; PR; CI green; merge on clean gates.

## 6. Close-out

- [ ] 6.1 Update docs (`llm.md` / `README.md` as relevant) to describe the pluggable
      transport layer, using only generic roster roles (`xo`, `backend`, `frontend`,
      `data`, …) — no deployment identifiers.
- [ ] 6.2 Archive the `transport-spi` change once PR1 is merged (PR2 remains an open,
      operator-gated follow-up tracked by #106).
