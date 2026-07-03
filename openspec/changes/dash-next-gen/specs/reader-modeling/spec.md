# reader-modeling Specification (delta)

## MODIFIED Requirements

### Requirement: The dash renders the operator's mental map from published session data

The system SHALL persist per-desk session mirror history (append-only `session-mirror/<agent>.jsonl`
under the roster directory) as the dash's source for modeled turn-final visibility. This artifact
**supersedes** the previously proposed atomic `latest-delta.json` per desk: the latest entry in
the jsonl provides the same glanceable "latest delta" UX while supporting conversation history.
The dash SHALL read session-mirror ledgers via the existing pure-reader-over-files pattern.

#### Scenario: Latest modeled delta is the last session-mirror entry

- **WHEN** a desk has published at least one non-suppressed session-mirror entry
- **THEN** the dash can render that desk's latest info body from the final jsonl line

#### Scenario: Tri-surface levels map to reader-modeling pipeline outputs

- **WHEN** the publish path processes an enveloped turn-final
- **THEN** the info rendering is `readermap.Render(envelope)`, the verbose rendering is the full
  turn-final, and the debug rendering includes the envelope struct and mirror decision metadata