# Tasks — agent-workspace

> **Gate:** this is the BUILD plan. It is unblocked only after (a) the XO ratifies
> design.md at the checkpoint and (b) the native-instruction-file fork (§"Open
> questions" 1) and the PR split (3) are decided. No code until then.

## 0. Design gate (current phase)

- [x] 0.1 Draft proposal + design.md + workspace/watch spec deltas.
- [x] 0.2 `openspec validate agent-workspace --strict` passes.
- [ ] 0.3 `/systems-review` + OCR on the design; fold findings.
- [ ] 0.4 Checkpoint the openspec to the XO; resolve the open forks. (BLOCKS 1+.)

## 1. `internal/workspace` package (schema + resolution)

- [ ] 1.1 `Dir(agent)` → `~/.flotilla/<agent>/` (honor `$HOME`); `LoadRecipe(agent)`
      reads `<dir>/launch.json` as a single `launch.Recipe`, reusing the existing
      validation (launch/cwd/tmux rules). Test: valid load; each invalid field rejected.
- [ ] 1.2 `ResolveRecipe(agent, flatCfg)` — workspace first, flat `flotilla-launch.json`
      fallback, else a clear error naming both. Test: the 3-way matrix.
- [ ] 1.3 `ResolvePrompt(agent, rosterMsg)` — `HEARTBEAT.md` → rosterMsg → default;
      `ResolveTracker(agent, flagPath, default)` — `state.md` → flag → default.
      Test: each precedence rung.
- [ ] 1.4 `IdentityFileName(surface)` — claude-code→`CLAUDE.md`, grok/cursor→`AGENTS.md`.
      Test: each surface; unknown surface → error.
- [ ] 1.5 Fleet-load shared-`tmux` rejection across workspaces. Test: two share → error.

## 2. `flotilla workspace` command (cmd/flotilla)

- [ ] 2.1 `workspace init <agent>` — roster-validate the agent, then create only
      missing files (commented `launch.json` template, empty `HEARTBEAT.md`/`state.md`,
      surface-named identity stub); never overwrite. Test: partial-scaffold idempotency,
      unknown-agent error, no-clobber.
- [ ] 2.2 `workspace path <agent>` — print the dir. Test: output.
- [ ] 2.3 Dispatch + usage in `main.go`.

## 3. `flotilla resume` consumes the workspace (PR-1 candidate)

- [ ] 3.1 Recipe resolution → `workspace.ResolveRecipe` (workspace-first, flat fallback).
      `runResume` core unchanged. Test: workspace used; flat fallback; neither → error.
- [ ] 3.2 `/takeover` state pointer defaults to `<workspace>/state.md`. Test: pointer source.

## 4. `flotilla watch` consumes the workspace (PR-2 candidate — live daemon)

- [ ] 4.1 Heartbeat prompt via `workspace.ResolvePrompt`. Test: HEARTBEAT.md overrides;
      absent → roster/default.
- [ ] 4.2 Detector tracker via `workspace.ResolveTracker`. Test: state.md hashed; absent → flag/default.
- [ ] 4.3 Confirm no-workspace behavior is byte-identical to today (regression).

## 5. Docs + migration

- [ ] 5.1 Workspace section in the watch runbook + a `~/.flotilla/<agent>/` reference;
      migration note (init per agent, move `.flotilla-state.md` → `state.md`, flat file
      stays a fallback).
- [ ] 5.2 Update the `flotilla-fleet-recovery` skill to prefer the workspace.
- [ ] 5.3 quickstart: `flotilla workspace init` in the launch recipe.

## 6. Review + PR

- [ ] 6.1 `/systems-review` + OCR on the implementation diff; fold findings.
- [ ] 6.2 PR(s) per the ratified split; CI green; merge-ready → XO reviews+merges.
