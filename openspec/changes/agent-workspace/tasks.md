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
- [x] 0.4 Checkpoint → XO ratified all 3 forks 2026-06-11 (Option C — `--add-dir`,
      later corrected to `--append-system-prompt-file` per the 1.4a empirical refutation;
      template the DETECTOR continuation prompt; PR-1/PR-2 split). Build unblocked.

## 1. `internal/workspace` package (schema + resolution)

- [x] 1.1 `Root()`/`Dir(agent)`/`LoadRecipe(agent)` (single `launch.Recipe` via the
      extracted `launch.ValidateRecipe`). Tests: valid load, each invalid field, root override.
- [x] 1.2 `ResolveRecipe(agent, flatCfg)` — workspace first, flat fallback, clear
      dual-location error. Tests: the 3-way matrix.
- [ ] 1.3 `ResolveTracker(agent, flagPath, default)` → ONE path (state.md → flag →
      default). `ResolvePrompt(agent, kind, tracker, settle, ack)` → HEARTBEAT.md
      template (substitute `{{tracker}}`/`{{settle}}`, append ack) → built-in. Test:
      each precedence rung; placeholder substitution; **the returned prompt names the
      SAME path ResolveTracker returns** (the P1-2 single-source invariant).
- [x] 1.4 `IdentityFileName(surface)` — claude-code→`CLAUDE.md`, grok/cursor→`AGENTS.md`,
      unknown→error. Tests: each surface. + `StatePointer` (non-empty state.md → flat → none).
- [x] 1.4a EMPIRICAL RESULT (2026-06-11, claude 2.1.172): `--add-dir` does NOT auto-load
      the dir's `CLAUDE.md` (sentinel → `NONE`; cwd-control loaded; `--append-system-prompt`
      control loaded). So `workspace init` emits `--append-system-prompt-file <ws>/CLAUDE.md`
      (verified), NOT `--add-dir`. Grok/Cursor `AGENTS.md` stays UNVERIFIED → deferred to
      the driver phase, NOT claimed here.
- [x] 1.5 `FleetTmuxCheck(agent, target, flat)` — glob sibling workspaces ∪ flat recipes;
      reject a collision; SKIP a malformed other workspace with an (actionable) warning,
      NOT fail-closed; glob error surfaced (not swallowed). Tests: collision, broken-sibling
      skipped, flat-union, empty-target no-op.

## 2. `flotilla workspace` command (cmd/flotilla)

- [x] 2.1 `workspace init <agent>` — roster-validate, create only missing files
      (`launch.json` with the `--append-system-prompt-file` convention + empty cwd to fill,
      empty `HEARTBEAT.md`/`state.md`, surface-named identity stub); never overwrite. Tests:
      idempotency/no-clobber, unknown-agent error, grok→AGENTS.md, arg ordering.
- [x] 2.2 `workspace path <agent>` — print the dir. 2.3 dispatch + usage in `main.go`.

## 3. `flotilla resume` consumes the workspace (PR-1)

- [x] 3.1 Recipe via `workspace.ResolveRecipe`; flat file now OPTIONAL (loaded only if
      present; malformed→fail-closed); `runResume` core verified UNCHANGED (systems-review).
- [x] 3.2 `printState` pointer = non-empty `<workspace>/state.md` → flat `state` → none.
- [x] 3.3 Wired `FleetTmuxCheck` into cmdResume (warnings to stderr, collision → error).

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

- [ ] 5.1 Workspace section in the watch runbook: schema, the `--append-system-prompt-file` recipe
      convention, migration (init → fill → move tracker → RESTART watch; one expected
      spurious wake), the user-service `$HOME` requirement.
- [ ] 5.2 Update the `flotilla-fleet-recovery` skill to prefer the workspace. (PR-1 follow-up.)
- [x] 5.3 quickstart: `flotilla workspace init|path` section. (5.1 watch-runbook → PR-2.)

## 6. Review + PR

- [x] 6.1 PR-1: adversarial systems-review (0 P1; runResume verified untouched, fail-closed
      preserved, ValidateRecipe behavior-preserving) + OCR; all findings folded (glob-error
      surfaced, StatePointer test added, actionable malformed-skip warning, stale-doc fixes).
- [~] 6.2 PR-1 up (workspace pkg + cmd + resume); CI → merge-ready → XO reviews+merges. PR-2 (watch) next.
