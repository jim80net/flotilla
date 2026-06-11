## ADDED Requirements

### Requirement: Per-agent continuation prompt and detector tracker from the workspace

`flotilla watch` SHALL source the change-detector's **continuation** prompt and the
detector's tracker file from the heartbeated/detected agent's workspace when present,
with the existing roster/flag values as fallback. The continuation prompt comes from
`~/.flotilla/<agent>/HEARTBEAT.md` (a template whose `{{tracker}}`/`{{settle}}`
placeholders are substituted and whose ack instruction is appended) when present,
else the built-in continuation prompt; in legacy (non-detector) mode the order is
`HEARTBEAT.md` → roster `heartbeat_message` → `DefaultHeartbeatPrompt`. The detector
tracker resolves `~/.flotilla/<agent>/state.md` → `--tracker-file`/
`$FLOTILLA_TRACKER_FILE` → `<roster-dir>/.flotilla-state.md`. A deployment with no
workspace SHALL behave exactly as before (same prompt, same tracker) — the change is
additive on the no-workspace path.

#### Scenario: A workspace HEARTBEAT.md overrides the detector continuation prompt
- **WHEN** the heartbeated agent runs under the change-detector and has `~/.flotilla/<agent>/HEARTBEAT.md`
- **THEN** the detector's continuation wake uses that template (placeholders substituted, ack appended), not the built-in prompt — and NOT the legacy `heartbeat_message`, which the detector never reads

#### Scenario: The prompt's named tracker path equals the detector's hashed path
- **WHEN** the tracker is resolved from the workspace `state.md`
- **THEN** that one resolved path is BOTH the file the change-detector content-hashes AND the path substituted into the continuation prompt's `{{tracker}}` — they never diverge, so the XO updates the same file the detector watches

#### Scenario: Switching the tracker source is a restart-time, not live, change
- **WHEN** the operator relocates the tracker to the workspace `state.md`
- **THEN** the new source takes effect on a `flotilla-watch` restart, and the first post-switch tick may emit one expected, harmless spurious material wake (the snapshot was keyed to the prior file)

#### Scenario: No workspace leaves today's behavior unchanged
- **WHEN** no workspace exists for the heartbeated agent
- **THEN** the prompt and tracker resolve to the built-in/roster/flag defaults exactly as before
