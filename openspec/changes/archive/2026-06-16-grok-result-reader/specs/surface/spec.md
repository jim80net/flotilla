# surface Specification (delta)

## ADDED Requirements

### Requirement: A surface driver MAY expose the desk's full latest result from its session store

The system SHALL define an OPTIONAL `ResultReader` capability a surface `Driver` MAY implement,
returning the full text of the desk's latest COMPLETED turn from the harness's own session store —
for harnesses whose pane capture shows only a truncated tail. A driver that does not implement it
is unaffected (callers fall back to the pane capture). The system SHALL provide a read-only
`flotilla result <agent>` command that prints this full result when the agent's driver implements
the capability, and otherwise reports that the surface has no session-store reader. The command
SHALL NOT write to any pane.

#### Scenario: A grok desk's full latest result is read from its session store
- **WHEN** `flotilla result <grok-agent>` is run for a grok desk that has completed a turn
- **THEN** the full text of the latest assistant turn is printed from the grok session store (not the truncated pane capture)

#### Scenario: A surface without a session-store reader reports it
- **WHEN** `flotilla result <agent>` is run for a driver that does not implement the result-reader capability
- **THEN** the command reports that the surface has no session-store reader (and the operator uses the pane capture instead), and no pane is written

### Requirement: The grok driver reads its full latest result from the grok session store

The `grok` driver SHALL implement the result-reader capability by reading xAI's official grok
session store: resolve the desk pane's working directory, find the active grok session for that
directory in the store's active-sessions index, and return the last `assistant` entry's content
from that session's chat history. Resolution SHALL key on the pane's working directory (the stable
key the store uses), and SHALL surface a clear error when no active grok session matches or no
assistant turn exists yet. The reader SHALL read only the JSONL session files (no sqlite/CGO
dependency).

#### Scenario: The latest assistant turn is returned in full
- **WHEN** a grok desk has produced a multi-screen result that the pane capture truncates
- **THEN** the result-reader returns the complete latest assistant turn text from the session's chat history

#### Scenario: No active grok session for the pane's directory
- **WHEN** the result-reader runs but no active grok session matches the pane's working directory
- **THEN** it returns a clear error rather than empty or wrong output
