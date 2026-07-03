# Design — subscription accounts (`CLAUDE_CONFIG_DIR` per `subscription_id`)

**Status:** Approved — COS greenlit 2026-07-03 (multi-account + ~Jul-7 seat-swap driver).

## Executive summary

Claude Code stores OAuth tokens in `<config-dir>/.credentials.json` (default config dir:
`~/.claude/`). The env var `CLAUDE_CONFIG_DIR` redirects the entire config root. flotilla maps each
logical `subscription_id` (already on `launch.HarnessSlot`, NOT a secret) to a host-local config
dir under `~/.flotilla/accounts/<id>/claude-config/`. Launch commands for `claude-code` slots are
wrapped at **resolution time** — stored `launch.json` files stay human-readable and migration can
add `subscription_id` without rewriting `launch` strings.

## Verified credential facts (Linux, read-only probe)

| Artifact | Role |
|---|---|
| `~/.claude/.credentials.json` | OAuth: `claudeAiOauth.accessToken`, `.refreshToken`, `.expiresAt`, `.scopes`, `.subscriptionType` |
| `~/.claude.json` | App state/cache + `oauthAccount` metadata — **no tokens** |
| `CLAUDE_CONFIG_DIR` | Redirects config root; credentials land under `<dir>/.credentials.json` |
| `CLAUDE_CODE_OAUTH_TOKEN` | Env injection alternative — rejected as primary (refresh rotation, `/proc` exposure) |

## Layout convention

```
~/.flotilla/accounts/
  <subscription-id>/          # validated slug; host-local; gitignored
    claude-config/            # CLAUDE_CONFIG_DIR target (mode 0700)
      .credentials.json       # created by operator `claude /login` (not by flotilla)
```

`FLOTILLA_ACCOUNTS_ROOT` overrides the accounts root (tests only; mirrors `FLOTILLA_WORKSPACE_ROOT`).

**`subscription_id` validation:** `^[a-z][a-z0-9_-]{0,63}$` — lowercase slug, path-safe, no secrets.

## Launch wrap (runtime, not stored)

When `slotRecipeByName` projects a chain slot onto a recipe-for-slot:

```go
if surface is claude-code (or "") && subscription_id != "" {
    launch = "export CLAUDE_CONFIG_DIR='<abs-config-dir>'; " + launch
}
```

- **Backward compatible:** empty `subscription_id` ⇒ no wrap (today's behavior byte-identical).
- **Idempotent guard:** if `launch` already contains `CLAUDE_CONFIG_DIR=`, do not double-wrap.
- **Absolute paths** in the export (resolved via `accounts.ConfigDir`).

## `flotilla accounts init <id>`

1. Validate id slug.
2. `MkdirAll(<accounts-root>/<id>/claude-config, 0700)`.
3. Print config dir path + one-time login recipe:

   ```
   CLAUDE_CONFIG_DIR='<path>' claude
   # then /login in the session
   ```

Does **not** run `/login`, read live creds, or copy tokens.

## `flotilla accounts list [--json]`

Scan `<accounts-root>/*/claude-config/.credentials.json`. For each:

| Field | Source |
|---|---|
| `subscription_id` | directory name |
| `config_dir` | absolute path |
| `cred_mtime` | file mtime |
| `expires_at` | `claudeAiOauth.expiresAt` (ms epoch) if parseable |
| `subscription_type` | `claudeAiOauth.subscriptionType` if parseable |
| `status` | `missing-creds` / `unreadable` / `expired` / `ok` / `expires-soon` (<24h) |

Never print `accessToken`, `refreshToken`, or scopes.

## P3 — shared rules symlink (design only, non-blocking)

**Problem:** isolated `CLAUDE_CONFIG_DIR` trees do not inherit `~/.claude/rules` / dot_claude symlinks.

**Options:**

| Approach | Pros | Cons |
|---|---|---|
| Symlink `claude-config/rules` → shared tree | Central doctrine updates | Per-account divergence if one account edits rules |
| Copy-on-init | Self-contained dirs | Drift from shared doctrine |
| `CLAUDE_CONFIG_DIR` + memex/doctrine in worktree only | Identity already in worktree | Claude-native rules/plugins still per-config-dir |

**Recommendation for migration runbook:** symlink `rules`, `skills`, `plugins` from a host-local
shared tree (`~/.flotilla/shared/claude-config/`) into each account dir at init time; document in
migration playbook. Defer automation until post-seat-swap dogfood validates the layout.

## Migration boundary (NOT this PR)

After merge, COS executes on a veto window:

1. `flotilla accounts init <id>` per subscription.
2. Operator one-time `/login` per id.
3. Add `subscription_id` to harness slots in host-local recipes (launch strings unchanged — wrap is runtime).
4. `flotilla resume` / `flotilla switch` pick up wrapped launches.

## Private/public firewall

All code, tests, and docs use generic ids (`anthropic-work`, `anthropic-personal`). Real fleet
subscription names live only in host-local recipes (gitignored).