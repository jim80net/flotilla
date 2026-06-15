## 1. cursor driver skeleton (full structure; INERT markers)

- [x] 1.1 `internal/surface/cursor.go`: `cursor` driver with injectable primitives; `Name()`→`cursor`; `Submit`→`deliver.Send`; `Assess`→(panecmd-err→Unknown; shell→Shell; capture-err→Unknown; else `parseCursorState`); **`Rotate`→`inject(pane,"/new-chat")` (Cursor's reset, NOT /clear — the 2nd non-/clear reset)**; `RotateStrategy`→SlashCommand; `init()`→`Register`. PROMINENT docstring: closed-source, markers PLACEHOLDER pending live-capture (#61), INERT until then.
- [x] 1.2 `parseCursorState` — claude-style ladder hypothesis (AwaitingApproval → Working → Idle-default) over the bottom non-empty chrome (reuse opencode helpers). Marker constants `cursorApprovalMarkers`/`cursorWorkingMarkers` are PLACEHOLDER sentinels matching no real render (INERT). Polarity is itself a #61 question.
- [x] 1.3 Tests: ladder structure (placeholder → AwaitingApproval/Working), **INERT proof** (realistic cursor output with no sentinel → Idle), bottom-chrome scoping, Assess routing (panecmd-err→Unknown; shell→Shell; capture-err→Unknown), Submit→deliver.Send, **Rotate→InjectSlash(`/new-chat`)**, RotateStrategy=SlashCommand, `Get("cursor")` ok.

## 2. config + docs

- [x] 2.1 Keep `workspace.IdentityFileName("cursor")` → `AGENTS.md` (docs-derived; CLI-honoring is a #61 live-capture confirm).
- [x] 2.2 Document `surface:"cursor"` as a valid roster selection (README + example roster) WITH the caveats: closed-source, markers placeholder/INERT until the operator-present live-capture (#61).
- [x] 2.3 Test/confirm: a roster with `surface:"cursor"` passes `validateAgentSurfaces`; `surface:"nope"` still errors.

## 3. follow-up (the ONLY remaining step — operator-present)

- [x] 3.1 File the live-capture tracking issue (#61, sibling of grok's #58): the operator-present session that fills `cursorApprovalMarkers`/`cursorWorkingMarkers` + the test fixtures from observed render, confirms the polarity, and flips the driver INERT→live. Referenced in the docstring + design.

## 4. review + ship (the SKELETON gate)

- [ ] 4.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
- [ ] 4.2 `/systems-review` AND `/open-code-review` in parallel on the skeleton diff; resolve findings.
- [ ] 4.3 PR referencing this change; CI green. **HOLD MERGE** — this skeleton merges only after the live-capture (#61) fills the markers and flips the driver live. Report ready for its gate + the scheduled-session need to the XO.

> **Merge gate:** the skeleton is built + gated now (free authorized work). It merges
> only after the operator-present live-capture (#61) replaces the placeholder markers,
> confirms the polarity, and flips the driver from INERT to live.
