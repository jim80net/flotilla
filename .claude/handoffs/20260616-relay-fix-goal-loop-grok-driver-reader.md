# Handoff — 2026-06-16: relay confirmed-delivery, goal-driven loop, grok driver+reader

Peer-to-peer handoff for the next flotilla-dev session. Everything below is DONE and merged
unless marked otherwise. Repo: `jim80net/flotilla` (this checkout:
`/home/jim/workspace/github.com/jim80net/flotilla`). The live fleet is the Spark/General-ML
deployment; the live roster + state live at
`/home/jim/workspace/github.com/General-ML/spark/state/` (flotilla.json, fleet-backlog.md, the
detector marker files), NOT in this repo.

## What landed this session (full standard flow each: design → SR+OCR reviews → TDD → PR)

1. **Inbound relay confirmed delivery** — operator mechanical fix #1. The relay last-mile was
   fire-and-forget (logged "delivered" on the tmux exit code, not on a turn starting; no idle-gate
   → fired into a busy composer). Fix: `surface.Confirm.Submit` (idle-gate → submit → confirm the
   `Idle→Working` edge → idempotent Enter-only retry via `deliver.SendEnter` → typed errors;
   escalation is the caller's, kind-aware), `watch.Injector` busy-defer (relay re-enqueues
   off-worker bounded; ticks drop), `watch.PaneMutexes` (serializes the confirm vs the detector
   `/clear` rotate). **PR #71 merged + DEPLOYED + VERIFIED LIVE.**
   - **Follow-up #74:** the live deploy false-negatived — diagnosed (measured, not assumed) that
     `deliver.ParseBusy`'s working marker was wrong for current claude-code (the `(Ns ·` counter
     only appears seconds in; early/short turns show `<glyph> <Verb>…` with no counter). Fixed
     ParseBusy to the gerund+`…` marker (Enter→Working ~60ms measured). **PR #74 merged + deployed
     + verified ("turn confirmed").** openspec change archived.

2. **Goal-driven loop (backlog gate)** — operator mechanical fix #2. The detector's `continueXO`
   could settle the XO while authorized work remained. Fix: a mechanical VETO — `internal/backlog`
   (a TOTAL fail-safe parser of `- [<status>]` items: in-flight/next=unblocked,
   blocked/needs-attention=operator-blocked, done=drained; markerless→flag+drive) + `continueXO`
   refuses to settle (overriding both the idle self-signal AND the cap) while unblocked items
   remain; per-item `driveCount` deprioritizes a stuck item (escalate once, drive the rest);
   `Awaiting` suppresses the drive; liveness `AckAge` watchdog independent of settle (test-locked);
   opt-in `--backlog-file`. **PR #75 merged.** openspec archived.
   - **NOT YET ENABLED:** the XO does the deliberate flip (`--backlog-file` in the flotilla-watch
     unit + migrate the live fleet-backlog.md to the `[status]` contract — already done per the
     live file). Until then the live detector runs the old binary; the XO keeps a ScheduleWakeup
     self-loop as backstop. The live `fleet-backlog.md` IS already in the `[status]` contract.

3. **grok driver reworked for xAI's OFFICIAL grok CLI** — #58 part A. The deployed grok-research
   desk runs `~/.grok/bin/grok` ("Grok Composer 2.5 Fast"), NOT superagent-ai/grok-cli ("grok-dev")
   that the old driver targeted (all its markers matched ZERO live → always-Idle → detector
   mis-assessed). Live-captured the official grok render: Working = the streaming arrow `⇣`
   (U+21E3) OR a braille spinner (U+2801–28FF), present throughout a turn, absent idle
   (`Turn completed in …`); reset `/new` (unchanged). **PR #77 merged.** openspec archived. Also
   corrected the stale #58 premise ("capture-pane blank / black hole" — refuted: capture returns
   the rendered TUI).

4. **grok full-result reader — `flotilla result <agent>`** — #58 part B. capture-pane returns only
   the visible tail; the full result is in the grok session store. `internal/grokstore.LatestResult`
   (active_sessions.json → session_id by cwd → glob `sessions/*/<id>/chat_history.jsonl` → last
   assistant entry, handling string AND `[{type,text}]` content); `surface.ResultReader` optional
   capability; `deliver.PaneCWD`; the read-only `flotilla result` command (warns to stderr if
   mid-turn). **PR #79 merged. Verified end-to-end live (read-only).**

## ⚠️ LOOSE END — archive grok-result-reader (do this first)

