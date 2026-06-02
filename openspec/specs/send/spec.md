# send Specification

## Purpose

The `send` capability is the outbound half of the coordination bus: an operator
or the XO agent issues `flotilla send`, the message is injected into the target
agent's tmux pane (which, for a turn-based agent, IS the wake), and a copy is
mirrored to the Discord coordination channel under the sender's identity for a
durable, readable-back audit trail. This spec is the backport of the shipped v0
(`cmd/flotilla`, `internal/{deliver,discord,roster}`).

## Requirements

### Requirement: Terminal-pane delivery is the wake

The system SHALL deliver a message by injecting it into the target agent's tmux
pane (resolved from the roster) and submitting it — no polling, no relay
process. Injecting the text IS the wake for a turn-based agent. Delivery success
is the command's primary success criterion.

#### Scenario: Message delivered to a resolved pane
- **WHEN** `flotilla send --from <sender> <agent> <message>` runs and the agent resolves to exactly one tmux pane
- **THEN** the message is typed into that pane and submitted, and the command reports the resolved pane target

### Requirement: Pane resolution by title, exact or single-glyph-prefixed

The system SHALL resolve an agent to a tmux pane by matching the agent name
against pane titles, accepting either the bare name or a single-status-glyph
prefix (Claude Code renames its pane to "<glyph> <name>", e.g. "✳ v12-dev"). A
name matching zero panes, or more than one, SHALL be an error — never a silent
mis-delivery.

#### Scenario: Glyph-prefixed title matches; substring does not
- **WHEN** agent "v12-dev" runs in a pane titled "✳ v12-dev"
- **THEN** resolution succeeds, and a request for "v12" does NOT match it

#### Scenario: Ambiguous title is an error
- **WHEN** two panes share the agent's title
- **THEN** resolution returns an error instead of delivering to an arbitrary one

### Requirement: Multi-line messages deliver as a single submission

The system SHALL deliver a multi-line message as ONE submission (one prompt),
not one-per-line. Delivery SHALL bracket-paste the body (literal newlines) and
submit with a single Enter after a settle delay that lets the receiving terminal
ingest the paste; the settle delay applies to single-line messages too (an
immediate Enter races paste ingestion and is dropped).

#### Scenario: A multi-line message lands as one prompt
- **WHEN** a four-line message is delivered to an agent
- **THEN** the agent receives it as a single prompt and acts on it once

### Requirement: Message body from argument, file, or stdin

The system SHALL accept the message body inline (positional words joined with
spaces) OR from a file via `--file <path>` (`-` reads stdin), the two being
mutually exclusive. A file/stdin body SHALL have trailing newlines trimmed. An
empty resolved message SHALL be rejected. `--file -` against an interactive
terminal SHALL fail fast rather than block.

#### Scenario: File body avoids shell quoting
- **WHEN** `flotilla send --from <sender> --file ./brief.md <agent>` runs
- **THEN** the file's contents (trailing newline trimmed) are delivered, and an additional inline message is rejected

### Requirement: The audit mirror is best-effort and never leaks the webhook

The system SHALL mirror the delivered instruction to the Discord channel under
the sender's webhook identity. A mirror failure or absence SHALL warn but SHALL
NOT fail the command (delivery already happened; failing would tempt a retry
into a double-delivery). The webhook URL is a credential and SHALL NEVER appear
in a returned error. Mirror content SHALL be clamped to the channel's
2000-character limit, with the operator warned when truncation occurs.

#### Scenario: Mirror failure does not fail delivery
- **WHEN** delivery succeeds but the audit mirror fails or is unconfigured
- **THEN** the command still succeeds with a warning, so a retry cannot double-deliver

#### Scenario: The webhook secret never appears in an error
- **WHEN** a webhook post errors (bad URL, network failure)
- **THEN** the returned error contains no part of the webhook URL

### Requirement: Roster and secrets validated at load

The roster (agents, channel/guild/operator identifiers) SHALL be a committable,
secret-free file; secrets (bot token, per-agent webhook URLs) SHALL live in a
separate file that is never committed. Loading SHALL reject duplicate agent names,
duplicate effective tmux titles (which would mis-route), and malformed secrets
lines (a non-blank, non-comment line without `=` — which could silently drop a
credential).

#### Scenario: Duplicate effective title rejected
- **WHEN** two roster agents resolve to the same tmux title
- **THEN** the roster fails to load with an error

### Requirement: Flags precede the agent name

The system SHALL reject a flag placed after the agent name with a clear error
message. Go's flag parser stops at the first positional, so a later flag would
otherwise be silently swallowed and misbehave.

#### Scenario: Misplaced flag errors clearly
- **WHEN** a flag appears after the agent name
- **THEN** the command errors, telling the operator to put flags before the agent (or use `--file` for a message that starts with `-`)
