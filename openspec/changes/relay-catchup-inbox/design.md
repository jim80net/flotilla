# Design — relay-catchup-inbox

## Problem statement (one sentence)

An operator message that the live Discord **gateway websocket never delivers** (a reconnect /
resume-failure gap, or a daemon-restart window) is currently **lost with no trace and no alert**; this
change adds a **REST-based, at-least-once ingestion backstop** plus a manual re-fetch command, without
altering the existing relay decision/delivery seam.

## Base rate — this is a frequent, ongoing hole, not a one-off (measured 2026-06-23)

`journalctl --user -u flotilla-watch` over Jun 3–23 (20 days): **303 `gateway disconnected`, 234
`gateway resumed`, 46 `gateway connected`.** The disconnect/resume deficit (**~69 disconnects that did
NOT cleanly resume**, ~3.4/day) is the gap-losing population (a failed resume → re-identify, no event
replay). The #161 incident is empirically confirmed in the same journal: `gateway disconnected` at
**20:53:19** on Jun 22, the operator's **20:54:40** message landed 81s into that window, and the next
resume was not until **21:31** — a ~38-minute gap that ended in re-identify, dropping the message
exactly as the root-cause analysis predicts. So the at-least-once poller is justified by data (it will
fire regularly), which also makes its **correctness** (cursor/persist ordering, pagination)
load-bearing, and makes the reconnect **event-trigger** valuable (303 reconnects to hang an immediate
sweep on).

## Canonical-source grounding (verified against source, not inferred)

1. **The relay sees only live websocket events.** `internal/discord/gateway.go:41-50` dispatches on
   `MESSAGE_CREATE`; nothing else feeds `MessageHandler`. The `Disconnect`/`Resumed` handlers already
   exist but only log (`gateway.go:53-58`) — the event-trigger below hangs off them.
2. **discordgo loses the gap on a failed resume.** `bwmarrin/discordgo@v0.29.0 wsapi.go:618-628`:
   Op9 (Invalid Session) → `identify()` (fresh session, no event replay). Resume tried first
   (`wsapi.go:126-127`, when `sessionID` is set) but not guaranteed.
3. **REST is independent of the websocket.** `restapi.go:1687 ChannelMessages(channelID, limit,
   beforeID, afterID, aroundID)` — a plain HTTP call; succeeds regardless of websocket state and does
   not require `Open()`.
