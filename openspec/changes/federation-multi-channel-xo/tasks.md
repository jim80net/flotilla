# Tasks — federation: allow an XO to hub multiple channels

## 1. Implementation

- [x] 1.1 `internal/roster/roster.go`: remove the `seenXO` uniqueness check (allow an agent
      to be `xo_agent` of multiple bindings); KEEP `seenChan` (one relay per channel).
- [x] 1.2 `internal/roster/roster.go`: clarify `ChannelForXO` — the XO's first-listed binding
      is its primary/home channel (no behavior change for single-channel XOs).
- [x] 1.3 `internal/roster/federation_test.go`: convert the "agent is xo of two bindings"
      fail-case to a positive test; assert each channel still routes to its XO, per-channel
      members are unaffected, `ChannelForXO` returns the first binding, and a channel bound
      to two XOs STILL fails.

## 2. Spec

- [x] 2.1 MODIFY the `federation` "Channel↔XO binding configuration" requirement (one
      channel→one XO preserved; one XO→many channels allowed; first-listed = primary).
- [x] 2.2 `openspec validate federation-multi-channel-xo --strict`.

## 3. Gate

- [ ] 3.1 Trio (systems-review + open-code-review + STORM) — confirm specifically: (a) one
      relay per channel still holds (no channel gets two XOs), (b) @-resolution + member
      semantics unaffected, (c) no regression to the legacy single-channel synthesis path.
- [ ] 3.2 PR; CI green; cubic via GraphQL isResolved; merge on clean gates.
- [ ] 3.3 After merge: hand the operator the ready-to-paste `channels[]` (14-channel map +
      `#spark-c2` with the FULL `agents[]` as members) + cutover steps (remove `channel_id`,
      restart `watch`).
