# Portable coordinator seat — readiness short-list

**Status:** operator/CoS gate (succession program, 2026-07-04)
**Scope:** What breaks if a **grok**, **codex**, or **opencode** seat runs XO/CoS duties —
gate reviews, merges, `flotilla notify`, doctrine adherence, detector loop, synthesis.

Training desk owns **model** fitness; this doc owns **harness + seat plumbing**.

Reference: [coordinator-seat-swap-runbook.md](../coordinator-seat-swap-runbook.md),
[codex-coordinator-seat design](../../openspec/changes/codex-coordinator-seat/design.md),
Era VI (#261, #263).

## Executive summary

| Harness | Execution desk | Project-XO trial | CoS/meta-XO |
|---------|----------------|------------------|-------------|
| **grok** | **Live** (many fleet desks) | **Code-ready** — supervised trial queued | Blocked until project-XO trial passes |
| **codex** | **Live** (codex-memex-dev, etc.) | **Code-ready** — codex-harness-dev post-auth gate | Same |
| **opencode** | **Driver only** — no live desk | **Not ready** | **Not ready** |

**Nearest non-Fable coordinator options today:** grok and codex (explicit `surface` on roster).
**OpenCode** is the best **cheap multi-model execution** candidate; coordinator path is one
probe + bridge sprint behind grok/codex.

## Ranked gaps (fix cost ascending)

Lower rank = cheaper / unblocks more. "Blocks XO" = cannot pass supervised trial script.

### Tier A — Small (hours; unblocks trial scheduling)

| Rank | Gap | Affects | Fix |
|------|-----|---------|-----|
| A1 | Supervised trial not **executed** on any project-XO | grok, codex | Operator window + runbook §6 script (48h) |
| A2 | `opencode` missing from `workspaceLaunchCommand` / `harnessLaunchWired` | opencode | Add `opencode .` recipe; remove fast-follow notice |
| A3 | OpenCode permission markers not live-elicited (#54) | opencode | Capture dialog on v1.3.15+ with tool-calling model |
| A4 | Grok coordinator permission template drift | grok XO | Keep `deploy/grok-coordinator-permission-allowlist.json` synced with launch `--allow`/`--deny` |

### Tier B — Medium (days; load-bearing for coordinator quality)

| Rank | Gap | Affects | Fix |
|------|-----|---------|-----|
| B1 | **`ComposerStateProbe` absent on opencode** | opencode XO | Cursor-indexed composer classifier (blocks confirmed send volume) |
| B2 | **`ComposerStateProbe` on codex** — shipped but post-auth fixtures gate | codex XO | codex-harness-dev: live revalidate markers after auth |
| B3 | **`ResultReader` absent on opencode** | opencode XO | Turn-final path for mirror, synthesis, idle-hold, delegatenudge |
| B4 | **`RecycleBridge` absent on opencode** | opencode XO, switch | Handoff/takeover + `flotilla switch` TO-target parity |
| B5 | **`harnessAllocationSurface` / `IsManagementHarness` exclude opencode** | opencode XO | Add `opencode` to coordinator surface switch + delegatenudge map (after B1) |
| B6 | **memex pull-only on grok** | grok XO | memex-grok MCP + corpus scope (parallel lane; not a driver blocker for trial) |
| B7 | **Doctrine budget / skills path on non-Claude cwd** | all non-Claude XO | `flotilla doctrine install --refresh`; verify AGENTS.md size + heartbeat skills resolve |

### Tier C — Larger (coordination / policy)

| Rank | Gap | Affects | Fix |
|------|-----|---------|-----|
| C1 | **No opencode coordinator launch env** (`FLOTILLA_SELF`, `FLOTILLA_SECRETS`) | opencode XO | Mirror grok/codex coordinator recipe in `workspaceLaunchCommand` |
| C2 | **No opencode coordinator rules file** | opencode XO | Scaffold coordinator rules (merge allowed; default-branch deny) |
| C3 | **Rate-limit / failover probes missing for opencode** | opencode, auto-switch | `RateLimitProbe` + harness-subscription-switching chain entry |
| C4 | **CoS/meta-XO trial policy** | CoS | Runbook forbids CoS swap until project-XO trial passes — keep |
| C5 | **Fable metered Jul 7** — empath-lead, inventrise-xo still on `claude-fable-5` | fleet | Operator succession pick per flotilla; not a plumbing fix |

## Per-harness XO duty matrix

| XO duty | grok | codex | opencode |
|---------|------|-------|----------|
| `flotilla send` / confirmed delivery | Probe + tests shipped | Probe shipped | Spinner-only confirm; **residual silent-drop risk** |
| `flotilla notify` (secrets) | Coordinator template allows | codex-harness-dev rules | **No recipe / rules** |
| `gh pr merge` (reviewer gate) | Allowed in coordinator template | Coordinator rules | **Not scaffolded** |
| Turn-final mirror / synthesis | grok transcript reader | codexstore | **No ResultReader** |
| `delegatenudge` IC detection | `IsManagementHarness("grok")` | `codex` included | **Not management harness** |
| `visibility-synthesis` wake | Works if turn-final readable | Works if turn-final readable | **Broken** without ResultReader |
| Rotate `/clear` | Wired | Wired | `/clear` wired |
| Recycle / handoff | RecycleBridge shipped | RecycleBridge shipped | **No bridge** |
| Doctrine in identity file | AGENTS.md | AGENTS.md | AGENTS.md (doctrine writes; launch unwired) |
| memex standing rules | MCP path (in flight) | codex adapter | **None** |

## Recommended succession sequence (harness plumbing only)

1. **Execute** supervised grok **or** codex project-XO trial (operator window) — proves seat parity.
2. **Stand up** `opencode-harness-dev` execution desk — revives cheap multi-model path.
3. **Ship** opencode P1–P3 (launch wiring, #54, ResultReader).
4. **Ship** opencode `ComposerStateProbe` — unblocks coordinator candidacy.
5. **Re-run** supervised trial on opencode project-XO if operator wants a third harness option.
6. Only then discuss CoS/meta-XO swap (operator gate).

## Operator decisions (for CoS rollup)

| Decision | Recommendation | Default if silent |
|----------|----------------|-------------------|
| First project-XO trial harness | **grok** (flotilla-dev already on grok surface; probe shipped) | Schedule 48h supervised trial |
| OpenCode trial desk | **Authorize** channel mint + `opencode-harness-dev` resume | Execution desk only |
| CoS swap before Jul 7 | **Defer** until project-XO trial passes | Keep Fable on CoS through Jul 7 |

## Verification commands

```bash
# Driver registration
go test ./internal/surface/ -run 'OpenCode|ManagementHarness' -count=1

# Live CLI version
opencode --version

# Coordinator policy (read-only)
rg 'harnessAllocationSurface|IsManagementHarness' cmd/flotilla/workspace.go internal/delegatenudge/
```