PR #79 merged but its openspec change is NOT yet archived. On a fresh checkout:
`git checkout main && git pull && openspec list` (you'll see `grok-result-reader` active) →
`git checkout -b chore/archive-grok-result-reader && openspec archive grok-result-reader -y` →
commit + PR (pattern: see the merged #73/#76/#78 archive PRs). It folds the `ResultReader` delta
into `openspec/specs/surface`.

## NEXT TASK (the XO will re-task you fresh after the batched deploy)

**Installer optional `FLOTILLA_BACKLOG_FILE` follow-up.** Make
`deploy/flotilla-watch-install.sh` support an OPTIONAL backlog-file → the systemd unit's
`ExecStart --backlog-file` arg, so a fresh host enables the goal-loop via `deploy/.env` instead of
a hand-made systemd drop-in. (Mirror how the other optional paths — secrets/ack/etc. — flow from
.env into ExecStart in that installer. The `--backlog-file` flag already exists in
`cmd/flotilla/watch.go`; unset ⇒ inert.) Standard flow. Do NOT start until re-tasked.

## Tracked follow-ups (noted in code + specs)

- grok official-CLI `AwaitingApproval` gate markers (auth/payment/tool-approval) — need a live
  capture of that state (the desk auto-executes; gates are rare). Until then a blocked grok reads
  Idle (documented gap; only the XO has a wedge timer, not desks). (#58)
- grok multi-line / bracketed-paste submit validation (single-line is live-confirmed). (#58)
- Cross-process atomicity for confirmed delivery (the in-daemon `paneMu` covers the daemon; an
  operator `flotilla send` racing a daemon confirm is the rarer residual). (relay-confirmed-delivery design)
- Voice confirmation inherit (voice already idle-gates; only the confirmation is missing) — a
  fast-follow noted at the relay design gate. (#72 = the relay paneMu wiring integration test.)

## KEY LEARNINGS (reflection — mostly reinforcements of existing rules)

- **VERIFY A STALE EMPIRICAL PREMISE BEFORE BUILDING ON IT.** I had propagated "grok capture-pane
  returns blank / grok is a black hole" into the #58 issue; live re-verification (15 KB capture
  with the result) REFUTED it, and revealed the actual bug (driver targets the wrong grok product).
  Building the sqlite read-path the premise demanded would have been wasted work on a false premise.
  This is `verify-stale-empirical-status-before-propagating` + fact-check-the-operator in action —
  the XO praised it twice ("live-capture over inference is exactly right"). Reinforce: when a task's
  PREMISE is an empirical status, live-re-verify it as step 1, even when it came from me.
- **LIVE-CAPTURE RENDER MARKERS; DON'T INFER.** Both the claude busy-marker (#74) and the grok
  working-marker (#77) bugs were inferred/source-verified markers that didn't match the live TUI.
  Measure the actual render (scratch pane / live desk), hexdump the exact bytes (the `⇣` = U+21E3,
  the `…` = U+2026), and pin tests to the measured strings.
- **A mislabeled regex is a code-truth violation.** OCR caught `[A-Z][a-z]+…` mislabeled "gerund"
  (it matches any Capitalized…, incl. prose `Note…`/`Done…`) — would have re-broken the grok fix.
  Replaced with the measured structural marker (braille spinner). Don't describe code as doing
  something narrower than it does.
- **`filepath.Join("", x)` returns `x`, NOT `""`.** I asserted (without verifying) that an empty
  home → empty grokHome; OCR proved `filepath.Join("",".grok") == ".grok"`. The guard was dead.
  Don't claim a stdlib behavior in a comment without checking it.
- **Decompose: a tested correctness fix ≠ a new-capability design.** #58 split cleanly into A
  (driver fix, urgent) and B (reader, enhancement) — two reviewable PRs > one mixed.

## Mechanics / environment notes

- `go` is at `/usr/local/go/bin` (not on PATH by default): prefix go commands with
  `export PATH=$PATH:/usr/local/go/bin`.
- Reviews: this repo has NO cubic — `/systems-review` + `/open-code-review` (dispatched as parallel
  background Agents on the diff) are the gates of record. The XO reviews the code diff hardest at
  the impl gate, esp. for the loop mechanism (continueXO) and the operator's interface (the relay).
- Merge policy: merge on clean gates (CI + SR + OCR), but the XO has been merging the substantive
  PRs (they claimed the hard code-review gate); I autonomously merge the doc-only openspec ARCHIVE
  chore PRs.
- Deploy is the XO's (it restarts the safety-critical heartbeat clock); the XO is batching the
  #77+#79 deploy.
