# provision Specification

## Purpose
The `federation` capability ROUTES across many Discord channels but cannot CREATE them —
channels are made by hand in the Discord UI and their ids copied into the roster. The
`provision` capability gives flotilla a mechanical ability to create, list, and delete
Discord channels via the bot token, so the squadron channel layout can be stood up
programmatically (the provisioning complement to F#105 routing). Provisioning is additive
and lazy: only the `flotilla channel` command touches the bot-token REST path; a fleet
that never provisions is unaffected.

## Requirements
### Requirement: Mechanical channel creation via the bot token

The system SHALL create a Discord channel in the configured guild via the Discord REST
API using the bot token (`FLOTILLA_BOT_TOKEN`), without opening a gateway connection. The
guild SHALL come from the roster `guild_id`. The created channel's identity SHALL be read
from Discord's returned channel object (the authoritative confirmation), never inferred
from stdout text. A 2xx response carrying no channel id SHALL be treated as an error.

#### Scenario: Create a text channel

- **WHEN** `flotilla channel create fleet-command` runs with a bot token and a roster guild_id
- **THEN** a text channel is created in that guild and its real snowflake id is reported from Discord's returned object

#### Scenario: Create a category

- **WHEN** `flotilla channel create "Alpha Group" --type category` runs
- **THEN** a category channel is created and its id reported

#### Scenario: Create under a parent category

- **WHEN** `flotilla channel create alpha-desk --category "Alpha Group"` runs and exactly one category by that name exists
- **THEN** the channel is created with its parent set to that category's id

#### Scenario: An unresolvable or ambiguous parent category is a clear error

- **WHEN** `--category` names a category that does not exist, or names a category name shared by more than one category
- **THEN** the command fails with a clear error (for ambiguity, instructing the operator to pass the category's snowflake id) and creates nothing

#### Scenario: Errors are reported in a deterministic precedence

- **WHEN** more than one precondition is unmet (e.g. neither the bot token nor the guild_id is set)
- **THEN** the command reports them in a fixed order — bot token, then guild_id, then the Manage Channels preflight, then create — so the first error surfaced is deterministic

#### Scenario: A rate-limited create is handled, never opaque

- **WHEN** a create is rate-limited by Discord (HTTP 429)
- **THEN** the command either transparently retries and succeeds, or fails with a clear rate-limit error naming the retry-after — never an unhandled or malformed error

#### Scenario: Missing bot token is a clear error

- **WHEN** `flotilla channel create x` runs with no `FLOTILLA_BOT_TOKEN` available
- **THEN** it fails with a clear error naming the missing token and creates nothing

#### Scenario: Missing guild_id is a clear error

- **WHEN** the roster has no `guild_id`
- **THEN** `flotilla channel create x` fails with a clear error and creates nothing

### Requirement: Manage Channels permission preflight

Before attempting to create a channel, the system SHALL compute the bot's effective
guild-level permissions (guild owner, or `Administrator`, or the union of the `@everyone`
role and the bot's roles) and SHALL fail with a clear, actionable error naming the missing
**Manage Channels** permission when it is absent — creating nothing. The `@everyone` grant
SHALL be read from the role whose id equals the guild id (the documented Discord invariant).
A `403` returned at create time SHALL be translated to the same clear permission error as a
backstop (covering category/channel permission overwrites or a permission change after the
preflight).

#### Scenario: Bot lacks Manage Channels

- **WHEN** the bot's effective permissions in the guild do not include Manage Channels (and it is neither owner nor Administrator)
- **THEN** `flotilla channel create x` fails the preflight with an error naming Manage Channels and how to grant it, and no create is attempted

#### Scenario: Owner or Administrator passes the preflight

- **WHEN** the bot is the guild owner, or has the Administrator permission
- **THEN** the preflight passes (Administrator/owner implies Manage Channels)

#### Scenario: A create-time 403 is reported as a permission error

- **WHEN** the preflight passes but the create call returns HTTP 403 (e.g. a category permission overwrite denies the bot)
- **THEN** the command fails with the clear Manage-Channels error noting the denial happened at create (check permission overwrites)

### Requirement: Idempotent create (skip-if-exists)

Because Discord does not enforce channel-name uniqueness, the system SHALL make create
idempotent under sequential single-actor use: before creating, it SHALL list the guild's
channels and SKIP creation (reporting the existing channel's id) when a channel of the same
type with a matching name already exists under the same parent. Name matching SHALL account
for Discord's documented text-channel normalization (lowercasing and space→hyphen) so a
re-run with the original requested name still matches the normalized stored name; it SHALL
NOT apply speculative transforms that could over-match and falsely skip a distinct channel.
The skip report SHALL disclose that an existing channel's topic/parent are NOT reconciled
(create is idempotent, not convergent). Concurrent provisioning MAY still race (Discord
enforces no name uniqueness); `list` is the authoritative read-back.

#### Scenario: Re-running a create does not duplicate

- **WHEN** `flotilla channel create fleet-command` runs a second time and that channel already exists under the same parent
- **THEN** no new channel is created and the command reports the existing channel's id as skipped, disclosing that topic/parent were not reconciled

#### Scenario: Requested name matches a normalized existing name

- **WHEN** a previous run created `fleet-command` from the request `"Fleet Command"` and `flotilla channel create "Fleet Command"` runs again
- **THEN** it matches the existing `fleet-command` channel and skips rather than creating a duplicate

#### Scenario: Same name under a different parent is a distinct channel

- **WHEN** a channel named `notes` exists under category A and `flotilla channel create notes --category B` runs
- **THEN** a new `notes` channel is created under category B (the match is scoped to the same parent)

### Requirement: One-flow binding emission

When create is given an XO agent (and optional members and role), the system SHALL print
the corresponding `federation` channel→XO binding as paste-ready JSON, with the
freshly-created channel's real id filled in, so routing can be wired immediately. The named
XO and members SHALL be validated against the roster's agents; an unknown agent SHALL be a
clear error. The system SHALL NOT rewrite the committable roster file in place.

#### Scenario: Create emits a binding for a named XO

- **WHEN** `flotilla channel create fleet-alpha --xo alpha-xo --member desk-1 --role project` runs successfully
- **THEN** it prints a JSON binding object with the new channel's id, `xo_agent` alpha-xo, members [desk-1], role project, ready to paste into the roster `channels[]`

#### Scenario: An unknown XO or member is rejected

- **WHEN** `--xo` or `--member` names an agent not present in the roster
- **THEN** the command fails with a clear error before emitting any binding

#### Scenario: Empty members and role are omitted from the binding

- **WHEN** `create --xo alpha-xo` runs with no `--member` and no `--role`
- **THEN** the emitted binding contains `channel_id` and `xo_agent` only (empty members/role are omitted), matching the roster `omitempty` shape

### Requirement: List channels

The system SHALL list the guild's channels (id, type, name, parent) via the bot token, so
the operator/CoS can read back what exists and harvest snowflake ids. A machine-readable
JSON form SHALL be available.

#### Scenario: List the guild's channels

- **WHEN** `flotilla channel list` runs with a bot token and guild_id
- **THEN** it prints one entry per channel including the snowflake id and type

### Requirement: Delete a channel by explicit id only

The system SHALL delete a channel only when given its explicit snowflake id — never by
name — so a name typo cannot delete the wrong channel. On success it SHALL report the
deleted id.

The delete verb SHALL require an explicit confirmation flag (`--yes`) and SHALL validate
that the argument is a snowflake (all digits) before any REST call, so the one destructive
verb is never a one-keystroke or fat-fingered fire. A delete of a well-formed but
non-existent id SHALL report a clear error, never a silent success.

#### Scenario: Delete by id

- **WHEN** `flotilla channel delete 123456789012345678 --yes` runs
- **THEN** that channel is deleted and the id is reported

#### Scenario: Delete requires confirmation

- **WHEN** `flotilla channel delete 123456789012345678` runs without `--yes`
- **THEN** it fails with a clear error and deletes nothing

#### Scenario: Delete has no name path

- **WHEN** an operator passes a non-snowflake (non-numeric) argument to delete
- **THEN** it is rejected before any REST call (there is no delete-by-name), so a mistyped name cannot target a real channel

#### Scenario: Delete of a non-existent id is a clear error

- **WHEN** `flotilla channel delete <well-formed-but-absent-id> --yes` runs and no such channel exists
- **THEN** it reports a clear error (not a silent exit-0)

### Requirement: The bot token is never logged

The bot token SHALL never appear in any output, log line, or error message. REST errors
SHALL be reduced to the HTTP status and Discord's API error message, with no request,
header, or credential content.

#### Scenario: A REST error carries no credential

- **WHEN** a Discord REST call fails
- **THEN** the surfaced error contains the status and Discord's message but neither the bot token nor an Authorization header