4. **`after`-pagination ordering — PROBED LIVE this session (F1's mandated verify), not recalled.**
   On channel `1511357941893304462`: `GET .../messages?after=<C>&limit=2` returned the **two OLDEST
   messages with id > C** (the block nearest the cursor), **ordered newest-first within the batch.**
   So `after=C` walks the contiguous block immediately above the cursor — the property the
   forward-walk + fail-closed page-cap rely on.

## Architecture: two ingestion sources, one relay seam, one dedup gate

```
                         ┌─────────────────────────────────────────────┐
  Discord gateway WS ──► │ gateway.go MESSAGE_CREATE (live, low-latency) │──┐
                         │ Resumed/Connect ──► kick(immediate sweep)     │  │
                         └─────────────────────────────────────────────┘  │
                                                                           ▼
                                                              ┌────────────────────────┐
                                                              │  dedup gate (mutex)     │
                                                              │  cursor C + seen-set S  │  relay iff id is new
                                                              └────────────────────────┘
                                                                           ▲
                         ┌─────────────────────────────────────────────┐  │
  Discord REST (poll) ─► │ catchup poller — ONE goroutine, ticker+kick  │──┘
                         │  synchronous sweep (single-flight by const.) │
                         │  REST-only session, independent of WS health │
                         └─────────────────────────────────────────────┘
                                                                           │ (new id, in id order)
                                                                           ▼
                                       relay.Route(content, XO, members) → injector.Enqueue(Job{relay})
                                                                           │
                                                                           ▼
                                       Confirm.Submit(WithSelfHeal) → confirmed delivery / never-silent alert
```

Both sources converge on the **unchanged** `relay.Route` + `injector.Enqueue` seam. The only new
shared mutable state is the dedup gate.

## The dedup gate (the correctness core)

```go
// package watch
type dedup struct {
    mu     sync.Mutex
    cursor map[string]uint64          // channelID -> highest snowflake the POLLER has processed
    seen   map[string]*seenSet        // channelID -> recently-relayed ids (pruned to > cursor, size-capped)
    store  cursorStore                // durable persistence of cursor (NOT written under mu — see F7)
}

// liveNew reports whether a LIVE (gateway) message is new and should be relayed.
// Records id in seen; DOES NOT advance cursor (the leapfrog guard). Called AFTER Accept + empty-guard.
func (d *dedup) liveNew(channelID string, id uint64) bool

// classify partitions an already-fetched, ascending batch (all ids > cursor) into the ids not in
// seen (to relay), marks them seen, and returns (toRelay, newCursor=max id). It DOES NOT persist and
// DOES NOT advance the durable position past unenqueued work — the caller enqueues, THEN calls
// commit(channelID, newCursor) which advances the in-memory cursor, prunes seen<=cursor, and persists.
func (d *dedup) classify(channelID string, batch []Message) (toRelay []Message, newCursor uint64)
func (d *dedup) commit(channelID string, newCursor uint64) error   // advance cursor, prune seen, persist
```

### Invariant 1 — only the poller advances the cursor (the leapfrog guard)

The live path records `seen` but never moves `cursor`. Counterexample if violated (single high-water
mark advanced by both): cursor=2; gap drops m3,m4 (never live); m5 delivered live → if live advanced
the mark, cursor=5, and the poller's `after=5` never sees m3,m4 → **permanently lost.** With the
invariant: live sets seen={5}, cursor stays 2; next poll `after=2` = [m3,m4,m5] (ascending), relays
m3,m4 (m5∈seen → skip), commits cursor=5. m3,m4 recovered. ∎

### Invariant 2 — enqueue BEFORE you persist the cursor (F7 — the at-least-once guard)

`classify` returns `toRelay` + the proposed `newCursor` but **does not persist.** The caller enqueues
every `toRelay` message first, and only then calls `commit` to advance+persist the cursor. A crash in
the window (enqueued, not yet committed) → on restart the cursor is still old → those messages are
re-fetched and re-relayed: a **duplicate**, never a drop. The inverse ordering (persist then enqueue)
would, on the same crash, leave the cursor advanced past un-enqueued messages → a **silent drop**,
reintroducing #161. At-least-once requires enqueue-then-persist. (Duplicates: rare, bounded to the
crash window, and a duplicated directive ≫ a dropped one — disclosed, not eliminated; exactly-once is
impossible.)

> RESIDUAL (honest disclosure, systems-review round 2): "enqueued" means the job has been handed to the
> injector's in-memory queue (`injector.Enqueue` returns once the job is on the buffered channel —
> `inject.go:256-261`), NOT that it has been delivered to the pane. So a crash in the narrow
> (sub-second) window between `commit` persisting the cursor and the injector worker actually delivering
> a still-buffered job loses that job with the cursor advanced past it. This is **the same class as the
> live relay path's existing behavior** — a crash always loses in-memory-queued-but-undelivered jobs,
> true of the live path today — so this change does **not introduce or worsen** that window; it is named
> here for completeness, not as a new defect. Closing it fully would require committing the cursor only
> on the injector's confirmed-delivery callback (couples the poller to async delivery + complicates
> batch ordering) — not warranted for v1 given the window is symmetric with existing semantics.

### Invariant 3 — the cursor never advances past an unfetched older message (F1 — fail-closed pagination)

