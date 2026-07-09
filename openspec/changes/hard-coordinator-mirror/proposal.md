# Proposal — hard coordinator egress mirror (P0)

**Dispatch:** `flotilla-dispatch-a4838558` (operator regression, 2026-07-08).

## Why

Operator reports **Codex COS conversation is invisible in Discord** — only `flotilla watch` error
alerts appear. Root cause: coordinator turn-final egress relies on the **Claude Code Stop hook**
(`deploy/flotilla-xo-discord-mirror.sh`). Codex has no Stop hook. Watch deliberately skips Discord
for the primary coordinator (`coordinatorMirrorOnFinish` sets `ledgerOnly=true`) assuming the hook
posts. Session-mirror dash ledger may append, but **operator Discord + reliable dash parity fail**.

This is a **product regression**, not operator misconfiguration. Operator-visible coordinator output
must be **mechanically enforced** — durable delivery, retry, chunking, audit log, failure surfacing —
not best-effort hook etiquette.

## What Changes

1. **Harness-agnostic egress** — watch `CoordinatorMirrorOnFinish` becomes the **authoritative**
   path for coordinator turn-finals (all surfaces with `ResultReader`, including codex).
2. **Tri-surface fanout** — every operator-visible coordinator finish posts to **Discord** +
   **session-mirror ledger** + **CoS context ledger** (when configured).
3. **Durable outbox** — disk-backed pending queue with retry (pattern: `#286` relay queue).
4. **Classification** — operator-visible vs inter-agent `--no-mirror` traffic (unchanged send rules).
5. **Dedup** — content-hash window prevents double-post when Claude Stop hook also fires during migration.
6. **Doctor B013** — fails when live coordinator has no successful mirror within policy window.

**Non-goals:** Replacing `flotilla notify` discretionary path; mirroring detector KindDetector
prompt injections; changing laminar-flow adjutant inject policy.

## Impact

- `cmd/flotilla/watch.go`, `cmd/flotilla/mirror.go`, `internal/watch/detector.go`
- `internal/egress/` (new outbox) or `internal/watch/coordinatormirror/`
- `openspec/changes/codex-coordinator-seat/` §2.3 correction
- Bootstrap doctor B013 (PR #520 cross-ref)

## Gate

P0 — surface PR to COS. Builder does not self-merge.