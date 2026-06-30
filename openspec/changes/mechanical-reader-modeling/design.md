# Design — mechanical reader-modeling (openspec change)

**Source of truth:** `docs/mechanical-reader-modeling-design.md` (design v2, design-trio folded). This
file captures the openspec-specific decisions, the grounded seams, and the trio refinements; where it
restates the source doc, the source doc governs the prose and this governs the requirement shape.

## The mechanical / judgment boundary (the crux — read first)

**Structure forces the SHAPE of reader-modeling; it cannot supply the CONTENT.** An envelope with an
`anchor` and a `decision` field forces the body to open-from-anchor and lead-with-decision — but a
desk can fill it `{anchor:"my work", delta:"made progress", decision:"none"}`, pass every structural
lint, and model nothing. *Choosing the true anchor* (what THIS reader tracks) and *distilling the one
decision* IS the reader-modeling judgment — the exact judgment that produced slop. So the honest
division of labor is:

| Facet | Mechanism | Enforcement |
|---|---|---|
| Field PRESENCE (anchor non-empty; decision present-or-"none"; body opens with anchor + leads with decision) | **deterministic structural lint** (tier-1) | cheap, synchronous, in-process |
| Field QUALITY (anchor is *really* the reader's map entry; decision is *the* decision; stands alone cold) | **LLM reader-model judge** (tier-2) reading as the named audience | a model call; CLI path only |
| Unshared specifics (IDs, paths, codenames) | **firewall refuse** (reuses the static guard's detector) | cheap regex; refuse, never rewrite |

The envelope does NOT make writing modeled; it makes the judge's job checkable and the dash's data
uniform. The *judge* (or the desk's own structured-output pass) supplies the modeling.

## Grounded seams (verified at this change's authoring — cite, do not re-derive)

- **The outbound publish ALREADY ships.** `internal/watch/detector.go` carries `MirrorOnFinish func(agent string)`
  (~`:87`), invoked once per non-XO desk that finishes a turn (dispatched async via `MirrorDispatch`,
  `func(run func()){ go run() }`, `cmd/flotilla/watch.go:491`). `cmd/flotilla/watch.go:890`
  (`deskMirrorOnFinish`) builds the side-effect: it resolves the desk's surface, reads the turn-final
  through the SHARED `surface.ResultReader` seam (`rr.LatestResult(pane)` — the SAME path
  `flotilla result` uses, so CLI and auto-mirror never diverge), and posts under the desk's own webhook
  via `tr.Post(...)`. `cmd/flotilla/mirror.go` (`deskMirror.run`) is the pure, injectable core
  (webhook → turnFinal → chunk → post), OBSERVE-ONLY and BEST-EFFORT (it never returns an error; every
  outcome logs exactly one decision line). **This is the seam Pillar A rides — NOT `inject.go:SetMirror`**,
  which is the INBOUND operator→desk audit hook (wrong direction).
- **#207's real cause.** `cmd/flotilla/pushsnippet.go:19-32` (`smartPushSnippetTemplate`) trains every
  smart-desk to report to the XO via `flotilla send` (secret-free pane injection) and to **"do NOT run
  flotilla notify and do NOT touch any secrets or webhook"** — because `notify` needs the fleet secrets
  a desk must not hold. So the all-XO brief fan-out (a free-text `send` asking desks to publish) was
  correctly refused by trained desks; only the diligent few translated it to a publish. **Pillar A
  rides the secret-free mirror, so a desk publishes WITHOUT ever touching `notify` or a secret.**
- **The two egresses.** Discord runtime (the mirror + the `notify`/`reply`/`brief` CLI) carries the
  envelope + lint + firewall. The git/GitHub static path (`gh`/`git`) is guarded by the existing
  `scripts/check-private-boundary.sh` + a NEW pre-commit/pre-push hook — it never traverses the runtime
  path. The raw pane is un-chokeable (out of scope).

## The ordered publish pipeline (composition — trio F6)

On the Discord runtime path, in strict order:

1. **(D) firewall refuse-check FIRST** — before any modeling work is wasted; a leak refuses + bounces.
2. **(B) envelope validate** — schema present + well-formed (or, for an internal channel post with no
   envelope, the back-compat warn-and-publish branch).
3. **(C-tier1) structural lint, synchronous, pre-post** — inside `deskMirror`, before the post, so a
   refusal happens *before* publish (you cannot un-send a Discord message).
4. **post via the mirror.**
5. **record to the envelope ledger (E).**

The **(C-tier2) semantic judge** runs ONLY on the explicit CLI path (`brief`/`notify`), *before* it
hands off to the mirror — NEVER in the best-effort auto-mirror (a slow judge would stall or be
skipped). The git/GitHub path runs **D + the structural lint** as a pre-commit/pre-push hook.

## The lint posture (the load-bearing rule)

The operator must NEVER lose a brief to a lint. Therefore:

- **PUBLIC git/GitHub artifacts** (issues/PRs/commits): **fail-closed** — a lint failure (incl. a
  missing/malformed envelope) blocks the artifact. Latency is acceptable there.
- **Operator briefs + internal Discord channels:** **warn-with-publish + flag** — the post is always
  delivered; a lint failure is recorded + surfaced, never a drop.
- **The firewall refuse (D) is fail-closed on BOTH egresses** — a private leak is never published. On
  the auto-mirror that means the post is SUPPRESSED + loudly logged (no interactive desk to bounce to
  mid-turn); on the CLI path the desk is bounced the offending token + a generic abstraction.
- **A malformed envelope** (present but schema-invalid) is fail-closed everywhere — it is a
  trivially-fixable structural defect, never a content trap.
- **An ABSENT envelope on an ordinary auto-mirror turn-final** (the common case — most turn-finals are
  not briefs) is the back-compat warn-and-publish branch: today's mirror behavior is preserved.

## Resolved open questions (from the design-trio)

1. **Publish primitive →** `flotilla brief` on the existing `deskMirror`/`MirrorOnFinish` (secret-free,
   deterministic), NOT a new transport, NOT `notify`.
2. **Envelope authoring →** desk structured-output (the desk's LLM exercises the judgment), validated
   by the path; the judge checks quality. A schema can't manufacture the judgment.
3. **Lint posture →** public = fail-closed; operator briefs + internal channels = warn-with-publish +
   flag. Never lose a brief.
4. **Strip vs refuse →** refuse-bounce; never silent-rewrite a modeled artifact. Inherit the static
   guard's never-rewrite posture.
5. **Dash source →** a new per-desk envelope ledger, read via the existing read-model pattern (extend
   `HistoryDoc`). Not `BoardDoc` (desk states), not the inbound `SetMirror` audit store.

## Phasing (one change, phased tasks — the precedent of `harness-subscription-switching`)

- **P0 — A + B + C-tier1.** `flotilla brief` on the mirror; the envelope type + schema; the
  deterministic tier-1 structural lint, sync pre-post inside `deskMirror`. Makes brief-fanout
  every-desk, modeled-shape. Smallest, highest-value, operator-visible cut.
- **P1 — C-tier2 + templates.** The LLM reader-model judge on the CLI path; per-audience templates;
  fail-closed-public / warn-briefs.
- **P2 — D (runtime firewall refuse) + the git pre-commit/pre-push hook.** Reuses #202's regex (which
  ships independently as its static-guard PR).
- **P3 — E (dash map view).** The envelope ledger + the map render #210 builds on.

P0 alone delivers operator-visible value. The envelope (B) is the spine the rest reads.

## Why this fits the architecture flotilla actually has

The publish mechanism is not invented — it is the already-shipped, already-tested `deskMirror` path,
extended with a sync pre-post pipeline at the one point a Discord post can still be suppressed. The
envelope reuses the structured-output the desks' harnesses already emit. The firewall reuses the
static guard's detector. The dash ledger reuses the pure-reader-over-files `HistoryDoc`/`readFileOrEmpty`
pattern. Each pillar EXTENDS a proven primitive rather than adding a parallel one — the elegant
solution that would have emerged if reader-modeling had been a foundational assumption.
