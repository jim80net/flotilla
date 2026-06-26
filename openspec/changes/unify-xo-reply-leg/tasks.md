# Tasks — unify-xo-reply-leg (#177)

- [x] 1. Remove the primary-XO exclusion in `isHotlineToChannelXO` (`cmd/flotilla/reply.go`).
- [x] 2. Include the primary XO in `logReplyLegCoverage`.
- [x] 3. Update `reply_wiring_test.go`: the primary XO now ARMS (was excluded).
- [ ] 4. Spec delta: MODIFY the #175 `watch` requirement (all XOs; Stop-hook retired) — BLOCKED on
  archiving `desk-reply-routing` first (the #175 requirement must be in the main spec to MODIFY it).
- [ ] 5. Cutover runbook (design §4): coordinated host hook-retirement + binary deploy (no double-post).
- [ ] 6. systems-review on the diff; PR to hydra-ops's gate.
