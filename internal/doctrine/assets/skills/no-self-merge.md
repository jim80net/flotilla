<!-- flotilla:no-self-merge -->
<!-- This flotilla:no-self-merge marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Never merge your own work — the merge IS the independent review

You do **NOT** merge your own changes. When your work is ready (review gates clean,
CI green), you **surface** the pull request to the agent one level ABOVE you — your
XO; for an XO, the meta-XO — and **they** review and merge it. A desk surfaces its PR
to its XO; the XO reviews and merges. The XO surfaces its PR to the meta-XO; the
meta-XO reviews and merges. Each level's output is reviewed by the level above. A
boat never grades its own homework.

**At the top of the hierarchy** — a seat with no level above it (the meta-XO, or a
solo agent in a not-yet-federated fleet) — the **operator is the reviewer of last
resort**: surface there. The rule is a *hierarchy-relative* control; it bottoms out at
the operator, never at "I reviewed myself." Only a genuine apex with no operator review
available at all merges its own clean-gated work — and that is a gap to close by growing
the layer above, not a license to self-approve.

**Why this is a rule, not a nicety.** Merge-on-clean-gates autonomy plus a shared git
identity make a self-merge easy and INVISIBLE — nothing in the *git* audit trail shows
the independent review was skipped. The merge IS the review gate; a self-merge silently
removes it. For real-money, irreversible, or otherwise high-stakes work, that
independent second pair of eyes is a control you do not give up.

**This does NOT slow autonomy.** Clean-gated work still merges without waiting on the
operator — the only thing that changes is WHO pushes the button: the level above the
author, not the author. Surface promptly, review honestly, merge what genuinely
clears. The reviewer's job is a real review (read the diff, run/trust the gates), not
a rubber stamp.
<!-- /flotilla:no-self-merge -->
