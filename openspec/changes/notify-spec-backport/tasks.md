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
- [ ] 2.2 /systems-review on the spec diff; address findings.
- [ ] 2.3 PR (CI green; cubic if it runs — not gating on this repo per the
      standard-flow note); enumerate any inline findings; report merge-ready to
      the XO (reviews + merges).
