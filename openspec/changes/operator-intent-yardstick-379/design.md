# Design — operator-intent yardstick (#379)

## The gap, stated in one line

Intake capture of an operator's verbatim ask already exists in practice (#319 quoted the
original words in the issue body). Nothing re-reads that quote against the SHIPPED deliverable
before the work is called done. A deliverable can clear every code gate (CI, review trio, cubic)
and still miss what the operator actually meant — and today that only surfaces when the operator
notices and repeats himself, which is the exact failure #379 was filed to close (and which
recurred a second time while #379 itself sat open, per the operator's 2026-07-06 comment).

## Grounded seams this design rides (cite, do not re-derive)

- **The goals schema already carries a per-work-item free-text field for exactly this shape of
  problem.** `internal/goals/types.go:25` (`Goal.Brief`) and `:37` (`WorkItem.Brief`) — added for
  #347/#349, a markdown "decision package" attached to a goal or work item. This design adds a
  sibling field pair the same way, rather than inventing a new attachment mechanism.
- **`internal/decisionbrief` is the exact detector shape to clone.** `decisionbrief.go:47`
  (`FindGaps(Inputs) []Gap`) is a pure, I/O-free gap scan over the compiled goals doc; `:149`
  (`Tracker`) dedupes dispatch with `TryClaim` (`:173`) so overlapping async ticks can't
  double-fire (the fix landed in #352 P2 specifically for this race); `:184` (`DispatchPrompt`)
  builds the injected instruction text. This is the load-bearing precedent: **the same shape closes
  #379's gap**, one step later in the goal's lifecycle.
- **The wiring point already exists and already runs a sibling hook.**
  `internal/watch/detector.go:139-143` defines `DecisionBriefOnTick func()` on `DetectorConfig`,
  invoked once per tick (`:908`), dispatched off `d.mu` and async via `MirrorDispatch`
  (`:910-911`, mirroring `:878-879`'s pattern for `MirrorOnFinish`). A new
  `YardstickOnTick func()` hook slots in beside it, not instead of it — the two run independently
  because they watch different lifecycle transitions (gated-without-brief vs. done-without-verdict).
- **The class enum the detector reads is already exhaustive and settled.**
  `internal/dash/goals.go:257` documents `Class` as `done | in-flight | awaiting | blocked |
  active | unknown`, computed per work item from desk state, backlog marker, or GitHub issue
  state (`:750-778`). "Delivered" for this design means `Class == "done"` — no new status vocabulary.
- **The identity-append doctrine mechanism is proven for exactly this kind of per-turn discipline.**
  `internal/doctrine/assets/skills/decision-brief-on-blocked.md` is the template: a marker-fenced
  block (`<!-- flotilla:decision-brief-on-blocked -->` / `<!-- /flotilla:... -->`) installed on
  every desk via `flotilla doctrine install`, read by `FindGaps` and installer alike so the block
  and the mechanism can never drift out of sync.

## Schema changes

Two new field pairs on `Goal` and `WorkItem` (`internal/goals/types.go`), following the existing
`Brief` precedent exactly:

```go
// OperatorAsk captures the operator's verbatim words that originated this goal or work
// item (#379). A paraphrase must NEVER replace Quote — see doctrine.
type OperatorAsk struct {
    Quote   string `json:"quote"`             // verbatim operator words, no paraphrase
    Date    string `json:"date,omitempty"`    // ISO date captured
    Channel string `json:"channel,omitempty"` // discord channel / direct / handoff, etc.
}

// Yardstick is the delivery-time intent-check verdict: did the shipped deliverable match
// the verbatim OperatorAsk? (#379)
type Yardstick struct {
    Verdict string `json:"verdict"`         // "matched" | "drifted"
    Notes   string `json:"notes,omitempty"` // how it drifted, or what confirmed the match
    By      string `json:"by,omitempty"`    // desk/reviewer who ran the check
    Date    string `json:"date,omitempty"`
}
```

Added to both structs as `OperatorAsk *OperatorAsk `json:"operator_ask,omitempty"`` and
`Yardstick *Yardstick `json:"yardstick,omitempty"``. Pointer (not embedded value) so an absent
capture round-trips as a true absence, not a zero-value struct that looks like an empty capture —
this matters because "never captured" and "captured empty" must be distinguishable states for the
detector below.

`fleet-goals.example.yaml` / `.example.json` get one annotated example each, same as `brief` does
today, so a desk authoring a new goal sees the shape without reading Go source.

## The detector: `internal/yardstick` (new package, mirrors `internal/decisionbrief`)

```go
// Gap is one goals item that shipped (Class == "done") with a captured OperatorAsk but no
// recorded Yardstick verdict — the delivery-time intent-check was skipped.
type Gap struct {
    GoalID, GoalTitle, ItemKey string
    Ask                        goals... // the OperatorAsk being checked
    Owner                      string
}

func FindGaps(in Inputs) []Gap   // pure; walks the compiled dash.RenderedGoal tree same as decisionbrief
func ResolveOwner(g goals.Goal) string  // identical resolution order to decisionbrief.ResolveOwner
type Tracker struct{ ... }              // identical TryClaim/Reconcile shape
func DispatchPrompt(g Gap) string       // the injected instruction (below)
```

`FindGaps` fires when, for a goal or work item: `Class == "done"` AND `OperatorAsk != nil` AND
`Yardstick == nil`. It does **not** fire on items with no `OperatorAsk` at all — a work item that
was never operator-originated has no yardstick to run, and manufacturing one would be busywork,
not signal. This is the honest boundary of what's mechanically detectable (see "What this design
does NOT mechanize" below).

`DispatchPrompt` instructs the owning desk:

1. Re-read the verbatim `operator_ask.quote` word for word.
2. State in one or two sentences what actually shipped.
3. Record a verdict: `matched`, or `drifted` with the specific gap named (register, scope,
   an omitted dimension — the #319 register-drift case is the canonical example: "accomplished"
   vs. the operator's own "proud of").
4. Write the `yardstick` field on the goal/work item in `fleet-goals.yaml`, then
   `flotilla goals compile`.
5. If `drifted`, that is not a failure to hide — surface it in the same completion report (below);
   legitimate intent evolution and genuine drift both get recorded, only their next step differs
   (drift may need a follow-up ask to the operator; evolution just gets logged).

## Wiring

`DetectorConfig.YardstickOnTick func()` alongside `DecisionBriefOnTick`
(`internal/watch/detector.go:139-143`), invoked in the same tick block (`:908-919`), dispatched
via `MirrorDispatch` identically. `cmd/flotilla/watch.go` constructs both from the same compiled
`dash.GoalsDoc` read already in hand for `DecisionBriefOnTick` — no new file I/O, one scan produces
both gap sets.

## Doctrine: two identity-append blocks, not one

Splitting capture and delivery into separate doctrine blocks matches the two distinct moments a
desk acts, and lets each evolve independently (e.g. the six-element brief template already has its
own block; this is not a reason to merge unrelated disciplines into one wall of text):

- **`operator-ask-capture`** (new): the moment an operator gives an ask that becomes a goal or work
  item, capture `operator_ask.quote` verbatim — no paraphrase — in the same turn, before doing the
  work. Cites the founding directive verbatim (2026-07-04) as the canonical example of what
  "verbatim" means (the whole quote, including the parenthetical, not a summary of it).
- **`operator-intent-yardstick`** (new): before marking a goal/work item `done` that carries an
  `operator_ask`, re-read the quote against the deliverable and record a `yardstick` verdict. A
  `done` item with a captured ask and no yardstick is a defect, exactly as an `awaiting`/`blocked`
  item with no `brief` is today.

Both install via `flotilla doctrine install` using the existing marker-fence mechanism; registry
count moves from today's count (8, per `operator-direct-tasking`) to 10.

## Surfacing to the operator (the actual point of the mechanism)

The dash's goals/decision views already render `brief` in the decision modal (owned by
flotilla-dash per #352's coordination note). This design adds the symmetric render: wherever a
`done` item carries both `operator_ask` and `yardstick`, show the operator's own quote directly
beside the verdict and the one-line "what shipped" — one glance answers "is this what I meant?"
without the operator having to reconstruct the ask from memory or re-read the original issue.
Drifted verdicts get a visual flag (same treatment as `awaiting`/`blocked` gets today) so they
don't require the operator to read every row to find the one that needs attention.

## Composition with #352 (decision-brief) — same pattern, opposite end of the lifecycle

#352 closes the loop at the FRONT of an operator-gated item (blocked-without-brief →
dispatch-to-author-brief). This design closes the loop at the BACK of an operator-originated item
(done-without-verdict → dispatch-to-compare-against-ask). Both are pure gap-scans over the same
compiled goals doc, both dedupe via an identical `Tracker.TryClaim`, both dispatch via the same
`MirrorDispatch` seam, and both encode their template as an identity-append doctrine block. A
future goals item can legitimately need both blocks to fire in sequence (gated while in flight,
then yardstick-checked at delivery) — nothing here requires them to be mutually exclusive.

## What this design does NOT mechanize (calibration, not a gap left open)

- **Detecting that an ask happened at all, when nobody captured it.** The `operator_ask` field's
  own presence is the only structural signal this design can act on — if a desk never wrote the
  quote down, there is nothing for a gap-scan to find (the same limitation `decisionbrief` has for
  "this item should have been marked blocked but wasn't"). This half stays doctrine-enforced
  (`operator-ask-capture`, above), not daemon-detected. A future improvement — flagging goals whose
  `conversation_agent` implies operator-direct tasking (`operator-direct-tasking` doctrine) but
  which carry no `operator_ask` — is a plausible P2 and is called out as an open question below,
  not silently promised here.
- **Judging drift quality.** The daemon dispatches the CHECK; it does not (and should not) grade
  whether "matched" is honestly claimed. That judgment is the same kind of thing
  `mechanical-reader-modeling`'s design (`openspec/changes/mechanical-reader-modeling/design.md`)
  draws the line on for reader-modeling quality — structure forces the comparison to happen, it
  cannot force the comparison to be honest. A dishonest "matched" is a desk-integrity failure,
  handled as such, not a schema problem.

## Phasing

- **P0** — schema fields (`OperatorAsk`, `Yardstick`) on `Goal`/`WorkItem`; `internal/yardstick`
  pure gap-scan + `Tracker`; `YardstickOnTick` wiring; the two doctrine blocks. Delivers the full
  loop for goals-tracked work — the majority of operator asks that reach a work item at all.
- **P1** — dash render of `operator_ask` + `yardstick` side by side in the decision/goal views
  (owned by flotilla-dash, same split #352 used).
  This assigns the render to the same subsystem that already owns `brief` rendering.
- **P2 (open question, not committed)** — a heuristic gap for "operator-direct-tasking-shaped goal
  with no `operator_ask` at all," using `conversation_agent` presence as the signal. Deferred
  because a false-positive rate here (flagging desk-originated goals that happen to have a
  conversation agent) needs to be characterized against real `fleet-goals.yaml` data before it's
  worth building — this is exactly the kind of unverified-requirement risk
  `verify-requirement-provenance-not-inferred-from-docs` warns against inferring ahead of evidence.

## Why this fits the architecture flotilla actually has

Every piece extends a primitive that already shipped and is already tested: the goals schema's
`Brief` pattern, the `decisionbrief` pure-scan-plus-tracker shape, the `DetectorConfig` tick hook,
the `MirrorDispatch` async seam, and the identity-append doctrine mechanism. Nothing here is a new
kind of machinery — it is the decision-brief gap-scan run against the opposite lifecycle edge, which
is exactly what #379 itself asked for when it named #352 as the composing precedent.

## Open questions for the operator

1. Should the two doctrine blocks (`operator-ask-capture` and `operator-intent-yardstick`) install
   as two separate identity-append members, or fold into one? This design defaults to two (each
   fires at a different, non-adjacent moment in the goal lifecycle) but folds easily if the
   operator prefers one combined block.
2. Is the P2 heuristic (flag `conversation_agent`-bearing goals with no `operator_ask`) worth
   building now, or should it wait for real false-positive data from P0 running first? This design
   recommends waiting; flagging it here rather than deciding it unilaterally.
