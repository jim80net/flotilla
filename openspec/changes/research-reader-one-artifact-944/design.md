# Design — one organized Research artifact (#944)

**Status:** design for operator review; no implementation authorization implied  
**Parent contracts:** #922, #925, #929  
**Independent precursor:** #932 (the bare-presentation link remains a link, not a reader mode)

## 1. Decision

One Research item has one reader route and one organized artifact. The operator
does not leave that artifact to read a generated `paper.html` or a separate
appendix page.

- The **presentation** remains the primary visual stage.
- **Read the paper** opens the canonical paper in a drawer in the same reader.
- The **left rail describes the open document**. It is no longer the Decisions or
  Learn collection once a document is open.
- **Footnotes stay with the paper prose** that cites them.
- **Appendixes follow the presentation** in the same artifact.

The presentation → paper → appendix model from #922 remains the content model.
This design changes composition and navigation, not package identity, source
bytes, authority state, publication admission, or provenance.

## 2. What the references say

The operator supplied two private reference pictures. They are design input only
and are not copied into the repository.

The close view demonstrates the useful reading relationship: a large visual
stage and a narrower adjacent narrative surface, with the layer action always in
the artifact's chrome. The full-page view exposes the product mismatch: the
outer left rail is still a collection navigator while the embedded presentation
already spends its own width on a second narrative pane. The result is nested
navigation and two competing ideas of “the paper.”

The refactor therefore belongs to the product-owned outer reader. It must not
reach through or depend on the internals of a sandboxed presentation. Existing
showpieces remain self-contained and compatible.

## 3. Information architecture

### 3.1 Collection state

Before a document opens, `/research` keeps the existing Decide / Learn shelves,
search, diagnostics, and honest empty states. This is the library, not part of an
open artifact.

### 3.2 Open-document state

