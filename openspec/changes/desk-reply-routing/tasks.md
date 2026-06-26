# Tasks — desk-reply-routing (TDD)

Load-bearing properties (assert across paths):
- **(R1) reliable, verbatim, every turn.** The reply is the XO's actual completed turn-final read from
  the session store (count-based), so fast/queued/sub-tick turns are NOT dropped (the #175 bug) and the
  operator gets the verbatim text, not a "read the pane" escalation.
- **(R2) never silent.** Every non-route outcome (no new assistant turn within TTL, origin-channel
  webhook unresolved, post failure) raises a LOUD operator alert — never a journald-only skip.
- **(R3) right destination.** The reply returns to the ORIGIN channel (the c2 channel the operator
  messaged from), resolved `BindingForChannel→XOAgent→Webhook`, under the XO's identity.
- **(R4) no loop.** The webhook-posted reply is dropped by the relay's `webhookID` guard — no re-ingest.
- **(R5) additive / no regression.** No change to the inbound relay, the detector tick, the XO
  Stop-hook, or the per-desk visibility mirror.

## 1. Assistant-turn-COUNT seam (the store ground-truth marker)

- [ ] 1.1 TEST FIRST (`internal/claudestore`): `assistantTurnCount(jsonlPath) (n int, ok bool)` over a
  fixture transcript — counts `type==assistant && message.role==assistant` text-bearing turns; skips
  user / tool_result / system / sidechain entries; a malformed line does not nuke the count.
- [ ] 1.2 Implement claudestore count (reuse `transcriptEntry` decode + the active-session resolution).
- [ ] 1.3 TEST FIRST (`internal/grokstore`): `assistantTurnCount(path,sessionID) (n int, err error)` —
  counts `type==assistant` extractable entries (mirrors `lastAssistant`'s skip rules; no timestamps).
- [ ] 1.4 Implement grokstore count.
- [ ] 1.5 Surface seam: an OPTIONAL `ReplyWatch` capability (or extend `ResultReader`) the driver
  implements — `LatestTurnMark(pane) (text string, count int, ok bool, err error)` — returning the
  latest text-bearing assistant turn AND the assistant-turn count, so the watcher snapshots `count` at
  delivery and detects `count>N` later. Claude + grok implement it; aider/opencode do not (→ escalate).
  Compile-time asserts.

## 2. The replyWatcher (anchor → poll → extract → route → escalate)

- [ ] 2.1 TEST FIRST: a pure `replyRouter`/`replyWatcher` with injected collaborators (mark-reader,
  webhook-resolver, post, alert, sleep) — NO tmux/store/Discord. Cases:
  - count advances + quiescent → reads latest turn-final → posts chunked to the resolved webhook (R1/R3).
  - count never advances within TTL → ALERT, no post (R2).
  - origin-channel webhook unresolved → ALERT, no post (R2).
  - surface lacks the mark capability (aider) → ALERT (R2).
  - post returns error → ALERT, redaction-safe (R2).
  - a newer message supersedes mid-watch → the prior watcher cancels; the route step re-checks ctx
    before each chunk post so no stale reply to the old channel.
  - multi-chunk reply → `discord.ChunkContent` + ordered `(i/n)` posts.
- [ ] 2.2 Implement the watcher: snapshot `count=N` at anchor; poll `LatestTurnMark` at a fast cadence
  (bounded TTL) until `count>N` + the read is stable (quiescence); extract + route; escalate per R2.
- [ ] 2.3 Per-XO single watcher: `map[xo]*watcher` + mutex; launch cancels the prior; route re-checks
  `ctx.Err()` before each post (supersede guard).

## 3. Destination resolution (origin-channel webhook)

- [ ] 3.1 TEST FIRST: `replyDest(cfg, secrets, originChannel) (url string, ok bool)` =
  `BindingForChannel(originChannel)→XOAgent→secrets.Webhook`; miss (no binding / no webhook) → ok=false
  (→ the watcher escalates). Cover the single-channel and a federated multi-channel roster.
- [ ] 3.2 Implement; attribution: post under `username=<xoName>` with a reply marker.

## 4. Wire into the watch daemon (gated to c2-channel XO targets)

- [ ] 4.1 In the `SetMirror` confirmed-delivery hook (`cmd/flotilla/watch.go`): when `j.Kind=="relay"`
  AND `j.Agent` is the `xo_agent` of `j.OriginChannel`'s binding (a c2 hotline message to that XO),
  launch/supersede the replyWatcher `(xo, j.OriginChannel)`. Inert when secrets/bindings absent.
- [ ] 4.2 Confirm NO behavior change to the existing inbound echo / CoS ledger / per-desk mirror /
  detector tick / XO Stop-hook (R5) — the watcher is purely additive.
- [ ] 4.3 A startup coverage log: which c2-channel XOs have a resolvable return-leg webhook (mirror
  `logMirrorCoverage`), so a mis-provisioned channel is visible at boot, not at first miss.

## 5. Spec delta

- [ ] 5.1 `specs/watch/spec.md` (delta): ADD `### Requirement: The c2 hotline has a never-silent return
  leg` — operator→XO hotline replies route back to the origin channel (verbatim, store-read), every
  non-route outcome alerts loudly; reference `Feedback-loop immunity` (the reply is webhook-dropped, no
  loop). Scenarios: a hotline reply routes back; a no-completion/TTL escalates; a webhook-miss escalates.
- [ ] 5.2 `openspec validate --all --strict` green.

## 6. Build, test, review, ship

- [ ] 6.1 `go build ./...` + `go test ./...` green; `go vet` clean.
- [ ] 6.2 Implementation-trio: systems-review + open-code-review + STORM (per the standard gate) on the
  diff; iterate until clean.
- [ ] 6.3 PR via the gh-token bypass to hydra-ops's gate (reference #175 + this change). Note the
  merged≠running deploy step (watch restart, operator/XO-timed).