Because `after=C` returns the **oldest** block above C (probed), each page is the contiguous-next
block. The walk: fetch `after=cursor, limit=pageLimit` → reverse to ascending → that page's **max** is
the next `after` → repeat **while a full page (==pageLimit) is returned** (a full page means more
contiguous messages exist above this page's max). The cursor is committed only to the max of the
**fully-processed contiguous run from C upward.** If the **hard page cap** (default 5 pages) is hit
before draining, the cursor commits to the last fully-processed page max and the remainder is left
above the cursor for the **next** sweep (plus a bulk-alert) — **never** advanced past an unfetched id.
Fail-closed by construction. (This is safe specifically because the probe established `after=C` =
oldest-above; a newest-above behavior would have required walking with `before` — hence the probe was
mandatory.)

### Bounded seen-set (F5)

After each `commit`, `seen[ch]` is pruned of ids `<= cursor[ch]`; since the poller never re-fetches
`<= cursor`, pruned entries are never needed again, so `seen` holds only `(cursor, latest]`. **Backstop
(F5):** if a single channel's poll persistently fails (channel-specific permission loss / deleted
channel) while the live path keeps adding ids, the prune never runs and `seen[ch]` could grow. So
`seenSet` is also **size-capped** (evict-oldest beyond, e.g., 1024 ids) — an evicted id at worst causes
a re-relay (dup) if the poll later recovers, never a lost-relay. Bounded regardless of poll health.

### At-least-once guarantee

For any operator message M in a bound channel with `id(M) > cursor` at M's arrival: the poller's
contiguous `after=cursor` walk includes M on its next sweep (REST returns it regardless of websocket
state), and M is relayed unless already in `seen` (already relayed live). Either way M is relayed
**≥ 1 time** (the commit-after-enqueue ordering preserves this across a crash; the fail-closed cap
preserves it across a backlog). The deliberate exclusion is the first-boot tail-init window
(below) — never replaying pre-deploy history. ∎

## Lock discipline (F2 — explicit; mirrors the detector's "no blocking call under d.mu")

- **Under `mu`:** the cursor read that seeds a sweep; `classify` (map reads/writes); `commit`'s
  in-memory advance + prune. **`commit`'s file persist is done OUTSIDE `mu`** (a fast local
  `os.Rename`, but kept off-lock so a slow fs never stalls the live path), guarded so only the single
  poll goroutine persists (no concurrent writer).
- **Never under `mu`:** the REST `FetchMessagesAfter` (network), and `injector.Enqueue` (blocks under
  backpressure — `inject.go:256-261`; holding `mu` across it would stall every live `liveNew`).
- Flow per channel per sweep: read cursor (lock) → fetch pages (off-lock) → `classify` (lock) →
  enqueue toRelay (off-lock) → `commit` advance+prune (lock) + persist (off-lock). `-race` test asserts
  no lock is held across a faked-slow fetch/enqueue.

## The poll loop (F3 — single goroutine, synchronous, single-flight by construction)

The sweep runs **synchronously in one ticker goroutine** (mirroring `detector.loop`), so there is
exactly one in-flight sweep ever and `time.Ticker` coalescing handles overrun (a slow paginating
sweep simply delays the next tick; it never overlaps). A reconnect **kick** is delivered on a
buffered channel and drained by the same `select` as the ticker, calling the same synchronous sweep —
so a kick can never run concurrently with a ticked sweep either.

```
loop (one goroutine):
  select { case <-ctx.Done(): return; case <-ticker.C: ; case <-kick: }
  for each bound channel ch:
     sweepChannel(ch)
  recordSweepHealth()   // liveness — see below

sweepChannel(ch):
  cur := gate.cursorOf(ch)                         // lock
  if ch has no cursor → FIRST BOOT:
     latest := rest.Latest(ch); gate.initCursor(ch, latest.id); persist; return  // tail-init, no relay
  pages, capped := walkAfter(rest, ch, cur, pageLimit, pageCap)   // off-lock; ascending, contiguous
  toRelay, newCur := gate.classify(ch, pages)      // lock
  recovered := disposition(ch, toRelay, capped)    // enqueue or alert (off-lock) — see below
  gate.commit(ch, newCur)                          // lock advance+prune; persist off-lock  (AFTER enqueue)
```

> NOTE `commit` runs even when `disposition` chose to ALERT rather than enqueue, because the alerted
> messages have been surfaced (the operator recovers via `inbox`); leaving the cursor un-advanced would
> re-alert the same backlog every sweep (alert storm). Advancing-after-alert is correct: the message
> was *handled* (surfaced loudly), just not auto-delivered.

## Reconnect event-trigger (STORM-1 — floor + accelerator + liveness, nearly free)

