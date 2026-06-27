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

`flotilla send`'s audit mirror to the Discord channel SHALL be **default-off** for
inter-agent traffic: it mirrors only when enabled by the roster `mirror_inter_agent`
setting (default `false`) or a per-call `--mirror` flag, and never when `--no-mirror`
is given. The precedence SHALL be: `--no-mirror` (off) → `--mirror` (on) → roster
`mirror_inter_agent` → off; `--no-mirror` and `--mirror` together SHALL be a clear
error. WHEN it does mirror, it posts under the sender's webhook identity and these
properties hold: a mirror failure or absence SHALL warn but SHALL NOT fail the
command (delivery already happened; failing would tempt a retry into a
double-delivery); the webhook URL is a credential and SHALL NEVER appear in a
returned error; mirror content SHALL be clamped to the channel's 2000-character limit
with the operator warned on truncation. `flotilla notify` is unaffected — it is the
operator-facing path and always posts.

#### Scenario: Inter-agent send does not mirror by default
- **WHEN** `flotilla send` runs with neither `--mirror` nor `--no-mirror` and the roster does not set `mirror_inter_agent: true`
- **THEN** the message is delivered to the agent's pane but is NOT posted to Discord

#### Scenario: Mirroring is enabled per-roster or per-call
- **WHEN** the roster sets `mirror_inter_agent: true`, or `flotilla send --mirror` is passed
- **THEN** the delivered message is mirrored to the channel (and a `--no-mirror` on the same call still forces it off)

#### Scenario: Mirror failure does not fail delivery
- **WHEN** mirroring is enabled and delivery succeeds but the mirror fails or is unconfigured
- **THEN** the command still succeeds with a warning, so a retry cannot double-deliver

#### Scenario: The webhook secret never appears in an error
- **WHEN** an enabled mirror post errors (bad URL, network failure)
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

### Requirement: Pane resolution by stable marker, then title

The system SHALL resolve an agent to a tmux pane by two tiers, in order: (1) a
**stable per-pane marker** — the tmux user-option `@flotilla_agent` equal to the
agent's resolution key — which is authoritative and immune to pane-title drift;
and (2) a **title** match (the bare name, or a single-status-glyph prefix such as
Claude Code's "✳ <name>") used ONLY when no pane carries the marker, so an
untagged fleet resolves exactly as before. Within each tier, zero matches OR more
than one SHALL be an error — never a silent mis-delivery. An empty marker SHALL
never match.

#### Scenario: Marker resolves a pane whose title has drifted
- **WHEN** a pane is tagged `@flotilla_agent=backend` and its title later drifts to a task summary
- **THEN** resolution of "backend" still returns that pane, by the marker, regardless of the title

#### Scenario: Marker is authoritative over a coincidental title match
- **WHEN** one pane carries the marker for "backend" (with a drifted title) and another pane's title coincidentally matches "backend"
- **THEN** resolution returns the marker-tagged pane

#### Scenario: Untagged fleet falls back to title
- **WHEN** no pane carries the marker and agent "backend" runs in a pane titled "✳ backend"
- **THEN** resolution succeeds by title, and a request for "back" does NOT match it

#### Scenario: Ambiguity in either tier is an error
- **WHEN** two panes carry the same marker, or (absent any marker) two panes share the title
- **THEN** resolution returns an error instead of delivering to an arbitrary one

### Requirement: Register a pane's stable marker

The system SHALL provide `flotilla register <agent> [--pane <target>]`, which
records the agent's resolution key as the `@flotilla_agent` marker on the target
pane (default the current pane via `$TMUX_PANE`), after validating the agent
exists in the roster. The agent positional SHALL be accepted either before or
after the flags. Running it once per desk at launch makes the desk resolvable for
the life of the pane regardless of title drift; running it with an explicit
`--pane` re-tags an already-running, already-drifted desk without interrupting it.

#### Scenario: Register tags the current pane
- **WHEN** `flotilla register <agent>` runs inside the agent's pane
- **THEN** the pane is tagged with `@flotilla_agent=<the agent's resolution key>` and resolves by that marker thereafter

#### Scenario: Register a drifted desk from elsewhere
- **WHEN** `flotilla register <agent> --pane <target>` runs from another pane against an already-drifted desk
- **THEN** the target pane is tagged and becomes resolvable, with no message injected into the desk

