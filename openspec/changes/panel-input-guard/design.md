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

## Detection algorithm (the new probe) — GEOMETRY-based whole-pane scan

### Re-reversal during implementation (supersedes the trio's header-anchored proposal — RECORDED, not silent)

The trio (STORM P1-A/S1/S2) proposed a HEADER-ANCHORED span scan to replace a first-draft
bottom-most rule. Implementation found header-anchoring carries a **false positive the trio missed**,
so the rule was re-derived to the GEOMETRY rule below. Recorded here so the reversal is documented,
not a silently-dropped trio finding:

- **P1-A's true root cause was the FIXED WINDOW, not the bottom-most logic.** The first draft
  truncated to a fixed `N≈12` tail before scanning; an 8-subagent panel overflowed it. Scanning the
  WHOLE captured pane fixes the long-panel miss while KEEPING the bottom-most rule. P1-A required
  dropping the window, not abandoning bottom-most.
- **Header-anchoring false-positives on a full-panel echo with NO live panel.** STORM S2 assumed
  "the LIVE header is the bottom-most `Enter to view` line" — true only when a live panel exists.
  When an entire panel capture is echoed into a desk's own scrollback and there is NO live panel
  (the verified `flotilla-dev` case), the bottom-most header IS the echoed one, and scanning below it
  finds the echoed cursor → it would BLOCK a healthy desk. The geometry rule does not: the live
  composer is below the echo, so the bottom-most `❯` is the composer.

The shipped GEOMETRY rule (whole-pane, bottom-most `❯`, header-corroborated) dominates header-
anchoring on the echo case and matches it on the long-panel + middle-row cases:

```
InputBlocked(pane) (blocked, ok bool):
  captured, err := capturePane(pane)            // the FULL visible pane (do NOT pre-truncate to N)
  if err != nil: return false, false            // undetermined — caller falls back (no false block)
  return parsePanelFocused(captured)            // (blocked, true)

parsePanelFocused(captured) (blocked, ok bool):
  lines := split(captured)                       // whole pane, NOT pre-truncated to a window
  bottom := bottom-most line bearing a "❯" prompt (after trimming leading whitespace)
  if bottom < 0 OR not isAgentRowCursor(lines[bottom]):
     return false, true                          // no "❯", or bottom-most "❯" is the composer → reachable
  if any line contains "Enter to view":          // corroborate a real panel
     return true, true
  log canary "bottom-most prompt is an agent-row cursor but no panel header"  // possible TUI drift
  return false, true

isAgentRowCursor(line): "❯" (after trim) immediately followed (after ws) by an agent glyph (◯/●)
```

**The load-bearing geometry fact (verified live, family-office %31, 2026-06-22):** the live agents
panel docks at the ABSOLUTE BOTTOM of the pane (its agent rows are the last lines, below the
composer + footer). So when FOCUSED, the panel's selection cursor is the bottom-most `❯`; when NOT
focused (or no panel), the bottom-most `❯` is the composer. A scrollback echo sits ABOVE the live
composer, so it is never the bottom-most `❯`. This single fact gives all three:
- **Long panel (memex 8 subagents):** whole-pane scan (no window) → the cursor (the only agent-row
  `❯`) is the bottom-most `❯`.
- **Cursor on a middle row:** rows below it carry no `❯`, so it is still the bottom-most `❯`.
- **Scrollback echo (lone OR full-panel, with or WITHOUT a live panel):** the live composer is the
  bottom-most `❯`, so an echoed cursor above it never decides — closing the header-anchored gap.

`isAgentRowCursor(bottom-most ❯)` (focus) + a panel header somewhere (corroboration, guarding a
composer whose literal content begins with an agent glyph) are both required. A merely-DISPLAYED
panel (composer focused) has its `❯` on the composer → bottom-most `❯` is not an agent row → not
blocked, so a healthy desk running background agents still receives deliveries.

