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

## Shared section navigation

The dash injects a shared controller when a presentation exposes this optional
HTML contract:

- sections use `.slide` and a stable `data-label`;
- controls use `data-prev` and `data-next`;
- the counter uses `data-current` and optionally `data-total`;
- an optional visible label uses `data-visible-label`.

The controller keeps buttons, navigation keys, the counter, document title, and
the greatest-visible section synchronized. It computes visibility from actual
viewport overlap, so sections may be taller than the phone viewport. Wheel and
touch scrolling remain free; the controller does not enable scroll snapping.
Presentations without this contract are left alone.

The presentation is the primary body, while document-level comments remain
available in R&D. Passage highlighting remains attached to `SOURCE.md`.

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
