## 1. Backport the notify capability spec

- [x] 1.1 Author `specs/notify/spec.md` (ADDED Requirements) from the shipped
      behavior in `cmd/flotilla/main.go:cmdNotify` + `internal/discord` +
      `internal/roster/secrets.go`, mirroring the `send`/`watch` backport style.
- [x] 1.2 Map every requirement+scenario to an existing test in
      `cmd/flotilla/notify_test.go` (and `internal/discord` for the
      webhook-never-leaks invariant) — no claim without a backing test.
- [x] 1.3 Capture the send-vs-notify distinction as a normative requirement:
      notify REJECTS over-length (operator-facing content) where the send audit
      mirror CLAMPS (best-effort copy).

## 2. Validate + review

- [x] 2.1 `openspec validate notify-spec-backport --strict` passes.
- [x] 2.2 Spec-vs-code review on the diff (adversarial spec-accuracy audit +
      OCR); findings addressed — SPEC ACCURATE, 0 P1, 3 parity P2s folded.
- [x] 2.3 PR #27 (CI green; no cubic on this repo); merge-ready reported to the
      XO, who reviewed + merged.