The `Resumed` and `Connect`/`Ready` handlers already exist (`gateway.go:53-58`). The gateway gains an
optional `OnReconnect func()` callback invoked from them; watch wires it to send a non-blocking `kick`
to the poll loop → an **immediate** catch-up sweep. This collapses recovery latency for the common
reconnect gap from ≤30s to ~0s, and (with 303 reconnects observed) makes the poller demonstrably
exercised. We do **NOT** raise a per-disconnect alert (303 flaps → 303 alerts = fatigue); the
disposition on what the sweep actually finds is the alert authority. The periodic ticker remains the
floor (covers the daemon-restart window, when no event was received because the daemon was down).

## Poller liveness (STORM-3 — the meta-#161: never let the backstop die silently)

The whole change exists because a silent failure became loud; a silently-dead poller would re-create
#161 with *false confidence*. So: each sweep logs its outcome (channels swept, recovered count, errors);
consecutive **sweep-level** failures (REST unreachable across a whole sweep) are counted and, past a
threshold (mirroring `relayController.escalateThreshold`), **escalate ONCE** to the operator ("relay
catch-up has failed N consecutive sweeps — the at-least-once backstop is DOWN; live gateway delivery
continues"). On recovery the count resets (re-arm). A per-channel persistent failure raises the same
degrade-warning and is covered by the F5 seen-bound.

## Recovered-message disposition (the fork, resolved on data — count-primary, freshness as a loose ceiling)

A message surfaced by the **poller** (∉ seen) is a *recovery*. The discriminator is **count-primary**
(STORM-2 / F6 — keying on message-age alone made every deploy >freshWindow an alert-storm):

```
recovered := toRelay (∉ seen)
if len(recovered) <= bulkCap  AND  oldest(recovered).age <= staleCeiling:
    enqueue(recovered) in ascending id order                 // AUTO-RELAY — the directive lands
    note once: "recovered N operator message(s) the live gateway missed via catch-up (gap near HH:MM)"
else:  // bulk OR ancient
    alert: "N operator message(s) were NOT auto-delivered (bulk/stale) — run `flotilla inbox <ch>`
            to view and re-send the still-relevant ones"
    (cursor still commits — they were surfaced; see NOTE above)
```

- **count-primary** (`bulkCap` default **5**): the common reconnect gap (1–2 messages) and a normal
  deploy's last-few messages auto-relay even if a slow sweep made them a few minutes old — no
  deploy-storm.
- **`staleCeiling` (default 24h)**: a *loose* sanity bound so a very long outage doesn't blind-inject
  day-old directives out of context; it does NOT fire on routine deploys. Evaluated on the batch's
  **oldest** message (explicit per F6).
- First-boot relays nothing → never triggers either branch.
- Defaults (`bulkCap=5`, `staleCeiling=24h`, `pollInterval=30s`, `pageLimit=100`, `pageCap=5`) are the
  recommended starting values; tunable via flags.

> ORDERING HONESTY (STORM-4): this is **at-least-once, NOT strict in-order across the live/poll seam.**
> A live message delivered after the gateway recovers can land before a gap message the next sweep
> recovers. The reconnect event-trigger shrinks this window to ~0 for the common case, and a recovered
> *batch* is delivered in ascending id order, but cross-seam global ordering is not guaranteed. The
> spec states this explicitly rather than over-claiming.

> DELIVERY-LAYER REUSE: recovered messages route through the SAME `injector.Enqueue` → `Confirm.Submit`
> → never-silent delivery alerts + per-pane transaction lock. A recovered message that then hits a
> blocked pane is alerted by the existing delivery layer; this change only guarantees it *reaches* that
> layer. (Verified: `inject.go:153-192`, `confirm.Submit`.)

## Cursor durability and the daemon-restart window

- `cursor` persists to `<roster-dir>/flotilla-relay-cursor.json` via the detector-snapshot fail-safe
  atomic pattern (`internal/watch/snapshot.go:48-100`: temp+rename; missing/corrupt → treat affected
  channels as first-boot tail-init, never a crash).
- **Restart recovery** falls out for free: a durable cursor resumes `after=cursor` and recovers
  everything sent while down — subject to the same disposition (a long downtime's backlog → bulk/stale
  alert, not a flood). A bonus over the strict reconnect-gap scope.
- The only residual is Invariant-2's bounded duplicate window (crash between enqueue and commit) —
  the deliberate at-least-once tradeoff.

## REST fetch helper (`internal/discord`, REST-only)

```go
func NewREST(botToken string) (*REST, error)                                   // discordgo.New("Bot "+token); never Open()
func (r *REST) MessagesAfter(channelID, afterID string, limit int) ([]Message, error)  // ChannelMessages(ch, limit, "", afterID, "") → reversed ascending
func (r *REST) Latest(channelID string) (Message, bool, error)                 // ChannelMessages(ch, 1, "", "", "")
type Message struct { ID, AuthorID, WebhookID, Content string; Timestamp time.Time; SnowID uint64 }
```

`MessagesAfter` reverses discordgo's newest-first batch to ascending (per the probe). Snowflake parse
`strconv.ParseUint(id,10,64)`, tolerant of empty/garbage (skip, never panic). `afterID==""` is never
used by the poller (first-boot uses `Latest`); `inbox` uses a plain recent fetch.

## `flotilla inbox <channel> [--limit N]`

- Resolve `<channel>` by binding `role` (case-insensitive) or raw `channel_id` (`cfg.Bindings()`);
  unmatched → error listing valid roles/ids.
- Load the bot token from secrets (same source as the relay), build a `discord.NewREST`, fetch the
  `--limit` newest, print ascending: `HH:MM:SS  [OP]/[..]/[wh]  <id>  <content>` — `[OP]` iff
  `authorID==OperatorUserID`. **Read-only** in v1. A `--relay` re-inject flag is OUT (security: it
  would be an operator-bypass of the gateway `Accept` guard — noted by the STORM security seat — and
  the auto-catch-up already re-injects; revisit separately with the Accept invariant designed in).

## Gateway handler signature change

`MessageHandler` gains the message **id** so the live path can consult the gate. (The message
**timestamp** is deliberately NOT threaded to the live path: the live path relays immediately
regardless of age, so it never needs the timestamp; only the poller uses timestamps for the
disposition's stale check, and it gets them from the REST `Message`. Carrying ts through the live
handler would be dead weight — so the shipped signature is minimal: id only.)

```go
type MessageHandler func(channelID, messageID, webhookID, authorID, content string)
```

`gateway.go` passes `m.ID`; `Relay.Handle` runs **Accept + empty-guard FIRST, then**
`liveNew` (F4 — so `seen` holds exactly the ids actually relayed, keeping Invariant-1's proof honest),
enqueues iff new. Single production caller (`watch.go:503`) + relay tests update together. (A
live-empty-but-poll-full message — gateway delivered blank for a missing Message Content intent — is
correctly NOT in `seen`, so the poller, whose REST fetch carries full content, recovers it: an
intentional bonus recovery, F4.)

## Wiring (`cmd/flotilla/watch.go`)

- Same enable gate as the relay (`len(channelIDs)>0 && botToken!="" && OperatorUserID!=""`).
- Build the gate (cursor store at `--relay-cursor-file` / `FLOTILLA_RELAY_CURSOR_FILE`, default
  `<roster-dir>/flotilla-relay-cursor.json`), inject into `NewRelay` (live path) AND the poller.
- Build `discord.NewREST(botToken)`; start the poll loop goroutine (ctx-bound); wire `OnReconnect`
  → kick. **Non-fatal:** a REST construct failure logs a warning and degrades to live-only (clock +
  live relay unaffected — mirrors `relayController`).
- Recovered notice → `post`; bulk/stale + poller-down escalation → `alert`.

## Alternatives considered & rejected

- **Single high-water mark advanced by both paths** — the leapfrog drop (Invariant 1). Rejected.
- **Persist cursor then enqueue** — the crash-window drop (Invariant 2 / F7). Rejected.
- **`after=last.id` forward walk assuming newest-first** — would have skipped the oldest gap messages
  under the actual API behavior; the probe (F1) settled the real semantics. Rejected in favor of the
  contiguous oldest-above walk.
- **Per-disconnect "gap detected" alert** (STORM-1 variant) — 303 flaps → alert fatigue; most flaps
  lose nothing. Rejected for kick-the-poller + disposition-on-what's-found.
- **Message-age as the auto/alert discriminator** — deploy alert-storm (STORM-2/F6). Rejected for
  count-primary + a loose stale ceiling.
- **Event-trigger as the PRIMARY (no periodic poll)** — misses the daemon-restart window (no daemon to
  receive the event) and couples to discordgo internals. Kept as accelerator only; poll is the floor.
- **Dispatching each sweep as a goroutine** — sweep-overrun race (F3). Rejected for one synchronous
  ticker goroutine.
- **A multi-surface `IngestionSource` abstraction** (architect seat) — YAGNI for v1; the gate is
  already surface-agnostic (operates on `string` channelID + `uint64` id). Revisit when a second
  inbound surface exists.
- **Persisting the seen-set** — unnecessary for at-least-once (cursor suffices); adds a corruption
  surface.

## Review findings incorporated (provenance)

Design + spec passed through the trio gates; every finding is resolved above:
- **systems-review (code-level):** F1 pagination silent-drop (→ probed + fail-closed walk, Inv-3);
  F7 persist-before-enqueue silent-drop (→ enqueue-then-commit, Inv-2); F2 lock scope (→ explicit);
  F3 sweep single-flight (→ one synchronous goroutine); F4 liveNew-after-Accept (→ pinned); F5
  unbounded seen (→ size cap); F6 disposition predicate (→ count-primary, oldest-message stale).
- **STORM (5-perspective):** S1 event-trigger via existing handlers (→ kick); S2 deploy alert-storm
  (→ count-primary disposition); S3 poller liveness (→ sweep-failure escalation); S4 ordering honesty
  (→ spec states at-least-once ≠ in-order); base-rate (→ measured: 303/234/46, incident confirmed);
  security seat (→ `inbox --relay` kept OUT).

## Test plan (TDD — fakes for REST + injector, no live Discord)

- **REST helper:** `MessagesAfter` maps to `ChannelMessages(ch,limit,"",afterID,"")` and reverses to
  ascending; snowflake parse tolerant of garbage; (a recorded-fixture test encodes the PROBED
  oldest-above-descending shape so the reverse is verified).
- **dedup gate:** `liveNew` records-not-advances; `classify` dedups vs seen + returns max; `commit`
  advances+prunes+persists; **leapfrog** explicit (live m5 after gap, poll recovers m3/m4, skips m5);
  **enqueue-then-commit** explicit (a crash injected between → re-fetch = dup, NOT drop; assert cursor
  not advanced before enqueue); seen size-cap bound under a never-committing channel; `-race` on
  concurrent `liveNew` + sweep asserts no lock held across a faked-slow fetch/enqueue.
- **walkAfter pagination:** ≤pageLimit above C → one page, cursor→max; >pageLimit → walks `after=max`
  upward contiguously; page-cap hit → cursor commits only to last full-page max, remainder left above
  cursor (NO drop) + bulk path; non-full page → drained, all relayed.
- **poller:** first-boot tail-init relays nothing + sets cursor; a gap batch (fresh+few) → relayed
  ascending + notice; a kick triggers an immediate sweep; a sweep slower than the interval does not
  start a second; consecutive sweep failures escalate once then re-arm.
- **disposition:** fresh+few → enqueue+notice; bulk (>bulkCap) → alert, cursor still commits; ancient
  (>staleCeiling) → alert; first-boot → neither.
- **cursor store:** atomic round-trip; corrupt/missing → first-boot tail-init (no crash).
- **gateway/relay:** id-aware `Handle` runs Accept+empty-guard then `liveNew`; enqueues iff new;
  live-empty/poll-full recovery; signature change compiles through the single caller; existing routing
  tests pass.
- **inbox:** resolution by role + by id; unmatched lists options; output format + `[OP]`/`[wh]` flags.
- **watch wiring:** poller starts under the relay enable-gate; a REST construct failure degrades to
  live-only with a warning (clock unaffected); `OnReconnect` wired to kick.
