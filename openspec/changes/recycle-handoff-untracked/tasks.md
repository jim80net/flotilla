## 1. Deliver layer (filesystem durability)

- [x] 1.1 `internal/deliver/recycle.go`: `HandoffDurable` / `HandoffAbsentAtHead` use `os.Stat`
  (validate path under cwd); remove git HEAD checks.
- [x] 1.2 `internal/deliver/recycle_test.go`: disk-based durability tests including non-git cwd.

## 2. Surface turns

- [x] 2.1 `internal/surface/claude.go` + `grok.go`: `HandoffTurn` forbids git commit; writes
  untracked only. `TakeoverTurn` read → `rm -f` → work.
- [x] 2.2 `internal/surface/recycle.go`: update `RecycleBridge` interface docs.
- [x] 2.3 `internal/surface/recycle_test.go`: assert no `git add`/`git commit`/`git rm`; assert
  untracked + `rm -f` + read-before-delete ordering.

## 3. Commands + docs

- [x] 3.1 `cmd/flotilla/recycle.go` + `switch.go`: absent→present copy; drop git-work-tree gate
  messaging.
- [x] 3.2 `cmd/flotilla/recycle_test.go` + `switch_test.go`: baseline-error refusal case.
- [x] 3.3 `docs/watch-runbook.md`: untracked handoff + disk durability gate.

## 4. Verification

- [x] 4.1 `go test -race ./internal/deliver/... ./internal/surface/... ./cmd/flotilla/...`
- [x] 4.2 `scripts/check-private-boundary.sh`