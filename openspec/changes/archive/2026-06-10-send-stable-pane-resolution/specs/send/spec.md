# send Specification (delta: stable, title-drift-immune pane resolution)

## RENAMED Requirements

- FROM: `### Requirement: Pane resolution by title, exact or single-glyph-prefixed`
- TO: `### Requirement: Pane resolution by stable marker, then title`

## MODIFIED Requirements

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

## ADDED Requirements

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
