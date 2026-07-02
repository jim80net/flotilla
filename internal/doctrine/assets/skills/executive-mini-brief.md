<!-- flotilla:executive-mini-brief -->
<!-- This flotilla:executive-mini-brief marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Operator turn-finals are executive mini-briefs

The operator is a **busy executive with many reports** — not watching your work move by
move. **Every message to the operator** (status, answers, decisions, task confirmations,
and **every turn-final** — the Discord mirror posts yours mechanically) must work for
that reader. Desk-to-desk and XO-internal traffic stays dense; **operator-facing text
does not.**

**Format — mechanical, no exceptions:**

1. **Bottom line first (1–2 plain-English sentences).** What changed in *their* world
   and whether anything needs them. Example shape: "The fleet tooling upgrade passed
   review and is ready to merge; nothing needs you."
2. **Mini brief (2–5 short bullets or sentences).** Each active work stream: what it
   is **for them**, where it stands, what happens next. Name streams by **what they do**
   ("the options-closing bug fix", "the coordination upgrade") — not by issue numbers,
   branch names, or internal codenames.
3. **Detail footer (optional, last).** PR numbers, SHAs, file paths, gate vocabulary —
   compressed, for drill-in only. Often omit entirely; the ledger holds identifiers.
4. **Always end with exactly one of:**
   - `Waiting on you: <one concrete ask>` — or —
   - `Nothing needs you.`

**Jargon discipline:** Never assume the operator knows internal vocabulary mid-skim
(automated reviewer names, merge gates, worktree, roster, seat flip, etc.). Translate to
plain English or gloss on first use. `#1234` is a pointer, not a name — lead with what the
thing **is**.

**The 20-second test:** A smart person with zero fleet context and ten fires elsewhere
can get their world's state and what they must do — without decoding a codename. If not,
rewrite before sending.

**Coordinators (every XO and the Chief of Staff):** this format is your default register
for operator communication — including turn-finals the mirror posts verbatim. Principle 5
(reader-modeling) sets the posture; this block is the **shape**.
<!-- /flotilla:executive-mini-brief -->
