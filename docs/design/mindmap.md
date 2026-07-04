# Goals map — the mind-map layout (org v3)

The goals map's `org` layout is a **pinwheel**: concentric rings anchored at one
center, every node's children fanned across a slice of the *global* circle. With
real fleet depth (flotilla → XO → desk → task) that crams the deep nodes into a
tight central cluster of uniform spokes — depth is illegible, and goal text
disappears into small cards. See `assets/goals-map-before-pinwheel.png`.

The **mind map** (`mindmap`) fixes the geometry: each node's children fan out
**locally from that node**, in the node's own outward heading — so depth reads as
**branches with sub-branches** (limbs), not one ring. See
`assets/goals-map-after-mindmap.png`.

## The layout algorithm (`layoutMindmap` in `assets/goals.js`)

1. **Leaf-weight = angular demand** (same memoized, cycle-safe model as `org`): a
   subtree's leaf count sizes how much fan its branch gets.
2. **Ring-1** (the flotillas / roots, or the hub's children) splits the full
   circle by leaf-weight — each becomes a **limb** heading outward from the hub.
3. **Every deeper node** fans its children within a **capped wedge**
   (`MAX_FAN ≈ 115°`) centered on the *parent's* outward heading (`_dir`), placed
   at `parent_center + segLen(depth)·unit(childHeading)`. The cap keeps a subtree a
   cohesive limb instead of spraying back toward the center.
4. **Curved edges**: `drawEdges` draws each parent→child link as a gently-bowed
   cubic between the two card centers, so a branch reads as an organic connector.
5. World bounds are computed from the placed cloud (nodes aren't on a known-radius
   ring), then shifted positive and sized to extent; pan/zoom + keyed-update are
   unchanged (positions are the same absolute `_x/_y` the other layouts use).

### No-overlap discipline (tuned on real fleet depth)

The first cut used a capped local wedge, which read fine on a shallow fixture but
**collided at real depth** (19+ nodes, two XO layers). The tuned engine:

- gives each subtree a **disjoint angular sector** proportional to its leaf-weight
  (siblings/cousins never share angle), with a **local circumference-minimum** so a
  node's children always arc-clear their card widths at their radius; then
- runs a short **collision-relaxation** pass (the hub pinned) that nudges any
  residual overlapping pair apart along its smaller-overlap axis.

Verified overlap-free on a 19-node / depth-4 generic fixture at the real fleet's
shape — see `assets/goals-map-realdepth-{before-pinwheel,after-mindmap}.png`
(captured against a fresh snapshot, no stale banner).

### Objective labels already ride the branches

Goal text (title), **priorities** (flotilla-level) and **milestones** ("current
work", desk-level) already render *on the cards* (org-graph v2 enrichment), not
only in the drawer — the pinwheel just made them illegible by cramming the cards.
The mind-map's legible limbs deliver the operator's "objective labels as layers"
directly; no separate label pass is needed. The genuinely-additive follow-ons are
**per-limb hue/grouping** and **sequence ordering** (F12).

## Status — first cut, UI-gate increment

Shipped as a **third selectable mode** (`tree` / `org` / **mind map**); `org`
stays the default so live use is undisturbed while the direction is reviewed. The
first cut delivers the **skeleton**: limbs, sub-branch fans, curved edges,
content-sized world.

Deliberately deferred to follow-on increments (after the direction is blessed):

- **Per-limb grouping** — a hue / gentle hull per flotilla subtree so each limb
  reads as one unit.
- **Sequence ordering** (F12) — order sibling branches by an authored `after:`
  sequence so a limb reads as a roadmap, not just a set.
- **Make it the default** (env seed + operator blessing), then retire the pinwheel.

(Objective labels are NOT deferred — they already ride the cards; see above.)
