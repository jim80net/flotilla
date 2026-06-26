# Design — Desk→operator reply routing (#175)

**Status:** DRAFT for the design-trio gate (systems-review + open-code-review) and hydra-ops review.
**Issue:** #175 — a desk's reply to an operator-direct message has no return path (only the XO pane
mirrors back to the operator). Operator-filed.
**Direction (operator via hydra-ops):** ship **(a) generalize the mirror** to close the gap fast;
**(b) flotilla-native reply-routing** as the durable end-state. The operator wants proper *mechanical*
routing (not an XO-relays-by-hand workaround).

## 1. The gap (code-grounded)

When the operator addresses a NON-XO desk directly (operator→desk via the relay, e.g.
`@empath-lead what do you need from me`), the desk answers in its tmux pane but the reply **never
reaches the operator**. Today the ONLY desk whose turn-final reaches the operator's channel is the XO,
via a host-local `Stop`-hook script that self-gates to `xo_agent`. Every non-XO desk is **write-only
from the operator's side**.

The context needed to route the reply **exists at delivery but is lost before turn-finish**:

| Stage | Where | Has reply-to context? |
|---|---|---|
| Relay routes operator msg | `internal/watch/relay.go` → `Job{Agent, OriginChannel, Kind:"relay"}` (`inject.go:49-59`) | ✅ `OriginChannel` captured |
| Confirmed delivery hook | `Injector.SetMirror(func(j Job))` (`inject.go:101-103,166-168`; wired `watch.go:181-195`) | ✅ receives the WHOLE Job (OriginChannel) — explicitly the "#108 seam: v1 only CARRIES it" |
| Turn-finish detection | detector `Working→Idle` → `pendingMirrors = append(…, name)` (`detector.go:562`) | ❌ agent NAME only |
| Per-desk mirror | `MirrorOnFinish(agent)` → `deskMirrorOnFinish` → posts to `secrets.Webhook(agent)` = the desk's OWN channel (`watch.go:648-690`, `mirror.go`) | ❌ no reply-to; wrong destination |

So the per-desk mirror that already exists posts a desk's turn-final to the **desk's own channel** on
every finish (per-desk *visibility*) — NOT back to the operator's locus when the operator addressed
the desk. (And a desk with no webhook — `logMirrorCoverage`'s `without` set — mirrors nowhere, which
is exactly empath-lead's case.)

**The fix is to carry `OriginChannel` across the delivery→finish gap and route the reply to it.**

## 2. Destination resolution (verified — reuses existing config)

To post a desk's reply *into the operator's origin channel, attributed*, resolve:

```
originChannel ──BindingForChannel(roster.go:336)──▶ Channel{XOAgent}
              ──secrets.Webhook(XOAgent)(secrets.go:62)──▶ a webhook BOUND to the origin channel
discord.Post(webhookURL, username=<deskName>, content)  // username override → "empath-lead" identity
```

- `secrets.Webhook(xo)` is exactly how the daemon's `post`/`alert` reach the operator channel today
  (`watch.go:119-133`). The per-channel webhook = the channel's XO webhook. In single-channel this is
  the one operator channel; in federation each origin channel resolves to its own XO's webhook → the
  reply lands in the channel the operator actually messaged from.
- **No feedback loop:** the gateway's feedback-filter drops webhook posts (`watch.go:179`), so a
  reply posted via webhook is never re-ingested as an operator message. Verified-safe.
- **Attribution:** post under `username = deskName` (the webhook username override) with a content
  marker so the operator reads it as the desk's reply to them (e.g. `↩ <deskName> (reply): …`).

## 3. Option (a) — Generalize the mirror (the fast fix to ship now)

A small in-daemon **reply router** carries the reply-to across the gap, armed at delivery, taken at
finish:

1. **`replyRouter`** — a concurrency-safe `map[agent]armed{channel string, at time.Time}` owned by the
   watch daemon (the detector tick goroutine reads it; the injector mirror hook + relay goroutine
   write it — so it needs its own mutex).
2. **Arm at delivery** — in the existing `SetMirror` hook (fires on CONFIRMED relay delivery with the
   full Job): when `j.Kind == "relay"` AND `j.Agent` is a DESK (not an XO — XOs keep the Stop-hook
   path), `replyRouter.Arm(j.Agent, j.OriginChannel)`. This is purely additive to the hook (which
   already posts the inbound echo + CoS ledger).
3. **Take at finish** — extend the per-desk mirror so `MirrorOnFinish` can consume an armed reply-to:
   on a desk's turn-finish, `replyRouter.Take(agent)` (atomic read-and-clear). If armed, ALSO post the
   desk's turn-final to the origin channel (resolved per §2), attributed + chunked (reuse
   `discord.ChunkContent` + the `deskMirror` chunking). Clearing on take means the reply fires **once
   per operator message** — the first finish after the message — and a later self-initiated turn does
   NOT mis-route.
