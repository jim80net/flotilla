# P0 message delivery health

The night's delivery and publication P0s are one incident. A publication-freeze
order in force at 13:11:00Z did not reach one seat until 16:06:20Z because it sat
behind this delivery defect; a push landed inside that 2h55m20s control-coverage
gap. The breach was non-culpable by non-delivery, but the safety control was still
unenforceable there: an order that cannot arrive cannot bind its recipient.

This change separates three facts that previously collapsed into “busy” or
“delivered.” A single rendered frame is evidence only for the instant captured.
A cursor-vouched working frame that remains byte-identical for two minutes while
the latest completed-result evidence is also unchanged becomes `wedge`, not benign
`working`. A changed frame or result resets the interval immediately and preserves
the fast path for genuinely moving panes. `wedge` remains non-deliverable: the
confirm gate does not paste into it, but the alert is immediate and names temporal
evidence rather than claiming “busy mid-turn.”

Delivery stages are durable metadata: `queued`, `attempted_refused` (with typed
classifier evidence), `submitted`, `recipient_consumed`, `canceled`, and `failed`.
Transport delivery becomes true at `submitted`, after paste + Enter is confirmed.
`recipient_consumed` is deliberately separate: it means the recipient later
handled/acknowledged the dispatch. This change does not collapse handled into
delivered and does not spend recipient inference to establish transport truth.

## Evidence and fixture provenance

The live fixture corpus is deployment-private and must never be copied into the
product repository. Tests model its generic shape and cite the private evidence by
path only:

- `.claude/desk-state/fixtures/build-wedge-frame-20260811/`
- `.claude/desk-state/fixtures/memex-wedge-frame-20260811/`
- `.claude/desk-state/fixtures/ventures-adj-wedge-frame-20260811/`
- `.claude/desk-state/fixtures/healthy-controls-20260811/`
- `.claude/desk-state/fixtures/outbox-head-state-20260811.json`
- `.claude/desk-state/fixtures/README-p0-delivery-fixtures-20260811.md`

The regression uses only generic strings and two observations: static working
chrome plus stable completed result crosses the bound; moving chrome resets it.

## Causal trace, 4–11 August

The deployed daemon is clean revision `5e4ea1cfa1f2` (2026-08-06). Its delivery
gate classifies from a single frame. The later recipient-authoritative push/pull
buffer change `25caeb02` is not an ancestor of that binary. It increased durable
queue occupancy and made the false-busy classifier’s cost visible; it did not
introduce the running classifier fault. The introducing mechanism is the original
single-frame `Driver.Assess` → `Confirm.Submit` authority path. The reviewed cursor
provenance commits close quoted historical chrome; this temporal layer closes the
remaining indefinitely-static working claim.
