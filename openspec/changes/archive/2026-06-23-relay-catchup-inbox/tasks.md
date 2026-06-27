# Tasks — relay-catchup-inbox

TDD throughout: write the failing test, then the code. Each group is one clean-context unit.
`go test ./... -race` green at every group boundary.

## 0. Verify (DONE — provenance)

- [x] 0.1 Probe Discord `after`-pagination ordering live (F1's mandated verify). Result (2026-06-23,
      channel `1500000000000000001`): `after=C` returns the **OLDEST** messages above C (the
      contiguous block nearest the cursor), ordered **newest-first within the batch**. ⇒ the helper
      reverses to ascending; the upward walk + page cap are fail-closed.
- [x] 0.2 Measure the base rate: 303 disconnects / 234 resumes / 46 connects over Jun 3–23; the 20:54
      incident sits in the 20:53:19 disconnect window (no resume until 21:31). Justifies the poller.

## 1. REST fetch helper (`internal/discord`)

- [x] 1.1 Test: `NewREST(botToken)` builds a session and does NOT open a websocket; `MessagesAfter`
      maps onto `ChannelMessages(ch, limit, "", afterID, "")` and **reverses newest-first → ascending**
      (a recorded-fixture encoding the PROBED oldest-above-descending shape verifies the reverse);
      `Latest` → `ChannelMessages(ch, 1, "", "", "")`. Use an injectable fetch seam (no live Discord).
- [x] 1.2 Impl: `REST` over a REST-only `*discordgo.Session` (no `Open()`); the projection
      `Message{ID,AuthorID,WebhookID,Content,Timestamp,SnowID}`; ascending reverse.
- [x] 1.3 Test+impl: snowflake parse (`ParseUint(id,10,64)`), tolerant of empty/garbage (skip).

## 2. Dedup gate (`internal/watch`)

- [x] 2.1 Test: `liveNew` true for fresh / false for repeat; records in `seen`; does NOT advance
      `cursor`.