4. **Staleness TTL** — an armed entry expires after a bounded window (e.g. the desk never finishes, or
   a long-deferred message). `Take` past the TTL returns "not armed" (logged), so a stale arm cannot
   mis-attribute a much-later turn. (TTL value is a design knob — propose ~15 min, generous vs a
   desk's longest single turn; tune in review.)

**Wiring shape (additive, no signature break to the detector):** `MirrorOnFinish func(agent string)`
stays; `deskMirrorOnFinish` closes over the `replyRouter` + `cfg` + `secrets` and does both the
existing own-channel visibility mirror AND (if armed) the operator-reply route. This keeps the detector
↔ cmd boundary unchanged and the new logic unit-testable with fakes (mirror.go's established pattern).

### Additive vs exclusive (a real sub-decision for the trio)

For an operator-triggered turn, do we ALSO keep the existing own-channel visibility mirror, or route
ONLY to the operator?
- **Additive (recommended v1):** keep the own-channel mirror unchanged; ADD the operator-reply route.
  Safest (zero change to existing behavior); cost = a desk that has its own channel double-posts an
  operator-triggered turn (own channel + operator channel). Low noise, clearly correct.
- **Exclusive:** an operator-triggered turn routes ONLY to the origin channel; self-initiated turns go
  to the own channel. Cleaner but changes existing behavior for operator-triggered turns.

Recommend **additive** for the fast fix (option a) — purely additive is the lowest-risk close of the
gap; revisit exclusivity if double-posting proves noisy.

### Decision log (consume-once) — the correlation semantics

The arm/take correlation assumes: the operator message is delivered while the desk is idle (the relay
busy-defers until idle — `inject.go` deferral), so the NEXT `Working→Idle` finish is the reply. This
holds because (i) delivery is confirmed only after the Idle→Working edge, and (ii) the detector fires
`MirrorOnFinish` on the subsequent Working→Idle. Edge cases handled: multiple queued operator messages
(last-arm-wins — the reply carries the latest origin channel; acceptable, same operator); a desk that
crashes mid-turn (no finish → the arm TTL-expires, no mis-route).

## 4. Option (b) — Flotilla-native reply-routing (the durable end-state)

First-class request/response, removing the dependency on per-pane Stop hooks + the arm/take heuristic:
- An operator→desk message carries a **correlation id** (the Discord message id is a natural one) and
  a reply-to (origin channel + the operator's message ref).
- The desk's response is correlated to that request id and routed back as a reply (Discord reply-to
  the operator's message), so the threading is explicit, not "next finish."
- This subsumes the XO Stop-hook too (the XO becomes just another reply-routed agent), retiring the
  host-local script the issue calls out as fragile.

(b) is the right end-state but is a larger change (message-id correlation through the relay + injector,
a response-emit path, retiring the Stop-hook). **Recommendation: ship (a) now to close the operator's
gap; pursue (b) as a follow-on chapter once (a) is validated live.** File (b) as a tracked issue.

## 5. Scope / what's NOT in (a)

- **In (a):** `replyRouter` (arm/take + TTL); arm in the `SetMirror` hook (relay+desk); take + route
  in the per-desk mirror; destination resolution (§2); attribution; chunking reuse; unit tests; a
  spec delta (a new `send`/`watch` capability requirement for desk→operator reply routing).
- **NOT in (a):** option (b) (filed separately); retiring the XO Stop-hook (b's job); changing the XO
  reply path; the per-desk own-channel visibility mirror's existing behavior (untouched — additive).

## 6. Open items for hydra-ops / the trio

1. **Additive vs exclusive** mirroring for operator-triggered turns (§3) — recommend additive.
2. **TTL value** for an armed reply-to (§3) — recommend ~15 min; confirm against the longest expected
   desk turn.
3. **Attribution format** — `↩ <desk> (reply): …` under the desk's webhook username; confirm wording.
4. **(b) as a follow-on** — confirm shipping (a) now + filing (b), vs holding for (b).
5. **Live-deploy note (merged≠running):** the running `flotilla-watch` daemon must be restarted on the
   rebuilt binary for this to take effect — operator/XO-timed (it briefly pauses the heartbeat clock).
