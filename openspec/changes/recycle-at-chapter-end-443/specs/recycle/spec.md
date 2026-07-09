## ADDED Requirements

### Requirement: Recycle aborts escalate to the owning coordinator

When `flotilla recycle` exits non-zero after an attempt that may have left the fleet in a
half-recycled state (or any fail-closed abort), the system SHALL escalate a first-class
notice to the desk's owning coordinator (inject into the coordinator pane when resolvable)
AND write a durable abort sidecar under the desk's host workspace. The notice SHALL name
the agent, abort class, phase, and prescribed recovery command. Log-only aborts are NOT
sufficient (#436).

#### Scenario: Phase-2 close timeout during unattended recycle

- **WHEN** graceful close does not confirm process exit within the close timeout
- **THEN** recycle aborts without relaunch AND escalates to the owning coordinator with
  `flotilla resume <desk> --force` guidance (if dead) — not only a process log line

### Requirement: Busy-desk recycle is retried before final abort

When recycle aborts because the desk did not settle Idle (phase 0 or under-lock re-verify
busy), the CLI SHALL re-attempt the full pipeline a small bounded number of times with a
short settle wait between attempts before escalating the final abort (#436).

#### Scenario: Desk mid-turn on first attempt

- **WHEN** phase 0 times out because the desk is Working
- **THEN** recycle retries (fresh handoff token) up to the busy-retry bound before returning
  the error to the caller and escalating

### Requirement: Focus-stealing overlays during close are healed

During the close poll, when the composer is on a subagent or list-nav overlay and self-heal
is available, recycle SHALL heal the overlay and continue polling for dead/shell rather than
silently exhausting the close timeout (#436 subagent exit-dialog class).

#### Scenario: Subagent panel open during /exit

- **WHEN** /exit is issued and the composer reads SubAgent during the close poll
- **THEN** self-heal runs and polling continues until dead/shell or timeout

### Requirement: Coordinator self-recycle uses handoff+rotate+takeover

`flotilla recycle <coordinator> --self` SHALL write a durable handoff, rotate context in
place (never bare `/clear` without handoff), and inject the takeover turn — without
graceful-close or process respawn — so a coordinator can rotate its own seat without
killing the driver that issued the command (#437).

#### Scenario: Coordinator runs recycle --self on its own pane

- **WHEN** `flotilla recycle xo --self` targets the command's own pane
- **THEN** handoff is durable, context is rotated, takeover is delivered, and the process
  is not killed

## MODIFIED Requirements

### Requirement: An XO-triggered desk recycle preserves context across a fresh restart

(Existing requirement retained.) Chapter-end detection (#443) MAY enqueue the same
`flotilla recycle` primitive automatically when a desk's lane is done; the XO/adjutant
trigger remains valid. Ceremonies SHALL ride this primitive (recycle-then-fresh-session),
not a parallel one-shot runner.
