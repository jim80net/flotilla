# Tasks — subscription accounts

## P0 — openspec + design trio

- [x] 0.1 Proposal + design + spec delta (this change)
- [x] 0.2 Design trio review (pre-impl gate — COS greenlit 2026-07-03)

## P1 — mechanism

- [x] 1.1 `internal/accounts`: root, validate id, config dir path, init, wrap launch
- [x] 1.2 `internal/accounts`: health probe (mtime, expiresAt, status; no secrets)
- [x] 1.3 `flotilla accounts init <subscription-id>`
- [x] 1.4 `flotilla accounts list [--json]`
- [x] 1.5 `workspace.slotRecipeByName`: runtime `CLAUDE_CONFIG_DIR` wrap from slot `subscription_id`
- [x] 1.6 Unit tests: accounts package, accounts CLI, slot wrap idempotency + backward compat

## P2 — health probe polish

- [x] 2.1 `accounts list` human table + JSON shape documented in design
- [x] 2.2 `expires-soon` threshold (24h) test

## P3 — deferred (design only)

- [x] 3.1 Shared-rules symlink runbook note in design.md (no code)

## Gates

- [ ] Impl trio review
- [x] `go test ./...` green
- [ ] PR surfaced to operator gate (no self-merge)
- [x] **NOT in scope:** live cred migration, live launch recipe edits