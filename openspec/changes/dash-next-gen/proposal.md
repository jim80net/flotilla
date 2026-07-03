# Proposal — dash next-gen: tri-surface session mirroring + goals DAG (#267)

## Why

The operator directed two coupled capabilities for **flotilla-dash next generation**
(flotilla#267, 2026-07-03):

1. **Tri-surface session mirroring with per-surface verbosity.** One agent session, three
   renderings — HTML dash (configurable info↔debug), Discord (info), tmux (verbose/raw). The
   level knob is **per-surface configuration**, not per-message authoring.

2. **A goals DAG as a first-class dash view** — same tier as Conversations and Issues. Operator
   framing: *"hierarchy is where we develop mental maps… I'm not getting much value just from a
   list of issues."* Design must seriously explore the DAG **replacing issues as the primary
   work-tracking surface**: work items hang off goal nodes; desk-level work rolls up to
   fleet-level goals. This is flotilla's **mental-map core feature made structural** — the dash
   today shows fleet *state*; the goals DAG shows fleet *purpose* and how current work serves it.

Today's gaps (grounded in code):

- **Mirroring is Discord-only for desk turn-finals.** `deskMirror.run` (`cmd/flotilla/mirror.go`)
  posts to Discord; tmux shows the raw pane; the dash reads only the CoS ledger (operator↔XO) and
  a flat backlog strip — desk mirror content never reaches the dash (`internal/dash/assets/dash.js`
  `renderReaderMapPlaceholder` is still a Pillar E stub).
- **Three mirror paths are independent** — desk Discord mirror, CoS append (`mirrorRelayToLedger` /
  `mirrorNotifyToLedger`), dash read — with no unified fanout from one session event.
- **Issues are a flat GitHub list** (`internal/dash/tracker/gh.go`) with no hierarchy linking
  work to fleet purpose.
- **Backlog is flat markers** (`internal/backlog/backlog.go`) — no graph, no roll-up to goals.

Mechanical reader-modeling (Pillars A–D largely shipped) already defines the publish pipeline
(`readerModelInternal` → envelope render → Discord). Pillar E (per-desk envelope ledger + dash
render) was spec'd but not shipped. **#267 subsumes and extends Pillar E** into tri-surface
mirroring rather than inventing a parallel path.

## What Changes

**Design scope only** — implementation follows design trio + COS gate.

- **Openspec design** for tri-surface session mirror fanout (extend `deskMirror`, not a new
  transport) and goals DAG (new capability, new persistence, dash IA).
- **Information architecture** — Goals tab at parity with Conversations and Issues; explore
  Goals-as-primary / Issues-as-drill-in.
- **DAG data model + persistence** — roster-adjacent artifact, federation roll-up semantics.
- **Maintenance model** — coordinators own goal structure; work items link from backlog/issues.
- **Issues coexistence / migration** — phased promotion of Goals over Issues tab.
- **Mirroring verbosity mapping** — relate info/debug/verbose to existing `readermap.Render`,
  raw turn-final, and mirror decision metadata.

## Lane split (coordinate before touching shared seams)

| Owner | Scope |
|---|---|
| **flotilla-dev** (this lane) | Mirror fanout + session ledger write path; goals graph schema + parser + rollup; dash read-model APIs (`/api/goals`, `/api/session-mirror`); openspec + core tests |
| **flotilla-dash desk** (follow-on) | Goals graph visualization UX, session-mirror thread rendering polish, accessibility — consumes flotilla-dev APIs; no duplicate read paths |

Shared seams requiring coordination: `cmd/flotilla/mirror.go`, `internal/dash/readmodel.go`,
`internal/dash/assets/*`.

## Out of Scope (this change)

- Live supervised fleet trial of goals DAG editing (operator-scheduled).
- Replacing GitHub Issues as the system of record (issues remain authoritative for issue-shaped
  work; goals are the mental-map layer above them).
- Tier-2 semantic judge in auto-mirror (unchanged — CLI-only per mechanical-reader-modeling).
- tmux pane filtering (verbose surface **is** the raw pane — no new tmux transport).

## Impact

- `cmd/flotilla/mirror.go`, `internal/watch/detector.go` (mirror hook wiring)
- `internal/dash/readmodel.go`, `internal/dash/server.go`, `internal/dash/assets/*`
- **NEW** `internal/goals/`, `internal/sessionmirror/` (or equivalent)
- `openspec/specs/dash`, **NEW** `goals`, **NEW** `session-mirror` capabilities
- `openspec/specs/fleet-visibility`, `openspec/specs/reader-modeling` (deltas)
- `flotilla.example.json` optional goals-path example (generic only)

## References

- flotilla#267 (operator directive)
- `openspec/changes/flotilla-dash/` (Phase 0 dash architecture — reader + control proxy)
- `openspec/changes/mechanical-reader-modeling/` (Pillars A–E; tri-surface extends E)
- `docs/mechanical-reader-modeling-design.md` (envelope ledger spine)