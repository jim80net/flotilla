# Public landing and docs claim-to-source record

Artifact binding: the immutable Git commit containing this record. The handoff
names that commit and its `site` tree object.

| Public claim | Source at the bound artifact | Verification |
|---|---|---|
| Flotilla coordinates existing coding-agent sessions instead of replacing them | `README.md`; `internal/surface/surface.go`; registered drivers under `internal/surface/` | `go test ./...`; command help lists the shipped surfaces and lifecycle |
| The dashboard shows Conversations, Goals, Issues, Parade, and R&D | `internal/dash/assets/index.html`; `internal/dash/assets/app.js` | Real dashboard capture at the bound source; phone and desktop render |
| Six registered drivers: Claude Code, Codex, Grok, OpenCode, Pi, and aider | driver registration under `internal/surface/`; `cmd/flotilla/main.go` startup validation | `go test ./internal/surface ./cmd/flotilla` |
| Automatic rate-limit switching is scoped to eligible Claude Code sessions | `internal/watch/detector.go`; `cmd/flotilla/watch_autoswitch.go`; `cmd/flotilla/switch.go`; negative fixtures in `internal/watch/detector_test.go` | `go test ./internal/watch ./cmd/flotilla` |
| Discord is optional for local send, status, clock, and dashboard use | `docs/quickstart.md`; mirror branch in `cmd/flotilla/main.go`; local dash and watch commands | Cold install/status/send fixture runs without Discord credentials |
| tmux is external and required for pane delivery | `docs/quickstart.md`; `internal/deliver`; `internal/surface` | Cold fixture registers a tmux pane and reaches the durable send path |
| Memex is external and optional | `cmd/flotilla/switch.go` writes only `MemexInjectionHint`; `cmd/flotilla/switch_test.go` validates pointer-only handoff; `docs/harness-subscription-switching.md` states that the handoff succeeds without a consumer | Full tests pass without a Memex dependency in `go.mod`; cold install and send do not use Memex |
| Commands on the task-organized reference exist | dispatch in `cmd/flotilla/main.go`; command implementations under `cmd/flotilla/` | Cold-built `flotilla help` contains every documented command family |

No capability in the public docs is inferred from the dashboard fixture data.
The fixture demonstrates layout and product surfaces only.
