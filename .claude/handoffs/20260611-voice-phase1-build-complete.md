# Handoff — flotilla voice Phase-1 is BUILD-COMPLETE (live activation operator-gated)

**Date:** 2026-06-11 · **Desk:** flotilla-dev · **Repo:** `/home/jim/workspace/github.com/jim80net/flotilla` (main checkout, branch `feat/voice-deploy-docs` at handoff time — main has all voice PRs merged) · **Context for the next session:** voice is done being built; do NOT auto-resume anything — hold for XO/operator steer on next work.

---

## 1. What landed — voice Phase-1, end-to-end (8 PRs, all merged)

The `discord-voice` openspec change is **build-complete**: operator↔XO Discord voice (Grok STT/TTS, push-to-talk), a separate `flotilla voice` process isolated from the heartbeat clock.

| Slice | PR | What |
|---|---|---|
| §1 per-pane injection lock | #35 | cross-process flock so voice/send/watch never interleave into a pane |
| §2 SpeechProvider + Grok + meter + normalize | #36 | STT/TTS driver, atomic cost meter, transcript normalize |
| §3a gate + resample + CGO-isolated codec seam | #37 | fail-closed SSRC `SpeakerTable`, 24→48k resample, `OpusCodec` interface + `!voiceopus` stub |
| §3b real libopus codec | #38 | first-party `#cgo pkg-config: opus` binding (`//go:build voiceopus`) |
| §5.1 `flotilla speak` spool | #39 | non-blocking drop-oldest outbound spool |
| §3.3+§4.1 inbound pipeline | #40 | gated → silence-endpoint → STT → busy-deferred inject |
| §4.2 outbound pipeline | #41 | spool → TTS(pcm@48k) → Opus → OpusSend |
| §3.1+§3.4+§5.2 command + adapter + recovery | #43 | `flotilla voice` + discordgo `Session` adapter + `Supervise` |
| §6 deploy + docs | #45 | `flotilla-voice.service` + installer + env templates + runbook |

**Invariants worth remembering:** core binary stays `CGO_ENABLED=0` (libopus isolated to the voiceopus voice process — proven in CI on amd64); the SSRC gate is fail-closed (only the operator's positively-mapped SSRC is injected); one shared cost meter caps STT+TTS (reserve-before-call); operator notices go to the **log, never the XO pane** (would self-inject as a command).

### ⚠️ LIVE ACTIVATION IS OPERATOR-GATED — do not flip it on yourself
Everything is built + deployable, but `flotilla voice` is **opt-in**: turning it on is metered Grok spend on a new live audio surface, needs the runtime config filled (`XAI_API_KEY` + guild/channel/operator IDs) and a voice-tagged binary installed. The exact install/enable/smoke steps are in **`docs/voice-runbook.md`**. That's the operator's call.

### openspec bookkeeping (small, do early next session if steered to)
`openspec/changes/discord-voice/tasks.md` §7.1/§7.2 are **satisfied by the per-PR systems-review + merges** (each slice was reviewed + CI-green + merged). They were being marked `[x]` in the wrap-up assets PR. The change is build-complete and **ready to archive at the operator's discretion** (I did NOT archive it — live activation + the follow-ups below are still open).

---

## 2. Tracked follow-ups (filed, neither blocks anything)

- **flotilla #42** — real drop→reconnect liveness signal. discordgo **v0.29.0 never closes `OpusRecv`** on a drop (it self-heals via its own internal reconnect, reusing the channel), so the adapter's close-based `lost` signal can't fire for ordinary drops; `Supervise`'s reconnect branch only fires on initial-connect-failure + clean shutdown today. **No live blast radius** — voice survives drops inside discordgo regardless. Hardening only. A genuine signal must not false-positive on normal push-to-talk silence (so a naive inter-packet-gap timer is wrong).
- **flotilla #44** — atomic-`mv`-in-dest-dir for BOTH systemd-unit installers (watch + voice together, to keep them structurally identical). OCR nit; current `cp` path is safe + self-healing. Low priority.

---

## 3. CANDIDATE next-work — three STALE partial openspec changes (INVESTIGATE, do NOT blind-resume)

There are three partially-complete openspec changes left over from earlier work. **Do NOT auto-resume any of them** — each needs investigation (is it still wanted? did the codebase move under it? is the partial work sound?) and then **XO/operator steer on priority**. Treat the task-counts as a starting hint, not a mandate.

| Change | Tasks done | Next-session action |
|---|---|---|
| `agent-workspace` | 19/22 | Investigate the 3 open tasks + whether the merged `~/.flotilla/<agent>/` workspace work (#32/#33) already covered them; the migration to `~/.flotilla` workspaces was NOT operator-authorized as of this session. |
| `send-mirror-default-off` | 9/11 | Investigate the 2 open tasks vs the merged mirror-toggle work. |
| `surface-driver` | 13/15 | Investigate the 2 open tasks vs the current `internal/surface` driver state. |

**Process for whichever the operator picks:** read its `proposal.md` + `tasks.md` + `design.md`, diff its assumptions against current `main` (the codebase has moved a lot — voice added ~10 files to `internal/voice/`), confirm with the XO it's still wanted, THEN resume via the standard flow. The fresh session HOLDS for steer before touching any of these.

---

## 4. Working state at handoff
- `main` is clean and has all 8 voice PRs. The local `feat/voice-deploy-docs` branch = #45 (merged).
- No uncommitted code. The wrap-up assets PR (this handoff + the §7.1/7.2 tasks.md mark) is the only thing in flight from the wrap-up.
- Reflection wrote 4 global skills (`verify-api-response-shape-with-probe`, `cgo-dependency-arch-split`, `isolate-untestable-transport-behind-seam`, `agent-control-notices-to-side-channel`) + a flotilla project memory (`voice.md`) — all outside the repo, already persisted.

## 5. Operational notes for the next flotilla-dev session
- Build voice: `CGO_ENABLED=1 go build -tags voiceopus -o ~/go/bin/flotilla-voice ./cmd/flotilla` (needs `libopus-dev`, installed on this host: opus 1.4).
- Go is at `/usr/local/go/bin` (`export PATH=$PATH:/usr/local/go/bin`).
- Gate every slice with systems-review; OCR **times out on large multi-file diffs** → fall back to systems-review as gate of record (per the unavailable-tool policy) and say so in the PR.
- Gatekeeper denies `sed`/`awk`/`rm -r`/`git branch -D`/amend/force/rebase — use Read/Edit/python, create-fresh, new commits, branch-from-origin/main.
- The XO reviews + merges flotilla-dev PRs on clean gates; my lane is merge-ready.
