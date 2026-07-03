## Why

Confirmed delivery (#213) closes the silent-drop class on the hot path, but a message
can still go unanswered when the fleet is wedged, the coordinator misses it, or only
a "working on it" soft-ack lands with no follow-up. The operator needs an
after-the-fact standing detector that surfaces un-acked requests without re-alerting
on every scan or flagging messages the fleet may still be mid-answering.

## What Changes

- **Pure scan** (`internal/unacked`): mechanical classifier over ascending channel
  history — operator requests lacking a substantive fleet webhook reply.
- **Watch poller** (`internal/watch`): 30m scan cadence, alert-once dedup state
  (`flotilla-unacked-alerted.json`), coordinator wake via confirmed delivery.
- **Transport seam**: optional `RecentHistory` capability on the coordination bus
  (discord REST `Recent`).
- **Busy retry**: coordinator wake skipped on `ErrBusy` retries next sweep without
  re-alerting; the channel digest is the persistent backstop.

## Non-Goals

- Semantic/LLM classification of operator intent (v1 is mechanical heuristics).
- Re-injecting or auto-relaying operator messages from the backstop.
- Folding #213 `confirmedDeliver` extraction (separate PR unless natural).