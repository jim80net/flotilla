# Tasks — codex-surface-driver

## 1. Openspec + design
- [x] 1.1 proposal.md + design.md + surface spec delta
- [x] 1.2 Design trio (systems-review + open-code-review)

## 2. Surface driver
- [x] 2.1 `internal/surface/codex.go` — driver + parseCodexState
- [x] 2.2 `internal/surface/codex_test.go` — fixtures (login live-captured; working/approval binary-sourced)

## 3. Result reader
- [x] 3.1 `internal/codexstore/codexstore.go` — LatestResult + ReplyAfter
- [x] 3.2 `internal/codexstore/codexstore_test.go` — rollout JSONL fixtures

## 4. Launch + doctrine wiring
- [x] 4.1 `workspace.IdentityFileName` + `workspaceLaunchCommand` for codex
- [x] 4.2 `harnessLaunchWired` includes codex
- [x] 4.3 Scaffold `.codex/rules/flotilla-desk.rules` on workspace init
- [x] 4.4 Registry + workspace tests

## 5. Ship
- [x] 5.1 `go test ./...`
- [x] 5.2 Implementation trio
- [ ] 5.3 PR surfaced to COS (no self-merge)

## Post-auth follow-ups (operator gate)
- [ ] Live-capture working/idle/approval/composer fixtures after codex login
- [ ] ComposerStateProbe for confirmed delivery (spinner-only v1)
- [ ] Revalidate binary-sourced in-session markers on logged-in desk