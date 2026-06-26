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
`webhookID` (`internal/relay/relay.go:18-22`, fed by `internal/discord/gateway.go:56`) — so a reply
posted via webhook is never re-ingested as an operator message. The attribution marker is cosmetic;
the `webhookID` drop is the loop guard.

## 3. Option (a) — reply-watcher anchored to confirmed delivery (the corrected mechanism)

Instead of arming a map and waiting for the coarse detector tick, **anchor a bounded reply-watcher to
the confirmed delivery edge** and watch THIS desk's turn to completion at a fast cadence:

1. **Anchor (at confirmed relay delivery).** The `SetMirror` hook fires after a CONFIRMED relay
   delivery (`inject.go:166`). **IMPORTANT (trio re-review P1-A — corrected):** "confirmed delivery"
   does NOT guarantee the desk is Working. `confirm.Submit` returns success on THREE distinct signals
   (`confirm.go:228-248`): `readWorking` (spinner up — the turn IS running), `readCleared` (a stable
   composer-cleared streak — the Enter was accepted, but a FAST turn can complete before the watcher's
   first poll), and `readQueued` (a SOFT-SUCCESS — the message is QUEUED behind the desk's CURRENT,
   unrelated turn; the operator's turn has NOT started). So the watcher must be **disposition-aware**,
   not assume Working. The disposition is therefore **plumbed from `confirm.Submit` through the
   SendFunc→`SetMirror` hook** (widen the hook to carry the delivery disposition) so the watcher acts
   deterministically instead of racing `Assess`. When `j.Kind=="relay"` AND `j.Agent` is a DESK (not
   an XO), launch (or supersede) the desk's **reply-watcher** carrying `(agent, j.OriginChannel,
   disposition)`.
2. **Watch to completion (fast cadence, independent of the 20m tick), disposition-aware.** Reuse the
   recycle `pollWorking`→`pollIdleCleared` sequence (`recycle.go:324,275`), reads-only (no lock —
   `Assess` is safe concurrent with delivery, `surface.go:58-60`):
   - **Working:** `pollIdleCleared` (bounded by TTL) → on completion, route (§3.3).
   - **Cleared:** `pollWorking` for a short start-window. If Working observed → `pollIdleCleared` →
     route. If Working NEVER observed (the turn was sub-poll-fast OR trivial — indistinguishable by
     `Assess`) → **escalate** (do NOT route an ambiguous turn-final).
   - **Queued:** the operator's turn has not started (it's behind another) → **escalate** ("your
     message to `<desk>` is queued behind its current turn; read its pane / it'll answer when free").
     (v1 does not try to wait out an arbitrary queue — that ambiguity is option (b)'s to resolve.)
3. **Route (on completion).** Read the desk's turn-final via the surface `ResultReader.LatestResult`
   (the SAME seam the per-desk mirror uses), chunk with `discord.ChunkContent`, and post each chunk to
   the origin-channel webhook (§2) under `username=<deskName>`, attributed (e.g. `↩ <desk> (reply to
   your message):`).
4. **Never silent (the #175 bar, applied to the reply leg).** EVERY failure escalates via the existing
   loud `alert`/`escalate` hook (`watch.go:133`, `inject.go:109`) — NOT the visibility mirror's
   journald-only SKIP:
   - TTL elapses with no completion → `alert("<desk> hasn't answered your message within <ttl>; read its pane")`.
   - **turn-start not confirmed** (Cleared with no Working observed, or Queued — the §3.2 ambiguous
     cases) → `alert("<desk> received your message but I couldn't confirm a distinct reply turn; read its pane")`.
   - origin-channel webhook unresolved (§2 miss) → `alert("<desk> replied to you but I can't route it (no webhook for <channel>); read its pane")`.
   - surface has no `ResultReader` (e.g. aider) → `alert(...)` (can't extract a turn-final).
   - chunk post fails → `alert(...)` (redaction-safe, like the per-desk mirror's MIRROR-FAIL).
   This satisfies `watch/spec.md`'s "A dropped operator message is never silent" requirement
   (`spec.md:328`) on the return path — every non-route outcome is LOUD.

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
**Supersede guard (trio re-review P3-A):** the route step re-checks `ctx.Err()` immediately before each
chunk post (mirroring the recycle generation re-check, `recycle.go:225-227`), so a watcher that was
superseded mid-route does NOT emit a stale reply to the old origin channel.

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

- **In:** the disposition-plumbing (Submit signal → SetMirror hook); the `replyWatcher` (anchor-at-
  delivery, disposition-aware poll-to-completion, TTL, supersede + ctx-guarded route); the destination
  resolution (§2); attribution + chunking reuse; **loud escalation on every miss** (§3.4); unit tests:
  Working→completion-routes, Cleared+Working→routes, Cleared+no-Working→escalate, Queued→escalate,
  TTL→escalate, webhook-miss→escalate, ResultReader-less→escalate, **chunk-post-fail→escalate**,
  supersede-re-anchors + ctx-guard-blocks-stale-post, no-self-turn-steal; a **`watch`** spec delta (a
  new reply-routing requirement, ADDED, tied to `Feedback-loop immunity` (`watch/spec.md:33`) and
  extending `A dropped operator message is never silent` (`watch/spec.md:328`) to the reply leg).
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

## 8. THE CRUX FORK (for hydra-ops / operator) — completion-detection mechanism

Two design-trio rounds caught TWO load-bearing flaws (P1-1 tick-too-coarse; P1-A confirmed-delivery≠
Working). Both trace to ONE hard problem: **reliably correlating a desk's turn-completion to the
operator's message by observing the pane** is inherently racy (fast turns, queued turns, the 20m
tick). This is exactly the problem option (b) (a first-class correlation id) exists to dissolve — so
"(a) fast / (b) durable" share their hard part. The routing/destination/escalation/never-silent design
(§2–§3.4) is SETTLED and correct; the open decision is *how to detect completion*:

- **Option A — disposition-plumbed poll-watcher (this design; RECOMMENDED).** Flotilla-native, NO
  per-pane hooks (aligns with b's "remove the Stop-hooks" direction). Cost: plumbs the Submit
  disposition through the confirm/inject layer; the Cleared/Queued/sub-tick cases ESCALATE ("read the
  pane") rather than always delivering the verbatim reply — so for fast/queued turns the operator
  sometimes gets a loud "go look" instead of the text. Ships real value now; reusable.
- **Option B — generalize the XO Stop-hook to every desk.** Fires EXACTLY on turn-completion (no
  poll, no disposition race) → simpler, more reliable completion detection. Cost: a per-pane,
  host-local Stop-hook on every desk — precisely the fragile per-pane machinery option (b) wants to
  RETIRE. A step away from the durable direction.
- **Option C — jump straight to (b) (correlation-id reply routing).** Since (a)'s hard part == (b)'s,
  skip the watcher and build the first-class request/response now. Most work up front; no interim
  heuristic; the true end-state. Larger chapter.

**My recommendation:** **Option A** — it closes the operator's gap now, is flotilla-native (no
per-pane hooks, consistent with b), and the escalate-on-ambiguity keeps it strictly never-silent. The
verbatim-reply UX is best-effort for the common (Working) case and loud-fallback for the racy cases.
If the operator wants the *verbatim* reply guaranteed for every turn (incl. fast/queued), that's
Option C (b) and we should invest there instead of A.

**Decision requested:** A (recommended, proceed now) / B / C. I'll proceed on **A** (openspec +
implementation) under a veto window unless redirected — the routing half is settled regardless of the
fork, so the work isn't wasted.

## 9. REFRAME (operator, 2026-06-26) — this is the c2-channel↔XO HOTLINE return leg

The operator did NOT type into a desk pane — they typed in the **#empath c2 channel**
(`channel_id …1519598744872423424`, `xo_agent=empath-lead`), the INTENDED hotline to the empath XO.
So #175 is the **designed c2-channel↔XO wiring with an incomplete RETURN leg**, not a workaround.
Operator verbatim: *"channels in c2 are supposed to be my hotline to each XO; that wiring is
incomplete and should be mechanically enforced; this is a task for flotilla-dev to execute."*

Consequences for the design:
- **The routing half is VALIDATED.** `BindingForChannel(originChannel)→XOAgent→secrets.Webhook` IS the
  hotline routing — the target is always the channel's `xo_agent`, and the reply returns to that
  channel. (The primary XO `hydra-ops` already has this return leg via its host-local Stop-hook; the
  gap is the FEDERATED c2-channel XOs — empath-lead et al. — which lack it.)
- **"Mechanically enforced" + "reliable hotline" ⇒ leans C, not A.** A escalates fast/queued/sub-tick
  turns to a manual "read the pane" — which is the EXACT failure the operator just hit. A reliable
  hotline must deliver the verbatim reply for EVERY turn. hydra-ops is surfacing A-vs-C to the operator.

### 9a. The mechanism that makes C clean + flotilla-native — a TRANSCRIPT-WATCHER (verified feasible)

A's unreliability is an artifact of observing the **pane** (the Working/Cleared/Queued race). But the
harness **session store is the ground truth of completed turns**, and flotilla ALREADY reads it
(`internal/claudestore`, `internal/grokstore` — the `ResultReader` seam). Verified structure
(`claudestore.go:163-260`, live-probed): the claude transcript is JSONL with typed entries
(`Type: user|assistant|system|queue-operation|…`, `Message.Role`, per-line `timestamp`) — so:

- The operator's hotline message, delivered into the XO's session, is recorded as a **`user` turn**;
  the **next `assistant` turn after it is the verbatim reply** → near-DETERMINISTIC correlation
  (content-match the user turn, take the following assistant turn), no protocol change, no pane race.
- It catches **fast/sub-tick turns** (they're in the transcript) AND **queued turns** (a
  `queue-operation` entry is literally recorded) — exactly the cases A escalates.
- It is **flotilla-native** (reads the store flotilla already reads — NO per-pane host-local Stop-hook,
  unlike B) and reliable (NO 20m-tick dependency, unlike the detector path).

**So "C done via a transcript-watcher" delivers the operator's ask — mechanically-enforced, reliable,
verbatim-every-turn hotline — WITHOUT A's escalation compromise OR B's host-hook fragility.** It reuses
the proven extraction (`claudestore.lastTurnText` / `grokstore.LatestResult`), adding a "turns since
<delivery marker>" read + the same destination/never-silent routing (§2–§3.4, settled). This is the
recommended concrete implementation of **C** if the operator picks it.

(Open: confirm the user-turn content-match is robust vs a timestamp-after-delivery snapshot; verify
the grok `chat_history.jsonl` exposes the same per-turn structure — quick to ground when C is blessed.)

**Status:** routing half settled + kept warm; completion-detection implementation HELD for the
operator's A/B/C steer (hydra-ops surfacing A-vs-C now). If C: implement via the §9a transcript-watcher.
