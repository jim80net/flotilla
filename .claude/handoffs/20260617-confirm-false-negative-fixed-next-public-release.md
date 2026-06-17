# Handoff: confirmed-delivery false-alarm fix (DONE) → next: flotilla public-release strategy

**Date:** 2026-06-17
**Branch (this session):** `fix/confirm-false-negative-composer-cleared` (merged as #86; safe to delete locally)
**Working directory:** `~/workspace/github.com/jim80net/flotilla` (main checkout, rt-dgx-sp001, arm64)
**Desk:** flotilla-dev

## Objective

flotilla is a fleet-coordination product (drop-in XO / chief-of-staff over Claude/Grok/Cursor/…
agents) dogfooded on the live Spark trading fleet. This session fixed a P1 operator-facing bug in
its operator↔XO message relay. The next session pivots to the **public-release strategy** (below).

## Session Summary

Took a P1 bug from the XO: the confirmed-delivery layer raised FALSE `⚠️ operator message NOT
delivered` alarms (the operator hit one). Measured the real rate from the live journal, found the
root cause (confirmation waited on the lagging working spinner), surfaced the design fork to the
operator (chose Option C), implemented it TDD, ran both review gates to clean, shipped PR #86 →
**merged**. Then wrapped up (reflection skill + memory + this handoff).

## Completed Work

### PR #86 — confirm submit on composer-clear, not the lagging spinner (MERGED)

**PR:** #86 — https://github.com/jim80net/flotilla/pull/86 (merged 2026-06-17T04:06:57Z, squash;
on `origin/main` as commit `4ffb812`).
**Problem:** `internal/surface/confirm.go` raised false `ErrUnconfirmed` → the relay
(`internal/watch/inject.go`) escalated `⚠️ operator message NOT delivered` for messages that
actually landed and were answered. Operator-facing; he saw one.
**Root cause:** confirmation confirmed a turn started ONLY via the Working spinner within a ~1.5s
window. The spinner is a LAGGING proxy — pressing Enter clears the composer immediately
(synchronous TUI) but the spinner renders SECONDS later on a ~500k-token session. So every poll
read Idle, the window closed, `ErrUnconfirmed` fired though the message landed.
**Measured evidence:** live `journalctl --user -u flotilla-watch` — 439 confirmed vs **12
`ErrUnconfirmed` (~2.7%)** for hydra-ops, **0 on any lighter desk** (perfectly correlated with pane
heaviness). Live `tmux capture-pane`: hydra-ops just-finished turn `✻ Churned for 3m 34s`
(multi-minute turns); working pane `· Ideating… (6m 53s · …)` (spinner the regex matches).
**Fix (Option C, operator-chosen):**
- new optional `surface.ComposerProbe` interface — `ComposerPending(pane) (pending, ok bool)`;
- claude-code implements it (`parseComposerPending`): `❯ <body>` = pending, bare `❯ ` = cleared,
  no prompt = undetermined (→ spinner fallback);
- `Confirm.Submit` confirms on composer-CLEARED **or** Working each poll; crash-fast to ErrCrashed
  on a mid-confirm shell; patient grace phase for no-probe surfaces; poll-count instrumentation
  (`logConfirmed`/`logUnconfirmed`);
- **paste-ingestion-race guard (review-driven):** composer-cleared trusted only after
  `clearedConfirmPolls` (2) CONSECUTIVE cleared reads — a not-yet-ingested paste flips to `pending`
  first → streak resets → Enter retry. Strict pending→cleared transition was rejected (incompatible
  with fast Enter-accept; documented).
**Files:** `internal/surface/surface.go` (ComposerProbe iface), `internal/surface/claude.go`
(ComposerPending + parseComposerPending), `internal/surface/confirm.go` (confirm loop + confirmRead
enum + grace + logging), `internal/surface/{confirm_test.go,surface_test.go}`,
`internal/watch/inject_confirm_test.go` (end-to-end), `docs/design-confirm-false-negative.md`.
**Tests:** parser incl. real hydra-ops capture w/ survey modal; confirm orchestration
(composer-clear, dropped-Enter-then-clear, transient-empty-then-pending must NOT false-confirm,
transient-empty-then-stable-clear recovers, stays-pending escalates, crash-fast, probe-undetermined
→ spinner grace); end-to-end injector (heavy-pane clear = no false alarm; stays-pending = still
escalates). `go build`/`vet`/`test -race` green; CI green.
**Review:** systems-review + open-code-review both run; both independently flagged that a single
"empty now" read re-opens the silent-drop in paste "failure mode A" → fixed with stable-cleared;
both re-verified RESOLVED and APPROVED.
**Deploy:** NOT done. Running `~/go/bin/flotilla` daemon predates the fix. The CoS/XO will batch the
deploy (`go install ./cmd/flotilla` or rebuild `~/go/bin/flotilla`, then
`systemctl --user restart flotilla-watch` — brief ~15s heartbeat/relay interruption) with a deferred
installer cutover. **flotilla-dev does NOT need to deploy.**

### Key Decisions

| Decision | Rationale | Rejected |
|---|---|---|
| Confirm on composer-clear (primary) + spinner (fallback) | composer-clear is fast + latency-independent; fixes the heavy-pane false negative at root | Just widening the spinner window (Option A) — still proxy-based, needs an unmeasured latency number, blocks the worker |
| Stable-cleared (2 consecutive) read | closes the paste-ingestion-race silent-drop without breaking the fast case | single empty read (unsafe); strict pending→cleared transition (breaks fast Enter-accept) |
| claude-code probe only; others use spinner fallback | all 12 false negatives were claude; never wrote render-parsing for aider/grok/opencode pending-composer without a live capture | implementing all four blind (would be guessing render formats) |

