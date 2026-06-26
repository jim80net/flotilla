# Design — Desk→operator reply routing (#175)

**Status:** Design-trio gate run (systems-review + open-code-review, both code-grounded). The trio
found a LOAD-BEARING flaw in the first draft (a detector-tick-driven reply silently drops short turns)
— the mechanism is reworked below (§3); all findings folded (§7). Ready for re-review + the openspec
change.
**Issue:** #175 — a desk's reply to an operator-direct message has no return path (only the XO pane
mirrors back). Operator-filed.
**Direction (operator via hydra-ops):** ship **(a) generalize the mirror** to close the gap fast;
**(b) flotilla-native reply-routing** as the durable end-state. The operator wants proper *mechanical*
routing.

## 1. The gap (code-grounded)

When the operator addresses a NON-XO desk directly (operator→desk via the relay), the desk answers in
its tmux pane but the reply never reaches the operator. Today the ONLY desk whose turn-final reaches
the operator is the XO, via a host-local `Stop`-hook script self-gated to `xo_agent`. Every non-XO desk
is **write-only from the operator's side**.

The reply-to context **exists at delivery but is lost before any reliable finish signal**:

| Stage | Where | Reply-to context? |
|---|---|---|
| Relay routes operator msg | `relay.go:96` → `Job{Agent, OriginChannel, Kind:"relay"}` (`inject.go:49-64`) | ✅ `OriginChannel` |
| Confirmed delivery | `Injector.deliver`→`in.mirror(j)` (`inject.go:166-167`); `SetMirror` wired `watch.go:181-195` | ✅ whole Job + the desk just confirmed Idle→Working |
| Turn-finish detection | detector `Working→Idle` → `MirrorOnFinish(agent)` (`detector.go:557-562,83`) | ❌ name only — **and too coarse (see §1a)** |
| Per-desk mirror | `deskMirrorOnFinish`→`secrets.Webhook(agent)` = desk's OWN channel (`watch.go:648-690`,`mirror.go`) | ❌ wrong destination |

### 1a. Why the detector tick CANNOT drive the reply (the trio's P1-1)

The detector ticks every `cfg.Interval == HeartbeatDur()` — **20m** on the live fleet
(`watch.go:84,328`; `detector.go:365`). The per-desk mirror fires only on a sampled
`prev==Working && cur==Idle` edge (`detector.go:561`). A desk answering a quick operator question
(seconds–minutes) starts and finishes ENTIRELY WITHIN one 20-minute tick window: the detector samples
`Idle` before and `Idle` after, never observes the `Working→Idle` edge, and **never mirrors the turn**
— silently. Building the reply on the detector tick re-introduces the exact silent-drop #175 exists to
kill. (The existing per-desk *visibility* mirror has this same lossiness; acceptable for best-effort
visibility, NOT for a reliable operator reply — flagged as a separate pre-existing gap, §7.)

So the reply MUST be triggered by **turn-completion detection independent of the 20-min tick**.

## 2. Destination resolution (verified — reuses existing config)

Post the reply *into the operator's origin channel, attributed*:

```
originChannel ──BindingForChannel(roster.go:336)──▶ Channel{XOAgent}
              ──secrets.Webhook(XOAgent)(secrets.go:62)──▶ webhook PROVISIONED for the origin channel
discord.Post(url, username=<deskName>, content)   // username override → the desk's identity (discord.go:51)
```

