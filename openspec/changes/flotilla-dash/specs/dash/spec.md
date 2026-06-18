# dash Specification (delta: new capability)

> Adds the `dash` capability: an optional, pluggable local web interface served by
> the `flotilla` binary (`flotilla dash`), covering fleet command-and-control (a
> read surface over the artifacts `flotilla watch` already writes, plus control
> actions over the existing confirmed-delivery library) and a native, GitHub-backed
> issue tracker. The dash is a PRESENTATION + ACTION layer — it introduces no second
> source of fleet truth and no new delivery mechanism.

## ADDED Requirements

### Requirement: Optional local web server subcommand

The system SHALL provide a `flotilla dash` subcommand that starts a local HTTP
server serving the web interface. The dash SHALL be entirely OPTIONAL: a fleet
that never runs `flotilla dash` SHALL behave identically to one without this
capability. The server SHALL be served by the existing `flotilla` binary (no new
binary, no hosted service) using the Go standard library HTTP stack and embedded
static assets.

#### Scenario: A fleet that never starts the dash is unchanged
- **WHEN** a deployment runs `send`/`notify`/`watch`/`voice` but never `flotilla dash`
- **THEN** its behavior is byte-for-byte identical to a build without the dash — the dash touches no other command's path

#### Scenario: The dash serves over the existing binary
- **WHEN** the operator runs `flotilla dash`
- **THEN** the `flotilla` binary starts a local HTTP server with the embedded assets, with no separate binary and no external service

### Requirement: Read-only consumption of existing fleet artifacts

The dash's read model SHALL derive all fleet state by READING the artifacts
`flotilla watch` already writes — the change-detector snapshot, the XO liveness
ack file, the roster, the CoS ledger, and the backlog file. The dash SHALL NOT run
its own pane prober, SHALL NOT write fleet state, and SHALL NOT start the watch
daemon. `flotilla watch` SHALL remain the single writer of fleet state, so the
dash can never diverge from or double-probe the fleet.

#### Scenario: Desk states come from the snapshot, with freshness
- **WHEN** the dash renders the fleet board
- **THEN** each desk's state is read from the detector snapshot and the view shows the snapshot's age ("as of"), so a stale read is never presented as live

#### Scenario: Three-state freshness (absent vs stale vs fresh)
- **WHEN** the dash renders the fleet board
- **THEN** it distinguishes ABSENT (no snapshot file — banner to start `flotilla watch --change_detector`, all desks `unknown`) from STALE (a snapshot whose age exceeds a threshold derived from the watch tick cadence — a clear "watch may be down" warning, states shown but marked stale) from FRESH (within the threshold — states shown live), and NEVER silently starts its own pane probe in any case

### Requirement: cnc read surfaces

The dash SHALL present three read surfaces: (1) a FLEET BOARD — one entry per
roster desk with its name, surface, assessed state (idle / working /
awaiting-input / awaiting-approval / errored / crashed / unknown), the XO marked as
hub, and the XO's liveness (ack age) + settled flag; (2) a FEDERATION TOPOLOGY —
the channel↔XO bindings rendered as the org chart (each channel → its XO → its
members, with the meta-XO → project-XOs → desks recursion visible); (3) a
COORDINATION HISTORY — the CoS ledger entries (reverse-chronological) and the
backlog drive-queue classification (unblocked / blocked / done). The fleet-board
JSON SHALL be a superset of the existing `flotilla status --json` contract
(`{ generated_at, xo, agents[{name, role?, surface, state}] }`).

#### Scenario: The fleet board reuses the status contract
- **WHEN** a client fetches the dash's fleet-state JSON
- **THEN** it is a superset of the `flotilla status --json` shape — `generated_at` and each agent's `name` and `state` are always present, `role` is present for the XO (`"hub"`), and `surface` is the effective driver — so the existing landing widget and the dash consume the same contract

#### Scenario: A single-fleet roster renders one binding correctly
- **WHEN** the roster uses the legacy single `channel_id` + `xo_agent` (one degenerate binding)
- **THEN** the topology view renders that one channel→XO→members box correctly (no federation required to use the dash)

#### Scenario: Read surfaces update live
- **WHEN** the snapshot, CoS ledger, or backlog file changes on disk
- **THEN** connected clients receive a push update (Server-Sent Events) without a manual refresh, with a JSON-poll endpoint as the fallback

