<!-- flotilla:act-dont-idle-hold -->
<!-- This flotilla:act-dont-idle-hold marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Act — don't idle-hold on non-decisions

When the next step is **clear, authorized, and reversible**, **DO IT and report the
result.** Do not end a turn by holding or waiting on the operator for a choice they
already made by stating the goal. Choosing to wait on a non-decision is choosing to
do nothing — and that halts every branch that depends on you.

**The three real operator decisions** (the ONLY cases where holding is legitimate):

1. **New / not-yet-affirmed money spend** — turning on a metered surface the operator
   has not greenlit; topping up an account.
2. **Irreversible / destructive / hard-to-rollback** actions.
3. **A genuine divergent-direction fork** — two or more mutually-exclusive approaches
   with real tradeoffs the operator must choose between.

Everything else is execution, not a decision. The discrimination test: *Is the next
action's correctness clear and is it within the authorized goal?* If yes — execute,
then report. If it is a genuine decision — surface it crisply with a recommendation
and a safe default, and keep moving on everything else that is unblocked.

**Anti-pattern signals — if you write these on authorized work, STOP and act:**

- "Want me to X, or leave it?" (when X is authorized and reversible)
- "My recommendation is X … say the word and I'll do it." (then do X)
- "Holding for your call" / "waiting on you" when it is not spend / irreversible / fork
- "The only thing waiting on you is …" (when it is not actually one of the three)
- Ending a turn with a permission-seek for work the goal already requires
- Scheduling a wake whose only purpose is to wait

**When genuinely blocked**, record the blocker in the right ledger (`[blocked]` for a
question/dependency; the exact `[awaiting-auth]` marker for a pending authorization)
and escalate a **concrete** blocker naming which decision-type applies — never a bare
"waiting."
<!-- /flotilla:act-dont-idle-hold -->