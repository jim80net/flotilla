## 1. Marker-aware resolution (internal/deliver)

- [x] 1.1 Add the `@flotilla_agent` per-pane user-option to the `list-panes` format (`<target>\t<title>\t<marker>`).
- [x] 1.2 `parsePane` two-tier precedence: marker match (authoritative; >1 → ambiguity error) → title fallback (exact/single-glyph; 0 → error; >1 → ambiguity). Robust to a missing marker field.
- [x] 1.3 `TagPane(target, key)` — `tmux set-option -p @flotilla_agent <key>`.
- [x] 1.4 Tests: marker resolves a drifted title; marker wins over another pane's title; duplicate marker → ambiguous; untagged fleet → title fallback; empty marker never matches; existing title cases unchanged.

## 2. The register command (cmd/flotilla)

- [x] 2.1 `flotilla register <agent> [--pane <target>]` → load roster, look up the agent, `TagPane(pane, agent.Title())`. Default pane `$TMUX_PANE`.
- [x] 2.2 Accept the agent positional before OR after the flags (Go's flag parser stops at the first positional; the migration form is `register <name> --pane <target>`). Pure `parseRegisterArgs` helper.
- [x] 2.3 Dispatch + usage in `main.go`.
- [x] 2.4 Tests: `parseRegisterArgs` both orderings, `=flag` form, default pane, no-agent/empty/extra-positional errors.

## 3. Docs + verify

- [x] 3.1 quickstart (launch recipe: `flotilla register <name>`) + watch-runbook (the marker, the `--pane` migration for drifted desks).
- [x] 3.2 gofmt/vet/build/`go test -race ./...` green; live end-to-end smoke (register → drift title → resolve+deliver by marker); openspec --strict valid.
- [~] 3.3 /systems-review on the diff (done — no P1; 1 P2 doc + 3 defensive P3s fixed: parsePane suppression-doc reword, roster tab/newline rejection, titleMatchesName empty-want guard, TagPane `--` guard + read-back verification; all unit-tested + live-smoked incl. dash-leading tmux_title); PR (CI + cubic; enumerate inline); merge-ready.
