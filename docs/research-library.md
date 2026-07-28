# R&D publication authoring

The operator-facing R&D shelf prefers a complete HTML5 showpiece package. Markdown
is the source record, not the finished presentation:

```text
state/research/
└── buzz-research/
    ├── SOURCE.md
    └── presentation/
        ├── index.html
        ├── assets/
        │   ├── presentation.css
        │   ├── presentation.js
        │   └── market-map.svg
        └── media/
            └── briefing.mp4
```

`SOURCE.md` supplies the stable R&D ID, title, summary, decision metadata, and
annotation record. A regular, non-symlink `presentation/index.html` makes the
package `showpiece` and the shelf opens it in the existing R&D reading canvas.
If the presentation is absent, the source remains inspectable but is labeled
`source-only · not ready`; it is not represented as a finished operator
publication. Showpieces sort ahead of source-only papers within the same shelf.

Presentation assets must be local to the package. The private dash serves a
bounded allowlist of HTML, CSS, JavaScript, JSON, image, font, and video files
under `/research-presentations/<paper-id>/presentation/`. Traversal, hidden path
components, symlinks, unsupported extensions, and presentation directories
without a sibling `SOURCE.md` fail closed. Presentation HTML runs in a sandboxed
frame with no forms or network connections. Build self-contained work: do not
depend on a CDN, remote script, analytics endpoint, or token.

Normal relative package URLs are preserved. From `presentation/index.html`,
`assets/showpiece.css`, `assets/showpiece.js`, `media/briefing.mp4`, and the
provenance link `../SOURCE.md` all resolve through the same guarded package
route. `SOURCE.md` is the only file served from the package root.

The presentation is the primary body, while document-level comments remain
available in R&D. Passage highlighting remains attached to `SOURCE.md`.

## Navigation contract

Free wheel scrolling and explicit presentation controls are separate inputs to
one section state. A showpiece must keep its visible section, counter, document
title, progress, Previous/Next controls, and keyboard controls synchronized.
Do not make an `IntersectionObserver` threshold the only source of the current
section: a long mobile section may never cross that threshold, leaving explicit
Next stuck on the same target.

`docs/examples/research-showpiece-navigation.js` is the tested reference
implementation. Copy it into the package as a local asset (or implement the same
contract). It:

- updates the requested section immediately for explicit controls;
- scrolls the presentation's own deck, not the outer R&D reader;
- derives wheel-scroll state from the section nearest the deck's top edge; and
- preserves free scrolling with `scroll-snap-type: none`.

Rendered acceptance must traverse every section forward and backward at 390px,
then wheel-scroll to a middle section and assert the visible label, counter, and
document title agree. Long sections must remain fully reachable.

## Publication directive

Readiness measurement does not hide any existing file. Authors can add one
leading metadata block to `SOURCE.md` or a legacy source-only paper:

```markdown
<!-- flotilla-publication
classification: research
reader-action: Compare the evidence and choose the next experiment.
support: text-only
support-rationale: The argument is fully contained and does not depend on external evidence.
-->
```

The schema is intentionally small:

- `classification` is `research`, `decision`, or `archival`.
- `reader-action` states the decision, next step, or archival reason.
- `support` is `material` or `text-only`.
- `support-rationale` is required when `support` is `text-only`.

Links, Markdown tables, images, and private-LAN videos count as supporting
material. A `decision` classification means the paper belongs on the existing
waiting shelf; it never means GO. In particular, this metadata cannot authorize
Authorization Domains.

The bare `/research` route opens the `Decisions / Waiting on you` focus. That
shelf is deliberately narrower than a Goals posture count: it admits only an
exact unresolved `awaiting` or `blocked` goal/work item with a decision brief
from current work-item truth. When a node has work items, a rolled-up node brief
cannot override them or revive a resolved parent request. The seat-loop posture
R&amp;D is one reading room with two deliberately separate operator jobs:

- **Decide** opens by default and contains only explicit authority-gated work
  with a decision-class paper.
- **Learn** contains only explicit `classification: research` publications that
  pass every publication check and have a complete local HTML5 presentation.

Raw Markdown, plausible titles, source-only notes, status summaries, diagnostics,
and archival material never enter Learn. They remain addressable by exact deep
link as provenance. This keeps the operator index focused without deleting
source evidence. Legacy `focus=library` and `focus=all` links resolve to Learn.

`awaiting-authority` is not a decision. Neither is a generic `blocked` roll-up:
exact `awaiting-auth` work items carry operator authority, while a blocked safety
fork must resolve to an explicitly `decision`-class publication. Every admitted
card requires that indexed decision paper and exposes one single-line reason,
one next move, and one paper jump; missing and ordinary-research papers fail
closed.

Visible decision and annotation copy is written for one anticipated reader:
`you`, `your fleet`, and `what you decide`. A routed annotation tells the
receiving desk that Jim wrote it while preserving the assignment boundary:
saved feedback and delivery are not proof of assigned ownership.

The API reports empty, title-only, or boilerplate bodies; a missing
reader action; missing supporting material or text-only rationale; and a missing
HTML5 presentation for non-archival operator-facing papers. Readiness counts
show showpiece and source-only totals. Diagnostics remain author tooling rather
than an operator collection: source Markdown is not rewritten, and no reader
action or presentation content is invented automatically.

## Legacy Markdown video

A source-only paper can embed a video stored beneath the same research directory
with a single block line:

```markdown
![Video: Authorization Domains briefing](authorization-domains-briefing.mp4)
```

Paths are relative to the paper. Nested assets work too:

```markdown
![Video: Threat-model walkthrough](media/threat-model.webm)
```

Supported formats are MP4, WebM, and Ogg video (`.ogv`). The viewer renders native playback controls plus an explicit **Full screen** action. Video files remain on the private dash: absolute URLs, parent traversal, hidden paths, unsupported extensions, and symlinks are not embedded or served.

Video production and any paid generation remain separate from the viewer. Place only approved output beside the paper; never place credentials or source material containing secrets in the research publication directory.
