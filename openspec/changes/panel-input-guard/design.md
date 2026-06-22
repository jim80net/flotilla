# Design — panel-input-guard

## Problem restated

A panel-focused Claude Code pane reads as `StateIdle`, so `Confirm.Submit` pastes into it; the
Enter then drives the panel, not a turn. The message is lost (composer never receives it) or
stranded (pasted, unsubmitted), and retries stack pastes. The system must (1) detect the state,
(2) refuse to paste, (3) report it as NOT delivered with an actionable alert, and (4) attempt a
restore where one is empirically available.

## Live ground truth (verified, not assumed)

Captured from the un-mutated `family-office` (%31) pane, 2026-06-22 (the operator-facing desk the
operator flagged). Pane height 77; the agents panel is the absolute bottom-most chrome:

```
  Want me to kick off the edge audit + cost number now …      ← last turn-final line
                                   new task? /clear to save 527.5k tokens
  ──────────────────────────────────────────────────────────  ← box rule
❯                                                              ← COMPOSER (empty), col 0
  ──────────────────────────────────────────────────────────  ← box rule
  jim@rt-dgx-sp001:…/spark-familyoffice [Opus 4.8] ctx:48%     ← status
  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents          ← mode (the "← for agents" hint)
  ● main                          ↑/↓ to select · Enter to view ← PANEL HEADER
  ◯ predmkt-build     predmkt-build: …                    idle
❯ ◯ portfoliosrc-fix  portfoliosrc-fix: …                 idle  ← BOTTOM-MOST ❯ — panel cursor
```

Glyphs: `❯` U+276F (cursor / composer prompt), `◯` U+25EF (idle agent), `●` U+25CF (active agent).

Key facts this dictates:
1. The composer prompt (`❯`) and the panel cursor (`❯`) are BOTH present and look identical; the
   discriminator is **what follows the `❯`** — an agent glyph (`◯`/`●` + name) = panel cursor; text
   or empty = composer.
2. When the panel is FOCUSED, its cursor row is the **bottom-most** chrome (panel docks below the
   footer). When the composer is focused, the composer is the bottom-most `❯` and the agent rows
   (if shown) carry NO `❯`.
3. `parseComposerPending`'s current bottom-up scan lands on `❯ ◯ portfoliosrc-fix` → reports
   `pending=true` (a false "stuck composer"). This is a real bug the change fixes.

## Empirical recovery finding (drives the honesty of ask #2)

On the stuck `memex` pane, via `tmux send-keys`, NONE of these cleared the panel:
`Escape`, `Right`, `Left`, `Tab`, kitty-encoded `Escape` (`1b 5b 32 37 75` = `ESC[27u`),
`ESC[I`(focus-in)+`Escape`. Plus the operator's reported `Enter` / `ctrl+x ctrl+k` / `Right`.

`tmux send-keys` writes bytes to the pty master — indistinguishable from a physical keystroke at
the application — so a key that recovers for a human MUST recover via `send-keys`. That none do
implies the human recovery is **not a keystroke** (most likely a mouse click into the composer, an
SGR-mouse event under `?1006h`). Conclusion: **ship detect+refuse+alert; treat auto-recovery as a
spike** (validate a mouse-click `RestoreComposerFocus` against a throwaway Claude Code instance;
include it ONLY if it empirically works; never fabricate a recovery).

## Detection algorithm (the new probe)

```
InputBlocked(pane) (blocked, ok bool):
  captured, err := capturePane(pane)
  if err != nil: return false, false          // undetermined — caller falls back (no false block)
  blocked = parsePanelFocused(captured)
  return blocked, true

parsePanelFocused(captured) bool:
  tail := last N lines (N≈12 — panel header + rows sit just below the footer)
  headerPresent := any tail line contains "Enter to view"   // the panel nav hint
  // bottom-most ❯ line, agent-row test:
  for i from len(tail)-1 down to 0:
     rest := TrimLeft(tail[i], " \t")
     if after, found := CutPrefix(rest, "❯"); found:
        a := TrimLeft(after, " \t")
        cursorOnAgent := HasPrefix(a, "◯") || HasPrefix(a, "●")
        return headerPresent && cursorOnAgent      // decide on the BOTTOM-MOST ❯ only
  return false                                       // no ❯ in tail → not blocked
```

**Why both conditions, and why bottom-most-only:**
- `cursorOnAgent` (the bottom-most `❯` is on an agent row) is the FOCUS signal — it is what makes
  Enter navigate instead of submit. A merely-DISPLAYED panel (composer focused) has its `❯` on the
  composer, so the bottom-most `❯` is NOT an agent row → not blocked.
- `headerPresent` corroborates "this is really the agents panel," guarding the pathological case of
  a composer literally containing `◯ …` text (which would have no panel header).
- **Bottom-most-only** defeats the scrollback-echo false positive proven during this work:
  `flotilla-dev`'s own pane showed `❯ ◯ portfoliosrc-fix` in scrollback (a printed capture), but
  its LIVE composer is below that — so the bottom-most `❯` is the composer, correctly not-blocked.
  A naive "any `❯ ◯` in the tail" grep false-positives here; the bottom-most rule does not.

