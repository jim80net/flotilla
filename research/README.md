# flotilla research log

Durable findings from experiments and evaluations — the **kill-or-keep
verdicts** and their measured numbers, so a future session reads the verdict
instead of re-spending compute to re-derive it (research-log discipline).

**Check this index before running any benchmark or evaluation.** If a question is
already answered here, read the note rather than re-running it.

| Note | Question | Verdict | Date |
|---|---|---|---|
| [ocr-as-third-reviewer.md](./ocr-as-third-reviewer.md) | Use Alibaba open-code-review (OCR) as a 3rd PR reviewer at $0 (local model)? | **SHELVED** — $0/local unviable (DNF on a real PR in 30 min, GPU pinned 93-96%, missed a known bug); paid path costly + redundant with cubic + /systems-review | 2026-06-08 |

## Conventions

- One note per investigated question. Lead with **Status** (e.g. `SHELVED`,
  `ADOPTED`, `OPEN`) and a verdict date.
- Record the measured numbers and **how** they were measured (provenance), not
  just the conclusion.
- Time-sensitive empirical claims (a vendor's pricing, a tool's speed on a
  specific host) carry their measurement date; treat them as point-in-time.
- Add a row to the table above for every new note.
