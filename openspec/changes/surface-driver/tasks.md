## 1. surface package: interface + registry + guard

- [ ] 1.1 `internal/surface/surface.go`: `State` enum (Unknown/Shell/Working/Idle/AwaitingInput/AwaitingApproval/Errored), `Strategy` enum (SlashCommand/RestartProcess), `Driver` interface (Name/Submit/Assess/Rotate/RotateStrategy), registry (`Register`, `Get(name)` default `claude-code`)
- [ ] 1.2 `RotateContext(d, pane)` helper + `ErrRestartRequired`: RestartProcess ⇒ ErrRestartRequired (NO injection); SlashCommand ⇒ d.Rotate
- [ ] 1.3 Tests: registry default/known/unknown; **RotateContext guard** — stub RestartProcess driver records calls, asserts ZERO injection + ErrRestartRequired (the XO-mandated invariant)

## 2. claude-code reference driver

- [ ] 2.1 `internal/deliver`: re-add `ClearContext` + `clearKeysArgs` (literal `send-keys -l -- /clear` then Enter — the live-verified method from closed #18) + arg test
- [ ] 2.2 `internal/surface/claude.go`: `claudeCodeDriver` — Submit→deliver.Send; Assess→(PaneCommand/IsShell→Shell; CapturePane err→Idle; parseBusy→Working/Idle); Rotate→deliver.ClearContext; RotateStrategy→SlashCommand; register as `claude-code`
- [ ] 2.3 Tests: Assess table (shell/working/idle/capture-err→idle/panecmd-err→shell parity with old logic); Submit routes through deliver.Send; Rotate issues /clear

## 3. roster surface field

- [ ] 3.1 `roster.Agent.Surface` (`json:"surface,omitempty"`)
- [ ] 3.2 Tests: parse absent→""(→claude-code at resolve); explicit value carried

## 4. route call sites (byte-identical for claude-code)

- [ ] 4.1 `cmd/flotilla` send: resolve driver from agent.Surface; `driver.Submit` instead of `deliver.Send`; startup surface validation (unknown→error) for all agents
- [ ] 4.2 `internal/watch` injector: wire SendFunc to resolve the target agent's driver + Submit
- [ ] 4.3 `internal/watch` gate: replace inline PaneCommand/IsShell/Busy with `driver.Assess(pane)` (crashed=Shell, busy=Working), preserving wd.Observe semantics
- [ ] 4.4 Tests: fake driver records Submit/Assess; assert send + watch route through the agent's driver; regression — a default roster behaves identically

## 5. review + ship

- [ ] 5.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green
- [ ] 5.2 `/systems-review` on the diff; address findings
- [ ] 5.3 PR referencing this change; CI + cubic green; enumerate cubic inline findings; report merge-ready
