## 1. grok state classifier (pure, tail-scoped, CLAUDE-STYLE, REDUCED set — the core)

- [x] 1.1 `internal/surface/grok.go`: `parseGrokState(captured string) State` — last-non-empty bottom-chrome scan (like opencode.go). Claude-style precedence: AwaitingApproval (`Payment required` / `Paste your xAI API key`) → Working (`Planning next moves` / `enter queue` / `esc interrupt`) → **Idle (DEFAULT)**. Do NOT rely on the animated spinner `⬒⬔⬓⬕`. Do NOT key on the Plan-mode generic `Confirm`. NO Errored branch (grok renders transient errors inline in the conversation scrollback — unreachable by the bottom scan + stale-prone; auth errors → AwaitingApproval via the api-key modal, other transient errors → the Working→Idle wake; systems-review).
- [x] 1.2 Tests (table-driven, EVERY branch): each approval marker → AwaitingApproval; each working marker → Working; idle composer → Idle; **auto-execute case** (a tool running, no approval marker) → Working/Idle NOT AwaitingApproval (proves the reduced set); a transient error in the conversation scrollback + idle composer below → Idle (no Errored); an auth error → AwaitingApproval (api-key modal); model output quoting a marker high up → not misled (bottom-chrome scoping).

## 2. grok driver

- [x] 2.1 `internal/surface/grok.go`: `grok` driver with injectable primitives; `Name()`→`grok`; `Submit`→`deliver.Send`; `Assess`→(panecmd-err→Unknown; shell→Shell; capture-err→Unknown; else `parseGrokState`); **`Rotate`→`inject(pane,"/new")` (NOT /clear — Grok's reset is /new, slash-menu.ts:19)**; `RotateStrategy`→SlashCommand; `init()`→`Register`. PROMINENT docstring: source-verified-NOT-live-captured (commit fb97af8) + Grok AUTO-EXECUTES shell/edits unprompted.
- [x] 2.2 Tests: `Assess` table (panecmd-err→Unknown; shell→Shell; capture-err→Unknown; classifier routing); Submit→deliver.Send; **Rotate→InjectSlash(`/new`)** (the first non-/clear reset — validates the Phase-2 generalization); RotateStrategy=SlashCommand; `Get("grok")` ok.

## 3. config + docs

- [x] 3.1 Verify + keep `workspace.IdentityFileName("grok")` → `AGENTS.md` (now source-verified, instructions.ts:39,50); the existing test case stays.
- [x] 3.2 Document `surface:"grok"` as a valid roster selection (README + example roster) WITH the two caveats: source-verified-not-live-captured, and Grok auto-executes shell/edits unprompted (a fleet-operator hazard).
- [x] 3.3 Test/confirm: a roster with `surface:"grok"` passes `validateAgentSurfaces` (it already did at Phase 1 via the registry; now a real driver backs it); `surface:"nope"` still errors.

## 4. follow-up

- [x] 4.1 File a tracking issue (mirror OpenCode's #54) for LIVE-CAPTURE validation of the whole grok driver, pending an operator-funded xAI session — and pin the markers to the released grok-dev build at that time. Reference it in the driver docstring + tasks.

## 5. review + ship

- [x] 5.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
- [ ] 5.2 `/systems-review` AND `/open-code-review` in parallel on the implementation diff; resolve findings.
- [ ] 5.3 PR referencing this change; CI green; merge on clean gates (systems-review gate of record; OCR if it doesn't hang). Checkpoint the XO at merge.