This is the SAME convention `alertHook = secrets.Webhook(xo)` (`watch.go:119-121`) already uses to
reach the operator channel. **Caveat (trio P1-2/C-3):** which Discord channel a webhook targets is
fixed at webhook-creation time and is a DEPLOYMENT CONVENTION, not a code invariant. So the resolution
can MISS (the origin channel's XO has no `FLOTILLA_WEBHOOK_<XO>`). A miss must NOT silently drop the
reply (that is bug #175 on the reply leg) — see §3's escalate-on-miss.

**No feedback loop (trio-verified):** the relay drops any inbound message carrying a non-empty
`webhookID` (`internal/relay/relay.go:18-22`, fed by `gateway.go:56`) — so a reply posted via webhook
is never re-ingested as an operator message. The attribution marker is cosmetic; the `webhookID` drop
is the loop guard.

## 3. Option (a) — reply-watcher anchored to confirmed delivery (the corrected mechanism)

Instead of arming a map and waiting for the coarse detector tick, **anchor a bounded reply-watcher to
the confirmed delivery edge** and watch THIS desk's turn to completion at a fast cadence:

1. **Anchor (at confirmed relay delivery).** The `SetMirror` hook fires after a CONFIRMED relay
   delivery (`inject.go:166`) — by which point `confirm.Submit` has already confirmed the desk's
   `Idle→Working` edge (the desk is now working on the operator's message). When `j.Kind=="relay"` AND
   `j.Agent` is a DESK (not an XO — XOs keep their reply path), launch (or supersede the desk's
   existing) **reply-watcher** carrying `(agent, j.OriginChannel)`.
2. **Watch to completion (fast cadence, independent of the 20m tick).** The watcher polls
   `surface.Assess(pane)` at a fast interval (reuse the recycle `pollWorking`/`pollIdleCleared` pattern
   — `cmd/flotilla/recycle.go`) until it observes the `Working→Idle` completion of this turn, bounded
   by a TTL. Reads only (Assess/capture/ResultReader) — no pane writes, so no transaction lock needed
   (Assess is safe concurrent with delivery, per the Driver contract).
3. **Route (on completion).** Read the desk's turn-final via the surface `ResultReader.LatestResult`
   (the SAME seam the per-desk mirror uses), chunk with `discord.ChunkContent`, and post each chunk to
   the origin-channel webhook (§2) under `username=<deskName>`, attributed (e.g. `↩ <desk> (reply to
   your message):`).
4. **Never silent (the #175 bar, applied to the reply leg).** EVERY failure escalates via the existing
   loud `alert`/`escalate` hook (`watch.go:133`, `inject.go:109`) — NOT the visibility mirror's
   journald-only SKIP:
   - TTL elapses with no completion → `alert("<desk> hasn't answered your message within <ttl>; read its pane")`.
   - origin-channel webhook unresolved (§2 miss) → `alert("<desk> replied to you but I can't route it (no webhook for <channel>); read its pane")`.
   - surface has no `ResultReader` (e.g. aider) → `alert(...)` (can't extract a turn-final).
   - chunk post fails → `alert(...)` (redaction-safe, like the per-desk mirror's MIRROR-FAIL).
   This satisfies `watch/spec.md`'s "a dropped operator message is never silent" on the return path.

**Why this is strictly better than arm/take-on-tick:**
- **No sub-tick drop (P1-1):** the watcher's fast cadence catches any turn length; it does not depend
  on a tick landing inside the turn.
- **No tick race / no mis-route of an in-flight self-turn (P1-3):** the watcher is anchored to the
  confirmed delivery edge of the operator message and watches forward for THAT turn's completion — it
  is not a "next finish anywhere" heuristic, so a concurrently-finishing self-initiated turn cannot
  steal the reply.
- **No federation last-arm-wins ambiguity (P2-3):** each watcher closes over its own specific
  `OriginChannel`; a second operator message from a different channel SUPERSEDES (cancels) the prior
  watcher and re-anchors to the new channel — the reply always goes to the channel of the message the
  desk is currently answering.

**Concurrency model:** a per-desk single watcher (a `map[agent]*replyWatcher` guarded by a mutex);
launching a new one cancels the prior (context cancellation). The watcher runs off the injector/detector
goroutines (its own goroutine), reads-only, self-absorbing errors into the escalate hook.

### Restart caveat (trio P2-1/P2-2 — documented, not silently lost)

The watcher is in-memory; a daemon restart between delivery and completion loses the in-flight reply.
Because THIS deploy itself requires a watch-daemon restart (merged≠running), an operator→desk exchange
straddling the restart loses its reply. v1 mitigations: keep the TTL short; log a startup line naming
any arms cleared; the operator can re-ask. Durable correlation is option (b)'s job. (Not worth
persisting a transient in-flight watcher for v1; documented as a known bound.)

### Relationship to the existing per-desk visibility mirror (additive)

The existing tick-driven per-desk visibility mirror (to the desk's OWN channel) is UNTOUCHED. The
reply-watcher is a SEPARATE, additive path (operator-triggered → origin channel). A turn could in
principle be posted by both (visibility to own channel + reply to origin channel) if a 20m tick happens
to span it — low-noise, clearly correct; documented.

## 4. Option (b) — flotilla-native reply-routing (the durable end-state)

First-class request/response: an operator→desk message carries a correlation id (the Discord message
id); the desk's response is correlated to that request and routed back as an explicit Discord reply,
removing the per-pane Stop hooks AND the watcher heuristic, and subsuming the XO Stop-hook (the XO
becomes just another reply-routed agent). Larger change (correlation through relay+injector, a
response-emit path, retiring the host-local Stop-hook). **Ship (a) now; file (b) as a follow-on
chapter** once (a) is validated live.

## 5. Scope / what's in (a)

- **In:** the `replyWatcher` (anchor-at-delivery, fast-poll-to-completion, TTL, supersede); the
  destination resolution (§2); attribution + chunking reuse; **loud escalation on every miss** (§3.4);
  unit tests (consume-once/completion, TTL-expiry→escalate, webhook-miss→escalate, ResultReader-less→
  escalate, supersede-re-anchors, no-self-turn-steal); a **`watch`** spec delta (a new reply-routing
  requirement, tied to the existing feedback-loop-immunity requirement).
- **NOT in:** option (b) (filed separately); retiring/centralizing the XO Stop-hook (b's job); changing
  the XO reply path; the existing per-desk visibility mirror's behavior (untouched). The pre-existing
  *visibility-mirror lossiness* (§1a) and the *per-desk-visibility-mirror spec gap* (§7 C-1) are
  flagged as separate follow-ups, not fixed here.

## 6. Open items for hydra-ops / the trio

1. **TTL value** for the reply-watcher (§3) — propose a few minutes (covers a normal desk answer; short
   enough that a stuck desk escalates promptly). Confirm against the longest expected operator-Q answer.
2. **Attribution format** (`↩ <desk> (reply to your message): …`) — confirm wording.
3. **(b) as a follow-on** — confirm shipping (a) now + filing (b).
4. **Live-deploy (merged≠running):** the running `flotilla-watch` must be restarted on the rebuilt
   binary — operator/XO-timed (briefly pauses the heartbeat clock).

## 7. Design-trio findings folded (systems-review + open-code-review, both code-grounded)

- **P1-1 (systems, LOAD-BEARING) — tick-driven reply drops sub-tick turns.** Reworked §3: the reply is
  now watcher-driven (anchored to confirmed delivery, fast-poll to completion), independent of the 20m
  detector tick. The empirical crux (heartbeat=20m → detector tick=20m) was VERIFIED, not assumed.
- **P1-2 (systems) / C-3 (OCR) — resolution-miss silent drop.** §3.4: every miss (webhook-unresolved,
  TTL, no ResultReader, post-fail) ESCALATES via the loud alert hook — never the journald-only SKIP;
  satisfies the "operator message never silent" bar on the reply leg.
- **P1-3 (systems) — arm/take tick race.** Dissolved: the watcher is anchored to the operator
  message's delivery edge, not a "next finish" heuristic, so an in-flight self-turn can't steal it.
- **P2-1/P2-2 (systems) — restart loses in-memory state.** Documented as a known v1 bound (§3 restart
  caveat); durable correlation is (b).
- **P2-3 (systems) — federation last-arm-wins mis-route.** Dissolved: each watcher closes over its
  specific origin channel; a new message supersedes + re-anchors.
- **P2-4 (systems) — additive double-post.** Documented (§3) — low-noise, clearly correct.
- **C-1 (OCR, HIGH) — the per-desk mirror has NO existing spec requirement.** The `watch` delta will
  ADD the reply-routing requirement explicitly and NOTE the pre-existing visibility-mirror spec gap
  (file it; don't silently build on unspec'd ground).
- **C-2 (OCR) — spec home is `watch`, not `send`.** Resolved: the delta lands in `watch`, tied to the
  existing feedback-loop-immunity requirement.
- **Mis-citation (OCR) — feedback filter is `internal/relay/relay.go:18-22`** (+ `gateway.go:56`), not
  `watch.go:179` (a comment). Corrected in §2.
- **C-4 (OCR) — testability confirmed** (injectable-fakes pattern, à la `mirror_test.go` /
  `detector_mirror_test.go`); the correctness-bearing cases are now enumerated in §5.
