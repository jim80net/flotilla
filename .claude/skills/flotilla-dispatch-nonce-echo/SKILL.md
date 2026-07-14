---
name: flotilla-dispatch-nonce-echo
description: "Execution-desk finish contract for inbound dispatches — echo the verbatim flotilla-dispatch-<hex> nonce in the operator-facing turn-final before going idle; re-echo once after a dropped-dispatch resume wake. Read on every flotilla send, dropped-dispatch resume, or before settling idle after coordinator tasking."
type: skill
queries:
  - "flotilla dispatch nonce echo turn-final"
  - "dropped-dispatch resume what do I do"
  - "flotilla-dispatch hex footer inbound ledger"
  - "before going idle on flotilla send"
keywords:
  - flotilla-dispatch
  - nonce-echo
  - dropped-dispatch
  - inbound-ledger
  - execution-desk
boost: 0.08
---

# Dispatch nonce echo — execution-desk finish contract

**Audience:** execution desks (grok dash/build lanes, product workhorses) — not coordinators.
Coordinators are skipped on the inbound ledger by design.

**Full loop:** `llm.md` §9 · code: `internal/inbound/`, `internal/watch/dropped_dispatch.go`

## The habit (three beats)

1. **Read the footer** on every `flotilla send` body. The sender appends a `#472` ack
   block with a nonce: `flotilla-dispatch-<hex>` (lowercase hex). Do not strip it before
   you internalize the task.

2. **Echo verbatim before idle.** Your operator-facing turn-final (footer is fine) MUST
   include the exact nonce string once work for that dispatch is done:

   ```text
   …bottom line + mini-brief…
   Nonce: flotilla-dispatch-a1b2c3d4
   ```

   Confirmed delivery only means the pane accepted the paste — the inbound ledger stays
   pending until the finish hook sees this echo (or a distinctive dispatch snippet).

3. **Dropped-dispatch resume → do it again.** A `[flotilla dropped-dispatch resume]` wake
   means watch re-injected a still-pending ledger entry (an intervening duty turn displaced
   the original). Resume the dispatch body, finish the work, then **re-echo the same nonce**
   before going idle again.

## Anti-patterns

- Going idle with `idle` / settled marker while a dispatch nonce was never echoed
- Paraphrasing the nonce (`dispatch-a1b2…`) — must be the full `flotilla-dispatch-<hex>` token
- Treating coordinator traffic as inbound-tracked (only execution desks are ledgered)

## Smoke (optional)

After `flotilla send --from meta-xo backend "…"`, a pending entry appears in
`flotilla-<recipient>-inbound.json` beside the roster until the nonce echo clears it.
Watch log: `inbound track <agent> recorded reason=ok`.