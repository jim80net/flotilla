## ADDED Requirements

### Requirement: Logical message identity is assigned at composition
The system SHALL assign an immutable message identity before the first delivery attempt and SHALL preserve it through durable queueing, retries, relays, nonce assignment, and serialization. Attempt and hop identities SHALL be subordinate to the logical message identity.

#### Scenario: One message is relayed under multiple nonces
- **WHEN** the same composed message arrives through multiple attempts with different dispatch nonces
- **THEN** the receiver recognizes one logical message
- **AND** records the additional attempts without consuming the message again

#### Scenario: Legacy message has no identity
- **WHEN** a nonce-less or identity-less legacy message arrives
- **THEN** it follows an explicit legacy/unattributed path
- **AND** content similarity is not reported as proven identity

### Requirement: Operator origin provenance is immutable and replyable
An operator-message envelope SHALL record at composition its typed origin surface (`chat_relay`, `pane`, or `dash`), origin channel/address, and the authenticated operator identity consumed by that channel. Every relay SHALL preserve that origin and append itself only as a hop. A reply SHALL use the recorded origin channel and authenticated identity and SHALL NOT silently fall back to the current pane, relay hop, or default channel.

#### Scenario: Operator message crosses a relay
- **WHEN** an operator composes a message on one supported surface and it reaches a desk through one or more relays
- **THEN** the desk's reply is delivered to the original surface and channel/address
- **AND** the origin channel authorizes the reply against the operator identity recorded at composition

#### Scenario: Origin transport is unavailable
- **WHEN** a reply cannot currently reach its recorded origin
- **THEN** it remains pending or fails visibly for that origin
- **AND** it is not silently redirected to another surface

### Requirement: Ack instructions and registries share one eligibility model
The component that emits an acknowledgement instruction and the component that creates the corresponding registry row SHALL use the same recipient-capability decision. The system SHALL emit an acknowledgement instruction only when the intended recipient can successfully acknowledge that row. Coordinator recipients SHALL have truthful, queryable receipt state.

#### Scenario: Coordinator receives an acknowledged dispatch
- **WHEN** a dispatch to a coordinator includes an acknowledgement instruction
- **THEN** a matching pending registry row exists for that coordinator and message
- **AND** the coordinator's acknowledgement can transition it to `acked`

#### Scenario: Recipient cannot acknowledge
- **WHEN** a recipient class has no supported acknowledgement path
- **THEN** the system does not emit an acknowledgement instruction
- **AND** inspection reports the actual supported delivery state without claiming acknowledgement

### Requirement: Delivery receipts distinguish every exit and recipient class
For every logical message and intended recipient, the system SHALL durably expose timestamped, path-attributed receipt history distinguishing `composed`, `queued`, `delivered`, `consumed`, `acked`, `dropped`, and `canceled`, plus duplicate attempts. Absence of evidence SHALL be reported as `unknown` rather than success.

#### Scenario: Query distinguishes delivered from dropped
- **WHEN** two messages are absent from active delivery state because one was delivered and one was dropped
- **THEN** a query by each message identity returns the distinct outcome, time, recipient, and delivery path

#### Scenario: Ack already exists before recipient command
- **WHEN** the recipient invokes acknowledgement for a message already marked `acked`
- **THEN** the command reports the existing durable transition idempotently
- **AND** inspection identifies the actor and path that recorded it
