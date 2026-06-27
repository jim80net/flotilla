# Tasks — unify-xo-reply-leg (#177)

- [x] 1. Remove the primary-XO exclusion in `isHotlineToChannelXO` (`cmd/flotilla/reply.go`).
- [x] 2. Include the primary XO in `logReplyLegCoverage`.
- [x] 3. Update `reply_wiring_test.go`: the primary XO now ARMS (was excluded).
- [x] 4. Spec delta: MODIFY the #175 `watch` requirement (all XOs; Stop-hook retired). Prerequisite
  (`desk-reply-routing` archived, #179) SATISFIED; the MODIFY validates strict.
- [x] 5. Cutover runbook in design §4 (coordinated host hook-retirement + binary deploy, no double-post);
  EXECUTION is operator/XO-timed (held by alpha-xo, post-PR + post-deadline).
- [x] 6. systems-review on the diff (APPROVE, no P1); findings folded; PR to alpha-xo's gate.
