# Ceremonies — walks, parade, synthesis

Daily rhythm: products **walked** in the evening, **paraded** in the morning.
Orchestrate from the coordinator seat; deck story grammar is in
[`parade.md`](../parade.md).

## Daemon-fired cadence

The `flotilla-watch` wall-clock scheduler fires prompts from `<roster-dir>/schedules/`:

- **Evening walk** — dispatch PRODUCT WALK orders to every product-owning XO via
  `flotilla send`.
- **Morning parade** — build and deliver the parade deck.

Schedule survives session rotation (unlike in-session crons). Cross-check
`flotilla-schedule-state.json` against watch service logs after any coordinator
rotation. Heed busy-pane caveat in [`incident-response.md`](./incident-response.md).

**Parade fires before plausible operator arrival on purpose** — the parade waits for
the reviewer, not the reverse.

## Evening walk

Each product-owning XO walks its live product as its user — operator litmus paths,
real device widths, end to end. A defect the operator finds that a walk would have
caught is a walk failure (treat like a gate failure).

Each walk produces:

1. **Seven-C scorecard** — complete · correct · comprehensive · calibrated · concise
   · compelling · consistent — each 0–2 with cited justification. Note day-over-day
   **delta** vs prior scorecard.
2. **Generated work** — every C below 2 gets concrete items filed or dispatched,
   never narrated.

**COMPELLING** needs rendered pixels judged by a seeing agent: execution desk
captures screenshots; vision-capable agent grades. Capture desk cannot grade its own
capture.

Walks capture demo assets under `parades/<date>/assets/` for the morning parade.

## Skip-as-satisfied

If the fleet walked within ~6 hours, skip the scheduled walk and **record why** in
the backlog. Silent skip looks like a missed cadence.

## Morning parade

Author `parades/<today>/slides.md` per [`parade.md`](../parade.md) story grammar
(hook → setup → movement → climax → ask → button; seven-C grid as climax with delta
arrows and follow-through column).

- **Source = durable state only** — scorecards, ledgers, mirrors, PRs. Not deep
  session history re-reads at ceremony time.
- **Deliver immediately:** `flotilla notify --attach` deck + highlights
  ([`operator-comms.md`](./operator-comms.md) for limits).

## Visibility synthesis discipline

From [`visibility.md`](../visibility.md) and the `visibility-synthesis` skill:

- **Delta-only** — post what changed since last synthesis.
- **`idle` is correct** — never manufacture content.
- **Skip unreadable subordinates** — don't guess; don't block the rollup.
- **Aggregate upward** — one summary per tier; signal not plumbing.

## Ceremony context isolation

Answer from durable artifacts (`fleet-backlog.md`, scorecards, PRs). If a fact isn't
in durable state, dispatch a one-line ask to the owning XO — don't trawl transcripts.

## Prompt files

Edit `<roster-dir>/schedules/*.md` when ceremony shape changes — the daemon injects
what's on disk.