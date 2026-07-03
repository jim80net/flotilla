# Coordinator seat swap — harness-portable XO/CoS runbook

Generic operator runbook for swapping a coordinator seat between management harnesses
(Claude default, explicit `surface: "codex"` trial). Codex-specific provisioning
(ComposerStateProbe, secrets launch, coordinator rules) is owned by **codex-harness-dev**
— see `openspec/changes/codex-coordinator-seat/design.md`.

## Preconditions (all required before live swap)

1. **Supervised trial only** — one low-stakes **project XO**, not meta-XO/CoS. Operator
   present for the trial window (~48h).
2. **Target harness ready** — for codex: post-auth fixtures + `ComposerStateProbe` shipped
   (codex-harness-dev lane). Do not roster a codex coordinator until that gate clears.
3. **Host binary** ≥ merge commit for generic parity (`harnessAllocationSurface`,
   `delegatenudge.IsManagementHarness`).
4. **Doctrine refreshed** after workspace init:
   ```bash
   flotilla doctrine install --refresh <agent> --roster "$FLOTILLA_ROSTER"
   ```
   Or fleet-wide: `deploy/flotilla-doctrine-refresh.sh` (runs `doctrine install --refresh --all`).
5. **Rollback path rehearsed** — `flotilla switch <agent> --to claude-code` (see
   `docs/harness-subscription-switching.md`).

## Roster template — codex coordinator trial

```json
{
  "name": "alpha-xo",
  "surface": "codex"
}
```

`alpha-xo` must already be a coordinator (project-XO `xo_agent` on its home channel, or
`cos_agent`). Empty/`claude-code` surface keeps Claude management harness (unchanged default).

## Provision (operator + codex-harness-dev)

```bash
flotilla workspace init --repo <repo-url> alpha-xo --roster "$FLOTILLA_ROSTER"
flotilla doctrine install --refresh alpha-xo --roster "$FLOTILLA_ROSTER"
# codex-harness-dev: verify launch exports FLOTILLA_SELF + FLOTILLA_SECRETS, coordinator rules
flotilla resume alpha-xo --roster "$FLOTILLA_ROSTER"
```

Restart `flotilla-watch` after the first codex roster entry (daemon must load the codex driver).

## Supervised trial script (operator-present)

Run all six; any silent-drop submit or doctrine budget overflow is **failure**.

| # | Exercise | Pass criterion |
|---|----------|----------------|
| 1 | Operator relay | Message → XO pane → `flotilla notify` reply visible in Discord |
| 2 | Dispatch | `flotilla send` to grok desk → material wake → XO handles in fresh context |
| 3 | Gate | XO reviews trivial PR; `gh pr merge` allowed on coordinator rules |
| 4 | Detector | Idle-hold / stranded break fires on synthetic holding turn-final (staging) |
| 5 | Rotate | Settled handling triggers harness `/clear`; ack file touched |
| 6 | Rollback drill | `flotilla switch alpha-xo --to claude-code` without fleet stall |

## Rollback

1. `flotilla switch alpha-xo --to claude-code` (preserves handoff per harness-subscription-switching).
2. Or revert roster `surface` to `""` / `claude-code`, `workspace init` + `doctrine install --refresh`,
   `flotilla resume alpha-xo`.
3. Postmortem in private channel; do not expand trial to CoS until project-XO trial passes.

## flotilla-dev vs codex-harness-dev boundary

| Generic (this repo lane) | Codex-specific (codex-harness-dev) |
|--------------------------|-------------------------------------|
| `harnessAllocationSurface` codex coordinator | `ComposerStateProbe` |
| `delegatenudge` management harness + neutral nudge | Coordinator launch env + `flotilla-coordinator.rules` |
| This runbook | `xo-outbound` doctrine member + AGENTS.md budget test |

Coordinate before either lane edits `internal/delegatenudge/` or `cmd/flotilla/workspace.go`.
