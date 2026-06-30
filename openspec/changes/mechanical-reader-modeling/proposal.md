# Proposal — mechanical reader-modeling (flotilla core feature; public-relaunch bar)

## Why

flotilla's value is **keeping the operator's mental map of the fleet current with minimal
attention.** Today that depends on each desk's *discipline* — to publish at all, and to write
reader-modeled (open from the reader's map, lead with the one decision, no unshared internal
specifics). Discipline fails predictably, and the operator named the failure directly
(`~/.claude/rules/mechanical-reader-modeling-mental-map-is-the-product.md`, 2026-06-30): the public
repo's issues were "slop, zero reader-modeling," the repo was taken private, and a quality relaunch
was demanded. **Mechanical reader-modeling IS the product value; its absence is the product
failing.** This change makes the discipline so consistent it is mechanical.

The design (`docs/mechanical-reader-modeling-design.md`, design v2, design-trio folded) pinned the
two failure halves and corrected the seam:

- **Publishing fails — but NOT for lack of a mechanism.** A per-desk auto-publish **already
  ships**: `internal/watch/detector.go` (the `MirrorOnFinish` field, ~`:87`) fires on every non-XO
  desk's turn-finish; `cmd/flotilla/mirror.go` (`deskMirror.run`) reads the turn-final via the
  `surface.ResultReader` seam (wired in `cmd/flotilla/watch.go:890`, `deskMirrorOnFinish`) and posts
  it to the desk's channel under its own webhook. The all-XO brief reached the operator from only a
  **fraction** of desks (#207) because the fan-out was a free-text
  `flotilla send "post your brief to your channel"` that each desk had to translate into
  `flotilla notify` — and `cmd/flotilla/pushsnippet.go:29` **trains desks to NEVER run notify** (it
  needs fleet secrets a desk must not hold). The diligent desks published; the rest correctly
  followed their training and replied in-pane. The fix is therefore *smaller than a new transport*:
  route the brief through the **already-shipped mirror**.
- **Modeling fails — the irreducible half.** The artifacts that did publish were "slop": written
  from the author's internal state, no lead-with-the-decision, unshared specifics. **Structure
  alone cannot fix this** — a desk can fill an `{anchor, decision}` envelope with
  `{anchor:"my work", decision:"none"}`, pass every structural lint, and model nothing. *Choosing
  the true anchor* and *distilling the one decision* IS the reader-modeling judgment. So the
  mechanical core is an **LLM reader-model judge on the publish path**, with the envelope as its
  **I/O contract** — not a JSON schema a desk fills while we declare victory.

## What changes

A new `reader-modeling` capability + a `watch` delta, structured as five pillars and an ordered
publish pipeline (full design in `docs/mechanical-reader-modeling-design.md`):

1. **Pillar A — deterministic publish on the shipped mirror (subsumes #207).** A new
   `flotilla brief <desk>` whose turn-final is published by the **existing** `deskMirror`/
   `MirrorOnFinish` path (secret-free — the desk never touches fleet secrets, honoring
   `pushsnippet.go`'s invariant). Determinism comes from the mirror firing on turn-finish without
   desk cooperation — NOT from `notify` (which desks are trained to refuse) and NOT a new transport.
   This turns brief-fanout from a-fraction-of-desks into **every desk**.

2. **Pillar B — the reader-map delta envelope (the judge's I/O contract).** Every published
   artifact carries `{audience, anchor, delta, decision}` (audience open-stringly-typed). The desk
   emits it as **structured output** (the desk's own LLM exercises the modeling judgment at
   authoring time); the publish path *validates the schema*. The envelope is the contract between
   author and judge, and the uniform data the dash (E) renders.

3. **Pillar C — two-tier lint, enforced honestly.** Tier-1 STRUCTURAL lint (deterministic, sync,
   cheap) runs **synchronously inside `deskMirror` before the post** (you cannot un-send a Discord
   message): envelope schema valid, decision present-or-explicit-"none", body opens with `anchor`
   and leads with `decision`. Tier-2 SEMANTIC judge (LLM, async) reads as the named audience
   (anchor-is-real, opens-from-the-map, stands-alone-cold) and runs **only on the willing-to-wait
   CLI path**, NEVER in the best-effort auto-mirror.

4. **Pillar D — the firewall: REFUSE, never strip (complements #202).** The publish path runs every
   outbound artifact through the private-firewall detector (the gitignored deployment denylist + the
   built-in generic patterns + the canonical `<prefix>:<n>.<m>` pattern this change introduces). On a hit
   it **refuses and bounces to the desk** with the offending token + its generic abstraction *as a
   suggestion the desk applies in-context* — it NEVER silently rewrites (a runtime strip would corrupt
   the modeled delta the operator's map ingests). **P2 OWNS the pattern** (it is unbuilt; "reuse #202's
   regex" is not possible); #202's static guard MIRRORS it when it ships (a conformance test enforces
   equivalence, since the Go/RE2 runtime guard and the bash/PCRE static guard cannot share regex code —
   they share the gitignored term-list data). Beside the fail-closed denylist, D carries an **advisory
   WARN tier** (mechanism from the relaunch leak-scanner): a gitignored deployment DOMAIN-VOCABULARY set
   that, on a hit, WARNS for human adjudication and still publishes — catching the leak class an
   identifier match misses (**#151** = generic-looking domain words that deanonymize the deployment, e.g.
   an example branch name). The mechanism ships; the vocabulary stays in the gitignored warnlist.

5. **Pillar E — the dash renders the operator's map (the data/view #210 builds on).** A new
   append-only per-desk envelope ledger (`latest-delta.json`) written by the publish path; the dash
   reads it via the existing pure-reader-over-files pattern (`readFileOrEmpty` → an envelope-extended
   `HistoryDoc`) and renders per desk the latest `anchor`→`delta` and any pending `decision`,
   glanceable, *pulled* not pushed. #210's full manage-conversations UX + its UX-designer desk remain
   #210's scope; Pillar E is the spine it renders against.

**The lint posture (the load-bearing rule — the operator must NEVER lose a brief to a lint):** PUBLIC
git/GitHub artifacts (issues/PRs/commits) are **fail-closed** on any lint failure; operator briefs +
internal Discord channels are **warn-with-publish + flag**. The firewall refuse (D) is fail-closed on
BOTH egresses — a known-denylist leak is never published (the firewall is a denylist backstop, not a
guarantee against a novel coined term — CLAUDE.md §1). The operator is protected from LOSING briefs (the
warn-with-publish posture) and from KNOWN leaks (the firewall) — NOT from RECEIVING a deficient brief: a
slop brief on an internal channel is flagged but still delivered, since never-lose-a-brief outranks
never-publish-a-deficient-one.

**Two egresses (no single chokepoint):** (1) the Discord runtime path (the mirror + the
`notify`/`reply`/`brief` CLI) — envelope + lint + firewall attach here; (2) the git/GitHub static
path (issues/PRs/commits via `gh`/`git`) — guarded by the static `scripts/check-private-boundary.sh`
+ a NEW pre-commit/pre-push hook. The raw pane the operator reads over a desk's shoulder is inherently
un-chokeable and is explicitly out of scope.

## Impact

- **Affected specs:**
  - `reader-modeling` (**NEW capability**) — `flotilla brief` on the shipped mirror; the envelope
    contract; the two-tier lint + posture; the firewall refuse; the ordered publish pipeline; the
    per-desk envelope ledger + dash read model; back-compat.
  - `watch` (ADDED) — `deskMirror.run` gains the SYNC pre-post pipeline (firewall refuse → envelope
    validate → tier-1 structural lint) before its existing post, with the warn/fail-closed posture;
    the tier-2 judge NEVER runs in the best-effort auto-mirror.
- **Affected code (by phase):** `cmd/flotilla/brief.go` (NEW — `flotilla brief`); `internal/readermap/`
  (NEW — the envelope type + the deterministic tier-1 lint + the firewall detector, pure/testable);
  `cmd/flotilla/mirror.go` (`deskMirror.run` gains the sync pre-post pipeline hook);
  `cmd/flotilla/main.go` (register `brief`); `internal/readermap/judge.go` (P1 — the LLM judge on the
  CLI path); a git pre-commit/pre-push hook (P2); the envelope ledger writer + the dash
  `HistoryDoc`/`readFileOrEmpty` extension (P3).
- **No behavior change** to `notify`/`send`/`recycle`/`switch`: `brief` is a NEW command; the envelope
  is additive (an un-enveloped channel post warns + publishes — today's auto-mirror behavior is
  preserved for ordinary turn-finals); the firewall only ever SUPPRESSES a leaking post (it never
  rewrites); the ledger is a new write alongside the existing CoS ledger.

## Trio findings folded (systems-review + open-code-review + STORM — design gate)

The design (`docs/mechanical-reader-modeling-design.md`) cleared the design-trio; the load-bearing
refinements are folded into it (and into the spec deltas where they conflict, the findings win):

- **Structure ≠ modeling (the crux):** the mechanical core is the LLM judge with the envelope as its
  I/O contract — NOT a schema the desk fills. The spec splits field-PRESENCE (deterministic tier-1)
  from field-QUALITY (the LLM judge, tier-2). Do NOT relabel the discipline as "a schema."
- **The seam correction:** the outbound publish ALREADY ships via `deskMirror`/`MirrorOnFinish`.
  `inject.go:SetMirror` is the INBOUND operator→desk audit hook — the WRONG seam; the spec does not
  use it for outbound.
- **#207's real cause named:** desks are TRAINED to refuse `notify` (`pushsnippet.go:29`), so the fix
  is `brief` on the mirror, secret-free — not a new notify-based primitive.
- **Refuse, not strip:** a runtime strip generalizes a deployment specific inside a sentence whose
  meaning depends on it, corrupting the modeled delta — worse than a refusal. Inherit the static
  guard's never-rewrite posture.
- **Never lose a brief to a lint:** fail-closed for PUBLIC artifacts; warn-with-publish + flag for
  operator briefs + internal channels.
- **Clustering corrected:** #207 subsumed (A); #210 NOT subsumed (E delivers the data/view it builds
  on, its UX stays its own); #202 complements (its own static-guard PR; D reuses its regex).

## Not in

- The LLM semantic judge (Pillar C tier-2) + per-audience templates — P1; P0 ships brief-on-mirror +
  envelope + the deterministic tier-1 structural lint only.
- The runtime firewall refuse (Pillar D) + the git pre-commit/pre-push hook — P2.
- The dash map view + the envelope ledger (Pillar E) — P3.
- #202's static-guard pattern PR — ships INDEPENDENTLY (it guards committed fixtures off the publish
  path); D reuses its regex but does not subsume it.
- #210's full manage-conversations UX + its dedicated UX-designer desk — stay #210's scope; E is only
  the data model + view #210 renders against.
- The raw pane surface — inherently un-chokeable; explicitly out of scope.
