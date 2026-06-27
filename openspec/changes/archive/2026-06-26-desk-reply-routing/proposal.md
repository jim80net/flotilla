# Proposal — desk-reply-routing: the c2-channel↔XO hotline return leg (#175)

## Why

The operator's **c2 channels are the hotline to each XO**: a message in `#gamma`
(`xo_agent=gamma-xo`) routes to that XO via the relay (`BindingForChannel→XOAgent`). But the
**return leg is incomplete** — only the PRIMARY XO (`alpha-xo`) mirrors its replies back to the
operator (via a host-local `Stop`-hook). A FEDERATED c2-channel XO (gamma-xo et al.) answers in its
pane and the reply **never reaches the operator** — the operator asks and gets silence, the exact
failure observed 2026-06-26.

Operator (2026-06-26): *"channels in c2 are supposed to be my hotline to each XO; that wiring is
incomplete and should be mechanically enforced; this is a task for desk-core to execute."*
**Mechanically-enforced + reliable** ⇒ the verbatim reply must return for EVERY turn — not a
best-effort that escalates fast/queued/short turns to "read the pane" (that IS the failure mode).

## What changes

A **flotilla-native, never-silent return leg** for operator→XO hotline messages, built on the harness
SESSION STORE (the ground truth of completed turns flotilla already reads) — NOT on pane observation
(racy) NOR a per-pane host-local Stop-hook (fragile):

1. **Anchor at confirmed delivery.** When the relay confirms delivery of an operator message to a
   c2-channel XO, snapshot that XO's active transcript's **assistant-turn count** `N` (the operator's
   message adds a `user` turn, not an assistant turn, so `N` is the pre-reply baseline).
2. **Watch the store to completion (count-based, uniform).** Poll until the assistant-turn count
   exceeds `N` and the transcript is quiescent, then extract the latest text-bearing assistant turn
   (reuse `claudestore.lastTurnText` / `grokstore.lastAssistant`) — the **verbatim reply**. This is
   reliable for fast/queued/sub-tick turns (the store records the actual completed turn) and needs no
   per-entry timestamps (grok has none) — count works for claude AND grok.
3. **Route to the origin channel, attributed.** Resolve `BindingForChannel(originChannel)→XOAgent→
   secrets.Webhook` (the validated hotline routing) and post the reply (chunked, under the XO's
   identity) back to the channel the operator messaged from.
4. **Never silent (the #175 bar on the return leg).** Every non-route outcome — no new assistant turn
   within the TTL, an unresolved origin-channel webhook, a post failure — raises a LOUD operator alert
   (mirroring the existing inbound `A dropped operator message is never silent` requirement), never a
   journald-only skip.

The watcher is per-XO single (a newer hotline message supersedes + re-anchors), reads-only (no pane
lock), and runs off the relay/detector goroutines.

## Impact

- **Affected spec:** `watch` — ADD `c2 hotline reply routing has a never-silent return leg`, tied to
  the existing `Feedback-loop immunity` (`watch/spec.md:33` — the reply, posted via webhook, is
  dropped by the `webhookID` guard, so no loop) and extending `A dropped operator message is never
  silent` (`watch/spec.md:328`) to the return leg.
- **Affected code:** a new assistant-turn-COUNT seam on `internal/claudestore` + `internal/grokstore`
  (+ an optional `surface` capability the watcher consumes); the `replyWatcher` (anchor/poll/extract/
  route/escalate/supersede) wired into the `SetMirror` confirmed-delivery hook (`cmd/flotilla/watch.go`)
  gated to c2-channel XO targets; the origin-channel destination resolution.
- **No behavior change** to the inbound relay, the detector tick, the XO Stop-hook, or the existing
  per-desk visibility mirror. Additive return leg only.

## Scope / not in

- **NOT in:** retiring/centralizing the primary XO's host-local Stop-hook (a separate consolidation);
  the per-desk visibility mirror's pre-existing tick-lossiness + spec gap (**filed #176**); a
  first-class Discord reply-threading protocol (this delivers C's reliability via the store; explicit
  message-id threading is a possible later refinement, not needed for the hotline).
- **Live-deploy (merged≠running):** the running `flotilla-watch` must be restarted on the rebuilt
  binary to take effect — operator/XO-timed (briefly pauses the heartbeat clock).
