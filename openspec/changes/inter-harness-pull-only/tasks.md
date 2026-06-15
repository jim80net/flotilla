## 0. design gate (current phase)

- [x] 0.1 Live-prove the surface-agnostic plumbing ($0 mixed aider+opencode fleet, opencode XO) — done this session; gap-trace produced.
- [x] 0.2 Draft proposal + design + spec delta + this plan; scope = pull-only (a) per operator ratification; G3 corrected as a non-gap.
- [x] 0.3 `openspec validate inter-harness-pull-only --strict`.
- [x] 0.4 `/systems-review` AND `/open-code-review` in parallel on the design; resolve findings.
- [x] 0.5 **CHECKPOINT the XO at the design gate** — surface the G3 scope correction prominently; get ratification before implementing.

## 1. G1 — per-driver submit-newline method (the one code change)

- [x] 1.1 `internal/deliver`: add `SendCtrlJ(target, text)` (type lines with `C-j` newlines, then `Enter`) + a pure `sendCtrlJArgs` arg-builder; arg test (per-line `send-keys -l`, `C-j` between, final `Enter`), like `slashKeysArgs`.
- [x] 1.2 `internal/surface`: confirm claude/aider/opencode drivers wire `send`→`deliver.Send` (bracketed paste); document the per-driver newline-method seam. grok: add a NOTE in its driver that the newline method is deferred to live-capture (#58) — NOT assumed-confirmed; cursor's equivalent note lands with the cursor driver (#62/#61).
- [x] 1.3 Live-confirm multi-line bracketed-paste Submit for claude/aider/opencode (aider+opencode already confirmed this session; claude is the reference) — record provenance.

## 2. Mixed-surface fleet regression

- [x] 2.1 Test: a roster with claude-code + aider + opencode agents resolves each agent's driver and routes Submit/Assess per-driver (locks the surface-agnostic guarantee proven live). (cmd/flotilla or internal/surface, whichever seam is cleanest.)

## 3. Docs — the pull-only inter-harness model

- [x] 3.1 New `docs/inter-harness.md`: the proven surface-agnostic plumbing + the PULL-PARTICIPANT model (non-claude desks are pull-only — collect by reading panes, state-cued by the driver Assess; one-way delegation) + the smart-desk follow-on. Explicit, honest.
- [x] 3.2 G2: `docs/xo-doctrine.md` — the XO LEANS ON the driver-Assess'd desk state for monitoring (not eyeballing non-claude panes).
- [x] 3.3 G4 (in-repo): note in `xo-doctrine.md`/`watch-runbook.md`/`agent-launch-recipes-design.md` that non-claude rotation = the driver's Rotate (`/new`,`/new-chat`) and recovery = relaunch via the launch recipe, not claude `/clear`/resume. README surface section: inter-harness fleet capability.

## 4. follow-ons (Non-Goals — tracked, not built here)

- [x] 4.1 Filed #63 — (b) SMART DESKS — flotilla reporting conventions in non-claude `AGENTS.md` so they `flotilla notify`-push (the first-class-peer B-follow-on).
- [ ] 4.2 G4 skill-layer (out-of-repo): update the `~/.claude` `fleet-session-rotation` + `flotilla-fleet-recovery` skills to be harness-aware (driver Rotate; launch-recipe relaunch for non-claude dead desks) — via the skill-sync flow, noted here so it's not lost.

## 5. review + ship (build phase — after 0.5)

- [ ] 5.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green; `openspec validate --strict`.
- [ ] 5.2 `/systems-review` AND `/open-code-review` in parallel on the implementation diff; resolve.
- [ ] 5.3 PR referencing this change; CI green; merge on clean gates. Archive; checkpoint the XO.

> **Build gate:** §1-3 implement only AFTER the design-gate checkpoint (0.5). G3 is
> dropped as a non-gap (surfaced at the checkpoint). Smart-desks (b) is the noted follow-on.
