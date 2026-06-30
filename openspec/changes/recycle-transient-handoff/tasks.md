# Tasks — recycle transient handoff (#212)

- [x] 1. TEST FIRST (`internal/surface/recycle_test.go`): the claude + grok `TakeoverTurn` each contain
  the removal step (`git rm` + the `drop transferred handoff` commit message) AND instruct READ before
  the removal (read → remove → work), so the fresh session has the content before it deletes the file.
- [x] 2. Implement the read-then-remove step in `claudeCode.TakeoverTurn` + `grok.TakeoverTurn`
  (`internal/surface/claude.go`, `grok.go`); update the `RecycleBridge.TakeoverTurn` doc
  (`internal/surface/recycle.go`).
- [x] 3. `go build ./...` + `go test ./internal/surface/...` + `go vet` + `gofmt -l` clean.
- [ ] 4. `openspec validate recycle-transient-handoff --strict` green; review gate (systems-review +
  open-code-review); PR referencing #212; CI green.
