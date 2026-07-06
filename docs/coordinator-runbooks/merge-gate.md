# Merge gate — coordinator merge procedure

You are the independent review gate. A merge is the fleet's review made real; run
this on every PR that reaches you. Implements Principle 7 (merge on clean gates;
the reviewer is independent of the builder) and Principle 8 (verify; never
fabricate).

## Preconditions

- The author's gate chain ran: desk → XO verdict. No agent merges its own work.
- A delegate verdict ("CLEAR", automated reviewer PASS) is an **input**, not your
  decision. Re-verify load-bearing claims at source.

## Procedure

1. **Pin the head.** `gh pr view N --json headRefOid` — every check is valid ONLY at
   this SHA. Re-verify if the branch moves.
   *(Trap: merged-on-stale-head — approvals at an earlier push let a failing test
   ride a later push into main.)*

2. **Checks green AT HEAD.** `gh pr view N --json statusCheckRollup` — zero pending,
   zero failed.

3. **Read EVERY review comment BEFORE merging.**
   `gh api repos/<owner>/<repo>/pulls/N/comments` — count and read them. Overall
   "pass" coexists with inline P1 findings.
   *(Trap: merging past unread P1 findings — check comments BEFORE merge, never after.)*

4. **Unresolved review threads:** graphql `reviewThreads{nodes{isResolved}}`. Open
   threads are unaddressed OR dispositioned false positives you must spot-check.

5. **Findings triage — three dispositions only:**
   - **FIX** (push required, re-gate at new head);
   - **REJECT WITH EVIDENCE** (desk cites checkable source);
   - **DOCUMENTED-DEFER** (real but out of scope → tracked issue ON the thread).
   "Narrated" and "the tool passed overall" are not dispositions.

6. **Cross-branch safety for stacked PRs:** conflicts via merge-base method —
   `git merge-tree $(git merge-base A B) A B` — NEVER `merge-tree branch main`
   (ancestor comparison is clean by construction and answers the wrong question).

7. **Merge.** `gh pr merge N --squash --delete-branch`. Squash-merging a stack base
   auto-closes children — plan the re-stack.

8. **Post-merge verify on main — non-negotiable:**
   ```bash
   git fetch origin main && git reset --hard origin/main
   set -o pipefail; go test ./... 2>&1 | tail -3
   ```
   *(Trap: `go test | tail` returns tail's exit code — pipefail is standing policy.)*
   *(Trap: deleting code without deleting its source-presence lock tests → red main.)*

9. **Deploy** if running services changed — [`deploy-flotilla.md`](./deploy-flotilla.md).

10. **Ledger it.** Anchor-replace append: what merged, verdicts, deploys, bounces.

## Bounce protocol

Deliver findings to the desk pane; if busy, post the SAME findings as a PR comment
(durable channel) and note "pane busy → durable channel."

## Automated reviewer notes

- Re-runs on every push; wait for completion at the **final** head.
- False positives happen; reject-with-evidence with empirical counter-proof wins.

## Speed

Gate latency on a serial lane is calendar time the operator feels. Gate promptly —
but never skip steps 3 and 8 under launch pressure (those skips produced banner
defects and red main in production).