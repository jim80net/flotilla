# watch Specification (delta: multi-channel relay for federation)

> This delta GENERALIZES the inbound relay from a single channel to a set of
> channel→XO bindings. It does NOT weaken the security model: feedback-loop
> immunity and operator-only authorization are PRESERVED, now applied per channel.
> The cross-tier delivery transport and any broadening of the author rule
> (Transport B's parent allow-list) are OUT OF SCOPE of this delta — see the
> `federation` capability and design.md §6.

## MODIFIED Requirements

### Requirement: Gateway relay of operator messages into agent panes

The system SHALL provide `flotilla watch`, a long-lived process that streams the
Discord gateway and injects accepted operator messages into the target agent's
tmux pane via the `send` capability's delivery. The relay SHALL listen on a SET of
channels, each **bound** to exactly one XO; a message's **origin channel**
determines its routing (the channel's bound XO and that XO's member scope). A
single-channel roster (one `channel_id` + `xo_agent`) is the degenerate
one-binding case and SHALL behave exactly as before. Injection is the wake; no
polling loop and no agent kept alive are required. A relayed delivery SHALL be
CONFIRMED — reported successful (logged and mirrored) only when a turn is confirmed
to have started (the `Idle → Working` edge), never on the bare exit code of the
tmux keystrokes. A relayed message that cannot be confirmed delivered SHALL raise a
LOUD operator alert; it SHALL NOT be reported as delivered.

#### Scenario: An operator message reaches the bound channel's XO
- **WHEN** the operator posts a bare message in a bound channel and `flotilla watch` is running
- **THEN** the message is delivered (typed + submitted) into that channel's bound XO pane and the delivery is confirmed (a turn started) before it is logged/mirrored as delivered

#### Scenario: A message off any bound channel is ignored
- **WHEN** a message arrives on a channel that is not in the binding set
- **THEN** the relay ignores it (the gateway admits only bound channels)

#### Scenario: A relayed message that does not start a turn is never reported delivered
- **WHEN** a relayed submit does not produce a confirmed turn after the bounded retries
- **THEN** a LOUD operator alert is raised and no "delivered" log or mirror is emitted

### Requirement: Feedback-loop immunity

The relay SHALL drop any gateway message carrying a non-empty webhook identifier,
author-agnostically, before any other processing — so the `send`/`notify` audit
posts can never re-enter the relay. This guard SHALL hold on EVERY bound channel,
and SHALL hold even if the author authorization is later broadened.

#### Scenario: The audit mirror does not feed back on any channel
- **WHEN** an audit/notify webhook post lands in any bound channel (a webhook message)
- **THEN** `flotilla watch` ignores it (no self-injection storm), on that channel and every other

### Requirement: Operator-only authorization

The relay SHALL act only on messages authored by the configured operator user id,
on every bound channel. All other authors SHALL be ignored. There is no per-command
authorization; the operator's account (and its two-factor authentication) is the
security boundary. (A future federation transport that lets a parent meta-XO
deliver into a child fleet's channel is a SEPARATE, explicitly-gated change that
introduces a pinned parent allow-list; it is not part of this delta.)

#### Scenario: Non-operator message ignored on every channel
- **WHEN** a message from any author other than the operator arrives on any bound channel
- **THEN** it is ignored

### Requirement: Routing to the XO or a named agent

Within a bound channel, a bare operator message SHALL route to that channel's bound
XO. A message of the form `@<name> <body>` SHALL route `<body>` to `<name>` when it
resolves (case-insensitive) against **that channel's member scope** — for a project
channel, the XO's desks; for the fleet-command channel, the project-XO members. An
unresolved `@<name>` SHALL route the whole message to the channel's XO with a
one-line notice. A leading `@@` SHALL remain the literal-`@` escape hatch to the
channel's XO. Bodies SHALL be preserved verbatim, including newlines.

#### Scenario: A desk is addressed within its project channel
- **WHEN** the operator posts `@backend run the tests` in project-alpha's channel and `backend` is one of alpha's members
- **THEN** `run the tests` is delivered to alpha's `backend` desk pane

#### Scenario: A project-XO is addressed within the fleet-command channel
- **WHEN** the operator posts `@alpha-xo status across your desks` in the fleet-command channel and `alpha-xo` is a member of the fleet-command binding
- **THEN** the message routes to `alpha-xo` (the project chief), per the configured cross-tier transport

#### Scenario: An unknown @name falls back to the channel's XO
- **WHEN** the operator posts `@nope do X` in a bound channel where `nope` is not in that channel's member scope
- **THEN** the whole message routes to that channel's bound XO with a one-line notice naming the unknown token