This mirrors `parseComposerPending`'s existing tail-scoped bottom-up scan (`claude.go:154-167`) and
shares its version-specificity caveat (revalidate the glyphs/hint on a Claude Code TUI upgrade).

## Seam choice — optional probe, not a new State

`ComposerProbe` and `ResultReader` establish the pattern: an OPTIONAL Driver capability that the
confirm mechanism (not `Assess`) type-asserts and uses, with a safe fallback when absent.
`InputBlockProbe` follows it exactly. Rejected alternative: add `StateInputBlocked` to the `State`
enum and return it from `Assess`. That changes Assess semantics for EVERY consumer (the detector's
liveness watchdog, materiality reads, the resume interlock's kill-gate) for a condition only the
SUBMIT path must act on — broad blast radius for no benefit. The probe is surgical and consistent.
(Open question Q1 invites the trio to stress this.)

## Gate + classification placement (`confirm.go`)

1. Idle-gate, after the `StateIdle` case proceeds: if the driver implements `InputBlockProbe` and
   `InputBlocked`→(true, ok): attempt restore (spike; below), re-check; if still blocked return
   `ErrPanelBlocked` — BEFORE `d.Submit`. No paste, no stacked retry.
2. `pollConfirm`: add `readPanelBlocked` — if a panel appears mid-confirm (subagents spawned during
   the window), return it; `check()` treats it as NOT-confirmed-and-settled → the body is reported
   not-delivered rather than ever confirmed-cleared. (A panel that steals focus AFTER the Enter was
   accepted is the rare case; classify conservatively as not-delivered so we never false-confirm.)
3. `ErrPanelBlocked` is a distinct sentinel (sibling of `ErrBusy`/`ErrCrashed`), wrapping nothing;
   callers match with `errors.Is`.

## Restore attempt (best-effort, honest)

`deliver.RestoreComposerFocus(pane)` — added ONLY if the implementation spike validates a recovery
against a throwaway instance. Candidate (untested on a safe target): an SGR-mouse left-click into
the composer row (`ESC[<0;col;rowM` / `…m`) if Claude Code enables mouse reporting. The gate calls
it once, re-checks `InputBlocked`, and proceeds to paste only if cleared; otherwise refuses. If the
spike finds NO reliable recovery, `RestoreComposerFocus` is omitted and the gate refuses directly —
the change still fully delivers detect+refuse+alert.

## Caller routing

- **`inject.go` (relay):** `errors.Is(err, ErrPanelBlocked)` → `in.raise(...)` an operator alert
  that (a) names the recipient, (b) includes the lost payload (bounded preview, full in the log),
  (c) states the action: "input-blocked behind the agents panel — needs a human keystroke/click into
  the composer at <pane>." This is a GENUINE actionable loss (unlike MSG-3's likely-delivered
  warning). Heartbeat/detector kinds stay non-alarming per the existing kind-awareness.
- **`main.go` (`send`/`notify`):** print "not delivered — <agent> is input-blocked behind the agents
  panel (needs a keystroke at its pane)"; non-zero exit. Never the silent-success path.
- **`library.go` (dash control):** a distinct `OutcomeInputBlocked` (or reuse the genuine-loss
  outcome with a reason) so the dash shows the desk as blocked.

## Open questions for the trio

- **Q1 (seam):** Probe vs `StateInputBlocked`. The design picks the probe (surgical, consistent). Is
  there a detector/liveness path that genuinely BENEFITS from Assess surfacing the block (e.g. the
  watchdog flagging a stuck desk) enough to justify the broader change? (Lean: no — the wake already
  routes through Submit and will alert.)
- **Q2 (detection robustness):** Is "bottom-most `❯` on an agent row + header present" the right
  predicate? Failure modes to probe: a panel with the cursor on the FIRST row scrolled just out of a
  too-small tail window; a composer multi-line paste that pushes the panel cursor's relative
  position; localization of the "Enter to view" hint. Should N (tail lines) be larger, or keyed off
  the box-rule/footer position?
- **Q3 (mid-confirm panel):** Is classifying a panel-appears-mid-confirm as NOT-delivered correct,
  or could the Enter have been accepted (turn started) the instant before the panel grabbed focus —
  risking a false not-delivered + a re-send? (Lean: not-delivered is safe — the relay alert is
  actionable, not a re-paste; and the no-re-paste invariant means we never double-submit.)
- **Q4 (restore spike):** Is a mouse-click SGR sequence acceptable to inject on a live pane once
  validated, or should restore be operator/XO-driven only (flotilla never auto-injects mouse)? What
  is the rollback if a malformed sequence lands as literal text?
- **Q5 (alert payload):** Same bounded-preview policy as MSG-3's genuine-loss alert — should the two
  share one `raisePayloadAlert` helper now, or stay separate until MSG-3 lands and refactor then?
- **Q6 (false-block cost):** If detection false-positives on a healthy busy desk, we refuse a real
  delivery. Is the refusal-with-alert clearly better than today's silent-loss-on-the-real-block, and
  is the asymmetry acceptable? (Lean: yes — a refusal is visible+retryable; a silent loss is not.)
