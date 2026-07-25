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
