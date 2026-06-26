# Tasks — desk-reply-routing (TDD)

Load-bearing properties (assert across paths):
- **(R1) reliable, verbatim, every turn.** The reply is the XO's actual completed turn-final read from
  the session store (content-correlated to the operator's user turn), so fast/queued/sub-tick turns are
  NOT dropped (the #175 bug) and the operator gets the verbatim text, not a "read the pane" escalation.
- **(R2) never silent.** Every non-route outcome (no new assistant turn within TTL, origin-channel
  webhook unresolved, post failure) raises a LOUD operator alert — never a journald-only skip.
- **(R3) right destination.** The reply returns to the ORIGIN channel (the c2 channel the operator
  messaged from), resolved `BindingForChannel→XOAgent→Webhook`, under the XO's identity.
- **(R4) no loop.** The webhook-posted reply is dropped by the relay's `webhookID` guard — no re-ingest.
- **(R5) additive / no regression.** No change to the inbound relay, the detector tick, the XO
  Stop-hook, or the per-desk visibility mirror.

## 1. Content-correlation seam — `ReplyAfter` (the SHIPPED mechanism)

> NOTE: an earlier draft used an assistant-turn-COUNT marker; the impl-trio found it mis-routes a
> queued/interleaved turn (a count delta doesn't correlate WHICH turn answers). The shipped mechanism
> CORRELATES the reply to the operator message's recorded USER turn — see §10 of design.md.

- [x] 1.1 TEST FIRST (`internal/claudestore`): `replyAfterUserMsg(jsonlPath, operatorMsg) (text, cwd
  string, found bool)` over a fixture transcript — anchor on the LATEST user turn whose recorded text
  EXACT-matches operatorMsg (whitespace-normalized, NOT a substring), return the text-bearing assistant
  turn following it; a substantive non-anchor user turn (a self-cont/later prompt) CLOSES the window;
  tool_result/sidechain entries do not. Cases: reply-after-user, no-reply-yet, re-anchor-on-reask,
  trailing-self-cont-not-mis-routed.
- [x] 1.2 Implement claudestore `ReplyAfter`/`replyAfterForCwd`/`replyAfterUserMsg` (reuse
  `transcriptEntry` decode + the collision-guarded active-session resolution + `normMsg`).
- [x] 1.3 TEST FIRST (`internal/grokstore`): `replyAfterUserMsg(path, operatorMsg) (text string, found
  bool, err error)` — same content-correlation over `chat_history.jsonl` (user/assistant entries).
- [x] 1.4 Implement grokstore `ReplyAfter`/`replyAfterUserMsg` (reuse `resolveHistoryPath` + `normMsg`).
- [x] 1.5 Surface seam: an OPTIONAL `ReplyReader` capability — `ReplyAfter(pane, operatorMsg) (text
  string, found bool, err error)` — returning the XO's verbatim reply that follows operatorMsg's user
  turn. Claude + grok implement it; aider/opencode do not (→ escalate). Compile-time asserts.

## 2. The replyWatcher (anchor → poll → extract → route → escalate)

- [x] 2.1 TEST FIRST: a pure `replyRouter`/`replyWatcher` with injected collaborators (reply-reader,
  webhook-resolver, post, escalate, sleep) — NO tmux/store/Discord. Cases:
  - reply found → posts chunked to the resolved webhook (R1/R3).
  - soft TTL with no reply → escalate ONCE ("still working") but keep watching; a late reply still routes.
  - hard TTL with no reply → ALERT, no post (R2).
  - origin-channel webhook unresolved → ALERT, no post (R2).
  - surface lacks `ReplyReader` (aider) → ALERT (R2).
  - post returns error → partial-delivery ALERT (R2); the ESCALATION post error falls back to the
    primary alert (never silent).
  - a newer message supersedes mid-watch → the prior watcher cancels; the route step re-checks ctx
    before each chunk post so no stale reply to the old channel; `Stop()` cancels in-flight on shutdown.
  - multi-chunk reply → `discord.ChunkContent` + ordered `(i/n)` posts.
- [x] 2.2 Implement the watcher: poll `ReplyAfter(pane, operatorMsg)` at a fast cadence (soft+hard TTL)
  until the reply is found; route; escalate per R2 on every non-route outcome.
- [x] 2.3 Per-XO single watcher: `map[xo]*watcher` + mutex; launch cancels the prior; route re-checks
  `ctx.Err()` before each post (supersede guard).

## 3. Destination resolution (origin-channel webhook)

- [x] 3.1 TEST FIRST: `replyDest(cfg, secrets, originChannel) (url string, ok bool)` =
  `BindingForChannel(originChannel)→XOAgent→secrets.Webhook`; miss (no binding / no webhook) → ok=false
  (→ the watcher escalates). Cover the single-channel and a federated multi-channel roster.
- [x] 3.2 Implement; attribution: post under `username=<xoName>` with a reply marker.

## 4. Wire into the watch daemon (gated to c2-channel XO targets)

- [x] 4.1 In the `SetMirror` confirmed-delivery hook (`cmd/flotilla/watch.go`): when `j.Kind=="relay"`
  AND `j.Agent` is the `xo_agent` of `j.OriginChannel`'s binding (a c2 hotline message to that XO),
  launch/supersede the replyWatcher `(xo, j.OriginChannel)`. Inert when secrets/bindings absent.
- [x] 4.2 Confirm NO behavior change to the existing inbound echo / CoS ledger / per-desk mirror /
  detector tick / XO Stop-hook (R5) — the watcher is purely additive.
- [x] 4.3 A startup coverage log: which c2-channel XOs have a resolvable return-leg webhook (mirror
  `logMirrorCoverage`), so a mis-provisioned channel is visible at boot, not at first miss.

## 5. Spec delta

- [x] 5.1 `specs/watch/spec.md` (delta): ADD `### Requirement: The c2 hotline has a never-silent return
  leg` — operator→XO hotline replies route back to the origin channel (verbatim, store-read), every
  non-route outcome alerts loudly; reference `Feedback-loop immunity` (the reply is webhook-dropped, no
  loop). Scenarios: a hotline reply routes back; a no-completion/TTL escalates; a webhook-miss escalates.
- [x] 5.2 `openspec validate --all --strict` green.

## 6. Build, test, review, ship

- [x] 6.1 `go build ./...` + `go test ./...` green; `go vet` clean.
- [x] 6.2 Implementation-trio: systems-review + open-code-review + STORM (per the standard gate) on the
  diff; iterate until clean.
- [x] 6.3 PR via the gh-token bypass to hydra-ops's gate (reference #175 + this change). Note the
  merged≠running deploy step (watch restart, operator/XO-timed).
