# Proposal — subscription accounts (Claude Code multi-account via `CLAUDE_CONFIG_DIR`)

## Why

A few Claude Code subscriptions should drive the whole fleet, but today every desk shares one
OAuth credential store (`~/.claude/.credentials.json`). Running concurrent desks on different
Anthropic accounts requires isolated config dirs — not manual `~/.claude.json` swap (wrong file;
refresh-token rotation invalidates saved copies; no concurrency).

This change adds the **generic credential-routing mechanism** flotilla needs before the operator's
~Jul-7 seat-swap / fleet migration: per-`subscription_id` `CLAUDE_CONFIG_DIR` isolation, wired
through launch recipes and resolved at relaunch time.

## What Changes

1. **`internal/accounts`** — host-local layout `~/.flotilla/accounts/<subscription-id>/claude-config/`,
   ID validation, `WrapClaudeLaunch`, credential health probe (mtime + `expiresAt` only; never token
   contents).

2. **`flotilla accounts init|list`** — scaffold a subscription config dir + one-time `/login`
   instructions; list registered accounts with health status.

3. **Launch credential routing** — when a harness slot is `claude-code` (or implied default) and
   carries a non-empty `subscription_id`, `ResolveHarness` / `slotRecipeByName` wrap the slot's
   `launch` with `export CLAUDE_CONFIG_DIR=…` at resolution time (stored recipes unchanged).

4. **Design note (P3, non-blocking)** — shared-rules symlink strategy documented in `design.md`;
   not implemented in this change.

## Constraints (operator, COS gate)

- **Public repo = generic mechanism only.** Example subscription ids (`anthropic-work`) appear in
  tests/docs as generic placeholders — never deployment-specific fleet names.
- **No live migration in this build.** Do not touch live credentials or host-local launch recipes;
  fleet cutover is a separate COS-executed deploy on a veto window after merge.
- **One-time `/login` surfaced at migration time**, not during mechanism merge.

## Impact

- **NEW capability:** `accounts` (`flotilla accounts`, `internal/accounts`)
- **EXTENDED:** `workspace` recipe resolution (`slotRecipeByName` credential wrap)
- **Pairs with:** `harness-subscription-switching` (`subscription_id` on `HarnessSlot` already exists)
- **Out of scope:** P3 shared-rules symlinks; live fleet recipe rewrite; `flotilla doctor` integration