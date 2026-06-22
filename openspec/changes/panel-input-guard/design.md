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

## Detection algorithm (the new probe) — HEADER-ANCHORED panel-span scan

The design-trio (STORM P1-A/S1/S2) retired the original "bottom-most `❯`" rule: it breaks on a
LONG panel (e.g. memex's 8 subagents — the panel's bottom chrome can exceed a fixed tail window,
pushing both the cursor and the composer out of view → missed detection → the silent loss returns
for the exact desk #152 was filed against), and it is unproven for a cursor on a MIDDLE agent row.
The replacement anchors on a STRUCTURAL landmark — the live panel header — which is invariant to
the subagent count and the cursor's row:

```
InputBlocked(pane) (blocked, ok bool):
  captured, err := capturePane(pane)            // the FULL visible pane (do NOT pre-truncate to N)
  if err != nil: return false, false            // undetermined — caller falls back (no false block)
  return parsePanelFocused(captured)            // (blocked, true)

parsePanelFocused(captured) (blocked, ok bool):
  lines := split(captured)
  // 1. Anchor: the LIVE panel header is the BOTTOM-MOST "… Enter to view" line. (A panel-capture
  //    echoed into scrollback sits ABOVE the live composer/panel, so the bottom-most header is the
  //    live one — this is what defeats the scrollback-echo false positive, structurally.)
  hdr := last index i where lines[i] contains "Enter to view"
  if hdr < 0: return false, true                // no live panel header → not blocked
  // 2. Focus: any agent-row cursor in the panel SPAN (header → pane bottom). The focused row is
  //    always below the header regardless of how many agents there are or which row is selected.
  for i := hdr+1 .. len(lines)-1:
     a := TrimLeft(lines[i], " \t")
     if after, found := CutPrefix(a, "❯"); found:
        g := TrimLeft(after, " \t")
        if HasPrefix(g, "◯") || HasPrefix(g, "●"):
           return true, true                     // ❯ cursor on an agent row, below the live header
  return false, true                             // panel shown but cursor not on it (composer focused)
```

**Why header-anchored dominates bottom-most (all three failure modes closed):**
- **Long panel (memex 8 subagents):** the cursor row is found by scanning the whole header→bottom
  span, never lost to a fixed window. We capture the full visible pane and locate the header, so the
  panel's height is irrelevant.
- **Cursor on a middle row:** scanning the span for ANY agent-row `❯` finds it wherever it sits.
- **Scrollback echo (the proven `flotilla-dev` false positive):** the LIVE panel docks at the
  bottom, so the bottom-most "Enter to view" is the live header; an echoed capture's header is above
  it and its agent-row `❯` is above the live composer — excluded by anchoring on the live header and
  scanning only BELOW it.

`headerPresent` (the anchor) + `cursorOnAgent` (focus) are still both required: a merely-DISPLAYED
panel (composer focused) has the header but no `❯` on any agent row → not blocked, so a healthy desk
running background agents still receives deliveries. Version-specific like
`parseComposerPending`/`deliver.workingSpinner` (revalidate glyphs + the "Enter to view" hint on a
Claude Code TUI upgrade). **Detection-coverage caveat (open):** this models the panel's
LIST-with-focus sub-state. Claude Code's panel may have OTHER focus-stealing sub-states (e.g. an
agent "view" sub-state with a different hint like "Esc to go back"). The restore/validation spike
must enumerate the sub-states and confirm "Enter to view" is the only focus-stealing one, or broaden
the header predicate to the full set of panel-chrome hints. A near-miss canary (below) makes a hint
that drifts VISIBLE rather than silently reverting to data loss.

**Near-miss canary (P1-C):** when `cursorOnAgent` is true but no recognized header anchors it
(`hdr < 0`), log a diagnostic — "agent-row cursor seen without a recognized panel header" — so a TUI
upgrade that reworps the hint surfaces in the journal instead of silently degrading to a paste-loss.

**Per-poll capture cost (Economist):** `Assess`, `ComposerPending`, and `InputBlocked` each call
`capturePane` — three captures per confirm poll at `confirmPollInterval=100ms`. Implementation
SHOULD thread a single capture through all three per poll (capture once, classify thrice) rather
than three independent tmux reads, to bound per-poll latency on a heavy pane. (Mechanism-internal;
no spec impact.)

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
- **Q2 (detection robustness) — RESOLVED: header-anchored span scan replaces bottom-most `❯`.** The
  fixed-N tail overflowed on a long panel (memex 8 subagents — the documented #152 desk) and was
  unproven for a middle-row cursor. The header-anchored rule (scan header→bottom for any agent-row
  `❯`) closes the long-panel, middle-row, AND scrollback-echo modes together. A near-miss canary
  logs a cursor-without-recognized-header so a TUI hint drift is visible. RESIDUAL (open, →spike):
  enumerate the panel's OTHER focus-stealing sub-states (e.g. an agent "view" with "Esc to go back")
  and confirm the header predicate covers them.
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
