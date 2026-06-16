## 0. design gate (current phase)

- [x] 0.1 Verify the security crux against source: `flotilla send` is pure tmux delivery, no secrets (main.go:240-249); `flotilla notify` needs the webhook secrets (notify_test.go; secrets.go:13-17). Push channel = send-to-XO, NOT notify-to-Discord.
- [x] 0.2 Draft proposal + design + spec delta + this plan.
- [x] 0.3 `openspec validate smart-desks-push --strict`.
- [ ] 0.4 `/systems-review` AND `/open-code-review` in parallel on the design; resolve findings.
- [ ] 0.5 **CHECKPOINT the XO at the design gate** — surface the security model (send-to-XO, no secrets to desks; desk→Discord-direct is a Non-Goal) + the one open decision (opt-in seeding: documented-snippet+helper (rec) vs a roster `push` flag). Get ratification before implementing.

## 1. The smart-push convention + provisioning (build — after 0.5; shape per checkpoint)

- [ ] 1.1 The canonical smart-push snippet (the desk's identity-file convention): WHEN (finished/blocked/errored) + HOW (`flotilla send --from <self> <xo> "<pointer>"`); pointer-not-transcript; NEVER `flotilla notify`/secrets.
- [ ] 1.2 Provisioning: document (and, if the checkpoint picks the helper, build) a small `flotilla` helper that prints the snippet filled with the desk's + XO's names from the roster — and NEVER emits any secret (the security invariant, tested). `$FLOTILLA_SELF` + roster + binary on PATH are the only provisioning (non-secret). LOW-3 (systems-review): the test SHALL assert `$FLOTILLA_SELF`/`--from` resolves to a real roster agent (a provisioning typo otherwise yields a bogus-sender report).
- [ ] 1.3 (only if the checkpoint picks the roster-flag option) a per-agent `push` flag the workspace honors when scaffolding the identity file; tests.

## 2. Security invariant (test)

- [ ] 2.1 Test/assert the smart-push path requires NO secrets: the documented push command is `flotilla send` (tmux); nothing in the smart-push helper/path loads `roster.LoadSecrets` or emits a webhook/bot-token. (Lock the "desks never get secrets" boundary.)

## 3. Docs

- [ ] 3.1 Extend `docs/inter-harness.md` (the "smart desks" section): the secure push-to-XO model + the security boundary (no secrets to desks; desk→Discord-direct is a Non-Goal); the convention snippet; provisioning.
- [ ] 3.2 `docs/xo-doctrine.md`: a smart desk's pushed report is the XO's cue to collect that desk + relay to the operator only if needed (the XO stays the sole Discord identity). LOW-1 (systems-review): the existing lines ~188-190 say a non-Claude desk "cannot push" — EDIT them (don't append): a *provisioned* smart desk IS a push peer (via `send`-to-XO), an unprovisioned one stays pull-only. Also note the structural property: a pushed report (pane injection) can never be misclassified as an OPERATOR message (those arrive only via the Discord relay's operator-id filter).

## 4. review + ship (build phase — after 0.5)

- [ ] 4.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green; `openspec validate --strict`.
- [ ] 4.2 `/systems-review` AND `/open-code-review` in parallel on the implementation diff; resolve.
- [ ] 4.3 PR referencing this change; CI green; merge on clean gates. Archive; checkpoint the XO.

> **Build gate:** §1-3 implement only AFTER the design-gate checkpoint (0.5) — the
> security model + the opt-in-seeding decision are the XO's to ratify first. Hard
> invariant regardless of shape: **desks never receive the secrets file**; push is
> send-to-XO (tmux), never notify-to-Discord (webhook).
