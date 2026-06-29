# fleet-visibility Specification

## Purpose

A newcomer running flotilla against their project and a Discord guild must be able to SEE what
each desk is doing, without writing a hook or a script. Desk work happens in ephemeral tmux panes;
this capability mirrors each desk's completed-turn output into its own Discord channel
automatically. This spec covers **Tier 1** — the mechanical per-desk mirror (daemon code). The
synthesis tiers (an XO/meta-XO curating its subordinates up the hierarchy) are a separate change.

## ADDED Requirements

### Requirement: Per-desk turn-final mirror on the work-finished edge

The system SHALL, when a non-XO desk completes a unit of work, post that desk's turn-final output
to the desk's own home Discord channel automatically — with no hook, script, or per-desk custom
code. "Completes a unit of work" SHALL be the change-detector's confirmed `Working→Idle` transition
for that desk (a desk only settles to Idle after a turn ends; an intermediate tool-heavy render
reads `Working` throughout). The mirror SHALL NOT fire on the detector's cold-start baseline, nor on
a `Working→Shell` (crash) or `Working→Unknown` (capture glitch) transition — only a genuine finish.
The XO is excluded (it has its own mirror). The mirror side-effect SHALL be performed OUTSIDE the
detector's state mutex, so a slow transcript read or Discord post can never stall the detector's
tick loop or any other detector writer.

#### Scenario: A desk finishing a turn mirrors to its channel

- **WHEN** a monitored non-XO desk transitions from Working to Idle
- **THEN** flotilla reads that desk's turn-final output and posts it to the desk's home channel, automatically, with no hand-rolled hook

#### Scenario: Non-finishes do not mirror

- **WHEN** the detector cold-starts, or a desk transitions Working→Shell or Working→Unknown
- **THEN** no mirror is posted (only a true Working→Idle finish mirrors)

### Requirement: Turn-final extraction is robust to the transcript's entry zoo

The mirror SHALL extract the desk's turn-final assistant text by reading the desk's active session
transcript and selecting the most recent assistant message bearing non-empty text, skipping
entries that are not the desk's own spoken output: tool-result and tool-use blocks, thinking
blocks, non-message entry types (system / attachment / snapshot / title), and sub-agent
(`isSidechain`) entries. It SHALL strip embedded command tags and treat a turn whose residue is
empty or pure noise as not-substantive (no post). The active session SHALL be selected as the
most-recently-modified transcript among the (possibly many) sessions for the desk's working
directory.

#### Scenario: The last real assistant turn is found past trailing tool/system entries

- **WHEN** the transcript ends with tool-result, system, or attachment entries after the last assistant text
- **THEN** the mirror walks back past them to the last text-bearing assistant turn and posts that

#### Scenario: A sub-agent's output is not mistaken for the desk's turn

- **WHEN** the desk's most recent transcript activity is a sub-agent (sidechain) entry
- **THEN** the mirror skips it and posts the desk's own main-thread turn-final (or nothing if there is none substantive)

### Requirement: Mirror delivery is bounded, chunked, observe-only, and audited

A mirrored turn-final exceeding the channel content limit SHALL be split into ordered chunks on
paragraph boundaries (each within the limit), rather than rejected or silently truncated. The
mirror SHALL be observe-only and best-effort: a failure to locate, read, chunk, or post the
transcript SHALL NEVER affect message delivery, the detector tick, or any other behavior — it is
logged and dropped. Every mirror decision (posted with N chunks, skipped with a reason, or a
chunk/post failure) SHALL emit exactly one log line, so a silent failure cannot hide (the original
mirror bugs survived undetected precisely because failures exited silently).

#### Scenario: An over-limit turn is chunked, not dropped

- **WHEN** a desk's turn-final exceeds the Discord content limit
- **THEN** it is posted as ordered paragraph-boundary chunks, each within the limit

#### Scenario: A mirror failure never harms the fleet

- **WHEN** the transcript cannot be read or the Discord post fails
- **THEN** the failure is logged on one line and dropped; delivery and the detector tick are unaffected
