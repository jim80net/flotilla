# reader-modeling Specification (delta)

## MODIFIED Requirements

### Requirement: `flotilla brief` publishes a desk's reader-modeled brief deterministically via the shipped mirror

The system SHALL provide `flotilla brief <desk>` — a single operation that elicits a reader-modeled
brief from a desk and publishes it to that desk's own Discord channel. The brief SHALL be published by
the EXISTING per-desk mirror (`internal/watch/detector.go`'s `MirrorOnFinish` → `cmd/flotilla/mirror.go`'s
`deskMirror.run`, fed the turn-final via the `surface.ResultReader` seam wired in `deskMirrorOnFinish`,
`cmd/flotilla/watch.go:890`), NOT by `flotilla notify` and NOT by a new transport. The desk SHALL never
touch fleet secrets to publish a brief (honoring the smart-desk secret-free invariant trained by
`cmd/flotilla/pushsnippet.go:29`). `flotilla brief <desk>` SHALL inject a brief-request into the desk's
pane (a `send`-class injection); the desk SHALL respond by emitting an enveloped brief (the reader-map
envelope, below) as its turn-final, and the mirror publishes the turn that CARRIES the envelope.
Determinism therefore means: a desk publishes WITHOUT calling a forbidden primitive (`notify`) or
touching a secret — NOT that any arbitrary subsequent turn is the brief. Because the mirror fires on
turn-finish, the brief turn-final SHALL be CORRELATED to the brief by the presence of the envelope
marker (below), so an unrelated intervening turn is published as an ordinary (un-enveloped) turn-final
and is NOT mistaken for the brief. The scope of `flotilla brief` is the desk's **channel** surface; the
raw pane surface is explicitly out of scope.

The command SHALL also accept `--all` to fan out the same brief request to every roster agent except
the primary `xo_agent` (the same exclusion the per-desk mirror uses — the primary XO has its own mirror
path). `--all` and `<desk>` SHALL be mutually exclusive. Fleet-wide fan-out SHALL continue on per-desk
errors and SHALL exit non-zero when any desk failed (busy, crashed, dark, or delivery unconfirmed).

`flotilla brief` is run by the orchestrator (which holds the fleet secrets); when secrets are available
it SHALL pre-check, at fan-out time, that each named desk's channel webhook resolves, and SHALL REPORT
any desk with no resolvable webhook as a "dark" desk (its brief cannot be published) — rather than
returning success while the desk's brief silently never reaches a channel (the unconfigured-webhook
re-skin of the #207 failure). This pre-check is an ORCHESTRATOR capability and does NOT weaken the
desk-secret-free invariant: the DESK still publishes via the mirror without ever holding a secret.

#### Scenario: A fleet-wide brief fan-out reaches every configured desk's channel

- **WHEN** `flotilla brief --all` runs against a roster with configured channel webhooks
- **THEN** every non-primary-XO agent with a resolvable webhook receives a brief injection (the #207
  failure — only a fraction published because the fan-out relied on each desk translating a free-text
  request into the forbidden `notify` — does not recur), and any dark desk is named