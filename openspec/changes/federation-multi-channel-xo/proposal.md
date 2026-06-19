## Why

F#105 validates `channels[]` with **one XO = one channel** (`internal/roster/roster.go`
rejected an agent that was the `xo_agent` of more than one binding). But the operator's
provisioned topology inherently needs an XO to be primary in **more than one** channel:

- a **C2 group** of several channels (`#c2`, `#fo`, `#product`, `#pa`) where the meta-XO
  (`hydra-ops`) is primary across the group; and
- a **per-flotilla group**, each with its own command channel — so a flotilla XO is primary
  in BOTH its C2-group channel (`#fo`/`#product`/`#pa`) AND its own command channel
  (`#fo-command`/`#product-command`/…).

The one-XO-≤1-channel rule cannot express that two-level structure. Confirmed empirically:
`roster.Load` on the operator's 14-channel map fails with *"agent family-office is the
xo_agent of more than one channel binding"* (and the same for `hydra-ops`, `flotilla-dev`,
`pa`). The alternative (collapse the structure to one channel per XO) deviates from what the
operator specified.

## What Changes

- **Relax `xo_agent`-of-≤1-binding → allow an agent to be the XO (hub) of MULTIPLE channels**
  (XO→channels is one-to-many). The **load-bearing invariant is preserved**: each CHANNEL is
  still bound to exactly one XO (the `seenChan` uniqueness check stays — one relay per
  channel, no double-delivery).
- **Define the multi-channel outbound semantics:** an XO's **first-listed binding** is its
  **primary/home channel** — what `ChannelForXO` returns for outbound (CoS-ledger) tagging.
  No behavior change for a single-channel XO; deterministic (roster order) for a multi.
- **No change** to: inbound routing (`BindingForChannel` routes by unique `channel_id`),
  per-channel `@name` member resolution, the legacy single-`channel_id` synthesis path, or
  the `channel_id`↔`channels[]` mutual exclusion.

## Impact

- **Affected spec:** `federation` (MODIFIED "Channel↔XO binding configuration" requirement).
- **Affected code:** `internal/roster/roster.go` — drop the `seenXO` uniqueness check; clarify
  `ChannelForXO` (first-listed = primary). `internal/roster/federation_test.go` — the
  "agent is xo of two bindings" fail-case becomes a positive test; add coverage that a
  channel bound to two XOs still fails, per-channel members are unaffected, and `ChannelForXO`
  returns the first binding.
- **Risk:** LOW. Only the inbound-irrelevant uniqueness check is removed; the routing-critical
  one-relay-per-channel invariant is intact and explicitly re-tested. Unblocks the operator's
  14-channel federation cutover.