After a document opens, the reader replaces collection navigation with a
document-scoped composition:

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ Document title · status · Full screen ↗                                 │
├──────────────────────┬───────────────────────────────────────────────────┤
│ OVERVIEW             │ PRESENTATION                                     │
│ Bottom line          │                                                   │
│ Why it matters to you│              visual stage                         │
│ What you decide/do   │                                                   │
│ Contents             │                        [Read the paper] [Appendix] │
│ Publication truth    ├───────────────────────────────────────┬───────────┤
│ Annotation truth     │ appendix sections, when reached      │ PAPER     │
│                      │ in normal artifact flow              │ drawer    │
└──────────────────────┴───────────────────────────────────────┴───────────┘
```

The paper drawer is a sibling of the sandboxed presentation frame. It is not
authored inside the frame and is never a second iframe. This preserves the
presentation security boundary and gives the dash ownership of focus, scroll,
annotations, footnotes, and failure states.

### 3.3 Overview rail

The open-document rail is a high-level skim aid, in this order:

1. plain title and classification/status;
2. bottom line (`summary`), with “No summary declared” if absent;
3. why it matters / the single `reader_action` in second-person language;
4. the paper heading outline, linking into the in-place drawer;
5. appendix inventory with honest available/missing/partial state;
6. publication and annotation delivery truth.

It does not show other decision cards. “Back to R&D” is the explicit transition
to the collection. A reader never has to infer whether the rail describes this
paper or navigates to another one.

## 4. Layer behavior

### 4.1 Presentation

The existing guarded `presentation_url` remains the stage source. The #932
**Full screen** anchor continues to open the exact bare package in a new tab; it
does not toggle this reader or replace layer navigation.

Presentation state stays intact when the paper drawer opens or closes: no iframe
reload, no reset of its section counter, and no loss of its internal scroll.

### 4.2 Paper drawer

`Read the paper` opens the already-fetched canonical `SOURCE.md`, rendered with
the existing escape-first Markdown discipline. The drawer contains:

- one heading naming the layer and a visible Close action;
- the canonical digest and source identity as quiet provenance;
- the complete paper body, excluding only the publication directive and the
  duplicate top-level title already removed by the current reader;
- passage highlights, document comments, and their saved/queued/delivered truth;
- inline footnote references and definitions.

Opening the paper moves focus to its heading. Escape closes the drawer and
returns focus to the exact `Read the paper` trigger. Close does the same. The
drawer remembers its own reading position while the document remains open.

There is no `paper.html`, no iframe around Markdown, and no route to a second
artifact. A same-document URL state such as `?layer=paper` may make the open
layer linkable and support Back/Forward, but `/research/{id}` remains the
canonical document route and hydration always reconstructs the same artifact.

### 4.3 Footnotes

Footnotes are part of paper rendering, not appendix inventory. The safe renderer
recognizes reference/definition pairs, emits escaped text through the existing
inline allowlist, gives references deterministic IDs derived from document order,
and provides reciprocal reference/definition links. An undefined reference is
rendered as literal source text; duplicate or malformed definitions fail loud in
publication diagnostics rather than silently attaching to the wrong passage.

On screen, a footnote definition appears immediately after the containing
section when the source structure makes that association unambiguous; otherwise
it appears at the end of the paper drawer under **Footnotes**. This keeps notes
in the main reading layer without inventing prose or changing source bytes.

### 4.4 Appendixes

Appendixes are bounded package resources declared by the canonical package
contract; arbitrary neighboring files are never swept in. The API returns a
typed ordered inventory with durable IDs, labels, media types, guarded URLs, and
availability state. #928 owns canonical-versus-auxiliary identity and #930 owns
contained external evidence behavior; this reader consumes those truths rather
than recreating them.

Appendix content follows the presentation in normal document flow. Text and
safe local media may render in bounded cards. Downloads and external evidence
remain explicit links with their source and open behavior visible. Missing,
unsupported, stale, or unavailable entries remain listed with that exact state;
the reader never labels the appendix complete from an empty response.

## 5. Read model

Extend the existing document response compatibly. Existing fields remain valid;
the new object makes layer truth explicit:

```json
{
  "id": "example/SOURCE.md",
  "digest": "sha256:…",
  "summary": "One-line bottom line.",
  "markdown": "…",
  "layers": {
    "presentation": {
      "state": "available",
      "url": "/research-presentations/example/presentation/index.html"
    },
    "paper": {
      "state": "available",
      "id": "example/SOURCE.md",
      "digest": "sha256:…"
    },
    "appendix": {
      "state": "partial",
      "items": [
        {
          "id": "evidence/measurement.json",
          "label": "Measurement",
          "media_type": "application/json",
          "url": "/research-appendix/example/evidence/measurement.json",
          "state": "available"
        }
      ]
    }
  }
}
```

Layer states are `available`, `missing`, `partial`, `stale`, or `unavailable`.
Those are read facts only. They do not modify `publication_valid`, decision
admission, delivery receipts, annotation assignment, or authorization.

The implementation should build a small typed Go layer model and serialize it
from the existing boundary-checked readers. URLs are server-issued; the browser
does not construct filesystem paths. Stable ordering is declaration order, then
durable ID as a deterministic tie-break.

## 6. Responsive composition

### Desktop (>900px)

- Overview rail: `clamp(16rem, 22vw, 21rem)`, sticky within the page's primary
  scroll, never a second vertical scroller.
- Stage: all remaining width with `min-width: 0`.
- Paper drawer: overlays the right side of the stage at
  `clamp(24rem, 38vw, 42rem)` and owns one reading scroll while open. The stage
  remains visible enough to retain context.
- Appendix: full main-column width after the stage, never squeezed underneath
  the open drawer.

### Phone (≤640px; verify 390×844 and 360×800)

- The library remains the initial single-column page.
- An open artifact starts with a compact, collapsible Overview above the stage;
  it does not retain the collection cards.
- `Read the paper` opens a full-viewport sheet (`inset: 0`) with a sticky 44px
  Close action. The sheet itself is the one active reading scroll while open;
  background page scroll is locked and restored on close.
- Appendixes follow the presentation in the page's natural scroll when the paper
  is closed.
- Layer actions wrap between complete 44px controls. Labels do not clip, and no
  root `overflow-x: clip` assertion substitutes for per-component containment.

At tablet widths the composition may use the phone sheet until the desktop
drawer has enough room; there is no compressed three-column intermediate state.

## 7. State and navigation

```text
library
  └─ open document ──> artifact / presentation visible
                           ├─ Read the paper ──> paper drawer open
                           │      ├─ Close or Escape ──> presentation visible
                           │      └─ Back/Forward ─────> same artifact layer state
                           ├─ Appendix anchor ──> appendix after stage
                           ├─ Full screen ──────> bare package, new tab (#932)
                           └─ Back to R&D ──────> library, prior focus restored
```

Changing layers never changes the selected document. Changing documents closes
the drawer, cancels stale fetch/render work, clears document-scoped annotation
state, and hydrates the next artifact once. A failed layer request preserves the
selected document and requested layer so Retry completes the same intent.

## 8. Accessibility and trust

- Use a labelled complementary region for Overview and a labelled dialog/sheet
  for the paper drawer; do not fake either with clickable `div` elements.
- The presentation iframe keeps a unique title. Drawer and appendix headings
  form a coherent outline outside it.
- All layer controls have visible focus, ≥44px touch targets, and honest
  `aria-expanded`/current-state text.
- Drawer focus is contained while modal on phone, but not on desktop where the
  adjacent stage remains intentionally available.
- Annotation anchors continue to resolve against canonical Markdown and digest;
  presentation content is never treated as the anchoring source.
- Error copy distinguishes missing, unavailable, and partial. “Saved” never
  becomes “assigned”; a publication classification never becomes GO.

## 9. Delivery slices

1. **Composition:** document-scoped overview, in-place paper drawer using the
   current document response, focus/history behavior, and responsive shell.
2. **Paper depth:** safe footnote rendering plus annotation/highlight parity in
   the drawer.
3. **Appendix depth:** consume #928/#930's bounded typed inventory and render it
   after the presentation with partial/error states.
4. **Contract hardening:** layer diagnostics and delivery-receipt integration
   under #925/#924 after more than one real package passes.

Each slice is reversible and useful. Slice 1 does not wait for a corpus rewrite,
does not generate `paper.html`, and does not weaken source-only or delivery
truth.

## 10. Rendered acceptance

Pin generic fixtures at 390×844, 360×800, and 1440×900:

- complete presentation/paper/appendix: overview is document-scoped, paper opens
  in place, presentation state survives open/close, footnote round-trip works,
  and appendix follows the stage;
- source-only paper: overview and paper work, presentation is honestly missing,
  and no Full screen control is invented;
- missing and partial appendix: exact states remain visible and retryable;
- long title, long paper, long overview, and 30+ appendix entries remain
  contained with deterministic order;
- keyboard: trigger → drawer heading, Escape/Close → trigger, Back/Forward layer
  hydration, and Back to R&D → originating card;
- phone: no nested-scroll trap, background position restores, all actions are
  ≥44px, and every component's box remains inside the viewport;
- security: traversal, symlink, unsupported type, unsafe URL, raw HTML, and XSS
  fixtures fail closed with no source bytes or private paths in logs.

Seeing review must use private generic fixtures. Operator reference pictures and
private production pixels remain unpublished.

## 11. Non-goals

- No Fullscreen API, overlay presentation mode, or replacement for #932's link.
- No mutation of Markdown, generated second paper artifact, mass corpus rewrite,
  or inferred summary/reader action.
- No presentation DOM introspection or required showpiece rewrite.
- No Authorization Domains implementation or GO inference.
- No new event bus, public publishing, deploy, spend, or multi-user permissions.

## 12. Operator review questions

The design intentionally makes two choices for confirmation before build:

1. On desktop, the paper opens as a right-side drawer while the overview stays
   left; on phone it becomes a full-viewport sheet. Is that the intended reading
   relationship?
2. The appendix stays in the main artifact flow after the presentation rather
   than opening another drawer. Is “after the presentation” meant spatially in
   that way?
