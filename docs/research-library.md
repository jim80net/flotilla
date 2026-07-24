# Research library authoring

The private-LAN Research page reads Markdown beneath the configured research directory.

## Publication directive

Phase 1 measures publication readiness without hiding any existing file. Authors can add one leading metadata block:

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

Links, Markdown tables, images, and private-LAN videos count as supporting material. A `decision` classification means the paper belongs on the existing waiting shelf; it never means GO. In particular, this metadata cannot authorize Authorization Domains.

The index and reader report empty, title-only, or boilerplate bodies; a missing reader action; and missing supporting material or text-only rationale. Diagnostics are measurement-only in this phase: invalid publications remain visible for migration and no reader action is invented automatically.

## Video support

A paper can embed a video stored beneath that same directory with a single block line:

```markdown
![Video: Authorization Domains briefing](authorization-domains-briefing.mp4)
```

Paths are relative to the paper. Nested assets work too:

```markdown
![Video: Threat-model walkthrough](media/threat-model.webm)
```

Supported formats are MP4, WebM, and Ogg video (`.ogv`). The viewer renders native playback controls plus an explicit **Full screen** action. Video files remain on the private dash: absolute URLs, parent traversal, hidden paths, unsupported extensions, and symlinks are not embedded or served.

Video production and any paid generation remain separate from the viewer. Place only approved output beside the paper; never place credentials or source material containing secrets in the research publication directory.
