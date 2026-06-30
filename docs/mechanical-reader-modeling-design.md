# Design — mechanical reader-modeling (flotilla core feature; public-relaunch bar)

**Status:** design (flotilla-dev), routed to the standard-flow trio next. Top priority, sequenced ahead of #208/#207/#210 (which this subsumes).
**Operator standard:** `~/.claude/rules/mechanical-reader-modeling-mental-map-is-the-product.md` — *"so-consistent-its-mechanical reader-modeling … the mental map is a CORE FEATURE of flotilla, and its lack is highly evident."* Given after the public repo's issues were judged "slop, zero reader modeling," taken private, and a quality relaunch demanded.

## The problem, stated from the reader's seat

flotilla's value is **keeping the operator's mental map of the fleet current with minimal attention.** Today that depends on each desk's *discipline* — to publish, and to write reader-modeled. Discipline fails predictably:

- **Publishing fails:** an all-XO brief call reached the operator from **2 of 17 desks** (#207) — the rest replied in-pane (invisible to the operator, who reads the channel) or were rate-limited. A free-text "post your brief" relies on the desk translating it into a `flotilla notify`; the natural behavior is an in-pane reply.
- **Modeling fails:** the artifacts that *did* publish were "slop" — written from the author's internal state, not the reader's map; no lead-with-the-decision; unshared internal specifics the reader can't decode.

Both are the **same disease**: a quality that should be structural is left to per-desk memory. The feature is to make it **mechanical** — enforced at the one place every artifact passes through on its way to a reader: the **publish path**.

## The architecture — one chokepoint, four enforcements

Every reader-facing artifact (a brief, a desk message, an issue, a PR, a commit) leaves a desk through a **publish path**. flotilla already has the seam: `internal/watch/inject.go:SetMirror(func(Job))` fires after every confirmed delivery (the audit mirror; the CoS-mirror seam #108), and `flotilla notify --from <agent>` posts to a desk's operator channel. Today the publish path only *carries* bytes. This feature makes it the enforcement point for the four pillars:

```
 desk turn ─▶ PUBLISH PATH ─────────────────────────────────────▶ reader surface
              │  A. deterministic publish (no in-pane-only)         (desk channel,
              │  B. reader-map delta envelope (structured)           command group,
              │  C. template + lint (open-from-map, lead-decision)   issue/PR/commit)
              │  D. firewall strip (no unshared specifics)
              └────────────────────────────────────────────────▶ E. dash renders the map
```

### Pillar A — deterministic publish (subsumes #207)

A first-class publish primitive so a brief/artifact **deterministically reaches the reader surface**, never an in-pane-only reply. Two mechanisms (the design picks/combines after the trio):
- **`flotilla brief <desk>` / a `--publish` call:** the desk responds with *structured output* the harness PUBLISHES (→ `notify` to the desk's channel + the command-group mirror), not a free-text turn. The call deterministically yields a published artifact.
- **Stop-hook auto-mirror for brief-turns** (like the existing XO Discord mirror via `SetMirror`): a brief-turn's final message is published automatically, so "post to your channel" can't degrade to an in-pane reply.

The publish path is the single chokepoint B/C/D attach to — make publishing mechanical and the other three enforcements have one place to live.

### Pillar B — the reader-map delta envelope

Every published artifact carries a structured **reader-map delta** — the unit that updates a reader's map:

```jsonc
{
  "audience":  "operator" | "desk:<name>" | "newcomer" | "maintainer",
  "anchor":    "what the reader is already tracking (their map entry this updates)",
  "delta":     "what changed — in their terms",
  "decision":  "the one action they must take" | "none"
}
```

The artifact **body is structured from the envelope**: open from `anchor` → lead with `decision` → then the `delta` detail. A desk emitting a brief produces the envelope (structured output), not free prose — so "open from the map" and "lead with the decision" are *data*, not a writing tip. The envelope is also what Pillar E (the dash) renders and what the audit mirror records.

### Pillar C — template + lint (the standard, enforced)

A **reader-model linter** runs on the publish path and **refuses (or hard-flags) slop**:
- **Structure:** the artifact opens from `anchor`, leads with `decision` (or an explicit "none"), is whole-and-concise. A brief that buries the decision, or dumps internal state with no anchor, fails the lint before it publishes.
- **Per-audience templates:** operator-brief / desk-message / issue / PR / commit each have a template the publish path applies, so the structure is forced, not remembered.
- **The cold-read test, mechanized:** a check that the artifact references no symbol it didn't introduce (the "stands alone" facet) — adjacent to the `documentation-newcomer-perspective` / cold-test-author-written-docs discipline.

Lint posture: **fail-closed for the public surface** (issues/PRs/commits to the public repo — a slop artifact can't ship), **warn-with-publish for fast internal channels** (don't block a desk mid-incident), tunable.

### Pillar D — the firewall strip (runtime, outbound)

The publish path runs every outbound artifact through the **private-firewall strip**: deployment IDs, host IPs, private paths, internal codenames, real channel ids are **stripped or refused on the way to a reader surface** — automatic, not a reviewer's lucky catch. This lifts the existing boundary guard (`scripts/check-private-boundary.sh` + the deployment denylist) and the #202 pattern-based detector (`<prefix>:<n>.<m>` session targets, `#<deployment>-c2` channels) from **CI-on-the-tree** to **runtime-on-the-publish-path**. A private specific can never reach a reader because the publish path won't carry it. (The same partition the repo already enforces statically, now enforced dynamically at the exact egress.)

### Pillar E — the dash visualizes the operator's mental map (subsumes #210)

The dash (auth-gated by #208) renders the operator's **mental map of the fleet** from the published reader-map deltas: per desk, the latest `anchor`→`delta` and any pending `decision`, as a living, conversation-centric map (#210) — *"here is what each desk changed and what's waiting on you,"* not a log to decode. The deltas (Pillar B) are the data model; the dash is the view. This is where "keep the operator's map current with minimal attention" becomes visible.

## How it clusters the open issues (one feature)

| Issue | Pillar | Relationship |
|---|---|---|
| **#207** mechanical publishing | A | The deterministic publish primitive IS Pillar A; this feature subsumes it. |
| **#210** conversation-centric dash | E | The dash map view IS Pillar E. |
| **#202** guard pattern-hardening | D | The runtime firewall strip reuses #202's pattern detector; #202 lands inside D. |
| the reader-modeling standard | B, C | The envelope + lint mechanize the standard. |

## Phasing (proposed openspec changes — refined in the trio)

- **P0 — publish chokepoint + envelope (A+B).** The deterministic publish primitive + the reader-map delta envelope (structured output → published). Subsumes #207. The foundation everything attaches to.
- **P1 — lint + templates (C).** The reader-model linter + per-audience templates; fail-closed for the public surface, warn for fast channels.
- **P2 — firewall strip (D).** The runtime outbound strip, reusing #202's detector; #202 folds in here.
- **P3 — dash mental-map view (E).** The conversation-centric map render; subsumes #210.

Each phase ships independently and is useful alone (P0 alone fixes #207's 2-of-17; P2 alone closes the leak class at runtime), but the envelope (B) is the spine the rest reads.

## Open questions for the trio
1. **Publish primitive shape:** `flotilla brief --publish` (explicit call) vs the Stop-hook auto-mirror vs both — which is deterministic *and* doesn't fight a desk's harness? (Grounded in the `SetMirror` seam + #207's two fix directions.)
2. **Envelope authoring:** does the desk emit the envelope as structured output (a schema the harness enforces), or does the publish path *derive* `anchor`/`decision` by lint? Structured-output is more reliable; derivation is less intrusive.
3. **Lint fail-closed vs warn:** where exactly is the line (public artifacts hard-fail; internal incident channels warn)? Avoid a lint that blocks a desk mid-incident.
4. **Firewall strip vs refuse:** when the publish path finds a private specific, does it *strip* (generalize it) or *refuse* (bounce to the desk to fix)? Strip risks a wrong generalization; refuse risks blocking. Likely: refuse for public, strip-with-flag for internal.
5. **Dash data source:** does the dash read the envelopes from the audit-mirror store, or a new per-desk delta ledger? Reuse the existing read-model (`BoardDoc`/`HistoryDoc`) where possible.

## Verification themes (pre-spec)
- A brief call deterministically yields a *published* Discord artifact (the #207 2-of-17 → 17-of-17 proof).
- A slop artifact (no decision lead / no anchor) fails the public lint; a modeled one passes.
- An artifact carrying a deployment specific is refused/stripped on the publish path (runtime, not CI).
- The dash renders a desk's latest delta + pending decision from the envelope, cold-readable by the operator.
- Back-compat: existing `notify`/`send` keep working; the envelope is additive (an un-enveloped message still publishes, flagged).
