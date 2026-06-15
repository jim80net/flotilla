## 1. deliver: generalize reset injection (claude byte-identical)

- [ ] 1.1 `internal/deliver/tmux.go`: add `InjectSlash(target, cmd string) error` — the current `ClearContext` body parameterized on `cmd` (literal `send-keys -l -- <cmd>`, compose delay, Enter, under the per-pane lock). Re-express `ClearContext(target)` as `InjectSlash(target, "/clear")`. Re-document both as surface-agnostic.
- [ ] 1.2 Tests: `InjectSlash` arg test for an arbitrary cmd; `ClearContext` still issues exactly `/clear` then Enter (claude-code byte-identity regression).

## 2. aider state classifier (pure, tail-scoped, IDLE-POSITIVE — the core)

- [ ] 2.1 `internal/surface/aider.go`: `parseAiderState(captured string) State` — scope scan to the bottom ~12 lines (mirror `deliver.ParseBusy` busy.go:42-44). **Idle-positive** precedence ladder: AwaitingApproval (`(Y)es/(N)o`) → Idle (last non-empty line is a recognized prompt `> `/`ask> `/`architect> `/`multi> `) → Errored (known `exceptions.py` phrase / `An uncaught exception occurred:` AND last line not a prompt) → **Working (the DEFAULT — mid-stream/streaming/`Retrying in `/`Waiting for `, anything not at a prompt)**. Do NOT key Working on the ambiguous shading glyphs or the `\r`-rewritten spinner.
- [ ] 2.2 Tests (table-driven, EVERY branch incl. the polarity + tail-scoping proofs): approval at bottom→AwaitingApproval; returned `> ` prompt→Idle; **stale approval/error in scrollback + `> ` last line→Idle**; **mid-stream (no prompt, no marker)→Working** (the polarity-fix regression); `Retrying in `→Working; live error phrase (no prompt)→Errored; error phrase WITH `> ` below→Idle (recovered).

## 3. aider driver

- [ ] 3.1 `internal/surface/aider.go`: `aider` driver with injectable primitives (paneCommand/isShell/capturePane/classify/send/inject); `Name()`→`aider`; `Submit`→`deliver.Send`; `Assess`→(panecmd-err→Unknown; shell→Shell; **capture-err→Unknown** (never a false finish); else `parseAiderState`); `Rotate`→`inject(pane,"/clear")`; `RotateStrategy`→SlashCommand; `init()`→`Register`.
- [ ] 3.2 Tests: `Assess` table (panecmd-err→Unknown; shell→Shell; **capture-err→Unknown**; classifier routing) with real `parseAiderState`; Submit→deliver.Send; Rotate→InjectSlash(`/clear`); RotateStrategy=SlashCommand; `Get("aider")` ok.

## 4. live-capture confirmation ($0 — local ollama)

- [ ] 4.1 Run an aider desk in a tmux pane against `ollama_chat/<local-model>` (zero metered spend); capture real pane frames for each state (idle / working / awaiting-approval via `Add file…?`/`Run shell command?` / errored / crash-to-shell).
- [ ] 4.2 Confirm or correct the §2 fixtures against the captured frames — above all the **prompt (Idle) anchor regex** (the load-bearing positive marker). Resolve the known soft spots: multi-line bracketed paste lands as literal newlines in aider's composer, including a body whose own line is exactly `}` or starts with `{` (aider's text-marker multiline, io.py:694-727, must not swallow it); and that `/clear` injection lands with the reused `clearComposeDelay` (tmux.go:48). Record the captures as the fixtures of record (cite them in the test file).

## 5. materiality end-to-end (no watch code change)

- [ ] 5.1 Test (`internal/watch`): a snapshot transition into `AwaitingApproval` / `Errored` for a non-XO desk yields a material wake reason ("entered awaiting-approval" / "entered errored") — proves the dormant gate (materiality.go:24-32) is now live via a driver that emits the states.

## 6. config + docs

- [ ] 6.1 Document `surface:"aider"` as a valid roster selection (README/roster docs); document the local-ollama $0 build/validation path and that a paid-model aider desk is an operator spend choice.
- [ ] 6.2 Test/confirm: a roster with `surface:"aider"` passes startup validation (watch.go:66) and resolves the aider driver; `surface:"nope"` still errors.

## 7. review + ship

- [ ] 7.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
- [ ] 7.2 `/systems-review` AND `/open-code-review` in parallel on the implementation diff; resolve findings.
- [ ] 7.3 PR referencing this change; CI green; report merge-ready (flotilla has no cubic — systems-review + open-code-review are the gates of record).

> **Build gate:** this change is DESIGN-ONLY until the operator authorizes the
> build at the Phase-2 checkpoint (it touches the harness abstraction). Tasks
> 1-7 execute only after that authorization.
