# Proposal: the issues tab becomes the fleet work ledger

## Why

The dash issues tab is a flat, single-repo mirror of GitHub. The operator's ask
(operator-approved design, 2026-07-14) names three gaps:

1. Issues must be **organized by flotilla and desk** — the org's real structure,
   not a flat list.
2. It must track **every fleet repo**, not just the product repo.
3. It must provide **data GitHub cannot know** — context flotilla itself generates:
   *what's happening* (which desk owns the issue, its live state, last activity)
   and *what's ordered* (which dispatches reference it, with provenance).

Requirement 3 is the differentiated layer: GitHub is the system of record for the
issue text; flotilla is the only system that knows the fleet's live relationship to
it. Generating that join and rendering it grouped by the org's structure is the
operator's mental map of fleet WORK — the adjutant layer made visible.

## What changes

- **A derived work-ledger read model** (`WorkLedgerDoc`): built on read, never
  stored as truth (the BoardDoc/TopologyDoc pattern). Per issue it joins:
  - **Owning desk + flotilla** — from a `desk: <name>` body trailer (extending the
    proven `goal-id:` trailer convention), with an honest fallback: repo → seats
    whose `primary_repo`/`secondary_repos` match → their org-DAG flotilla, rendered
    as "unassigned within <flotilla>". Flotilla is always derivable; desk is exact
    when stamped, honestly unassigned when not. Nothing fabricated.
  - **What's happening** — the owning desk's live surface state + loop posture
    (the existing per-agent read model) and last ledger activity.
  - **What's ordered** — dispatches referencing the issue (issue-ref scan over the
    ledger/consumed registry), each with live dispatch status and provenance class
    (operator-direct / coordinator-routed / walk-generated / desk-filed).
  - Every generated field carries the freshness contract (fresh/stale/absent) —
    a stale snapshot renders stale, never as a confident current state.
- **Multi-repo tracking** (`MultiTracker`): a multiplexer implementing the existing
  `Tracker` interface over one gh-backed tracker per unique repo in the roster's
  `primary_repo ∪ secondary_repos` (plus the pinned repo). Refs become
  `owner/repo#N` (the existing `IssueRef` format). The repo set derives from the
  roster — zero new configuration.
- **The view**: default grouping flotilla → desk → issues in org-DAG order; each
  row carries a state chip, a happening line, and an ordered badge; existing
  filters retained plus a repo filter. A GitHub-only row (no fleet context) renders
  its absence honestly.

## What does NOT change

- GitHub remains the issue system of record — flotilla generates and provides the
  context layer; it does not persist a second truth.
- The existing single-repo tracker behavior remains the degraded mode when the
  roster carries no repo fields.
- Fleet-specific names render in the deployment's private dash only; committed
  artifacts (fixtures, docs, tests) use the generic example roster roles.

## Status / sequencing

- Operator-approved design (2026-07-14). This change is opened at scaffold stage.
- **Visual direction is being reconciled with the fleet's UX design work and lands
  IN this change (design.md + view spec deltas) before implementation** — one
  agreed design, no forked halves.
- Implementation is sequenced BEHIND the current delivery-reliability batch; the
  first implementation increment must be demonstrable (a rendered by-flotilla
  grouping on real data, not a schema-only PR).

## Impact

- Affected: `internal/dash/tracker` (MultiTracker), `internal/dash` (work-ledger
  read model + view), dash assets (issues tab), roster read (repo-set derivation),
  ledger/dispatch read (issue-ref join).
- New capabilities: work-ledger read model; multi-repo tracking; desk-attribution
  trailer.
