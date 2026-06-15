## 1. opencode state classifier (pure, tail-scoped, CLAUDE-STYLE polarity — the core)

- [x] 1.1 `internal/surface/opencode.go`: `parseOpenCodeState(captured string) State` — bottom-region scan (mirror `deliver.ParseBusy` busy.go:42-44). Claude-style precedence: AwaitingApproval (`Permission required` / `Allow once` / footer `△`…`Permission`) → Errored (`A fatal error occurred!`) → Working (`esc interrupt` / `esc again to interrupt` / `[⋯]` / `[retrying `) → **Idle (the DEFAULT — safe because the working marker persists the whole non-idle duration, prompt/index.tsx:1504)**. Do NOT rely on the animated spinner glyph.
- [x] 1.2 Tests (table-driven, EVERY branch): each approval marker (incl. wrapped/scrolled) → AwaitingApproval; each working marker → Working; retry → Working; fatal banner → Errored; no-marker idle composer → Idle; approval co-rendered with working hint → AwaitingApproval (precedence); stale marker in scrollback + clean bottom → correct (tail-scoping); **torn/partial working frame** (a successful capture mid-repaint where the `esc interrupt` line is absent) — document the expectation (this is the generic scrape-torn-frame risk shared with claude-code, which has the same no-Working→Idle-debounce; the live-capture task confirms the `esc interrupt` line is emitted atomically by `capture-pane` since it is a single text node).

## 2. opencode driver

- [x] 2.1 `internal/surface/opencode.go`: `openCode` driver with injectable primitives (paneCommand/isShell/capturePane/classify/send/inject); `Name()`→`opencode`; `Submit`→`deliver.Send`; `Assess`→(panecmd-err→Unknown; shell→Shell; **capture-err→Idle** (claude-style fail-open — safe under Working-positive polarity); else `parseOpenCodeState`); `Rotate`→`inject(pane,"/clear")` (alias for session.new, app.tsx:573); `RotateStrategy`→SlashCommand; `init()`→`Register`.
- [x] 2.2 Tests: `Assess` table (panecmd-err→Unknown; shell→Shell; capture-err→Idle; classifier routing) with real `parseOpenCodeState`; Submit→deliver.Send; Rotate→InjectSlash(`/clear`); RotateStrategy=SlashCommand; `Get("opencode")` ok.

## 3. config + docs

- [x] 3.1 `workspace.IdentityFileName("opencode")` → `AGENTS.md` (OpenCode's native instruction file, instruction.ts:61,65) + test (extend TestIdentityFileName).
- [x] 3.2 Document `surface:"opencode"` as a valid roster selection (README + example roster); note the $0 local-model (Ollama via opencode.json baseURL) build/validate path.
- [x] 3.3 Test/confirm: a roster with `surface:"opencode"` passes `validateAgentSurfaces`; `surface:"nope"` still errors.

## 4. live-capture confirmation ($0 — local ollama)

- [x] 4.1 Run an OpenCode desk in a tmux pane against a local model (Ollama via `opencode.json` `provider.ollama.options.baseURL=http://localhost:11434/v1`), zero spend; capture real pane frames for idle / working / awaiting-approval (incl. the `$ <command>` bash permission) / errored / crash-to-shell.
- [x] 4.2 Confirm/lock the §1 fixtures against the captured frames — the approval anchors (`Permission required` / `Allow once` / footer counter), the working anchors (`esc interrupt` / `[⋯]` / `[retrying `; confirm the spinner is NOT needed), and that no-marker → Idle holds with no false "finished" mid-stream. Confirm `pane_current_command` is the OpenCode process (exec-as-pane-process rule), the `/clear` rotate, and multi-line bracketed-paste Submit. Record the captures as the fixtures of record (cite in the test file).
  - **PARTIAL — honest result** (opencode v1.3.15 RELEASED build vs survey HEAD; $0 local ollama): LIVE-VALIDATED idle / working (`esc interrupt` persists whole turn → claude-style polarity confirmed) / idle-after-turn / `pane_current_command=opencode` (IsShell=false) / spinner-not-relied-upon / $0-on-ollama. AwaitingApproval (`Permission required`/`Allow once`, permission.tsx:391,407) + Errored (`A fatal error occurred!`) are SOURCE-VERIFIED (re-confirmed) but NOT live-elicited — local 1.5b/7b ollama didn't reliably tool-call (printed tool-call JSON as text) and the CPR-dependent capture client ended the session first. Follow-up: live-elicit the permission dialog with a tool-calling model + permission:ask (low risk; cited in opencode_test.go provenance block).

## 5. review + ship

- [x] 5.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
- [ ] 5.2 `/systems-review` AND `/open-code-review` in parallel on the implementation diff; resolve findings.
- [ ] 5.3 PR referencing this change; CI green; merge on clean gates (flotilla has no cubic — systems-review + open-code-review are the gates of record). Checkpoint the XO at merge.