### Requirement: cnc control actions over the existing delivery library

The dash SHALL expose three control actions, each implemented as a thin proxy over
the EXISTING delivery library — NOT a reimplementation: (1) ROUTE an instruction to
an XO or `@desk`, via `surface.Confirm.Submit` (the `flotilla send` path) using the
same `relay.Route` addressing; (2) POST an operator note via `discord.Post` (the
`flotilla notify` path); (3) RESUME a crashed desk via the `flotilla resume` recipe
path. The dash SHALL surface the library's TYPED outcome (delivered+confirmed /
busy / transient / crashed / unconfirmed for route; posted / missing-webhook /
over-length for notify; resumed / no-recipe / live-refused / ambiguous for resume),
and SHALL NOT inject into a pane by any path other than the confirmed-delivery
library (never raw `tmux send-keys`).

#### Scenario: Routing an instruction confirms a real turn
- **WHEN** the operator routes an instruction to a desk from the dash
- **THEN** the dash calls `surface.Confirm.Submit` and reports delivered only when a turn is confirmed — a busy/crashed/unconfirmed pane is reported as such, never as a silent success

#### Scenario: `@desk` addressing matches Discord routing
- **WHEN** the operator routes `@alpha do X` from the dash
- **THEN** the message is routed by the same `relay.Route` primitive used over Discord, so dash and Discord addressing are identical

#### Scenario: Resume reuses the recipe path
- **WHEN** the operator resumes a crashed desk from the dash
- **THEN** the dash invokes the existing `flotilla resume` recipe path and reports its typed outcome (resumed / no-recipe / live-refused / ambiguous)

#### Scenario: Stale board state does not mislead a control action
- **WHEN** the board (a possibly-stale snapshot) shows a desk idle but the desk is live-busy at action time
- **THEN** the control library's live re-assessment is authoritative — the dash reports the LIVE outcome (e.g. "desk is busy — not delivered, retry") rather than a bare failure, so the action is never silently mis-applied on stale state

### Requirement: Cross-process pane serialization for control

A pane-driving control action SHALL serialize against the `flotilla watch`
daemon's context-rotate via a CROSS-PROCESS per-pane transaction lock held across
the whole confirmed-delivery transaction. This is required because the dash is a
SEPARATE process from `flotilla watch` and cannot share watch's in-process
per-pane mutex, so without a cross-process lock a `/clear` rotate could interleave
between a submit and its Enter-retry and corrupt the composer. Until that
cross-process lock is in place, the dash SHALL NOT expose pane-driving control
(gating control to the phase that lands the lock).

#### Scenario: A dash route does not interleave with a watch rotate
- **WHEN** the dash drives a confirmed delivery to a desk while `flotilla watch` concurrently rotates that desk's context
- **THEN** the cross-process pane-transaction lock serializes them, so the rotate cannot land between the submit and the Enter-retry (no composer corruption)

### Requirement: Dash control actions are recorded for audit

A control action issued from the dash SHALL be mirrored to the CoS who-knows-what
ledger with a dash-provenance marker (distinguishable from a Discord-originated
exchange), so that "what the dash did" is auditable in the same record as Discord
traffic. The mirror SHALL be best-effort (a ledger failure never fails the
delivery), consistent with `notify`'s existing ledger append.

#### Scenario: A routed instruction is auditable with provenance
- **WHEN** the operator routes an instruction from the dash
- **THEN** the CoS ledger gains an entry tagged with dash provenance, so the action is distinguishable from a Discord-originated one in the audit record

### Requirement: Native GitHub-backed issue tracker

The dash SHALL provide a native issue tracker backed by the repository's GitHub
Issues: list, view, create, comment, label, and close. The backend SHALL talk to
GitHub directly via the `gh` CLI (reusing its existing host authentication — no new
secret in v1). This SHALL be a single backend behind a minimal internal interface
for testability; it SHALL NOT introduce a config-selected multi-tracker strategy
registry (Linear/Jira are explicitly deferred — that is the separate, deferred #103
abstraction, NOT this capability).

#### Scenario: The operator triages issues without leaving the dash
- **WHEN** the operator opens the tracker view
- **THEN** the dash lists the repo's open GitHub issues (including the `operator-idea` label) and can open, comment on, label, create, and close them natively

