---
name: flotilla-seven-c-walk
description: "Product XO / dash-walker SEVEN-C evening walk — 390px-first Playwright captures, grade grid 0–2 per C, file a GitHub issue when COMPELLING or CORRECT is below 2, store assets under state/<xo>-walk-<YYYYMMDD>/assets/ (not parades/). Read before a product walk, evening-walk dispatch, or SEVEN-C scorecard."
type: skill
queries:
  - "SEVEN-C product walk capture scorecard"
  - "evening walk 390px playwright flotilla dash"
  - "file issue after walk COMPELLING CORRECT"
  - "flotilla-dev-walk assets where to save screenshots"
keywords:
  - seven-c-walk
  - product-walk
  - 390px
  - scorecard
  - COMPELLING
  - CORRECT
boost: 0.08
---

# SEVEN-C product walk — capture + grade + file

**Audience:** product-owning XOs and dash execution walkers (not CoS parade authors).
Ceremony context: `docs/coordinator-runbooks/ceremonies.md` (evening walk).

## Asset paths (load-bearing)

Walk captures and scorecards live under the roster state tree — **not** `parades/`:

```text
<roster-dir>/state/<xo>-walk-<YYYYMMDD>/assets/<view>.png
<roster-dir>/state/<xo>-sevenc-scorecard-<YYYYMMDD>.md
```

Example (product-dev lane): `state/flotilla-dev-walk-20260711/assets/w11-goals-viewport-390.png`

`parades/<date>/assets/` is for **morning parade demo slides** only. Product-walk evidence
stays in the walk directory so scorecards and issues cite durable, non-ceremony paths.

## Capture recipe (390px first)

1. **Viewport:** `390×844` (phone proxy). Desktop captures are supplementary, not the grade bar.
2. **Tool:** Playwright chromium from a throwaway venv (`python3 -m venv /tmp/pw-venv`;
   `pip install playwright`; uses `~/.cache/ms-playwright` — no sudo chrome install).
3. **Per surface:** `goto` live URL → `wait_for_selector` on a real element → `pageerror` hook
   must stay zero → `screenshot(full_page=True)`.
4. **Name files by view:** e.g. `goals-viewport-390.png`, `conversations-390.png`, `parade-list-390.png`.
5. **Vision grade:** COMPELLING requires a seeing agent on the PNGs — the capture desk does not
   grade its own screenshots.

Minimal script shape:

```python
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={"width": 390, "height": 844})
    errors = []
    pg.on("pageerror", lambda e: errors.append(str(e)))
    pg.goto("http://127.0.0.1:<port>/")
    pg.wait_for_selector("#real-anchor", timeout=10000)
    pg.screenshot(path="assets/goals-viewport-390.png", full_page=True)
    b.close()
assert not errors
```

## Grade grid (0–2 each; cite evidence)

| C | Question |
|---|----------|
| **COMPLETE** | Did we walk every litmus path we owe? |
| **CORRECT** | Does the UI tell the truth (no false-healthy chrome)? |
| **COMPREHENSIVE** | Are the operator's daily questions answerable from what rendered? |
| **CALIBRATED** | Are staleness/age/session labels honest? |
| **CONCISE** | 390px: no horizontal scroll; primary actions in thumb reach? |
| **COMPELLING** | Would the operator want to open this on their phone? (pixels + vision) |
| **CONSISTENT** | Cross-surface signals agree (banner vs row vs tab)? |

Record **DELTA** vs the prior scorecard file. **TOTAL** = sum of seven grades (max 14).

## Generated work — file issues, don't narrate

**Mechanical gate:** when **COMPELLING < 2** OR **CORRECT < 2**, open a GitHub issue in the
product repo before settling. Every other C < 2 should also produce filed work, but those two
are the non-negotiable filing triggers.

### Issue acceptance shape (model: flotilla#609)

```markdown
## Summary
One sentence: what failed the walk and user impact.

## Evidence
- Capture: `state/<xo>-walk-<YYYYMMDD>/assets/<file>.png` (roster-local path)
- Walk: SEVEN-C <ISO timestamp> (`flotilla-dispatch-<hex>` if dispatched)
- Prior: #<issue> if regression

## Acceptance
Concrete, testable UI outcome at 390px (readable label, no false-green, etc.).

## Scope
Named subsystem only — not a drive-by refactor.
```

Dispatch the owning build desk after filing; link the issue in the scorecard **GENERATED WORK**
section.

## Scorecard footer

End the scorecard with total, delta vs prior, and every filed issue URL. Skip-as-satisfied
(~6h) is allowed only with an explicit backlog note — never a silent skip.