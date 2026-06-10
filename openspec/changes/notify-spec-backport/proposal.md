## Why

`flotilla` ships three commands that touch Discord — `send`, `watch`, and
`notify` — but only `send` and `watch` have a capability spec under
`openspec/specs/`. `notify` is the **operator-facing outbound path** (the XO
posts the operator's executive briefs through it), and it has subtle, hard-won
invariants that exist only in `cmd/flotilla/main.go` and its tests today:

- it posts **directly to the operator**, under the sending agent's own webhook
  identity, and (unlike `send`) injects into **no tmux pane**;
- its body is the **operator-facing content**, so an over-length message is
  **rejected with nothing posted** — the exact opposite of the best-effort audit
  mirror, which silently *clamps* with an ellipsis;
- the 2000-char limit is counted in **runes** (multi-byte safe);
- the webhook URL is a credential that must **never** appear in an error.

Leaving these unspecified is the same institutional-knowledge gap the `send` and
`watch` backports closed: the behavior is real, tested, and load-bearing, but a
future change could regress it with no spec to catch the drift. Closes #15.

## What Changes

- Add a `notify` capability spec (`openspec/specs/notify/spec.md` on archive)
  that **backports the shipped v0** behavior of `flotilla notify`
  (`cmd/flotilla/main.go:cmdNotify`, `internal/{discord,roster}`), mirroring the
  style of the existing `send`/`watch` backport specs.
- **No code change.** Every requirement is a backport of already-shipped,
  already-tested behavior (`cmd/flotilla/notify_test.go` — 10 tests covering the
  webhook-identity, over-length-reject, at-limit, rune-count, file-body,
  missing-webhook, required-`--from`, empty-message, missing-secrets, and
  flag-after-message cases).

## Capabilities

### Added Capabilities
- `notify`: the operator-facing outbound path — an agent posts a message
  directly to the operator on Discord under its own webhook identity, with no
  tmux involvement, and with over-length bodies rejected (not truncated) so the
  operator always sees the whole message.

## Impact

- **Spec:** new `openspec/specs/notify/spec.md` (added on archive). Brings the
  spec set to parity with the shipped command surface (send / watch / notify).
- **Code:** none — documentation/spec-only. The spec is verified against the
  existing implementation and its test suite; no behavior changes.
- **Docs:** the `notify` flags + semantics are already documented in
  `flotilla help`; this change adds the durable capability spec behind them.
