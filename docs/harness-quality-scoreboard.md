# Harness quality scoreboard

The harness quality ledger answers a narrow operational question with observed
events: which surface/model combinations complete work cleanly, and which return
from independent gates for rework? It does not assign an intrinsic model score
and never launches a same-prompt evaluation.

## Durable artifacts

`<roster-dir>/harness-quality.jsonl` is the append-only event ledger. Each event
uses `flotilla.harness_quality_event/v1` and records:

- completion or gate outcome;
- seat, effective live surface, configured model, optional known harness version,
  and the running flotilla binary version;
- #801 work class (`strategic`, `maintenance`, or `ktlo`);
- event-local bounce count and completion rework count;
- a stable work reference and optional session-mirror pointer.

The watch finish edge emits a completion event after its session-mirror record is
durable. A missing task context is recorded as `work_class=unclassified` and an
unresolved model as `model=unknown`; the scoreboard exposes classification
coverage so incomplete tagging cannot masquerade as measured quality. Malformed
ledger input makes the summary unavailable instead of silently dropping rows.

Session bodies are not copied into this ledger. `session_mirror_ptr` points at
the existing roster-adjacent transcript record. Fleet operations owns transcript
rotation and compression.

## Tag a task

Set the active task context when work is assigned:

```text
flotilla quality context <seat> \
  --work-class strategic \
  --work-ref owner/repo#123 \
  --harness-version 1.2.3
```

The host-local context is stored mode `0600` under
`<roster-dir>/harness-quality-context/<seat>.json`. Replace it at each new task;
never reuse an old work reference for unrelated work.

Record an independent gate result or a terminal completion explicitly:

```text
flotilla quality record <seat> --event gate --outcome bounced \
  --bounce-count 1 --work-class strategic --work-ref owner/repo#123

flotilla quality record <seat> --event gate --outcome passed \
  --work-class strategic --work-ref owner/repo#123

flotilla quality record <seat> --event completion --outcome merged \
  --rework-count 1 --work-class strategic --work-ref owner/repo#123
```

Explicit records require a classified work context or `--work-class`; they do
not accept an invented default.

## Read the scoreboard

```text
flotilla quality show
flotilla quality show --json
flotilla status --json
```

The dashboard footer consumes the same summary from `/api/status`. Aggregates
are grouped by surface × model × work class and retain the observed harness and
flotilla version sets. Bounce rate is bounced gate events divided by all gate
events. Rework rate is completion events with `rework_count>0` divided by all
completion events. Zero denominators render as zero with the raw event counts,
not as a claim of quality.

## No-spend boundary

Same-prompt multi-model evaluation remains a corpus shelf only under fleet-ops
`state/research/harness-eval-corpus/`. No scoreboard command reads that corpus,
starts a harness, invokes a provider, or authorizes metered spend. A future eval
runner requires separate operator money approval.
