# F#383 conversations standalone-surface — acceptance walk

**Date:** 2026-07-09  
**Branch:** `feat/383-conversations-standalone` (main tip + residual close-out)  
**Fixtures:** partition-safe invented names only (`alpha-xo`, `cos`, `alpha`, channel `C_FIXTURE`).

## Operator acceptance (2026-07-04)

| # | Criterion | Result |
|---|-----------|--------|
| 1 | CoS / coordinator thread first-class | **PASS** — `/api/status` exposes distinct `cos`; rail lists CoS; **first paint selects `cos`**; thread shows ledger + session-mirror + history calibration; composer present. |
| 2 | Wider drive queue + wider popup modal | **PASS** — work-queue column **480px at 1440** (≥1400 residual; 420 base ≥1101); queue-item modal **720px at 1440**. |
| 3 | Desktop not capped by mobile | **PASS** — shell width 1440 at 1440 viewport; 3-column app shell; mobile stacks only via breakpoints. |
| 4 | Composer on thread | **PASS** (`composerHidden: false`). |
| 5 | Latest-at-bottom | **PASS** (Wave 4 Inc C locked + re-verified). |

## Measured layout (headless chromium)

### 390 (phone)

```json
{
  "viewport": {
    "w": 390,
    "h": 844
  },
  "wrapWidth": 390,
  "grid": "350px",
  "navW": 350,
  "threadW": 350,
  "ctxW": 350,
  "selectedDesk": "cos",
  "composerHidden": false,
  "cosInRail": true,
  "threadSnippet": "History begins Jul 4, 2026 — earlier coordinator turns weren’t recorded (a firewall issue, since fixed). Shown from here down.\noperator → cos\n2026-07-04T12:00:00Z\n#C_FIXTURE\n\nDispatch conversations standalone surface acceptance.\n\ncos → oper",
  "modalW": null
}
```

Capture: [`conversations-383-390.png`](assets/conversations-383-390.png)

### 1440 (desktop)

```json
{
  "viewport": {
    "w": 1440,
    "h": 900
  },
  "wrapWidth": 1440,
  "grid": "260px 632.812px 480px",
  "navW": 260,
  "threadW": 632,
  "ctxW": 480,
  "selectedDesk": "cos",
  "composerHidden": false,
  "cosInRail": true,
  "threadSnippet": "History begins Jul 4, 2026 — earlier coordinator turns weren’t recorded (a firewall issue, since fixed). Shown from here down.\noperator → cos\n2026-07-04T12:00:00Z\n#C_FIXTURE\n\nDispatch conversations standalone surface acceptance.\n\ncos → oper",
  "modalW": 720
}
```

Capture: [`conversations-383-1440.png`](assets/conversations-383-1440.png) (modal open for width measure)

## Prior landings

- Wave 4 Inc A #385 — desktop-space audit  
- Wave 4 Inc B+C #386 — CoS pin + composer + latest-at-bottom  
- #406/#408 honest CoS history; #405 rail regroup; #518/#575–#578 feed + mirror; #572 coordinator mirror-self  

## Residual in this PR

- First paint selects distinct CoS when present  
- Coordinator empty-state copy (not "desk")  
- ≥1400 work-queue column 480px + modal 720px (breakpoint-only)  
- HTTP lock: `TestHandleStatus_CosField`  
- Walk captures + this note  

## Verdict

Standalone conversations surface clears the 2026-07-04 acceptance bar on tip. Ready to close #383 after independent review.
