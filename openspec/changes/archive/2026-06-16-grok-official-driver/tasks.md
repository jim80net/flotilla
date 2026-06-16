## 1. Rework the grok driver for xAI's official grok CLI (#58, A)

- [x] 1.1 Investigate the deployed grok: confirm capture-pane is NOT blank (premise refuted); identify the binary (`~/.grok/bin/grok`, official "Grok Composer 2.5 Fast", not superagent grok-dev); confirm 0/5 old markers match live; correct the #58 issue.
- [x] 1.2 LIVE-CAPTURE the official grok render states: Working = `⇣` (U+21E3) + braille spinner (U+2801–28FF), present throughout a turn, absent idle (`Turn completed in …`); reset `/new` (slash menu).
- [x] 1.3 Rewrite `parseGrokState` + markers + driver docs for the official grok; Working-positive/Idle-default; no AwaitingApproval yet (documented gap + liveness caveat).
- [x] 1.4 Rewrite `grok_test.go` with the live-measured renders; pin the arrow branch AND the spinner branch independently; regression: a `Note…` prose ellipsis in an idle tail → Idle.
- [x] 1.5 Re-anchor the stale grok-dev citation in `workspace.go` (AGENTS.md for official grok).
- [x] 1.6 `gofmt`/`go vet`/`go build`/`go test -race ./...` green.

## 2. Spec + review + ship

- [x] 2.1 Rewrite the `surface` spec grok requirement (grok-dev → official grok); `openspec validate --strict`.
- [x] 2.2 `/systems-review` + `/open-code-review` on the diff; resolve findings (P2 over-broad gerund → braille spinner; arrow-branch test; spec staleness; liveness-note + workspace.go citation).
- [ ] 2.3 PR #77 — CI green; XO review + merge. Archive this change.

> #58 part B (the full-result reader via the `~/.grok` session store) is the next, separate unit.
> The official grok's AwaitingApproval gate markers + multi-line submit are tracked follow-ups.
