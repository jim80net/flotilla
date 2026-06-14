# send Specification (delta: inter-agent mirror default-off)

## MODIFIED Requirements

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
