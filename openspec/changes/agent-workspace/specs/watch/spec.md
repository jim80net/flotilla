## ADDED Requirements

### Requirement: Per-agent heartbeat prompt and detector tracker from the workspace

`flotilla watch` SHALL resolve an agent's heartbeat/continuation prompt and the
change-detector's tracker file from that agent's workspace when present, with the
existing roster/flag values as fallback defaults. The prompt resolution order is
`~/.flotilla/<agent>/HEARTBEAT.md` → roster `heartbeat_message` →
`DefaultHeartbeatPrompt`; the detector tracker order is
`~/.flotilla/<agent>/state.md` → `--tracker-file` / `$FLOTILLA_TRACKER_FILE` →
`<roster-dir>/.flotilla-state.md`. A deployment with no workspace SHALL behave
exactly as before — same prompt, same tracker — so the change is purely additive.

#### Scenario: A workspace HEARTBEAT.md overrides the roster default
- **WHEN** the heartbeated agent has `~/.flotilla/<agent>/HEARTBEAT.md`
- **THEN** the clock injects that prompt instead of the roster `heartbeat_message` / built-in default

#### Scenario: A workspace state.md is the hashed tracker
- **WHEN** the detected agent has `~/.flotilla/<agent>/state.md`
- **THEN** the change-detector hashes that file as the tracker instead of `--tracker-file`

#### Scenario: No workspace leaves today's behavior unchanged
- **WHEN** no workspace exists for the heartbeated agent
- **THEN** the prompt and tracker resolve to the roster `heartbeat_message` and `--tracker-file` exactly as before