## Current State

### Git
```
origin/main: 4ffb812 fix(surface): confirm submit on composer-clear … (#86)   ← this fix, merged
this branch: fix/confirm-false-negative-composer-cleared (merged; delete it)
uncommitted: ?? .claude/skills/   (the reflection skill — goes in the session-assets PR)
```
### Open PRs (mine)
- #62 `feat(surface): the cursor driver SKELETON — INERT until operator-present live-capture [HELD]`
  — unrelated to this session, still HELD (needs operator-present live capture). Leave it.
### Deployed
- `~/go/bin/flotilla` = Jun-16 binary (pre-#86). Watch daemon (`flotilla-watch.service`, USER unit)
  running it. Fix is live ONLY after the batched deploy above.

## Remaining Work

### 1. flotilla PUBLIC-RELEASE strategy [TOP PRIORITY — next session]

**What:** Define + start executing what stands between flotilla and a public "drop-in XO / drop-in
chief of staff" release, marketed so a new user sees "shiny" within **30 seconds of install**.
Operator directive 2026-06-17, delegated by the CoS/XO to flotilla-dev's XO. The CoS will hand the
fresh session the FULL framing + recon when re-tasking — this is the advance flag.
**The operator's framing (four pillars):**
  1. **MODES** — distinct modes of behavior; drive the fleet BY mode. Modes are NOT first-class
     today.
  2. **REPORTING** — underdeveloped; today reporting is just the Discord push/mirror. Build it out.
  3. **INTERFACES** — pluggable interfaces; Discord is ONE interface. Make interfaces pluggable
     (the way harnesses already are).
  4. **PITCH / ON-RAMP** — a README that states the product in 1–2 sentences + an obvious easy
     on-ramp; possibly a separate landing site ("a separate boat").
**CoS recon to fold in:** current README is decent but NOT 30-sec-shiny; modes aren't first-class;
reporting is just the Discord push; pluggability EXISTS for harnesses/surface-drivers
(claude/grok/cursor/aider/opencode behind `internal/surface.Driver`) but NOT for modes/interfaces.
**Where to look first:** `README.md`, `cmd/flotilla/` (entrypoints/subcommands), `internal/watch/`
(the daemon: relay, heartbeat, detector, reporting via Discord mirror), `internal/surface/` (the
Driver SPI = the existing pluggability model to generalize), `internal/discord/` (the one
interface). Separate-circumstantial-from-generalizable applies hard here — this is product work.
**Approach:** likely a brainstorm → design.md → /systems-review + /open-code-review → openspec →
TDD, per the standard flow. Start by mapping current state vs the four pillars, then propose the
modes + interfaces abstractions + a reporting model + the README/on-ramp rewrite.
**Pitfalls:** don't bolt modes on as config flags — the operator wants them first-class. Don't
fragment the surface-Driver uniformity (see decouple-with-foundation-tradeoff). 30-sec-shiny is the
acceptance bar for the on-ramp — cold-test it (cold-test-author-written-docs).

### 2. (parked) voice Phase-1 — operator MONEY decision; do NOT start.

### 3. (optional, non-blocking) extend ComposerProbe to aider/grok/opencode

**What:** implement `ComposerPending` for the other surface drivers so they get the fast
composer-clear confirmation instead of only the spinner+grace fallback.
**Blocked by:** needs a LIVE `tmux capture-pane` of each surface's PENDING composer render (don't
guess render formats — verify-before-acting). Low priority: none of them has shown a false negative.
**Where:** `internal/surface/{aider,grok,opencode}.go` — each already has a composer/idle classifier
to extend. See `.claude/skills/flotilla-confirmed-delivery/SKILL.md`.

## Gotchas & Environment Notes

- **Go toolchain not on PATH by default:** `export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin`.
  arm64 host.
- **flotilla-watch is a USER systemd service:** `journalctl --user -u flotilla-watch` (plain
  `journalctl -u` shows nothing). It logs every delivery/failure — the canonical place to measure
  delivery behavior.
- **`tmux capture-pane -p -t <pane>` is READ-ONLY** (injects no keystrokes) — safe to observe live
  XO/desk panes. "Don't poke the panes" = don't SEND keystrokes; reading is fine.
- **No cubic in flotilla** — gates of record are `/systems-review` + `/open-code-review` + CI.
- **Merge policy:** the operator reserved the merge for #86 ("my review+merge"); generally flotilla
  merges on clean gates, but honor an explicit "my merge" when stated.
- **SSH agent flakiness:** `git fetch`/`push` may warn `signing failed … 1password ssh key … agent`;
  fetch still works. If push fails, the 1Password ssh-agent needs unlocking (operator action).

## To Resume

1. `cat .claude/handoffs/20260617-confirm-false-negative-fixed-next-public-release.md`
2. Wait for the CoS/XO to hand over the full public-release framing + recon (workstream #1), or read
   `README.md` + `internal/{watch,surface,discord}/` to pre-load the current-state map.
3. Verify #86 is on main: `git log --oneline origin/main -1` → should show `…(#86)`.
4. Read `.claude/skills/flotilla-confirmed-delivery/SKILL.md` before any future change to confirm.go.
