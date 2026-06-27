## Why

The `grok-desk` desk runs **xAI's official grok CLI** (`~/.grok/bin/grok` — the "Grok
Composer 2.5 Fast" TUI with a structured `~/.grok` session store), but the `grok` surface driver
(and its spec) were written against **`superagent-ai/grok-cli` ("grok-dev")** — a different
product the operator does not run, which he calls outdated. **Measured:** all of the grok-dev
driver's markers (`Planning next moves`/`enter queue`/x402/`Paste your xAI API key`) match ZERO
against the live official-grok pane, so `parseGrokState` always defaulted to `StateIdle` and the
change-detector could never see `grok-desk` transition (it never diffed, never woke the XO on
a grok finish). The operator ruled REPLACE: match the driver — and the spec — to deployed reality.

This also corrected a stale premise (`capture-pane returns blank / grok is a black hole`): live
re-verification showed capture-pane returns the rendered TUI including the result; the
driver/product mismatch was the real bug.

## What Changes

- The `grok` driver is reworked for **xAI's official grok CLI** with **live-captured** render
  markers (2026-06-16): the Working signal is the live streaming arrow `⇣` (U+21E3) OR a braille
  spinner frame (U+2801–28FF) — both grok chrome present throughout a turn and absent when
  idle/done (`Turn completed in …` + empty composer). Working-positive, Idle-default.
- The reset stays `/new` (confirmed in the official grok's slash menu) and submit stays
  bracketed-paste+Enter (single-line live-confirmed; multi-line a tracked follow-up).
- **No `AwaitingApproval` branch yet** — the official grok's blocking gates (auth/payment/tool
  approval) are not yet live-captured (a documented gap; a crashed desk still alerts via the Shell
  path, but an auth-blocked-but-alive desk is invisible to the XO-only wedge timer until captured).
- The `surface` spec requirement is retitled + rewritten from the grok-dev contract to the
  official-grok reality (markers live-captured, not source-verified; reduced state set
  Shell/Working/Idle; no AwaitingApproval yet).

## Non-Goals

- **#58 part B** — a full-result reader via the `~/.grok/sessions/<cwd>/<session>/chat_history.jsonl`
  store (the complete result for long turns vs capture-pane's visible tail). Separate unit.
- The official grok's blocking-gate (`AwaitingApproval`) markers — pending a live capture of that
  state (#58 follow-up).
- Multi-line / bracketed-paste submit validation for the official grok composer — tracked.
- Re-adding a grok-dev driver — the operator does not run that product.
