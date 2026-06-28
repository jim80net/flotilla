# Tasks — pluggable-issue-tracker (#103)

> **Design-first lane.** Phase 0 (this change) is the reviewed design + spec — trio-gated,
> merged as the blessed design. Phase 1 is the implementation follow-on (TDD); its tasks are
> laid out here so the design carries its own plan, but they are executed in a later lane.

## Phase 0 — design + review (this change)

- [ ] 0.1 Proposal — the visible-idea-backlog motivation; productize the XO's `gh issue` path
      behind a config-selected strategy (mirrors `surface.Driver` / `SpeechProvider`).
- [ ] 0.2 Design — `Tracker` interface + registry, the `gh`-wrapper GitHub strategy (with the
      gh-vs-API decision + rationale), `tracker` config + startup validation, the
      `flotilla issue` CLI, Linear/Jira stubs, the §8 decisions, phasing.
- [ ] 0.3 Spec — the `tracker` capability (strategy selection, GitHub-via-gh, the CLI, the stubs).
- [ ] 0.4 Trio review — `/systems-review` + `/open-code-review` + `/storm` on the design + spec.
      Iterate to clean. (OCR reviews the design/spec diff at this gate.)

## Phase 1 — implementation (follow-on lane, TDD)

- [ ] 1.1 `internal/tracker`: the `Issue`/`Draft`/`Query`/`Patch` types + the `Tracker`
      interface + the `registry` (`Register`/`Get`/`DefaultTracker`), mirroring `internal/surface`.
      Test: register a fake, `Get` resolves it; empty name → default; unknown → !ok.
- [ ] 1.2 `internal/tracker/github.go`: the `gh`-CLI strategy. Exec is injected (a
      `run func(args …string) ([]byte, error)` seam) so tests mock `gh` without a network/gh
      dependency. Map each op → `gh issue …` (`--json` parse for create/list; `edit`/`close`/
      `reopen` for update/close). Test each op against a fake `gh` exec returning canned JSON;
      assert the argv built and the `Issue` mapping; assert a non-zero `gh` exit → clear error.
- [ ] 1.3 `internal/tracker/linear.go` + `jira.go`: stub strategies that register and return
      `ErrNotImplemented` with a helpful message. Test: they resolve via `Get`; methods error clearly.
- [ ] 1.4 `internal/roster`: add the optional `tracker` field (kind string; `""`→`github`).
      Test: default; explicit; (no fail-closed at load — unknown is validated in cmd, like surface).
- [ ] 1.5 `cmd/flotilla`: `validateTracker(cfg)` (fail-closed on unknown, the
      `validateAgentSurfaces` precedent), called by the `issue` command before dispatch.
- [ ] 1.6 `cmd/flotilla/issue.go`: the `flotilla issue create|list|update|close` command +
      `case "issue"` dispatch. Reuse the `--file`/stdin body resolver; `--tracker` override
      (flag > roster > default). Test the arg parsing + dispatch against a fake `Tracker`
      (create prints URL; list formats one line/issue; update/close call the right method;
      `--tracker linear` → not-implemented error; bad/missing args → clear usage error).
- [ ] 1.7 Docs: a "Issue tracker" section in the quickstart (the `tracker` field, `flotilla
      issue`, the GitHub default + `gh` requirement, the Linear/Jira stub status).
- [ ] 1.8 Trio review on the implementation diff; iterate to clean; PR; CI green.

## Out of scope (documented decisions — design §8/§9)

- Direct-API GitHub strategy (decision 8.1) — future registry entry under its own key.
- Real Linear/Jira strategies — stubs only here.
- Issues-as-backlog-source / goal-loop wiring (decision 8.2) — separable, #104/future.
