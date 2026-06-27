# Proposal — relay-catchup-inbox (at-least-once operator-message ingestion + a re-fetch path)

## Why

An operator Discord message can reach the bound channel, be **accepted by Discord**, and **never
reach the agent — with NO alert**. Observed live (#161, 2026-06-22): the 20:54 home-channel message
*"Don't be a deadbeat. Be proactive chief of staff"* showed in the channel from the operator but had
**no `flotilla-watch: → xo:` relay echo and raised no not-delivered alert.** The operator only
found it by sending a separate nudge; the XO had to hand-roll a Discord-API call with the bot token to
recover the original text. **A vanished operator directive with no trace** — strictly worse than the
panel-block (#156), which at least stranded a desk *visibly*.

**Root cause — verified against canonical source, NOT inferred:**

1. **The relay observes only the live gateway websocket.** `internal/discord/gateway.go` dispatches a
   message only when a `MESSAGE_CREATE` event arrives on the open websocket (`gateway.go:41-50`). Its
   own doc comment **documents the gap as accepted behavior** (`gateway.go:62-63`): *"messages sent
   during a disconnect window are not replayed (the operator can resend)."*
2. **discordgo re-IDENTIFIES on a failed resume, losing the gap.** On Op9 (Invalid Session) discordgo
   calls `identify()` (`bwmarrin/discordgo@v0.29.0 wsapi.go:618-628`). A *fresh identify replays no
   `MESSAGE_CREATE` events*. Resume is attempted first (`wsapi.go:126-127`, when `sessionID` is set),
   but a *failed* resume falls back to re-identify — and every message that arrived between the socket
   dropping and the new session is gone **at the gateway-protocol level**, never delivered to flotilla.
3. **flotilla runs no catch-up and does not monitor reconnects.** Once the gateway opens,
   `relayController` stops watching it (`cmd/flotilla/relay.go:47-48`: *"a later disconnect after the
   relay is up is handled by discordgo's internal auto-reconnect (this controller no longer monitors
   it)"*). There is no REST reconciliation, no last-seen high-water mark, no acknowledgement — so a
   gap message is **silently and undetectably lost.**

The existing `### Requirement: A dropped operator message is never silent` (`openspec/specs/watch/spec.md`)
covers drops at the **delivery** layer (post-enqueue: pane-lock contention, busy-defer exhaustion,
unconfirmable submit). #161 is a hole **before** enqueue — the message never reaches the relay at all.
This change closes the **ingestion** layer.

**This is a frequent, ongoing hole — measured, not a one-off (2026-06-23).** The flotilla-watch
journal (Jun 3–23, 20 days) shows **303 `gateway disconnected`, 234 `gateway resumed`, 46 `gateway
connected`** — a ~69-event disconnect/resume deficit (~3.4/day) of disconnects that fell back to
re-identify (the gap-losing path). The incident is confirmed in the same journal: `gateway
disconnected` at **20:53:19** Jun 22, the operator's **20:54:40** message landed 81s in, and no resume
came until **21:31** — a ~38-min gap ending in re-identify. So an at-least-once backstop is justified
by data (it will fire regularly), which makes its correctness load-bearing and makes hanging an
immediate catch-up sweep off the (303) reconnect events high-value.

## What Changes

- **A REST catch-up reconciler, independent of the gateway websocket (the at-least-once backstop).**
  REST works even while the websocket is mid-reconnect — exactly when it is needed. A periodic poller
  (~30s) reconciles each bound channel against a **durable per-channel cursor** via the Discord REST
  `GET /channels/{id}/messages` API (`bwmarrin/discordgo restapi.go:1687 ChannelMessages`), relaying
  (or alerting on) any operator message the live path missed. The poller owns its OWN REST-only
  discordgo session (constructed, never `Open()`ed), so it is wholly decoupled from gateway websocket
  health and lifecycle.

- **A durable cursor + bounded dedup seen-set; two ingestion paths converge on one relay decision.**
  - **Cursor `C`** = the highest message snowflake the POLLER has processed, per channel. Persisted
    atomically to `<roster-dir>/flotilla-relay-cursor.json` (reusing the detector-snapshot
    fail-safe-atomic-write pattern). Snowflakes are time-ordered 64-bit ids → numeric `>` compare.
  - **Seen-set `S`** = a bounded set of recently-relayed message ids, shared by both paths, pruned of
    ids `<= C` after each poll (so it stays small).
  - **Live gateway path** stays the latency optimization: relay iff id ∉ S, add to S. It does **NOT**
    advance `C`. (This is the load-bearing correctness choice: if the live path advanced a single
    high-water mark, a post-gap live message would *leapfrog* the cursor past the gap messages and the
    poller's `after=C` fetch would never see them. The poller is the sole cursor authority.)
  - **Poller** fetches `after=C` (paginated; the live probe established `after=C` returns the
    **oldest** block above C, so the contiguous upward walk + page cap are **fail-closed** — the
    cursor never advances past an unfetched older message), and for each message in ascending id order:
    if it passes Accept (operator, non-webhook, non-empty) and id ∉ S → relay via the same
    `relay.Route` + `injector.Enqueue` seam, add to S. **The poller enqueues FIRST, then advances +
    persists `C`** (a crash in that window → a re-fetched duplicate on restart, NEVER a drop — the
    inverse ordering would silently drop, reintroducing #161). After commit, prune S of ids ≤ C.
    Result: every operator message with id > C is relayed **at least once**; the seen-set prevents a
    double-relay of one the live path already delivered.

- **Recovered-message disposition: count-primary (auto-relay few; alert on bulk; a loose stale
  ceiling).** When the poller relays a message the live path missed, that IS a recovery. The
  discriminator is **count-primary** (keying on message-age alone would alert-storm on every deploy
  longer than the window — STORM finding): recovered messages **≤ a bulk cap (default 5)** AND within a
  **loose stale ceiling (default 24h, on the batch's oldest)** are **auto-relayed in id order** with a
  one-line trace notice (*"recovered N operator message(s) the live gateway missed via catch-up"*). A
  **bulk** backlog (> cap) OR **ancient** messages (> ceiling — a very long outage) are **NOT
  blind-injected**; a LOUD alert names the count + points at `flotilla inbox` for human triage. This is
  **at-least-once, NOT strict in-order across the live/poll seam** — the spec states that honestly; the
  event-trigger below shrinks the reorder window to ~0 for the common case.

- **A reconnect event-trigger (floor + accelerator + liveness, nearly free).** The `Resumed`/`Connect`
  handlers already exist (`gateway.go:53-58`, only logging today). The gateway gains an optional
  `OnReconnect` callback that **kicks an immediate catch-up sweep** — collapsing recovery latency for
  the common reconnect gap from ≤30s to ~0s (303 reconnects/20d make this high-value). No per-disconnect
  alert (303 flaps would be alert fatigue); disposition on what the sweep finds is the alert authority.
  The periodic poll remains the floor (covers the daemon-restart window).

- **Poller liveness (the meta-#161 — never let the backstop die silently).** A silently-dead poller
  would re-create #161 with *false confidence*. Each sweep logs its outcome; consecutive sweep-level
  REST failures **escalate ONCE** to the operator past a threshold (mirroring
  `relayController.escalateThreshold`) and re-arm on recovery.

- **First-boot tail-init (no history flood).** On the first poll for a channel with no persisted
  cursor, `C` is initialized to the channel's latest message id (a `limit=1` fetch) WITHOUT relaying
  history, then the daemon goes live + polls from there. (Avoids relaying the entire channel backlog
  on a fresh deploy.)

- **`flotilla inbox <channel> [--limit N]` — the manual re-fetch / recovery path (gap 2).** A new CLI
  subcommand that REST-fetches recent messages from a bound channel (resolved by `role` label or raw
  channel id from the roster) and prints them (timestamp, author, id, content), operator messages
  flagged. Pure REST (own session, no gateway), reusing the same fetch helper. Closes the recovery gap
  that forced the hand-rolled bot-token Discord-API call. Read-only in v1 (the auto-catch-up does the
  re-injection); a `--relay` re-inject flag is a noted follow-up, not in scope.

## Composition with the existing relay

- The catch-up poller feeds the **same** `relay.Route` decision and `injector.Enqueue` path as the live
  gateway handler — so routing (bare → channel XO; `@name` → member), Accept (operator-only,
  drop-self-mirror), the empty-content guard, the confirmed-delivery confirm, and the existing
  never-silent delivery-layer alerts all apply **unchanged** to recovered messages. This change adds an
  ingestion source + a dedup gate in front of that seam; it does not alter the seam itself.
- The dedup gate (cursor + seen-set) is the only shared mutable state between the gateway handler and
  the poller; it is mutex-guarded (concurrent gateway dispatch goroutine vs the poll goroutine).

## Out of scope

- **Re-injecting from `flotilla inbox`** (a `--relay` flag). The auto-catch-up already re-injects;
  `inbox` is the read-only diagnostic/recovery view in v1. Noted as a follow-up.
- **Exactly-once delivery.** Impossible in general; the guarantee here is **at-least-once with dedup**
  (a message is relayed ≥1 time; the seen-set makes a double-relay of the same id unlikely, never
  guaranteed-zero across a daemon restart mid-flight — acceptable: a rare duplicated operator message
  is infinitely better than a silently-dropped one).
- **Reconstructing the panel-block class (#156).** Orthogonal — that is a *delivery* failure the
  self-heal addresses; this is an *ingestion* gap. (If a recovered message hits a blocked pane, the
  existing delivery-layer alerts/self-heal apply.)
- **Persisting the seen-set `S` across restarts.** Only the cursor `C` is durable; `S` rebuilds from
  the live path + poll after restart. (The cursor is sufficient for the at-least-once guarantee; `S`
  only dedups the live/poll overlap within a running daemon.)
- **A backfill of messages older than the cursor on first deploy** (tail-init is deliberate).

## Impact

- **`internal/discord/`** — a REST fetch capability: a `FetchMessagesAfter(channelID, afterID, limit)`
  / `FetchLatest(channelID)` helper over `ChannelMessages` on a REST-only session (no `Open()`),
  shared by the poller and the `inbox` command.
- **`internal/watch/`** — a new `catchup` reconciler (cursor + seen-set dedup gate, the poll loop, the
  recovered-message disposition) and its durable cursor store (atomic write). The dedup gate is wired
  into the live `Relay.Handle` path (id-aware) so both sources share it.
- **`internal/discord/gateway.go`** — surface the message id (and timestamp) to the `MessageHandler`
  so the live path can consult the dedup gate. (Today the handler signature omits the id.)
- **`internal/watch/relay.go`** — `Handle` consults the dedup gate (relay iff new) before enqueue.
- **`cmd/flotilla/watch.go`** — construct + start the catch-up poller alongside the relay controller
  (same enable condition: bound channels + bot token + operator id), wire the cursor path flag, and
  the recovered-message notice/alert hooks. Non-fatal like the relay (a poller that cannot start
  degrades to live-only + a warning; the clock is unaffected).
- **`cmd/flotilla/inbox.go`** (new) + `main.go` dispatch — the `inbox` subcommand.
- **Risk:** MEDIUM. The poller strictly ADDS an ingestion source; the dedup gate guards against
  double-relay. The risks to guard (covered in design): (a) the leapfrog bug (solved by cursor-authority
  living only in the poller), (b) a first-boot history flood (solved by tail-init), (c) a long-outage
  flood (solved by the freshness/bulk alert switch), (d) the gateway-vs-poll race on the shared gate
  (mutex), (e) Discord REST rate limits (a ~30s poll over a handful of channels is far under the
  budget; discordgo's rate-limiter handles bursts). A poller failure degrades to today's behavior
  (live-only) — no regression, with a warning.
