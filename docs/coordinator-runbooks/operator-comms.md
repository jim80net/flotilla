# Operator communications

The operator is the scarcest resource. Every message either keeps their mental map
current at a glance or costs attention decoding your internal state. Implements
Principles 5 (reader-modeling) and 12 (executive mini-brief). The distilled shape
also ships as the `executive-mini-brief` doctrine block via `flotilla doctrine install`.

## Reader model

A busy executive who has **not** been watching your work move by move. They do not
remember issue numbers, branch names, or which desk owns what. Operator-facing text
is plain; desk-to-desk traffic stays dense.

## Mini-brief format (mechanical)

1. **Bottom line** — 1–2 plain sentences: what changed in *their* world and whether
   they need to act.
2. **Mini brief** — 2–5 bullets: each stream by **what it does**, not codenames.
3. **Detail footer** — optional: PR numbers, SHAs, paths (drill-in only).
4. **Action status** — explicit ask OR clear all-clear, **varied wording** each turn.

### 20-second test

Read cold as a smart person with zero fleet context. State of their world + what
they must do in under 20 seconds, without decoding jargon?

### Jargon translation

| Internal | Say instead |
|---|---|
| automated code reviewer | the automated code reviewer |
| gate / re-gate | final review before merge |
| head SHA | latest version of the change |
| worktree | the desk's working copy |
| roster | the fleet's desk list |
| watch daemon | the background fleet ticker |
| `#1234` | lead with what the thing **is** |

## Decision queue

**Only three operator decisions** (Principle 2). Everything else: execute and report.

When surfacing a real decision, give all six elements (decision-brief doctrine):

1. What it is (plain language)
2. Concrete dollar value if spend (or "unknown — fetching")
3. Mechanics on approval
4. Alternatives + tradeoffs
5. Recommendation + safe default
6. Reversibility

Keep executing parallel work not blocked on the answer.

## Verification — never fabricate (Principle 8)

Never state status you did not verify **this session**. Three honest moves when
you lack data: ask, defer naming the gap, surface the blocker.

**Match this:** a brief shipped with honest "not measurable right now" provenance
when the live probe could not run — not a plausible invented number.

**Avoid this:** relaying a partial signal (handshake, green CI on the wrong SHA) as
end-to-end capability. Verify the full claim before "done/shipped" reaches the operator.

## Corrections — highest-priority signal

1. Capture **verbatim** into the ledger.
2. Fix the instance now.
3. Build **mechanical** enforcement (gate, fail-closed path, template) — not promises
   (Principle 6).
4. Propagate to every level it's true for (seat rules, desks, public constitution).
5. On **repeat** correction: show where the prior fix landed and why it missed.

## Forensic-audit vs epistemic questions

*"Where else have we…", "is this fully captured", "double-check that"* → full audit
(hours, file:line citations), not a partial answer.

*"What am I missing?" / "is this done?"* → state **report** (including "nothing
material missing"), not invented scope expansion.

## Delivery — `flotilla notify`

```bash
flotilla notify "<reply>"
flotilla notify --file ./reply.md
flotilla notify --from <persona> "<reply>"
flotilla notify --chunk --file ./long.md
flotilla notify --attach ./deck.png "<caption>"
```

- Bodies over 2000 chars: `--chunk` or summarize.
- Non-zero exit = NOT DELIVERED — re-send.
- Partition firewall may refuse suspected leaks — rephrase, never disable
  ([`private-public-boundary.md`](../private-public-boundary.md)).

**Notify for:** operator replies, decisions, blockers, completions they're waiting on.
**Do not notify for:** heartbeat ticks, routine plumbing.

## Recovering operator messages

`flotilla inbox <channel> --limit N` — REST read of bound channel history when
relay dropped or truncated a message. Image attachments need bot-token fetch via
channels API — never put the token in a notify body.