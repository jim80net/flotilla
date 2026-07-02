# Proposal — codex surface driver (OpenAI Codex CLI fleet desks)

## Why

flotilla's execution tier today is grok (`internal/surface/grok.go`). The operator directed
Codex CLI desks (GPT-5.5-codex) as a peer harness. Without a `codex` surface driver, roster
agents with `surface: "codex"` fail `surface.Get` at startup and cannot be driven by watch/send.

## What Changes

- **`internal/surface/codex.go`** — surface driver: Assess (login/launcher, working, idle,
  awaiting-approval, shell), Submit, Rotate (`/clear`), Close (`ErrNoGracefulClose` until live-
  verified), RecycleBridge, ResultReader, ReplyReader.
- **`internal/codexstore/`** — read latest completed turn from `~/.codex/sessions/**/rollout-*.jsonl`
  keyed by pane cwd (mirrors `grokstore`).
- **`cmd/flotilla/workspace.go`** — codex launch recipe (`codex -m gpt-5.5-codex --sandbox
  workspace-write --ask-for-approval on-request`) + scaffold `.codex/rules/flotilla-desk.rules`
  forbidding `git merge` / `git push` / `gh pr merge` (execution-desk permission bounding).
- **`internal/workspace/workspace.go`** — `codex` → `AGENTS.md` identity convention.
- **`cmd/flotilla/doctrine.go`** — wire codex into `harnessLaunchWired` (AGENTS.md loads natively).

## Ground truth (2026-07-02)

- codex-cli **0.142.5** on PATH; **not logged in** on this host — login-screen fixtures are live-
  captured; in-session working/idle/approval fixtures are **binary-sourced + post-auth follow-up**.
- Session store: `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl` + `state_*.sqlite` index.
- AGENTS.md: [Codex docs](https://developers.openai.com/codex/guides/agents-md) — native cwd walk.
- Approvals: `--ask-for-approval on-request` + `--sandbox workspace-write`; deny via `.rules`
  `prefix_rule(..., decision = "forbidden")`.

## Out of scope

- ComposerStateProbe (confirmed delivery uses Working-spinner fallback until post-auth live capture).
- Live-session fixture refresh (blocked on operator auth).
- memex-codex adapter (sibling desk `codex-memex-dev`).

## Impact

- `internal/surface/`, `internal/codexstore/`, `cmd/flotilla/workspace.go`, `internal/workspace/`,
  registry tests, `flotilla.example.json`.