- [x] 2.2 Test: `classify(ch, ascendingBatch)` returns ids ∉ seen + `newCursor=max`; marks them seen;
      DOES NOT persist and DOES NOT advance the durable cursor (that is `commit`'s job, after enqueue).
- [x] 2.3 Test (leapfrog — explicit): cursor=2; `liveNew(5)`=true (cursor stays 2);
      `classify([3,4,5])`→([3,4], 5); commit(5). No orphaned gap message.
- [x] 2.4 Test (enqueue-then-commit — F7, explicit): a crash injected AFTER classify but BEFORE commit
      → on the next sweep the messages are re-fetched (dup), NOT dropped; assert the cursor is not
      advanced/persisted before enqueue.
- [x] 2.5 Test (seen size-cap — F5): a channel whose poll never commits while live ids keep arriving →
      `seen[ch]` is bounded (evict-oldest), never unbounded.
- [x] 2.6 Test (`-race` — F2): concurrent `liveNew` + a sweep using `classify`/`commit` with a
      faked-slow fetch/enqueue → no lock held across fetch or enqueue; no double-relay, no lost relay.
- [x] 2.7 Impl: `dedup` (mutex, cursor map, size-capped `seenSet` map, store), `liveNew`, `classify`,
      `commit` (advance + prune seen≤cursor under lock; persist OFF-lock), `initCursor`, `cursorOf`.

## 3. Cursor store (`internal/watch`)

- [x] 3.1 Test: atomic write + read round-trip (channelID→snowflake map); temp-then-rename.
- [x] 3.2 Test: missing → empty map (first-boot all); corrupt JSON → empty map + no error (fail-safe,
      mirrors `snapshot.go`).
- [x] 3.3 Impl: `cursorStore` (load, save-atomic); default `<roster-dir>/flotilla-relay-cursor.json`.

## 4. Pagination walk (`internal/watch` or `internal/discord`)

- [x] 4.1 Test: ≤pageLimit above C → one page, drained, all returned ascending.
- [x] 4.2 Test: >pageLimit above C → walks `after=page.max` upward contiguously until a non-full page.
- [x] 4.3 Test (fail-closed cap — F1): page cap hit → returns only the contiguous fully-processed run;
      the remainder stays above the cursor (caller does NOT advance past it) + a `capped` flag for the
      bulk path.
- [x] 4.4 Impl: `walkAfter(rest, ch, cursor, pageLimit, pageCap) ([]Message, capped bool)`.

## 5. Catch-up poller + disposition + liveness (`internal/watch`)

- [x] 5.1 Test: first-boot tail-init — no cursor → `Latest` sets cursor, relays NOTHING, persists.
- [x] 5.2 Test: a gap batch (≤bulkCap, fresh) → enqueued ascending + one catch-up notice;
      enqueue happens BEFORE commit (ordering asserted).
- [x] 5.3 Test (disposition — F6/STORM-2): count > bulkCap → alert, no enqueue, cursor still commits;
      oldest > staleCeiling → alert; ≤bulkCap & fresh → enqueue + notice; first-boot → neither.
- [x] 5.4 Test (single-flight — F3): a sweep slower than the interval does not start a second concurrent
      sweep (one synchronous ticker goroutine; ticker coalescing).
- [x] 5.5 Test (kick — STORM-1): an `OnReconnect` kick triggers an immediate sweep via the same loop.
- [x] 5.6 Test (liveness — STORM-3): consecutive sweep-level REST failures escalate ONCE past the
      threshold, then re-arm on a successful sweep.
- [x] 5.7 Impl: the poll loop (one goroutine; `select` on ctx / ticker / kick; synchronous
      `sweepChannel`), disposition (bulkCap=5, staleCeiling=24h, oldest-message eval), notice + alert
      hooks, sweep-failure escalation (mirror `relayController.escalateThreshold`), ctx-bound,
      non-fatal start.

## 6. Live path made id-aware (`internal/discord` + `internal/watch`)

- [x] 6.1 Test: gateway `MessageHandler` receives `messageID`; `Relay.Handle` runs
      Accept + empty-guard FIRST, then `liveNew` (F4 — seen holds only relayed ids); enqueues iff new;
      routing/Accept/empty-guard tests unchanged. (Timestamp is NOT threaded to the live path — it is
      unused there; only the poller uses timestamps, which it gets from REST. Minimal signature.)
- [x] 6.2 Test (F4 bonus): a live-EMPTY message (missing-intent shape) is NOT in seen → the poller
      (full-content REST) later recovers it. (Covered by the empty-guard-before-liveNew ordering test.)
- [x] 6.3 Impl: widen `MessageHandler` to `(channelID, messageID, webhookID, authorID, content)`;
      `gateway.go` passes `m.ID` + an `OnReconnect` hook off the existing Connect handler (covers resume
      AND re-identify); `Relay.Handle` parses id + consults the gate after Accept/empty-guard.
- [x] 6.4 Update the single production caller (`watch.go:503`) + relay tests to the new signature.

## 7. `flotilla inbox` command (`cmd/flotilla`)

- [x] 7.1 Test: channel resolution by `role` (case-insensitive) and by raw `channel_id`; unmatched →
      error listing valid roles/ids.
- [x] 7.2 Test: output format (ascending; `HH:MM:SS [OP]/[..]/[wh] <id> <content>`), `[OP]` iff
      `authorID==OperatorUserID`, using a fake REST batch. Read-only (no `--relay`).
- [x] 7.3 Impl: `cmdInbox` (load roster+secrets, build `discord.NewREST`, fetch newest `--limit`,
      print); register `case "inbox"` in `main.go`; add to `help`.

## 8. Wiring (`cmd/flotilla/watch.go`)

- [x] 8.1 Test: under the relay enable-gate (channels+token+operator id), the poller starts and
      `OnReconnect` is wired to kick; a REST construct failure degrades to live-only with a warning
      (clock unaffected).
- [x] 8.2 Impl: `--relay-cursor-file` / `FLOTILLA_RELAY_CURSOR_FILE` flag (default
      `<roster-dir>/flotilla-relay-cursor.json`); build `dedup` + cursor store; inject into `NewRelay`
      AND the poller; build `discord.NewREST`; start the poller goroutine (ctx-bound); wire
      `OnReconnect`→kick; recovered notice → `post`, bulk/stale + poller-down → `alert`.

## 9. Docs + spec close-out

- [x] 9.1 Correct any relay-doc / README line that says "the operator can resend" → "recovered by
      catch-up"; document `flotilla inbox` + the cursor file + the new flags.
- [x] 9.2 Fold the #155 doc-rot fix if touching `confirm.go` comments (per handoff) — only if in the
      diff; do not cut a separate PR for it.
- [x] 9.3 `go test ./... -race` green; `go vet`; gofmt; `openspec validate relay-catchup-inbox --strict`.
