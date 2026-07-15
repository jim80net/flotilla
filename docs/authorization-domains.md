# Authorization Domains

> **Design record — proposed.** This document defines the product boundary and
> first grant. The generic grant resolver and broker are not shipped yet. The
> PA-only Gmail runbook documents the narrower interim deployment.

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

Grants are additive; absence is denial. A child desk does not weaken an
ancestor's constraint. A node may narrow authority but never broaden it.

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
