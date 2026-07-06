# OpenCode revival — trial desk design

**Status:** ready for operator/meta-XO gate (driver re-verified 2026-07-04)
**Driver:** `internal/surface/opencode.go` (PR #56 era)
**CLI probed:** OpenCode **v1.3.15** (`~/.opencode/bin/opencode`)

## Why OpenCode in the succession stack

OpenCode is a **multi-provider harness** — one TUI can front GLM, GPT, and other models via
`opencode providers` / `opencode models`. That makes it a candidate **cheap execution and
coordination seat** independent of Anthropic metered Fable.

The flotilla driver already exists and is registered. No fleet desk runs it today; revival is
plumbing + verification, not a greenfield driver.

## Driver re-verification (2026-07-04)

| Check | Result |
|-------|--------|
| Unit tests (`go test ./internal/surface/ -run OpenCode`) | **PASS** |
| Live idle capture (`Ask anything...` + footer `1.3.15`) | **PASS** — markers unchanged vs fixtures |
| Working marker (`esc interrupt`) | Fixture + prior live capture validated (#56) |
| Permission dialog (`Permission required` / `Allow once`) | Source-verified; live-elicit tracked **#54** |
| `ComposerStateProbe` | **Absent** — confirm uses Working-spinner fallback |
| `RecycleBridge` | **Absent** |
| `ResultReader` | **Absent** |
| Graceful `Close` | **Refused** (`ErrNoGracefulClose` — handoff-gated kill) |

**Verdict:** Safe for a **trial execution desk** (send/receive, detector, rotate). **Not** ready
for a coordinator seat until `ComposerStateProbe` + outbound notify path ship.

## Trial desk charter

Generic agent name: **`oc-desk`** (product lane; host-local roster entry — see `flotilla.example.json`).

| Field | Value |
|-------|-------|
| Surface | `opencode` |
| Repo worktree | `<org>/flotilla` (or any parity repo the trial targets) |
| Identity file | `AGENTS.md` (OpenCode loads natively) |
| Role | Execution desk only — **not** a coordinator trial |
| Model | Operator picks via `opencode models` (e.g. GLM 5.2, GPT) — training desk evals judgment models separately |

### Launch recipe (workspace init wired — P1)

`workspaceLaunchCommand` emits `opencode .` from the worktree cwd (`harnessLaunchWired` = true).
Optional model pin remains host-local:

```bash
# cwd = worktree root; OpenCode takes project path as positional
opencode .
```

Optional model pin (provider-specific — verify with `opencode models` on host):

```bash
# example only — confirm provider slug on target host
OPENCODE_MODEL=<provider>/<model> opencode .
```

### Provision steps (supervised; no XO swap)

1. `flotilla workspace init --repo <url> oc-desk --roster "$FLOTILLA_ROSTER"`
   — launch recipe should be `opencode .` in `flotilla-launch.json`
2. `flotilla doctrine install --refresh oc-desk --roster "$FLOTILLA_ROSTER"`
   — writes constitutional blocks into `AGENTS.md`
3. Mint Discord webhook + channel binding (meta-XO / fleet ops — not the product desk)
4. `flotilla resume oc-desk`
5. Restart `flotilla-watch` if daemon started before first opencode roster entry

### Trial pass criteria (execution desk)

| # | Exercise | Pass |
|---|----------|------|
| 1 | `flotilla send` → pane receives message | confirmed submit (spinner path OK; note probe gap) |
| 2 | Detector sees Working → Idle on turn finish | materiality fires once, not spuriously |
| 3 | `Assess` during tool permission | `AwaitingApproval` if #54 markers still match |
| 4 | Rotate `/clear` | context reset without shell drop |
| 5 | Cross-harness read | optional: Claude desk writes; opencode desk reads same repo |

### Product follow-ups (product repo lane)

| Priority | Item | Est. |
|----------|------|------|
| P1 | Wire `opencode` in `workspaceLaunchCommand` + `harnessLaunchWired` | **done** |
| P2 | Live-elicit permission dialog markers (#54) on v1.3.15+ | small |
| P3 | `ResultReader` for turn-final mirror/synthesis | medium |
| P4 | `ComposerStateProbe` (coordinator prerequisite) | medium |
| P5 | `RecycleBridge` + switch-target parity | medium |
| P6 | Add `opencode` to `harnessAllocationSurface` coordinator switch | small (after P4) |

## Coupled trial (operator/meta-XO 2026-07-04)

**Shape:** `beta-xo` (grok project-XO) supervises `oc-desk` during the 48h succession trial.
Trial XO dispatches pass-criteria exercises, gates outputs, reports to the meta-XO.
Desk stays **fleet-command member only** (fleet-internal desk pattern) — no dedicated Discord channel.

Pass criteria: host-local trial doc in the deployment's roster state directory (not in this repo).

## Boundaries

- **Not in scope:** model quality eval — training desk.
- **Blocked:** rostering opencode on a **coordinator** seat before P4 + supervised trial script.
- **Meta-XO gate:** trial-XO channel/webhook mint + operator veto window before first `resume`.