# Tasks — agent-workspace

> **Gate:** this is the BUILD plan. Unblocked only after the XO ratifies design.md
> at the checkpoint AND resolves the open forks (identity-file Option C/A/B;
> HEARTBEAT.md templating scope; PR split). No code until then.

## 0. Design gate (current phase)

- [x] 0.1 Draft proposal + design.md + workspace/watch spec deltas.
- [x] 0.2 `openspec validate agent-workspace --strict` passes.
- [x] 0.3 Adversarial systems-review + OCR on the design; fold findings (3 P1 + 6 P2
      folded: detector-prompt targeting, single-source tracker, migration restart,
      fleet-scan posture, Option C, $HOME, empty-state suppression).
- [ ] 0.4 Checkpoint the revised openspec to the XO; resolve the open forks. (BLOCKS 1+.)

## 1. `internal/workspace` package (schema + resolution)

- [ ] 1.1 `Root()` = `<os.UserHomeDir()>/.flotilla` with `--workspace-root`/
      `$FLOTILLA_WORKSPACE_ROOT` override; `Dir(agent)`. `LoadRecipe(agent)` reads
      `<dir>/launch.json` as a single `launch.Recipe`, reusing the existing
      validation. Test: valid load; each invalid field; root override.
- [ ] 1.2 `ResolveRecipe(agent, flatCfg)` — workspace first, flat fallback, else a
      clear error naming both. Test: the 3-way matrix.
- [ ] 1.3 `ResolveTracker(agent, flagPath, default)` → ONE path (state.md → flag →
      default). `ResolvePrompt(agent, kind, tracker, settle, ack)` → HEARTBEAT.md
      template (substitute `{{tracker}}`/`{{settle}}`, append ack) → built-in. Test:
      each precedence rung; placeholder substitution; **the returned prompt names the
      SAME path ResolveTracker returns** (the P1-2 single-source invariant).
- [ ] 1.4 `IdentityFileName(surface)` — claude-code→`CLAUDE.md`, grok/cursor→`AGENTS.md`,
      unknown→error. Test: each surface.
- [ ] 1.5 `FleetTmuxCheck(agent, target)` — glob sibling workspaces ∪ flat recipes for
      unmigrated agents; reject a collision; SKIP a malformed other workspace with a
      warning (NOT fail-closed). Test: collision rejected; broken sibling skipped, this
      agent still resolves.

## 2. `flotilla workspace` command (cmd/flotilla)

- [ ] 2.1 `workspace init <agent>` — roster-validate the agent, then create only
      missing files (commented `launch.json` emitting the `--add-dir` convention, empty
      `HEARTBEAT.md`/`state.md`, surface-named identity stub); never overwrite. Test:
      partial-scaffold idempotency, unknown-agent error, no-clobber.
- [ ] 2.2 `workspace path <agent>` — print the dir. 2.3 dispatch + usage in `main.go`.

## 3. `flotilla resume` consumes the workspace (PR-1)

- [ ] 3.1 Recipe via `workspace.ResolveRecipe`; `runResume` core unchanged. Test:
      workspace used; flat fallback; neither → error.
- [ ] 3.2 `/takeover` pointer = non-empty `<workspace>/state.md` → flat `state` → none
      (empty-state suppression). Test: each source incl. empty-suppressed.
- [ ] 3.3 Wire `FleetTmuxCheck` into resume. Test: collision; broken-sibling-skipped.

## 4. `flotilla watch` consumes the workspace (PR-2 — live safety-critical daemon)

- [ ] 4.1 Detector continuation prompt via `ResolvePrompt`, tracker via `ResolveTracker`
      — resolved ONCE at startup; the resolved tracker path feeds BOTH `trackerHasher`
      AND the prompt's `{{tracker}}` (P1-2). Test: HEARTBEAT.md overrides the detector
      continuation prompt; **prompt-named path == hashed path**.
- [ ] 4.2 Legacy-mode prompt resolution (HEARTBEAT.md → heartbeat_message → default).
- [ ] 4.3 Regression: **no workspace ⇒ byte-identical to today** (watch.go:66-80, 156-177,
      257-261 paths). Migration transition documented as restart-time + one spurious wake
      (P1-3) — assert the restart semantics, not live re-resolution.

## 5. Docs + migration

- [ ] 5.1 Workspace section in the watch runbook: schema, the `--add-dir` recipe
      convention, migration (init → fill → move tracker → RESTART watch; one expected
      spurious wake), the user-service `$HOME` requirement.
- [ ] 5.2 Update the `flotilla-fleet-recovery` skill to prefer the workspace.
- [ ] 5.3 quickstart: `flotilla workspace init`.

## 6. Review + PR

- [ ] 6.1 `/systems-review` + OCR on each implementation diff; fold findings.
- [ ] 6.2 PR-1 (workspace pkg + cmd + resume) then PR-2 (watch); CI green; merge-ready
      → XO reviews+merges.
