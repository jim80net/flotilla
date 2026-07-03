# Design — Codex surface driver v1

**Status:** Ready for implementation trio. Login-screen fixtures LIVE-CAPTURED 2026-07-02;
in-session markers sourced from codex-cli 0.142.5 binary strings (post-auth revalidation required).
**Reference:** `internal/surface/grok.go` + `grokstore` (#77 driver rework, #79 result reader).

## 1. Objective

Register surface `codex` so OpenAI Codex CLI desks run at the same flotilla tier as grok: state
assessment, confirmed-delivery submit, launch-recipe integration, AGENTS.md doctrine, and rollout
result reader.

## 2. State classifier (`parseCodexState`)

Claude-style polarity: **Working-positive, Idle-default.** Order: launcher/login → approval →
working → idle.

| State | Anchors | Provenance |
|---|---|---|
| `AwaitingInput` (login) | `Welcome to Codex` + `Sign in with ChatGPT` | LIVE tmux capture 2026-07-02 |
| `AwaitingInput` (hooks) | `Hooks need review` + `Press enter to continue` | binary 0.142.5 |
| `AwaitingApproval` | `[ ! ] Action Required` or `[ . ] Action Required`; `main needs approval`; `Approve for me` | binary 0.142.5 |
| `Working` | ` to interrupt` (footer chrome); `while a task is in progress`; `Waiting for background terminal` | binary 0.142.5 |
| `Idle` | default (no working/approval/login chrome in tail) | — |
| `Shell` | `deliver.IsShell` on pane command | shared |

Tail scan: last 12 non-empty lines (same as grok/opencode).

**Post-auth gap:** working/approval markers are NOT yet live-validated on a logged-in desk.
Re-capture after operator auth before treating v1 markers as closed.

## 3. Submit + confirmed delivery

- Submit: `deliver.Send` (bracketed paste + Enter) — same as grok/opencode.
- **No ComposerStateProbe v1** — `Confirm.Submit` falls back to Working-spinner corroboration
  (`confirm.go` contract). Add ComposerStateProbe in a follow-up once idle/pending composer is
  live-captured.

## 4. Launch recipe + permission bounding

```text
codex -m gpt-5.5-codex --sandbox workspace-write --ask-for-approval on-request
```

Codex loads `AGENTS.md` from cwd natively — no `--append-system-prompt-file` equivalent needed
(worktree `AGENTS.md` from `flotilla workspace init`).

**No-self-merge:** scaffold `<worktree>/.codex/rules/flotilla-desk.rules` with `forbidden`
`prefix_rule` entries for `git merge`, `git push`, `gh pr merge` ([rules docs](https://developers.openai.com/codex/rules)).
Project `.codex/` loads when the project is trusted; flotilla desks operate in trusted worktrees.

## 5. Result reader (`codexstore`)

Rollout JSONL format (from openai/codex `recorder_tests.rs`):

- `{"type":"event_msg","payload":{"type":"agent_message","message":"..."}}`
- `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"..."}]}}`

Resolution: glob `~/.codex/sessions/*/*/*/rollout-*.jsonl` (Go `filepath.Glob` — no `**`
globstar; four single-segment wildcards for YYYY/MM/DD), match `session_meta.payload.cwd` to
pane cwd, pick latest rollout by filename timestamp. `LatestResult` / `ReplyAfter` mirror
`grokstore`.

## 6. RecycleBridge

Portable-markdown handoff at `<cwd>/.flotilla/handoffs/recycle-<token>.md` — same shape as grok
(#158). HandoffTurn forbids git commit; TakeoverTurn deletes handoff after read (#218).

## 7. Rotate / Close

- Rotate: inject `/clear` (documented slash command — fresh chat).
- Close: `ErrNoGracefulClose` (honest refusal until `/exit` or equivalent is live-verified).