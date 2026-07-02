# Tasks — unacked-operator-backstop-234 (#234, TDD)

## 1. Pure scan
- [x] 1.1 TEST FIRST (`internal/unacked/unacked_test.go`): MinAge gate, no-reply, webhook ack, working-only, trivial skip, MinAge ≥ scan interval.
- [x] 1.2 Implement `Scan`, `Config`, defaults, `classify.go` heuristics.

## 2. Watch poller + state
- [x] 2.1 TEST FIRST (`internal/watch/unacked_test.go`): 7d prune, alert-once, busy wake retry.
- [x] 2.2 Implement `UnackedBackstop`, `unacked_state.go` with bounded dedup file.

## 3. Transport + daemon wiring
- [x] 3.1 Add `transport.RecentHistory` + discord adapter; `transportRecentReader` bridge.
- [x] 3.2 Wire in `cmd/flotilla/watch.go`: `--unacked-file`, start goroutine when relay prerequisites met.

## 4. Gate
- [ ] 4.1 `go test -race ./...` green; `openspec validate unacked-operator-backstop-234 --strict` green; partition grep clean.
- [ ] 4.2 Impl-trio + PR to operator/COS merge gate.