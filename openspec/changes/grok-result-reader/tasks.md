## 1. `internal/grokstore` — the session-store reader (TDD, the testable core)

- [x] 1.1 TEST `LatestResult(grokHome, cwd)` against a temp fixture store: an `active_sessions.json`
      ([{session_id, pid, cwd, opened_at}]) + `sessions/<encoded-cwd>/<session-id>/chat_history.jsonl`
      with mixed entries → returns the LAST `type:"assistant"` entry's full content. Match the
      session by cwd; glob `sessions/*/<session-id>/chat_history.jsonl` (no cwd re-encoding).
- [x] 1.2 TEST errors: no active session for cwd → clear error; session dir/file missing → error; no
      assistant entry yet → error; malformed jsonl line → skipped, not a panic (read the last VALID
      assistant entry). Never panics.
- [x] 1.3 IMPL `internal/grokstore.LatestResult(grokHome, cwd string) (string, error)`: read
      active_sessions.json → session_id by cwd; locate the session's chat_history.jsonl; return the
      last assistant `content`.

## 2. `deliver.PaneCWD` + the `ResultReader` interface + grok impl

- [x] 2.1 TEST `deliver.paneCWDArgs(pane)` argv (pure) = `display-message -p -t <pane> #{pane_current_path}`.
- [x] 2.2 IMPL `deliver.PaneCWD(pane) (string, error)` (bounded by commandTimeout).
- [x] 2.3 IMPL `surface.ResultReader` interface (`LatestResult(pane string) (string, error)`); the
      grok driver implements it (PaneCWD → grokstore.LatestResult with grokHome=~/.grok). Injectable
      seams (paneCWD, grokHome) so it is unit-testable without tmux. TEST the grok impl with stubs.

## 3. `flotilla result <agent>` command

- [x] 3.1 IMPL `cmd/flotilla` `result <agent>`: resolve agent → driver + pane; if driver implements
      `ResultReader`, print `LatestResult`; else report "surface has no session-store reader (use the
      pane capture)". Read-only (never writes a pane). Register in the command dispatch + usage.
- [x] 3.2 VERIFIED end-to-end (runtime path, read-only): `flotilla result grok-research` printed the full
      live result from ~/.grok; a claude-code desk reported the no-reader message, no pane write.

## 4. review + ship

- [x] 4.1 `gofmt`/`go vet`/`go build ./...`/`go test -race ./...` green; `openspec validate --strict`.
- [ ] 4.2 `/systems-review` + `/open-code-review` in parallel on the diff; resolve.
- [ ] 4.3 PR referencing this change (#58 part B); CI green; XO review + merge. Archive.

> Batched deploy: the XO rebuilds + restarts ONCE after this lands (the grok driver from part A AND
> this reader go live together — no per-PR heartbeat-clock restart).
