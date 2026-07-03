# accounts Specification (delta)

## ADDED Requirements

### Requirement: Each `subscription_id` maps to an isolated Claude Code config directory

The system SHALL store per-subscription Claude Code credentials under a host-local directory
`~/.flotilla/accounts/<subscription-id>/claude-config/` (overridable via `FLOTILLA_ACCOUNTS_ROOT`
for tests). The `<subscription-id>` SHALL be validated as a lowercase path-safe slug
(`^[a-z][a-z0-9_-]{0,63}$`). The mechanism is generic; actual ids are host-local deployment content.

#### Scenario: A valid subscription id resolves to a config dir

- **WHEN** `subscription_id` is `anthropic-work`
- **THEN** `ConfigDir("anthropic-work")` returns `<accounts-root>/anthropic-work/claude-config`

#### Scenario: An invalid subscription id is rejected

- **WHEN** `subscription_id` is `../escape` or `My-Account`
- **THEN** validation errors before any directory is created

### Requirement: `flotilla accounts init` scaffolds a config dir without touching credentials

`flotilla accounts init <subscription-id>` SHALL create the config directory (mode 0700) and print
one-time `/login` instructions using `CLAUDE_CONFIG_DIR`. It SHALL NOT read, copy, or write OAuth
tokens and SHALL NOT modify live `~/.claude` credentials.

#### Scenario: Init creates the directory once

- **WHEN** `flotilla accounts init anthropic-work` runs on a host with no existing dir
- **THEN** `<accounts-root>/anthropic-work/claude-config/` exists with mode 0700 and login instructions are printed

#### Scenario: Init is idempotent

- **WHEN** `flotilla accounts init anthropic-work` runs and the dir already exists
- **THEN** the command succeeds and reports the existing path (no destructive overwrite)

### Requirement: `flotilla accounts list` reports health without secret contents

`flotilla accounts list` SHALL scan registered account dirs and report `subscription_id`,
`config_dir`, credential file mtime, parsed `expires_at` and `subscription_type` when readable, and a
derived `status`. It SHALL NEVER print `accessToken`, `refreshToken`, or scope values.

#### Scenario: List reports missing credentials

- **WHEN** an account dir exists but `.credentials.json` is absent
- **THEN** status is `missing-creds`

#### Scenario: List reports expiry without token dump

- **WHEN** `.credentials.json` contains `claudeAiOauth.expiresAt`
- **THEN** the output includes `expires_at` as a timestamp and omits token fields

### Requirement: Claude-code harness slots with `subscription_id` get runtime `CLAUDE_CONFIG_DIR` wrap

When resolving a harness slot for relaunch (`slotRecipeByName` / `ResolveHarness`), if the slot's
surface is `claude-code` (or empty/implied default) and `subscription_id` is non-empty, the resolved
`launch` SHALL be prefixed with `export CLAUDE_CONFIG_DIR='<abs-path>';` unless the launch already
sets `CLAUDE_CONFIG_DIR`. Stored recipe files SHALL NOT be modified by this wrap.

#### Scenario: A slotted claude desk with subscription_id gets wrapped launch

- **WHEN** primary slot is `{surface: claude-code, subscription_id: anthropic-work, launch: claude -w xo}`
- **THEN** resolved launch begins with `export CLAUDE_CONFIG_DIR=` pointing at the anthropic-work config dir

#### Scenario: A slot without subscription_id is unchanged

- **WHEN** primary slot has no `subscription_id`
- **THEN** resolved launch equals the stored `launch` string (backward compatible)

#### Scenario: Double-wrap is prevented

- **WHEN** stored launch already contains `CLAUDE_CONFIG_DIR=`
- **THEN** resolution does not add a second export prefix