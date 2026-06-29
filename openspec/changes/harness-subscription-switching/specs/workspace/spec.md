# workspace Specification (delta)

## ADDED Requirements

### Requirement: A launch recipe MAY declare a primary-plus-fallbacks harness chain

A launch recipe MAY declare an ordered failover chain, and when it does the system SHALL accept a
`primary` slot plus an ordered `fallbacks[]` list without breaking any existing recipe. Each slot
SHALL carry a `surface` (a registered driver
name), a `launch` (this harness's shell command), and a **`provider`** (a logical provider identity
such as `anthropic`/`xai`/`zai`, DISTINCT from an optional `subscription_id` — two subscriptions of
the same provider share one `provider`), plus an optional `model` and optional `subscription_id` (a
billing/account bucket within a provider, not a secret). The recipe-level `cwd`/`tmux`/`state` SHALL be
shared across slots (the desk — worktree + pane — is stable; only the foreground process changes).
**Backward-compatibility:** when `primary` and `fallbacks` are ABSENT, the existing top-level `launch`
IS the primary slot and `roster.Agent.surface` (or the default `claude-code`) is its implied
`surface` — every current recipe SHALL keep working byte-identically. Per-slot validation SHALL be
applied at load (non-empty `launch`; no `\t`/`\n`/`\r` control chars), with the `surface` known-driver
check deferred to switch/resume time (load may stay surface-agnostic to avoid an import cycle, mirroring
the roster's cmd-layer surface validation).

#### Scenario: A flat recipe with no chain is treated as primary-only

- **WHEN** a recipe has a top-level `launch`/`cwd` but no `primary`/`fallbacks`
- **THEN** the top-level `launch` resolves as the primary slot with the agent's roster `surface` (or
  default), exactly as before this change

#### Scenario: A slot with an empty launch or control chars is rejected

- **WHEN** a chain slot has an empty `launch` or a `launch` containing a `\t`/`\n`/`\r`
- **THEN** loading the recipe errors, never resolving a half-valid slot

#### Scenario: Each fallback slot carries a distinct provider identity

- **WHEN** a chain declares `primary` on provider `anthropic` and fallbacks on `xai` and `zai`
- **THEN** each slot's `provider` is preserved distinctly from any `subscription_id`, so failover
  target selection can require a DIFFERENT provider

### Requirement: A runtime active-harness overlay names the live slot without a roster commit

The system SHALL support a host-local `~/.flotilla/<agent>/active-harness.json` overlay that names the
currently-active slot (`"primary"`/`"fallback-N"`) together with its `surface`, `provider`,
`subscription_id`, `switched_at`, `switch_token`, `reason`, `cooldown_until`, and
`poisoned_providers[]`. An ABSENT overlay SHALL mean the primary slot. The overlay SHALL be written
atomically. It exists so a mid-incident failover does NOT require editing the committable roster: the
overlay is the runtime source of truth for which harness a desk is currently running.

#### Scenario: An absent overlay resolves to primary

- **WHEN** no `active-harness.json` exists for an agent
- **THEN** the agent resolves to its primary slot

#### Scenario: A present overlay names the live slot

- **WHEN** `active-harness.json` names `fallback-0` with surface `grok`
- **THEN** the agent's active slot is `fallback-0` and its active surface is `grok`

### Requirement: `ResolveHarness` resolves the chain then applies the overlay

The system SHALL provide `ResolveHarness(agent, flat)` (and the recipe-shaped `ResolveActiveRecipe`
view) that: (1) resolves the recipe chain via the EXISTING precedence (workspace `launch.json` first,
then the flat `flotilla-launch.json` — unchanged); (2) reads the `active-harness.json` slot name; and
(3) returns the matching slot's launch + surface. A read error on the overlay SHALL be fail-SAFE —
falling back to the primary/roster resolution — so a missing or torn overlay never makes a live desk
unresolvable.

#### Scenario: The overlay slot's launch is resolved

- **WHEN** `ResolveActiveRecipe` runs with an overlay naming `fallback-0`
- **THEN** it returns `fallback-0`'s launch command and surface from the resolved chain

#### Scenario: A torn overlay falls back to primary

- **WHEN** `active-harness.json` is present but unreadable or unparseable
- **THEN** `ResolveHarness` falls back to the primary slot (fail-safe) rather than erroring out the desk

### Requirement: Routing reads the active-overlay surface before the roster surface

`agentSurface` SHALL resolve a desk's surface as: the active-overlay surface when set, else the roster
`Agent.surface`, else the default. This is the seam that makes `watch`/`send`/`assess` route to the
LIVE harness after a switch with NO roster commit. A read error on the overlay SHALL be fail-SAFE
(fall back to the roster surface) — a missing or torn overlay SHALL NOT make a live desk unroutable.

#### Scenario: A switched desk routes via its overlay surface

- **WHEN** a desk's roster surface is `claude-code` but its `active-harness.json` surface is `grok`
- **THEN** `watch`/`send` route through the `grok` driver (the overlay wins), with no roster commit

#### Scenario: An unset overlay routes via the roster surface

- **WHEN** a desk has no `active-harness.json`
- **THEN** `agentSurface` returns the roster `Agent.surface` exactly as before this change
