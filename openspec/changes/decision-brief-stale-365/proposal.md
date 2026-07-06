# Proposal — decision-brief stale re-dispatch (#365)

## Why

The auto decision-brief detector re-dispatched brief requests for items that already
carry briefs on `work_items[].brief`, costing desk turns. Restart amnesia (in-memory
TryBeginDispatch / Confirm) and pre-compile scan staleness compounded the misfires.

## What Changes

1. **Node-level scan fix** — skip goal-level triggers when any `work_items[].brief` is present.
2. **Dispatch-time re-verify** — re-read `fleet-goals.json` immediately before enqueue.
3. **Persistent claims** — `flotilla-decision-brief-claims.json` survives watch restarts.

## Impact

- `internal/decisionbrief/`, `cmd/flotilla/watch.go`