# Design — mechanical reader-modeling (flotilla core feature; public-relaunch bar)

> **This is a design record, not a runnable guide.** It specifies a *proposed*
> feature (a reader-model judge + firewall on desk publish paths). Most of what it
> describes is unbuilt; where a piece already ships it says so inline. For the
> reader-modeling *principle* it enforces, see
> [OPERATING-PRINCIPLES.md §5](./OPERATING-PRINCIPLES.md).

**Status:** design v2 (design-trio folded — systems-review + OCR + STORM). The architecture (one enforcement point per egress; the envelope as the judge's contract; the dash as the view) was confirmed sound; v2 corrects the seam, owns the mechanical/judgment boundary, and fixes the clustering. Routed to openspec next.
**Operator standard:** `~/.claude/rules/mechanical-reader-modeling-mental-map-is-the-product.md` — *"so-consistent-its-mechanical reader-modeling … the mental map is a CORE FEATURE of flotilla."* Given after the public repo's issues were judged "slop, zero modeling," taken private, relaunch demanded.

## The problem, from the reader's seat

flotilla's value is **keeping the operator's mental map of the fleet current with minimal attention.** Today that depends on each desk's *discipline* — to publish, and to write reader-modeled. Discipline fails predictably, and the trio pinned *why*:

- **Publishing fails — but NOT for lack of a mechanism.** A per-desk auto-publish **already ships**: `detector.go:MirrorOnFinish` fires on every non-XO desk's turn-finish; `cmd/flotilla/mirror.go:deskMirror.run` (wired at `watch.go:490`) reads the turn-final via the `surface.ResultReader` seam and posts it to the desk's channel under its webhook. The all-XO brief reached the operator from only **2 of 17 desks** (#207) because the fan-out was a free-text `flotilla send "post your brief to your channel"` that desks must translate into `flotilla notify` — and `cmd/flotilla/pushsnippet.go:14,29` **trains desks to NEVER run notify** (it needs fleet secrets a desk must not hold). The 2 diligent desks published; the rest correctly followed their training and replied in-pane. The fix is therefore *smaller*: route the brief through the **already-shipped mirror**, not a new notify-based primitive.
- **Modeling fails — and this is the irreducible half.** The artifacts that did publish were "slop": written from the author's internal state, no lead-with-the-decision, unshared internal specifics. Structure alone cannot fix this (below).

## The mechanical / judgment boundary (read this first — the trio's crux)

**Structure forces the SHAPE of reader-modeling; it cannot supply the CONTENT.** An envelope with an `anchor` and a `decision` field forces the body to open-from-anchor and lead-with-decision — but a desk can fill it `{anchor:"my work", delta:"made progress", decision:"none"}`, pass every structural lint, and model nothing. *Choosing the true anchor* (what THIS reader tracks) and *distilling the one decision* IS the reader-modeling judgment — the exact judgment that produced slop.

So the mechanical core of this feature is an **LLM reader-model judge on the publish path**, with the envelope as its **I/O contract** — not a JSON schema the desk fills and we declare victory. The honest division:

| Facet | Mechanism | Enforcement |
|---|---|---|
| Field PRESENCE (anchor non-empty, decision present or explicit "none", body opens with anchor + leads with decision) | **deterministic structural lint** | cheap, synchronous, fail-closed everywhere |
| Field QUALITY (anchor is *really* the reader's map entry; decision is *the* decision; stands alone cold) | **LLM reader-model judge** reading as the named audience | costs a model call; runs on the willing-to-wait CLI path; fail-closed public, warn internal |
| Unshared specifics (IDs, paths, codenames) | **firewall refuse** (reuses the static guard's detector) | cheap regex; refuse, never rewrite |

The design **does not over-claim** that the envelope makes writing modeled. The envelope makes the judge's job checkable and the dash's data uniform; the *judge* (or a desk's own structured-output pass) supplies the modeling.

## The egress map (corrected — there is no single chokepoint)

Reader-facing artifacts leave a desk by **two** distinct egresses, enforced differently:

1. **The internal publish path (runtime):** turn-finals via `deskMirror`/`MirrorOnFinish` always enter the dash ledger; only explicit parade markers continue to Discord. Curated `flotilla notify` and direct replies bypass the turn-final mirror. This is where the envelope + lint attach at runtime.
2. **The git/GitHub path (static):** issues/PRs/commits authored by a desk via `gh`/`git` directly — these never traverse the Discord path. They are guarded by the **static** `scripts/check-private-boundary.sh` (already) + a pre-commit/pre-push lint hook (new), not the runtime path.

A third surface — the **pane** the operator reads over a desk's shoulder — is inherently un-chokeable; the feature is "mechanical for the published surfaces," explicitly not the raw pane.

## The five pillars (re-grounded)

### Pillar A — deterministic publish on the SHIPPED mirror (subsumes #207)

`flotilla brief <desk>`: the desk produces a brief whose turn-final is published by the **existing** `deskMirror`/`MirrorOnFinish` path (secret-free — the desk never touches fleet secrets, honoring `pushsnippet.go`'s invariant), to the desk's channel. Determinism comes from the mirror firing on turn-finish without desk cooperation — *not* from a new primitive the desk must remember to call, and *not* from `notify` (which desks are trained to refuse). This makes brief-fanout 2-of-17 → 17-of-17. (`brief --publish` as an explicit structured call is the same mirror path with an enforced envelope schema; the determinism is the mirror, not the call.) Scope: the **channel** surface; the pane surface is out of scope.

### Pillar B — the reader-map delta envelope (the judge's I/O contract)

Every published artifact carries a structured **reader-map delta**:

```jsonc
{ "audience": "operator" | "desk:<name>" | "newcomer" | "maintainer",  // open-stringly-typed; extension path documented
  "anchor":   "the reader's map entry this updates (in their terms)",
  "delta":    "what changed",
  "decision": "the one action they must take" | "none" }
```

**Authoring (open-Q2 resolved → desk structured-output):** the desk emits the envelope as structured output — i.e. the desk's own LLM exercises the modeling judgment at authoring time — and the publish path *validates the schema* (structural lint) and *checks the quality* (the judge, on the CLI path). Lint-derivation alone cannot manufacture the anchor/decision judgment, so the judgment must be exercised by an LLM (the desk's, validated by the path's). The envelope is the contract between them, and the uniform data the dash (E) renders.

### Pillar C — two-tier lint (the standard, enforced honestly)

- **Tier 1 — structural lint (deterministic, sync, cheap):** envelope schema valid; `decision` present or explicit "none"; body opens with `anchor` and leads with `decision`; no firewall-denylist/pattern hit. Runs **synchronously inside `deskMirror` before the post** (so a refusal happens *before* publish — you cannot un-send a Discord message). Fail-closed everywhere; it only ever blocks on trivially-fixable missing structure, so it never traps a desk mid-incident on content.
- **Tier 2 — semantic judge (LLM, async, on the willing-to-wait path):** anchor-is-real, opens-from-the-reader's-map, stands-alone-cold. Runs on the explicit `brief`/`notify` CLI path (the desk waits), **never** in the best-effort auto-mirror (a slow judge would stall or be skipped). Posture: **fail-closed for public-repo artifacts** (issues/PRs/commits — latency is acceptable there); **warn-with-publish + flag for operator briefs and internal channels** (the operator must NEVER lose a brief to a lint).

### Pillar D — the firewall: REFUSE, never strip (complements #202)

The publish path runs every outbound artifact through the private-firewall detector (the static guard's deployment denylist + #202's `<prefix>:<n>.<m>` / `#<deployment>-c2` pattern). On a hit it **refuses and bounces to the desk** with the offending token + its generic abstraction *as a suggestion the desk applies in-context* — it **never silently rewrites**. A runtime strip would generalize a deployment specific (a `<desk>:<window>.<pane>` target) → `<a desk>` inside a sentence whose meaning depends on *which* desk, corrupting the modeled delta the operator's map ingests — worse than a refusal. This inherits the static guard's never-rewrite posture (it only ever fails). **#202 stays its own static-guard PR** (it guards committed fixtures, which never traverse the publish path). D **owns the canonical `<prefix>:<n>.<m>` pattern** (it is unbuilt today — #202's static guard MIRRORS D's pattern when it ships; a conformance test over a shared fixture corpus enforces equivalence, since the Go runtime guard (RE2, no lookahead) and the bash static guard (PCRE) cannot share regex code — they share the gitignored TERM-LIST data, not the patterns). D complements #202; it does not subsume it.

**Beside the fail-closed denylist, D carries an advisory WARN tier** (the generalizable mechanism from the relaunch leak-scanner, #151): a deployment-supplied DOMAIN-VOCABULARY set (loaded from a gitignored `.flotilla/private-warnlist`, exactly as the denylist is — never hard-coded) that, on a hit, EMITS A WARNING for human adjudication and does NOT refuse/suppress/fail (advisory on both egresses). It catches the class an identifier match misses — generic-looking domain words that deanonymize the deployment (e.g. #151's example branch name) — too false-positive-prone to fail-close on but worth a human look. The MECHANISM ships; the vocabulary stays circumstantial in the gitignored warnlist. A denylist hit still REFUSES (denylist precedence); the WARN tier only adds an advisory class below it.

### Pillar E — the dash renders the operator's map (the data/view #210 builds on)

A new minimal **per-desk envelope ledger** (`latest-delta.json` per desk — an ATOMIC overwrite of the latest record, not an unbounded append log; written by the publish path next to the existing CoS ledger) is the data model. The dash (auth-gated by #208) reads it via the existing pure-reader-over-files pattern (`readFileOrEmpty` → an envelope-extended `HistoryDoc`) and renders the operator's mental map: per desk, the latest `anchor`→`delta` and any pending `decision`, glanceable, *pulled* not pushed (not another live surface to babysit). **This is the data model + view that #210 builds on — #210's full "see + manage conversations" UX and its dedicated UX-designer desk remain #210's scope.** Pillar E is the spine #210 renders against, not a replacement for it.

## The ordered publish pipeline (composition — trio F6)

On the Discord runtime path, in order: **(D) firewall refuse-check FIRST** (before any modeling work is wasted) → **(B) envelope validate** → **(C-tier1) structural lint, sync, pre-post** → **post via the mirror** → **record to the envelope ledger (E)**. The **(C-tier2) semantic judge** runs only on the explicit CLI path *before* it hands off to the mirror, never in the best-effort auto-mirror. The git/GitHub path runs D + the structural lint as a **pre-commit/pre-push hook**.

## Clustering (corrected)

| Issue | Pillar | Relationship (corrected) |
|---|---|---|
| **#207** mechanical publishing | A | Subsumed — `brief` on the shipped mirror; #207's real cause (notify forbidden to desks) named. |
| **#210** conversation-centric dash | E | **NOT subsumed** — Pillar E delivers the map data model + view #210 builds on; the manage-conversations UX + UX-desk org stay #210's. |
| **#202** guard pattern-hardening | D | **Complements** — #202 ships as its own static-guard PR (early); D reuses its regex at runtime egress. |
| the reader-modeling standard | B, C | Mechanized — the envelope (shape) + the two-tier lint/judge (quality). |

## Phasing (proposed openspec changes)

- **P0 — `brief` on the shipped mirror + the envelope (A+B).** Re-route brief-fanout through `deskMirror`; the structured envelope + the tier-1 structural lint, sync pre-post. Subsumes #207; immediately makes brief-fanout 17-of-17 with modeled-shape briefs. Smallest, highest-value cut.
- **P1 — the semantic judge + templates (C-tier2).** The LLM reader-model judge on the CLI path; per-audience templates; fail-closed-public / warn-briefs.
- **P2 — the runtime firewall refuse (D) + the advisory WARN tier.** P2 owns the canonical `<prefix>:<n>.<m>` pattern + the generic patterns + the gitignored denylist/warnlist load, wired at the publish egress + a git pre-push hook (CI is the enforcing authority). #202's static guard mirrors P2's pattern (conformance-tested).
- **P3 — the dash map view (E).** The envelope ledger + the map render #210 builds on.

P0 alone delivers operator-visible value (17-of-17 published, structurally-modeled). The envelope (B) is the spine the rest reads.

## Open questions — resolved by the trio
1. **Publish primitive →** `flotilla brief` on the **existing `deskMirror`/`MirrorOnFinish`** (secret-free, deterministic), NOT a new transport, NOT `notify`.
2. **Envelope authoring →** **desk structured-output** (the desk's LLM exercises the judgment), validated by the path; the judge checks quality. Schema can't manufacture the judgment.
3. **Lint posture →** public git/GitHub = **fail-closed**; operator briefs + internal channels = **warn-with-publish + flag**. Never lose a brief to a lint.
4. **Strip vs refuse →** **refuse-bounce** (never silent-rewrite a modeled artifact); inherit the static guard's never-rewrite posture.
5. **Dash source →** a new **per-desk envelope ledger**, read via the existing read-model pattern (extend `HistoryDoc`). Not `BoardDoc` (desk states), not the inbound `SetMirror` audit store.

## Verification themes (pre-spec)
- Brief-fanout deterministically yields a *published* Discord brief from every desk (the #207 2-of-17 → 17-of-17 proof), secret-free.
- A slop envelope (`anchor:"my work"`, no decision) fails the tier-1 structural lint; a content-but-unmodeled one fails the tier-2 judge on a public artifact; a modeled one passes both.
- An artifact carrying a deployment specific is **refused** (not stripped) on the publish path at runtime; a leaked *fixture* is caught by #202's *static* guard (the two egresses, separately).
- The dash renders a desk's latest delta + pending decision from the envelope ledger, cold-readable by the operator, pulled not pushed.
- Back-compat: existing `notify`/`send` keep working; an un-enveloped channel post warns + publishes; an un-enveloped public artifact fails closed.
