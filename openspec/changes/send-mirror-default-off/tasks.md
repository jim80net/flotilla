# Tasks — send-mirror-default-off

> **Gate:** build after the XO ratifies the default-off behavior change at the checkpoint.

## 0. Design gate (current phase)

- [x] 0.1 Draft proposal + design.md + send spec delta (MODIFIED, same header — no rename hazard).
- [x] 0.2 `openspec validate send-mirror-default-off --strict` passes.
- [ ] 0.3 Systems-review + OCR on the design; fold findings.
- [ ] 0.4 Checkpoint to the XO; confirm default-off-when-absent. (BLOCKS build.)

## 1. Roster setting (internal/roster)

- [ ] 1.1 Add `MirrorInterAgent bool` (`json:"mirror_inter_agent,omitempty"`), default
      `false`. A roster without the field loads to `false` (off) with no error. Test:
      absent → false; explicit true/false round-trip.

## 2. send mirror gating (cmd/flotilla)

- [ ] 2.1 Add a `--mirror` flag (force on) beside the existing `--no-mirror` (force off);
      both `bool`, default false; passing both → a clear "mutually exclusive" error. Test.
- [ ] 2.2 Replace the unconditional `if *noMirror { return nil }` (main.go:222) with the
      precedence gate: `noMirror ? off : (mirror ? on : cfg.MirrorInterAgent)`. Test the
      full matrix (flag-off / flag-on / roster-on / roster-off-default / both-flags-error).
- [ ] 2.3 Update the `send` usage block (document `--mirror`, the roster setting, default-off).

## 3. Docs

- [ ] 3.1 quickstart + watch-runbook: inter-agent mirroring is default-off; set
      `mirror_inter_agent: true` to restore the audit trail; `notify` always posts.

## 4. Review + PR

- [ ] 4.1 `/systems-review` + OCR on the implementation diff; fold findings.
- [ ] 4.2 PR; CI green; merge-ready → XO reviews+merges.
