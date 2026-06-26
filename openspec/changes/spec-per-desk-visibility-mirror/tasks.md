# Tasks — spec-per-desk-visibility-mirror (#176, spec-only)

- [x] 1. ADD the `watch` requirement capturing the per-desk visibility mirror's intended behavior
  (own-channel destination, observe-only/best-effort, ResultReader extraction, boot coverage,
  feedback-loop immunity) + the DOCUMENTED tick-lossy sub-tick-drop property.
- [x] 2. `openspec validate --all --strict` green.
- [ ] 3. systems-review on the delta (docs-only); PR to hydra-ops's gate.
- [ ] 4. (SEPARATE follow-on, NOT this change) the reliability FIX — reliable per-turn store-completion
  detection so sub-tick turns are mirrored. File/track as its own scoped change.
