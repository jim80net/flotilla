# Authorization Domains

> **Design record.** The schema-v1 grant loader and deny-by-default resolver are
> shipped as a secret-free internal broker seam. Provider brokers and operator
> surfaces remain later delivery slices. The PA-only Gmail runbook documents
> the narrower interim deployment.

An Authorization Domain is a named trust boundary that receives capabilities
without receiving the fleet's whole secret environment. Domains answer two
different questions:

1. **Who may act?** A desk, a flotilla subtree, or a future node workload.
2. **What may it do?** A capability, provider scope, resource selector, and
   approval policy.

Network reachability is never authority. A process being reachable over a
private LAN, a mesh VPN, or a managed endpoint does not grant it a capability.

## Grant shape

The first ratified grant is PA read access through Gmail API:

```yaml
schema: 1
id: pa-gmail-readonly
principal:
  kind: desk
  name: pa
capability: gmail.api
oauth_scopes:
  - https://www.googleapis.com/auth/gmail.readonly
actions:
  - gmail.messages.list
  - gmail.messages.get
  - gmail.threads.list
  - gmail.threads.get
  - gmail.labels.list
  - gmail.labels.get
resources:
  accounts:
    - operator-primary
  labels: [] # empty means no narrower broker selector yet
secret_ref: pa-gmail-oauth
approval:
  send: deny
  modify: deny
audit:
  mode: metadata-only
  retain: P30D
```

The committed grant contains a logical `secret_ref`, never a filesystem path,
client secret, refresh token, account address, node address, or deployment
identifier. A host-local resolver binds that reference to secret material.

Schema v1 is strict: each file contains exactly one grant, unknown fields and
additional YAML documents are errors, and a set of files is adopted only when
every grant validates and every grant ID is unique. Principals must name a
roster desk or flotilla coordinator. Capabilities, actions, OAuth scopes, and
resource selectors use exact identifiers—wildcards and implicit expansion are
not accepted. Optional `expires_at` and `revoked_at` values are RFC 3339
timestamps; either becomes a denial once reached. `revoked: true` denies
immediately.

`oauth_scopes` and `actions` are deliberately separate. OAuth is the provider's
outer ceiling; the broker's action allowlist is the product boundary. Google
classifies `gmail.readonly` as a **restricted** scope and describes it as access
to messages and settings. It does not restrict access to a particular Gmail
label or flotilla. Any account/label/query restriction must therefore be
enforced by the broker, not inferred from the token. See Google's
[Gmail API scope reference](https://developers.google.com/workspace/gmail/api/auth/scopes).

## Resolution and inheritance

Resolution is deny-by-default:

1. Authenticate the calling workload as a roster principal.
2. Collect grants attached directly to that desk and its flotilla ancestors.
3. If it runs on a node, intersect those grants with the attested capabilities
   the node exposes.
4. Resolve `secret_ref` inside the broker; do not return provider credentials to
   the caller.
5. Enforce action, resource, approval, expiry, and revocation policy on every
   operation.
6. Emit a metadata-only audit result without message bodies, tokens, or secrets.

An allow is released to a provider broker only after the metadata audit sink
accepts its decision event; an audit failure therefore fails closed. The opaque
authorization can pass the grant's logical `secret_ref` to a broker-internal
lookup, but denied requests cannot invoke that lookup. Decision events contain
only time, principal, capability, action, grant ID, result, and reason.

Grants are additive; absence is denial. A child desk does not weaken an
ancestor's constraint. A node may narrow authority but never broaden it.
When a grant lists labels, the request must name one of those labels; an empty
label is not evidence of an allowed selector. An empty grant label list means
the grant has no narrower label selector yet.

## Secret boundary

The durable design keeps provider credentials in a broker or platform secret
store and gives the seat only the ability to request approved operations.
Provider refresh tokens do not belong in:

- the shared flotilla secrets file;
- a committable roster, grant, launch example, or worktree file;
- a coordinator environment;
- a prompt, issue, PR, chat message, log, or audit body.

The interim PA deployment uses a host-local `0600` file and exposes only its
path to the PA launch recipe. That is valuable **configuration isolation**, but
it is not a hard security boundary when every harness process runs as the same
Unix user: another same-UID process can read a known path. A deployment needing
adversarial isolation before the broker ships must run PA/the broker under a
distinct OS identity or an equivalent platform credential boundary. The
[PA Gmail runbook](./pa-gmail-api-runbook.md) states this limitation explicitly.

## Node phase

A future node joins with a device key and attested node identity. Tailscale,
private LAN, and managed addressing are transport adapters only. Enrollment
binds the device identity to:

- permitted local capabilities and secret references;
- eligible desk/flotilla workloads;
- expiry and revocation state;
- an authenticated channel such as mutually authenticated TLS or the
  equivalent identity-aware mesh transport.

Effective authority is `(desk + flotilla grants) ∩ node capabilities`. Secrets
remain on the node and operations cross the authenticated broker interface.
Node resolution requires an explicit attestation containing the requested
capability, action, scope, and resource. Missing attestation is denial, and an
attestation can only narrow an otherwise-valid desk/flotilla grant.

## Delivery sequence

1. **Interim:** PA-only Gmail OAuth material and PA-only launch path.
2. **Grant core:** schema validation, principal resolution, deny-by-default
   secret references, and metadata audit.
3. **Gmail broker:** read-only methods first; resource selectors and revocation.
4. **Operator surface:** list, grant, expire, and revoke without revealing secret
   values.
5. **Nodes:** enrollment, attestation, transport adapters, and node-local broker.

Any move beyond `gmail.readonly` is a new grant decision. Draft, send, modify,
delete, delegation, and domain-wide access never inherit from the read grant.

### Read-only Gmail broker

The first provider broker is bound to effective principal `pa` and grant
`pa-gmail-readonly`. Every operation completes grant authorization and its
metadata-only decision audit before consulting `FLOTILLA_PA_GMAIL_OAUTH_FILE`.
The file must be a regular, non-symlink file owned by the broker's OS identity
with mode `0600`, and must contain a strict Google `authorized_user` document.

The broker refreshes only an exact `gmail.readonly` token, then verifies the
approved account with `users.getProfile` and performs a `users.labels.list`
smoke check. Its callable surface is limited to list/get for labels, messages,
and threads. It rejects send, modify, delete, unknown operations, and resource
IDs that could alter the provider path. Audit events contain policy and method
metadata only—not token material, response bodies, account addresses, or host
paths. Provider and refresh failures are returned as sanitized error classes.

The production entry point is `flotilla gmail --grant <grant.yaml> <operation>`.
It derives the effective seat from `FLOTILLA_AGENT` and refuses any value other
than the roster principal `pa`; it does not accept a caller-principal flag.
Approved account, logical account resource, audit path, and logical-label to
Gmail-ID bindings are host-private configuration in
`FLOTILLA_PA_GMAIL_APPROVED_ACCOUNT`, `FLOTILLA_PA_GMAIL_ACCOUNT_RESOURCE`,
`FLOTILLA_PA_GMAIL_AUDIT_FILE`, and `FLOTILLA_PA_GMAIL_LABEL_BINDINGS`. The last
is a JSON object such as `{"inbox-label":"INBOX"}`; grant files continue to use
portable logical labels while provider IDs remain host-local.

Credential loading opens the OAuth file once with no-follow semantics, validates
type, owner, and mode from that descriptor, and reads the same descriptor. A
restricted thread listing is reduced to thread IDs/history IDs; provider
snippets are never released as proof of a message-scoped label.
