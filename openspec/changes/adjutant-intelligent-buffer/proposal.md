# Proposal — adjutant intelligent conversation buffer (#593)

**Dispatch:** `flotilla-dispatch-6a3fa90e`, `flotilla-dispatch-9769c2e6`.

## Why

#549 shipped mechanical dual-fork: every operator message to a coordinator split into leader
verbatim + adjutant observation envelope. That is not an intelligent buffer — it spams
Discord, instructs the adjutant not to act, and still interrupts the leader.

Operator intention (issue #593, verbatim):

> The intention was that the adjutant acts as an intelligent buffer for conversations,
> including operator words, not this mechanical forking path.

Operator framing (issue #593 comment, verbatim):

> If you think of it, the adjutant is actually the intelligence of flotilla manifest. It is
> the brainstem or central nervous system to the brain. And as such, reflexes and signals
> need to be faithfully reproduced there.
>
> And the intelligence of the whole design needs to be manifest at that point. So what I'm
> trying to say is that the adjutant is going to be carefully developed in order to refine
> how the interaction with the chief of staff and the XOs and the desks are tuned for
> performance.

## What changes

### Product acceptance (core)

1. **Coalesce** — related operator messages conveying one idea arrive as one coherent unit
   before leader interrupt / seam forward.
2. **Disaggregate** — multi-intent message(s) split into discrete dispatches (right owner /
   work item) with provenance.
3. **No mechanical dual-fork** — single ingress to adjutant; #592 re-Apply hygiene secondary.
4. **Verbatim at delivery** — leader pane receives operator body byte-for-byte when engaged.
5. **One audit line** per operator message at ingress.

### Phase 1 (mechanical — PR #594)

- Single ingress to adjutant front office
- Durable operator buffer items + seam verbatim forward
- `ingressResolved` / `bufferRecorded` busy-defer hygiene
- `adjutantBufferContract` names coalesce / disaggregate judgment duties

### Phase 2+ (judgment — ongoing)

- Arc assembly windows and intent segmentation automation
- Charter-driven performance tuning of buffer / interrupt policy
- Discrete dispatch provenance in buffer / ledger

## Impact

- `internal/watch/coordinator_ingress.go` — ingress topology
- `cmd/flotilla/watch.go` — buffer hook, seam forward, prompt contract
- `internal/watch/adjutantbuffer/` — operator item encoding
- Supersedes dual-fork interpretation of #549
- Retains hygiene from #592

## Closes

- #593 (product arc; Phase 2 items remain in `tasks.md`)