**Residual (verified-geometry dependent):** if a future Claude Code TUI renders a `❯`-bearing line
BELOW the panel cursor (a new footer), the bottom-most `❯` would no longer be the cursor and
detection degrades to NOT-blocked (today's behavior — no regression, but the guard would miss). This
matches the CURRENT verified geometry (the panel cursor IS the bottom-most `❯`); revalidate on a TUI
upgrade. Version-specific like `parseComposerPending`/`deliver.workingSpinner`.

**Near-miss canary (P1-C):** when the bottom-most `❯` is an agent-row cursor but NO recognized
header corroborates it, log a diagnostic — so a TUI change that reworps the "Enter to view" hint
surfaces in the journal instead of silently degrading detection.

**Detection-coverage caveat (open):** this models the panel's LIST-with-focus sub-state. Claude
Code's panel may have OTHER focus-stealing sub-states (e.g. an agent "view" with "Esc to go back").
The follow-up spike enumerates the sub-states and broadens the predicate if needed.

**Per-poll capture cost (Economist, M1 — deferred with a visible TODO).** `Assess`, `InputBlocked`,
and `ComposerPending` each call `capturePane`: up to 3 captures per poll at
`confirmPollInterval=100ms` (the panel case short-circuits at 2; the common confirmed-by-spinner
case is 1). Threading one capture through all three needs a Driver-interface change (the probes
capture internally by interface contract) — out of proportion to a SHOULD-level optimization on a
ms-scale tmux read. Left as a TODO in `pollConfirm` referencing this note (not silently dropped);
revisit if the per-poll cost shows up in practice.

## Seam choice — optional probe, not a new State

`ComposerProbe` and `ResultReader` establish the pattern: an OPTIONAL Driver capability that the
confirm mechanism (not `Assess`) type-asserts and uses, with a safe fallback when absent.
`InputBlockProbe` follows it exactly. Rejected alternative: add `StateInputBlocked` to the `State`
enum and return it from `Assess`. That changes Assess semantics for EVERY consumer (the detector's
liveness watchdog, materiality reads, the resume interlock's kill-gate) for a condition only the
SUBMIT path must act on — broad blast radius for no benefit. The probe is surgical and consistent.
(Open question Q1 invites the trio to stress this.)

## Gate + classification placement (`confirm.go`)

1. **Idle-gate refusal (the primary fix — catches the #152 already-blocked case).** After the
   `StateIdle` case proceeds: if the driver implements `InputBlockProbe` and `InputBlocked`→(true):
   return `ErrPanelBlocked` BEFORE `d.Submit`. No paste, no stacked retry.

2. **`pollConfirm` precedence (STORM A1 — SHIP-BLOCKER; catches the panel-appears-mid-confirm
   case).** The CURRENT `pollConfirm` (`confirm.go:226-242`) reads `ComposerPending` after the
   Working/Shell assess; a panel-focused pane assesses `StateIdle` and its empty composer (the `❯ `
   ABOVE the docked panel) reads CLEARED → it would FALSE-CONFIRM a lost message as delivered. So
   `InputBlocked` MUST be consulted in `pollConfirm` BEFORE `ComposerPending`. The precedence is:

   ```
   pollConfirm: Working → readWorking
                Shell   → readCrashed
                InputBlocked(true) → readPanelBlocked        // <-- BEFORE ComposerPending
                ComposerPending: pending→readPending, cleared→readCleared
                else → readNone
   ```

   In `check()`, `readPanelBlocked` is handled like `readPending` for the streak (it **resets
   `clearedStreak = 0`** so a stale cleared streak can never tip a now-panel-blocked pane to
   confirmed) — it NEVER counts toward `readCleared`. A turn that genuinely started and THEN spawned
   subagents is unaffected: confirmation completes on `readWorking`/the cleared streak and `check()`
   returns BEFORE a later poll could see the panel (the streak-completion short-circuit at
   `confirm.go:174-176`). So `readPanelBlocked` only fires when NO delivery signal preceded it.

3. **`parseComposerPending` is left UNTOUCHED (STORM H1).** `ComposerPending`/`parseComposerPending`
   is called ONLY from `confirm.go` (verified: `pollConfirm:234`, `logUnconfirmed:261`). With
   `InputBlocked` checked first in `pollConfirm`, `parseComposerPending` never runs on a
   panel-focused pane — so the originally-planned "skip the panel cursor" edit is UNNECESSARY and is
   DROPPED, preserving the probe's documented "never a false success" invariant (`claude.go:147-153`)
   untouched. (The earlier worry that the panel cursor is the bottom-most `❯` is moot: the confirm
   path short-circuits to `readPanelBlocked` before the composer is ever parsed.)

4. **Classification + diagnostics.** At window expiry, if the pane is panel-focused the result is
   `ErrPanelBlocked` and a `logPanelBlocked` records it; the `ErrUnconfirmed` path
   (`logUnconfirmed`) is NOT reached for a panel-blocked pane, so the journal never lies
   `composer=cleared` about a panel block (STORM A3).

5. `ErrPanelBlocked` is a distinct sentinel (sibling of `ErrBusy`/`ErrCrashed`), wrapping nothing;
   callers match with `errors.Is`.

## Recovery: detect + refuse + alert (the mouse-inject restore is dropped to a follow-up)

`flotilla` ships **detect + refuse + alert** — the design's stated full deliverable. The
best-effort focus-restore is REMOVED from this change (STORM E1): the only untried candidate is an
SGR-mouse click (`ESC[<0;col;rowM`), and if Claude Code does not have mouse reporting enabled the
bytes land as LITERAL TEXT in the composer — corrupting the very composer we are trying to rescue,
on a live operator desk. Its expected value is low (6 keys already failed; mouse-mode state is
unverifiable from outside) and its downside is high. It is split to a SEPARATE follow-up change,
gated on a throwaway-instance spike that measures (a) whether mouse reporting is on, (b) whether a
click recovers, and (c) whether a malformed sequence is harmless. This change does not depend on it
— stopping the silent loss + an actionable alert is the whole win.

## Caller routing — `ErrPanelBlocked` is TERMINAL, not deferrable (STORM H3)

`inject.go`'s failure switch (`inject.go:155-165`) routes `ErrBusy`/`ErrTransient` → `handleBusy`
(defer + re-enqueue up to `maxRelayDeferrals`=60 over ~5 min) and everything else → the `default`
arm (escalate + drop, `isRelay`-gated). `ErrPanelBlocked` MUST go in the `default` arm (terminal),
NOT `handleBusy`: a panel does NOT self-heal on a timer like a busy turn, so deferring it 60× would
just DELAY the operator alert that is the entire point. The `default`-arm alert is the actionable
one: names the recipient, carries the payload (bounded preview), states the action — AND hedges
that the body may have started (STORM S3): "input-blocked behind the agents panel — needs a
keystroke/click at <pane>; verify the turn did not already start before re-sending."

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

## Open questions — RESOLVED in the design trio (systems-review + STORM, 2026-06-22)

- **Q1 (seam) — RESOLVED: optional probe, not `StateInputBlocked`.** Affirmed by STORM H2: mirrors
  the `ResultReader`/`ComposerProbe` precedent; the enum change would touch every Assess consumer
  (detector liveness/materiality, resume interlock) for a submit-only concern. Caveat folded:
  `InputBlockProbe` is consulted at TWO sites (gate + `pollConfirm`) — both must fall back identically
  when the probe is absent (a grok/cursor desk behaves exactly as today). Tasks cover both fallbacks.
- **Q2 (detection robustness) — RESOLVED, then RE-DERIVED in implementation to a GEOMETRY rule.**
  The trio proposed a header-anchored span scan; implementation found it false-positives on a
  full-panel echo with NO live panel (the echoed header becomes the bottom-most header), and that
  P1-A's actual root cause was the fixed tail window, not the bottom-most logic. The shipped rule is
  the GEOMETRY rule: whole-pane scan, bottom-most `❯`, agent-row test, header corroboration — which
  closes the long-panel, middle-row, AND both scrollback-echo flavors (the live composer is always
  the bottom-most `❯`). See the "Re-reversal during implementation" subsection above for the full
  rationale; this supersedes the header-anchored proposal, recorded not silently dropped. A near-miss
  canary logs a cursor-without-header (TUI hint drift). RESIDUAL (open, →spike): a future TUI footer
  with a `❯` below the panel cursor would degrade to NOT-blocked; and the panel's OTHER focus-stealing
  sub-states (e.g. an agent "view") are unenumerated — the follow-up spike covers both.
- **Q3 (mid-confirm panel) — RESOLVED: not-delivered, with precedence + an alert hedge.** A genuinely
  started turn confirms (Working/cleared-streak) and `check()` returns BEFORE a later panel poll, so
  `readPanelBlocked` only fires when no delivery signal preceded it (correct not-delivered). The
  residual human-amplified double-deliver (operator re-sends on a false not-delivered) is bounded by
  the streak-completion short-circuit and mitigated by the alert hedge "verify the turn did not
  already start before re-sending" (STORM S3). The no-re-paste invariant prevents any AUTOMATIC
  double-submit.
- **Q4 (restore spike) — RESOLVED: dropped to a follow-up.** The mouse-inject restore is removed from
  this change (literal-text corruption risk on a live desk if mouse-mode is off; low EV). Ship
  detect+refuse+alert; the validated restore is a separate change (STORM E1).
- **Q5 (alert payload helper) — RESOLVED: inline now, refactor when MSG-3 lands.** Add the
  `ErrPanelBlocked` arm to `inject.go`'s `default` switch inline; extract a shared payload-alert
  helper only once `submit-confirm-disposition` actually introduces the sibling case (avoid
  speculative generality — STORM E2).
- **Q6 (false-block cost) — RESOLVED: acceptable, and minimized by the refined detection.** A false
  block requires a live header + an agent-row `❯` while the composer is actually focused — but an
  agent-row `❯` IS the focus, so the case is near-impossible with the refined rule. A refusal is
  visible + retryable; a silent loss is not. A MISSED block degrades to today's behavior (no
  regression). One deferred check for the implementer (STORM E3): only add a distinct
  `OutcomeInputBlocked` to `library.go` if the dash RENDERS it distinctly; otherwise reuse the
  failed-outcome + reason string.

## At-most-once invariant (distributed-systems framing, STORM)

flotilla confirmed-delivery is **at-most-once by design** (the no-re-paste invariant — never
double-submit). The panel-blocked-mid-confirm case is the one seam where that guarantee meets a
HUMAN side-channel (the operator acting on the alert) that could re-introduce at-least-once. The
hedge in the alert copy is what keeps the human side aligned with the at-most-once intent — the
machine never double-delivers; the alert tells the human not to either, without verifying first.