#### Scenario: No multi-tracker abstraction is built
- **WHEN** the tracker is implemented
- **THEN** there is exactly one backend (GitHub via `gh`) behind a minimal seam, with NO Linear/Jira implementation and NO config-selected provider registry

#### Scenario: Issue writes require authorization
- **WHEN** a client creates, comments on, labels, or closes an issue
- **THEN** the action is gated by the same authorization posture as cnc control (§ security), and a destructive verb (close) is confirmed explicitly in the UI

#### Scenario: `gh` failures surface as typed errors, never silent success
- **WHEN** `gh` is unauthenticated, rate-limited, the repo is not found, or the network is down
- **THEN** the tracker returns a clear typed error surfaced in the UI (never a swallowed failure or an empty list masquerading as "no issues")

#### Scenario: Issue content cannot inject `gh` flags or retarget a repo
- **WHEN** an issue title/body/label from a request is passed to `gh`
- **THEN** it is passed injection-safely (free-form bodies via stdin, an option terminator so a leading `-` is never read as a flag) and the target repo is the one pinned at startup — a request body can NEVER select an arbitrary repo

### Requirement: Fail-closed security posture

The dash SHALL default to binding loopback (`127.0.0.1`). When bound to ANY
non-loopback address, the server SHALL REFUSE TO START unless a bearer token is
configured (`$FLOTILLA_DASH_TOKEN` / `--auth-token-file` preferred over a
`ps`-visible `--auth-token`), validated at startup (fail-closed). When a token is
configured, control actions and issue WRITES SHALL require it (`Authorization:
Bearer …`, constant-time comparison); read endpoints SHALL require it on a
non-loopback bind by default. The token SHALL never be logged.

#### Scenario: Non-loopback bind without a token fails closed
- **WHEN** the operator starts `flotilla dash --bind 0.0.0.0:8080` with no token configured
- **THEN** the server refuses to start with a clear error — it never exposes an unauthenticated control surface beyond loopback

#### Scenario: Loopback needs no bearer token (but still defends against the browser)
- **WHEN** the dash binds the default loopback address
- **THEN** read and control are available without a bearer token (loopback reachability is host-shell-level trust), AND state-changing requests are still defended against a malicious web page by the browser-attacker controls below

#### Scenario: A configured token gates writes
- **WHEN** a token is configured and a client issues a control action or issue write without a valid `Authorization: Bearer` header (or, for SSE, a valid session cookie on a non-loopback bind)
- **THEN** the request is rejected (401/403) and no pane injection or GitHub write occurs

### Requirement: Browser-attacker defenses (CSRF, DNS-rebinding, XSS) on every state-changing request

Because the operator's own browser can reach a loopback bind, the dash SHALL NOT
treat loopback as exempt from web attacks. Every state-changing request (control +
issue writes) SHALL require a custom request header that a cross-origin page
cannot set without a CORS preflight the dash never approves (anti-CSRF, enforced
on loopback too); every handler SHALL validate the `Host` header against an
allowlist (`127.0.0.1`/`localhost`/`[::1]` + the configured bind host) to defeat
DNS rebinding; and dynamic data SHALL reach pages via `fetch`ed JSON (never
server-rendered into a `<script>` literal) with `html/template` contextual
escaping (anti-XSS). The SSE stream SHALL NOT depend on an `Authorization` header
(the `EventSource` API cannot set one): on a non-loopback bind it is authorized by
a short-lived `HttpOnly`, `SameSite=Strict` cookie minted by an authenticated
request, so the token never travels in a URL or a log.

#### Scenario: A cross-origin page cannot forge a control POST on loopback
- **WHEN** a web page the operator visits issues a cross-origin POST to the loopback dash without the required custom header
- **THEN** the request is rejected — the custom-header + Origin checks defeat the browser-CSRF even though no token is required on loopback

#### Scenario: A rebound hostname is rejected
- **WHEN** a request arrives with a `Host` header outside the allowlist (a DNS-rebinding attempt)
- **THEN** the dash rejects it regardless of the bind address

#### Scenario: SSE is authorized without leaking the token
- **WHEN** a client opens the `/events` SSE stream on a non-loopback bind
- **THEN** it is authorized by the session cookie (not a URL token), and the bearer token never appears in a URL or a log
