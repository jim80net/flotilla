# notify Specification

## Purpose
TBD - created by archiving change notify-spec-backport. Update Purpose after archive.
## Requirements
### Requirement: Direct operator post under the agent's webhook identity, no tmux

The `notify` capability SHALL post a message directly to the operator on Discord,
under the **sending agent's own webhook identity**, and SHALL NOT inject into any
tmux pane. This is the operator-facing outbound path: an agent (typically the XO)
reaching the operator — as distinct from `send`, which wakes another agent's pane
and mirrors the wake to the audit channel. There is no separate audit copy; the
post IS the message.

#### Scenario: Agent reaches the operator without touching a pane
- **WHEN** `flotilla notify --from <agent> <message>` runs with the agent's webhook configured
- **THEN** the message is posted to Discord under `<agent>`'s username, no tmux pane is resolved or written to, and the command reports that it notified the operator as `<agent>`

### Requirement: Webhook resolved from the sending agent's secret key

The system SHALL resolve the webhook URL from the secrets file by the `--from`
agent's key — `FLOTILLA_WEBHOOK_<AGENT>` with the name upper-cased and `-`
replaced by `_` — and post under that agent's name. `--from` SHALL be required
(defaulting to `$FLOTILLA_SELF`); an empty sender SHALL be rejected. A secrets
file with no key for the sender SHALL produce a clean error that names the agent
and the expected key — never a panic and never a post under a wrong identity.

#### Scenario: Posts to the agent's own resolved webhook
- **WHEN** notify runs with `--from hydra-ops` and the secrets file maps `FLOTILLA_WEBHOOK_HYDRA_OPS` to a webhook URL
- **THEN** the request is sent to exactly that URL, with `username = "hydra-ops"`, `Content-Type: application/json`, and flotilla's explicit User-Agent

#### Scenario: Missing webhook for the sender errors by name
- **WHEN** notify runs with `--from hydra-ops` but the secrets file has no `FLOTILLA_WEBHOOK_HYDRA_OPS` key
- **THEN** the command errors, naming the agent (and the expected key), and posts nothing

#### Scenario: Sender is required
- **WHEN** notify runs with no `--from` and `$FLOTILLA_SELF` unset
- **THEN** the command errors instead of posting under an empty identity

### Requirement: Over-length messages are rejected, never truncated

The system SHALL reject a message exceeding Discord's 2000-character limit,
posting nothing — because the notify body IS the operator-facing content, the
opposite of the best-effort audit mirror, which clamps an over-length copy with
an ellipsis. A message at exactly the limit SHALL post. The limit SHALL be
counted in **runes** (so a multi-byte body within 2000 runes posts even though it
exceeds 2000 bytes). The rejection error SHALL cite the 2000-character limit.

#### Scenario: Over-limit message posts nothing
- **WHEN** notify is given a body of 2001 runes
- **THEN** the command errors citing the 2000-char limit and the webhook receives zero requests

#### Scenario: At-limit message posts
- **WHEN** notify is given a body of exactly 2000 runes
- **THEN** the message is posted (the boundary is inclusive)

#### Scenario: The limit is counted in runes, not bytes
- **WHEN** notify is given 2000 multi-byte runes (4000 bytes)
- **THEN** the message is posted (within the rune limit)

### Requirement: Message body from argument, file, or stdin

The system SHALL accept the message body inline (positional words joined with
spaces) OR from a file via `--file <path>` (`-` reads stdin), the two being
mutually exclusive. A file/stdin body SHALL have trailing newlines trimmed. An
empty or whitespace-only resolved message SHALL be rejected. `--file -` against
an interactive terminal SHALL fail fast rather than block on a read that will
never receive input.

#### Scenario: File body delivers verbatim with the trailing newline trimmed
- **WHEN** `flotilla notify --from <agent> --file ./brief.md` runs on a two-line file ending in a newline
- **THEN** both lines are posted as the content with the trailing newline removed

#### Scenario: Whitespace-only message is rejected
- **WHEN** notify is given a body that is only whitespace
- **THEN** the command errors instead of posting an empty message

#### Scenario: A file body and an inline message are mutually exclusive
- **WHEN** notify is given both `--file <path>` and inline message words
- **THEN** the command errors (the two sources are mutually exclusive) and posts nothing

### Requirement: Secrets required; the webhook secret never appears in an error

A secrets path SHALL be required (`--secrets` or `$FLOTILLA_SECRETS`); its
absence SHALL error before any network attempt. Message-body validation (empty
and over-length) SHALL occur before secrets are loaded, so a message that will be
rejected never triggers a credential read. The webhook URL is a credential and
SHALL NEVER appear in a returned error (a malformed URL yields a content-free
error; a transport error is reduced to its URL-free cause).

#### Scenario: Missing secrets path errors before posting
- **WHEN** notify runs with no `--secrets` and `$FLOTILLA_SECRETS` unset
- **THEN** the command errors and makes no network request

#### Scenario: The webhook secret never leaks in an error
- **WHEN** the webhook post fails (bad URL or network failure)
- **THEN** the returned error contains no part of the webhook URL

### Requirement: Flags precede the message

The system SHALL reject a flag placed after the positional message words with a
clear error that names the swallowed flag. Go's flag parser stops at the first
positional, so a later flag would otherwise be silently dropped and the operator
would receive a partial message.

#### Scenario: Misplaced flag errors clearly
- **WHEN** a flag (e.g. `--secrets`) appears after the message words
- **THEN** the command errors, naming the swallowed flag, and posts nothing

