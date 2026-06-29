# Design — heartbeat as a per-recipient judgment (#189, refines #183)

## 1. The shift, in one line

#183: *heartbeat a recipient because it is ELIGIBLE* (a static roster boolean). #189: *heartbeat a recipient
because there is OUTSTANDING ACTIONABLE WORK for it* (a per-recipient judgment, re-asked every tick). The
roster boolean does not go away — it becomes the HARD ELIGIBILITY gate; the judgment is a second, narrower
gate stacked on top of it.

## 2. The judgment model

For each monitored, non-primary-XO recipient the detector asks, per tick:

```
beat(recipient) ⇐  HeartbeatEnabled(recipient)          // #183 HARD gate (roster): XO-excl + #184 + explicit flag
              AND  state == Idle                          // #183: only an idle recipient is a heartbeat candidate
              AND  NOT settled(recipient)                 // #183: a recipient that touched its settle marker
              AND  NOT stopped(recipient)                 // #183: capped + escalated, awaiting re-arm
              AND  cadence elapsed                         // #183: per-recipient quiet-tick counter
              AND  HeartbeatWarranted(recipient)           // #189 NEW: is there live actionable work?
```

`HeartbeatWarranted(recipient)` is the new conjunct. Everything to its left is #183, unchanged. The judgment
is the LAST gate — cheapest to reason about, and it can only ever SUPPRESS a beat that #183 would have sent
(it adds a reason NOT to beat; it never forces a beat #183 would have withheld). That ordering is the safety
property: the judgment cannot resurrect a beat to an opted-out, settled, stopped, or non-idle recipient.

**The conjunct is a PURE injected boolean — the backlog I/O is NOT in this under-lock decision.** The
per-recipient backlog read is FILE I/O; the detector decision (`deskHeartbeatLocked`) runs UNDER `d.mu`,
and the detector has a load-bearing off-mutex invariant (no tmux/transcript/file I/O under the lock —
honored by synthesis at `internal/watch/detector.go:795-848` and the mirror). So `HeartbeatWarranted` is
NOT evaluated by reading a file inside the lock. It follows the SAME two-phase split as the visibility
synthesis: a phase-1 step OFF the lock reads + parses each eligible recipient's backlog and snapshots a
pure `map[agent]bool` warrant (computed from the parsed `Status` via the predicate below); the under-lock
phase-2 decision consults that already-computed pure boolean as its last conjunct. The detector seam type
(`HeartbeatWarranted func(agent) bool`) is therefore a pure lookup against pre-read data, never a
read-under-lock. (Implementation note: the seam may snapshot the warrant map at the top of the tick, or
the wiring may pre-read; the invariant the design fixes is that NO backlog `ReadFile`/`Parse` executes
while `d.mu` is held.)

### The warrant predicate

```
HeartbeatWarranted(recipient) ⇐ live actionable work exists for recipient
  where "live actionable work" = an actionable to-do that is
        NOT in the OPEN-QUESTIONS ledger        (not blocked-and-tracked)
    AND NOT in the AUTHORIZATIONS ledger          (not awaiting operator authorization)
```

Concretely, against the recipient's parsed backlog `Status` (§4):

```
warranted ⇐ !Status.Found || len(Status.Unblocked) > 0
```

The `len(Status.Unblocked) > 0` arm is the actionable check: `backlog.Parse` classifies an item as
`Unblocked` ONLY when it is `[in-flight]` or `[next]` (actionable) — and an item moved to
`[blocked]`/`[needs-attention]` (open-questions ledger) or the new `[awaiting-auth]` (authorizations
ledger) is, by construction, NOT in `Unblocked`. So "an actionable to-do not in either ledger" is exactly
"a non-empty `Unblocked` queue". The two ledgers are the two settle-NEUTRAL status classes; `[done]` is
drained.

The `!Status.Found` arm is the fail-safe that closes a real gap in a bare `len(Unblocked) > 0` predicate:
a present, readable backlog file that has NO `## Backlog` section parses to `Found=false, Unblocked=nil`
(verified: `backlog.Parse` sets `Found` only on a `## Backlog` heading — `internal/backlog/backlog.go:54-56`).
A bare `len(Unblocked) > 0` would read that as "no actionable work ⇒ suppress", contradicting the stated
fail-safe that ONLY an absent/unreadable backlog suppresses-via-warrant. A readable-but-sectionless file
cannot PROVE the recipient has no work, so it must keep beating. Hence `warranted = !Found || len(Unblocked)
> 0`: suppression requires a CLEANLY-PARSED `Found` backlog whose `Unblocked` queue is empty — i.e. the
recipient has affirmatively recorded that everything is done, blocked-and-tracked, or awaiting-auth. The
warrant read mirrors `backlogStatusGate`'s alert-once latch (`cmd/flotilla/watch.go:683-706`): a
present-but-sectionless (`!Found && non-empty content`) file raises ONE operator-visible alert on the edge
into that state, so the always-beat is loud, not a silent format slip. No live actionable work AND a clean
`Found` parse ⇒ `Unblocked` empty ⇒ NOT warranted ⇒ no beat (the recipient is legitimately idle/done).

**Fail-safe direction (load-bearing).** The judgment must fail toward WARRANTED (keep the recipient moving),
never toward suppressed (silently stall it — the exact regression #183 fixed). So: an ABSENT or UNREADABLE
per-recipient backlog ⇒ warranted (we cannot prove there is no work, so we keep beating — #183 behavior). A
present-but-SECTIONLESS backlog (`Found=false`, no `## Backlog` heading) ⇒ warranted (the `!Found` arm — it
cannot prove no work; alert-once on the edge). A MALFORMED item ⇒ warranted (it is counted in `Unblocked` by
`backlog.Parse`'s existing fail-safe). A torn mid-write read ⇒ warranted (self-heals next tick). A recipient
that keeps NO per-recipient backlog FILE ⇒ warranted via the missing-ledger fallback (§4) — NOT via the
shared backlog. The ONLY path to NOT-warranted is a CLEANLY-PARSED backlog that is `Found` AND whose
`Unblocked` queue is empty — i.e., the recipient has affirmatively recorded that everything is done,
blocked-and-tracked, or awaiting-auth. Suppression requires PROOF of no work, never its absence.

### Why this is a judgment, not a config

The same recipient flips between warranted and not-warranted across ticks WITHOUT any roster change, purely
from the evolving content of its ledgers: it finishes its last `[in-flight]` item and marks everything
`[blocked]`/`[awaiting-auth]` ⇒ stops being beaten; the operator answers a question and the recipient moves
an item back to `[next]` ⇒ becomes warranted again on the next tick. That dynamism is the whole point — it is
the "is there work?" decision the operator wants, recomputed continuously, not a static opt-out set once in
the roster.

## 3. Where the judgment lives (resolved: extend the resolver + a detector seam)

Three candidate seams were considered; the design uses a SPLIT that keeps each layer doing what it already
does, mirroring exactly how #183 split roster-policy from detector-mechanism.

| Layer | #183 today | #189 adds |
|---|---|---|
| `roster.Config` | `HeartbeatEnabled(name) bool` — static eligibility (XO-excl, #184, explicit flag). I/O-free. | `HeartbeatWarranted(name, st backlog.Status) bool` — composes the HARD gate with the warrant predicate over an INJECTED Status. Still I/O-free. |
| `internal/watch` Detector | `deskHeartbeatLocked` gates on the `HeartbeatEnabled(agent)` func seam. | a `HeartbeatWarranted(agent) bool` func seam consulted as the last conjunct — a PURE lookup against a per-recipient warrant computed OFF the lock (the seam never does file I/O under `d.mu`; two-phase, mirroring synthesis). Default seam = always-true ⇒ #183 byte-identical. |
| `cmd/flotilla/watch.go` | wires `HeartbeatEnabled` from the roster. | wires the per-recipient backlog read into the `HeartbeatWarranted` seam OFF-lock: resolve the recipient's OWN backlog (`<dir>/flotilla-<agent>-backlog.md`); if the per-recipient file is ABSENT ⇒ warranted (the missing-ledger fallback — #183 behavior, NOT the shared backlog); else `Parse` and call `cfg.HeartbeatWarranted(agent, st)`. |

**Resolved Q1 — extend `HeartbeatEnabled`, or a new seam? BOTH, by composition, not replacement.**
`HeartbeatEnabled` stays EXACTLY as is (the roster opt-OUT / #184 / XO HARD gate — its callers and tests are
untouched). The NEW `roster.Config.HeartbeatWarranted(name, st)` calls `HeartbeatEnabled(name)` FIRST and
returns false if disabled, THEN applies the warrant predicate. The detector gets a SEPARATE `HeartbeatWarranted
func(agent) bool` seam (so the backlog read — real I/O — is injected via an OFF-`d.mu` two-phase read, never
done in `roster`, never under `d.mu`). Rationale:
- The HARD gate and the judgment are different KINDS of "no": one is policy (never beat this recipient), one
  is state (nothing to do right now). Conflating them into one mutated `HeartbeatEnabled` would (a) force the
  roster to do filesystem I/O it deliberately avoids, and (b) make the #184 carve-out a soft, overridable
  data-driven decision instead of the HARD gate it must remain. Keeping them as two composed predicates makes
  the precedence explicit and testable: HARD gate ALWAYS wins.
- A separate detector seam (not widening `HeartbeatEnabled`'s signature) keeps #183's inert-when-nil story:
  `HeartbeatWarranted` nil ⇒ always-warranted ⇒ the judgment conjunct is a no-op ⇒ #183 exactly.

## 4. The two ledgers, concretely (resolved)

**Resolved Q2 — what are the two ledgers, and how does a recipient record into them?** They are two STATUS
CLASSES on the recipient's backlog markdown — NOT two new files, NOT sub-states bolted onto an in-memory
struct. This reuses the entire `backlog` substrate (the documented status-marker contract, the total/fail-safe
parser, the per-recipient read) instead of inventing a parallel mechanism.

| Ledger | Backlog status marker(s) | Meaning | Today |
|---|---|---|---|
| (actionable — NOT a ledger) | `[in-flight]`, `[next]` | live actionable work → warrants a beat | exists (`Unblocked`) |
| **OPEN-QUESTIONS** ledger | `[blocked]`, `[needs-attention]` | blocked-and-tracked: the recipient raised a question / hit a dependency; tracked, not actionable now | exists (operator-blocked) |
| **AUTHORIZATIONS** ledger | `[awaiting-auth]` **(NEW)** | awaiting operator AUTHORIZATION (a go/no-go, a spend approval) — distinct from a blocking question | **conflated into `[blocked]` today** |
| (drained) | `[done]`, `[x]`, `~~…~~`, `✅` | complete | exists (`Done`) |

How a recipient RECORDS "blocked-and-tracked" / "awaiting-auth": it edits its OWN backlog file — moving an
item's marker from `[in-flight]`/`[next]` to `[blocked]`/`[needs-attention]` (a question/dependency) or to
`[awaiting-auth]` (a pending authorization). This is the SAME write the XO already does on the fleet backlog
("mark it `[blocked]`/`[needs-attention]`" — `internal/watch/detector.go:1011-1013`), now done by each
recipient on its own ledger and now with a third class. The recipient-behavior contract (§6) instructs this
recording explicitly.

**Exact marker token (brittleness fix).** The classifier matches the marker WORD case-insensitively but the
SPELLING is fixed: `classify` (`internal/backlog/backlog.go:97-117`) recognizes exactly `awaiting-auth`. A
desk that writes a near-miss — `[awaiting-authorization]`, `[awaiting auth]`, `[awaiting_auth]` — produces an
UNRECOGNIZED marker, which the fail-safe flags `Malformed` AND treats as actionable: the item then warrants a
heartbeat FOREVER and never settles (the feature silently fails for that desk). Two guards: (a) the backlog
spec names the exact accepted token authoritatively; (b) the refined `deskContinuationBuiltin` prompt (§6)
QUOTES the literal `[awaiting-auth]` string the parser accepts, so the desk writes what the parser reads.
(Alternatives considered for the marker: a namespaced `[blocked:auth]` was rejected because the existing
`classify` keys on the WHOLE bracket word, not a prefix — a colon-namespaced token would itself be malformed
without a parser change; the flat `[awaiting-auth]` is parser-compatible with the existing single-word match.)

**Missing per-recipient ledger ⇒ always-warranted (NOT the shared backlog).** A recipient that keeps NO
`<dir>/flotilla-<recipient>-backlog.md` falls back to ALWAYS-WARRANTED — driven exactly as #183, the
pre-judgment behavior. It does NOT fall back to the SHARED fleet backlog. Falling back to the shared backlog
would be a design error: the shared backlog is the XO's drive queue, not THIS desk's work, so a busy fleet
queue would warrant EVERY ledger-less desk indiscriminately — re-creating the very poke-because-eligible
behavior this change exists to end, and making the judgment a near-no-op on a day-one deployment where no
desk yet keeps a per-recipient ledger. The rule is: per-recipient ledger PRESENT ⇒ judge by it; ABSENT ⇒
warranted (the desk has not opted into the judgment; it is driven as #183). This keeps the judgment a strict,
OPT-IN narrowing: a desk earns suppression only by maintaining its own ledger and emptying its actionable
set.

**Why split `[awaiting-auth]` out of `[blocked]`?** The operator's heuristic names TWO distinct ledgers, and
they ARE distinct: a `[blocked]` item is waiting on an ANSWER/dependency (the recipient is stuck and tracking
it); an `[awaiting-auth]` item is COMPLETE-pending-a-decision the operator owns (money / irreversible /
divergent fork — the three genuine operator decisions). Both are settle-neutral for the heartbeat (neither
warrants a beat), but they route and surface differently: an authorizations item is a STANDING operator-owned
decision that should be visibly resurfaced, where a blocked item may self-resolve. Collapsing them (as today)
hides authorization debt inside the blocked pile. For the WARRANT predicate they behave identically (both
keep an item OUT of `Unblocked`); the split is what makes the two ledgers nameable, separately countable
(`Status.Blocked` vs `Status.AwaitingAuth`), and separately surfaceable.

**`Status` shape change (additive).** `backlog.Status` gains `AwaitingAuth int` (count) alongside the existing
`Blocked`/`Done`/`Unblocked`/`Malformed`/`Items`/`Found`. The classifier maps `awaiting-auth` to a new
`clsAwaitingAuth`. `Unblocked` is UNCHANGED (an `[awaiting-auth]` item is NOT actionable, so — exactly like
`[blocked]` — it never enters `Unblocked`). Total/fail-safe contract unchanged: an unrecognized marker is
still flagged `Malformed` AND driven (warranted). Backward compatible: a backlog with no `[awaiting-auth]`
items parses byte-identically to today.

## 5. Preserving #183/#184 safety + the cap (load-bearing)

**The #184 approval-sensitive opt-OUT stays a HARD gate the judgment NEVER overrides.** TWO independent
checks enforce this, by design (defense-in-depth, NOT an accidental duplicate): (1) the DETECTOR's own
`HeartbeatEnabled(agent)` conjunct in `deskHeartbeatLocked` (`internal/watch/detector.go:744`) is the
PRIMARY HARD gate — it runs FIRST, and an opted-out desk `continue`s before the warrant is ever consulted;
(2) the roster `HeartbeatWarranted(name, st)` ALSO returns false immediately when `HeartbeatEnabled(name)`
is false — BEFORE it ever looks at the backlog. Check (1) alone is sufficient for safety; check (2) is
intentional redundancy so the roster judgment is correct in isolation (and unit-testable without the
detector). **Do not let a future DRY refactor collapse the two:** the detector's `HeartbeatEnabled`
conjunct is the load-bearing HARD gate and must remain even though the roster judgment re-checks it — they
guard the same invariant at two layers on purpose. So an approval-sensitive desk with a backlog FULL of
`[in-flight]` items is STILL not beaten: warrant-true cannot resurrect an opted-out recipient. The same
holds for the XO exclusion and an explicit `heartbeat:false`. Asserted by a dedicated scenario
(`approval-sensitive stays opt-OUT even when warranted`). The judgment is strictly a NARROWING of the #183
candidate set, never a widening.

**The cap → escalate → stop backstop is unchanged — but understand WHICH stuck desks it catches.** The cap
accrues only on DELIVERED beats with no intervening progress (`internal/watch/detector.go:773-786`). A
NOT-warranted idle tick delivers NO beat, so — exactly like a settled tick — it accrues NO cap and NO
cadence (the recipient is legitimately idle, not wedged). There are TWO distinct ways a stuck item stops
being poked, and the design must state both accurately:

- **Ledger-mark is the PRIMARY path.** A compliant desk that hits a blocker MARKS the item `[blocked]` /
  `[needs-attention]` / `[awaiting-auth]`. The item leaves `Unblocked`, the judgment stops warranting, and
  the desk settles CAP-NEUTRAL — NO escalation. This is INTENDED: a correctly-parked item is not a wedge,
  so the cap deliberately does NOT fire for it. The framing "the judgment cannot mask a wedge" is therefore
  too absolute as stated — a desk that marks its stuck item `[blocked]`/`[awaiting-auth]` DOES settle
  silently and is not escalated, and that is correct.
- **The cap is the BACKSTOP for a NON-compliant wedge.** It catches the desk that has live `[in-flight]`
  work it is NOT progressing AND will NOT mark its blocker. Such a desk stays warranted (live work by
  definition), keeps being beaten, and after capN no-progress beats escalates once and stops. THAT is the
  wedge the judgment "cannot mask": an unmarked, un-progressing `[in-flight]` item stays warranted, so the
  cap still fires.

The two paths are complementary: the ledger-mark settles the BEAT for an honest desk; the cap is the
safety net for a desk that won't self-report. What NEITHER path does is SURFACE a ledger-parked item to the
operator — a desk that correctly settles on `[awaiting-auth]` is now invisible to the beat AND uncaught by
the cap (by design). That surfacing backstop (resurfacing ledger-parked items, especially `[awaiting-auth]`
authorizations, to the operator/XO) is OUT OF SCOPE for this change and tracked in **issue #193**; this
change lands the substrate it needs (the two named ledgers + the `AwaitingAuth` count + the dash field).

**Byte-inert when off.** Two independent inert axes, both preserved:
- `HeartbeatEnabled` nil (the detector seam) ⇒ the WHOLE desk-heartbeat block is skipped (#183 regression-lock,
  `internal/watch/detector.go:738-740`). Untouched.
- `HeartbeatWarranted` nil (the new detector seam) ⇒ the judgment conjunct defaults to always-warranted ⇒ the
  trigger is #183's exactly. A deployment that does not wire per-recipient backlogs is byte-identical to #183.

## 6. The recipient-behavior contract (resolved Q3: refine the prompt)

**Resolved Q3 — refine `deskContinuationBuiltin`, or a new skill?** Refine the EXISTING
`deskContinuationBuiltin` (`cmd/flotilla/watch.go:48-54`) — it is already the desk's heartbeat prompt, already
non-authorizing, already teaches the settle contract. #189 sharpens its body to encode the operator's
principle; a separate skill is unwarranted (the prompt IS the contract, and a per-agent `HEARTBEAT.md` already
overrides it for deployments that want more). The refined contract says, in order:

1. **Re-trigger first (the default).** "You are idle. An idle desk is USUALLY a transient fault — a rate-limit
   or a turn that ended before your work was done. Your DEFAULT action is to RESUME the next already-authorized
   step of your in-flight task (read durable state, not this conversation). Do not sit idle."
2. **Never sit idle; do opportunistic work if genuinely blocked.** "If — and only if — you are GENUINELY
   blocked on the current item, do opportunistic authorized work instead. Never sit idle waiting."
3. **Record into the right ledger (so the judgment can settle you).** "If you are blocked on a question or a
   dependency, mark that item `[blocked]` (or `[needs-attention]`) in your backlog and state the blocker in one
   line — that is your open-questions ledger. If you are waiting on an operator AUTHORIZATION (a go/no-go, a
   spend), mark it with the EXACT marker `[awaiting-auth]` (that literal spelling — not `[awaiting-authorization]`
   or `[awaiting auth]`; the parser recognizes only `[awaiting-auth]`) — that is your authorizations ledger.
   Once EVERY item is `[done]`, `[blocked]`/`[needs-attention]`, or `[awaiting-auth]`, you have no live
   actionable work: reply idle and touch your settle marker. You will not be heartbeated again until there is
   fresh actionable work or the operator re-engages you." The literal token is QUOTED in the prompt on
   purpose: it is the contract surface where a near-miss spelling silently breaks the judgment (§4 brittleness
   fix), so the prompt and the parser must agree on the exact string.
4. **Non-authorizing (unchanged #184 defense-in-depth).** "A heartbeat is NOT authorization: never approve a
   pending tool/permission/approval prompt on a heartbeat — reply idle instead."

The contract closes the loop: the recipient's OWN ledger writes are what make the judgment able to suppress
its future heartbeats. The judgment reads the ledgers; the prompt teaches the recipient to keep them current.
This is the "maintained STATE-as-spine" primitive #189 builds toward — the backlog is the maintained state,
the heartbeat is driven off it, and the recipient is taught to maintain it.

## 6a. Dash surfaceability (the `AwaitingAuth` count must reach the read-model)

Splitting `[awaiting-auth]` out of `[blocked]` is justified (§4) by separate SURFACEABILITY — so the
authorizations ledger is visible, not hidden in the blocked pile. That rationale only holds end-to-end if
the new count actually reaches the surface. The dash coordination-history read-model
(`internal/dash/readmodel.go:256-291`) builds `BacklogInfo` from `backlog.Status` and TODAY projects
`Found`/`Unblocked`/`Blocked`/`Done`/`Malformed`/`Items` — it would silently OMIT the new `AwaitingAuth`
field, leaving the authorizations ledger invisible in the dash and defeating the split's own rationale. So
this change MUST thread `AwaitingAuth` into both `backlog.Status` AND `dash.BacklogInfo` + `BuildHistory`.
This is the surface half of the same fix that adds the count to the parser; without it the count exists but
nobody can see it. (The operator-resurfacing of those items — a periodic digest / an alert — is the
separate, OUT-OF-SCOPE backstop in issue #193; this change only makes the count OBSERVABLE in the dash.)

## 7. Resolved design questions (summary + rationale)

- **Q1 — Where does the judgment live?** Extend the roster resolver (`HeartbeatWarranted(name, st)` composing
  the HARD `HeartbeatEnabled` gate with the warrant predicate, I/O-free) AND add a `HeartbeatWarranted
  func(agent) bool` detector seam (the backlog read injected, default always-true ⇒ #183 inert). NOT a mutation
  of `HeartbeatEnabled` (that would force roster I/O and soften the #184 HARD gate). Rationale: §3.
- **Q2 — What are the two ledgers, concretely?** Two STATUS CLASSES on the recipient's existing per-recipient
  backlog markdown: OPEN-QUESTIONS = `[blocked]`/`[needs-attention]` (exists); AUTHORIZATIONS = a NEW
  `[awaiting-auth]` marker carved out of the overloaded `[blocked]`. A recipient records by editing its own
  backlog's marker. NOT new files, NOT in-memory sub-states. Rationale: §4 (reuses the whole `backlog`
  substrate; makes the two ledgers separately nameable/countable/surfaceable).
- **Q3 — The recipient-behavior contract.** Refine `deskContinuationBuiltin` (not a new skill) to:
  re-trigger-first (idle is usually a technical fault) → never sit idle / opportunistic work if blocked →
  record into the right ledger so the judgment can settle you → stay non-authorizing. Rationale: §6.
- **Q4 — Preserve #184 + the cap?** The judgment is the LAST conjunct and can only SUPPRESS; the #184/XO HARD
  gate is checked FIRST and is never overridable; the cap accrues only on delivered beats (a not-warranted
  idle tick is cap-neutral, like a settled tick); byte-inert on both the `HeartbeatEnabled`-nil and
  `HeartbeatWarranted`-nil axes. Rationale: §5.

### Alternatives considered and REJECTED

- **Shared-backlog fallback for a ledger-less desk.** Rejected (§4): the shared backlog is the XO's drive
  queue, not the desk's; using it would warrant every ledger-less desk on a busy fleet, re-creating
  indiscriminate poking. Chosen: missing per-recipient ledger ⇒ always-warranted (#183 behavior).
- **A namespaced `[blocked:auth]` marker** (instead of a flat `[awaiting-auth]`). Rejected: `classify`
  (`internal/backlog/backlog.go:97-117`) matches the WHOLE bracket word against a fixed set — a colon-
  namespaced token would be Malformed without a parser rewrite. Chosen: the flat `[awaiting-auth]` is
  parser-compatible with the existing single-word match (§4).
- **Enriching the judgment with item AGE / item TYPE** (e.g. "warrant only if the oldest actionable item is
  > N minutes old", or weight `[in-flight]` over `[next]`). Rejected for THIS change: it requires durable
  per-item TIMESTAMPS the backlog format does not carry, and any age threshold risks the exact regression
  #183 fixed — a silent stall (an actionable item that is "too fresh" or "too old" gets suppressed and the
  desk sits idle). The judgment here is deliberately the binary "is there a live actionable item?" — proof
  of no work, never a heuristic about which work. Age/priority enrichment, if ever wanted, is a separate
  change that must first add timestamps and re-establish the fail-toward-warranted invariant.

### Prerequisite: archive `recursive-desk-heartbeat` (#183) FIRST

This change's `watch` delta MODIFIES the requirement "Recursive per-agent desk heartbeat", which is
introduced by the still-UNARCHIVED `recursive-desk-heartbeat` change (#183). That requirement is NOT yet in
the base `openspec/specs/watch/spec.md` on main (the #183 CODE is merged in `origin/main` `f91882f`, but its
SPEC delta is not yet archived). `openspec validate --strict` checks delta structure, not cross-change
archive ordering, so it passes regardless — this ordering hazard is invisible to the validator. Therefore
`recursive-desk-heartbeat` MUST be archived into the base `watch` spec BEFORE this change is validated
against the base spec / merged, or the MODIFIED target requirement will not exist. Tracked as a task in
`tasks.md` §6.

## 8. Cited grounding (file:line)

- `internal/roster/roster.go:382-394` — `HeartbeatEnabled`: the static resolver #189 composes the judgment on
  top of (HARD gate retained).
- `internal/roster/roster.go:36-42` — `Agent.Heartbeat *bool` (opt-OUT) + `Agent.ApprovalSensitive` (#184).
- `internal/watch/detector.go:737-793` — `deskHeartbeatLocked`: the per-desk loop the `HeartbeatWarranted`
  conjunct slots into (last gate, after the `HeartbeatEnabled(name)` HARD gate at line 744).
- `internal/watch/detector.go:773-786` — the cap accounting the judgment leaves untouched (cap on delivered
  beats only).
- `internal/watch/detector.go:964-1017` — `continueXO`: the XO's existing "settle only when no Unblocked work"
  veto — the SEED #189 generalizes per-recipient. Lines 1011-1013: the XO marking an item `[blocked]` — the
  same write each recipient now does on its own ledger.
- `internal/backlog/backlog.go:33-40` — `Status`: gains `AwaitingAuth`.
- `internal/backlog/backlog.go:97-117` — `classify`: gains the `awaiting-auth` case (carved from `blocked`).
- `internal/backlog/backlog_test.go:23` — evidence of the conflation today (`[blocked] … awaiting operator
  value sign-off`) that `[awaiting-auth]` resolves.
- `cmd/flotilla/watch.go:48-54` — `deskContinuationBuiltin`: the prompt refined to the re-trigger-first +
  two-ledger contract.
- `cmd/flotilla/watch.go:399` — `deskHeartbeatEnabled` wiring; #189 adds the parallel `HeartbeatWarranted`
  wiring + the per-recipient backlog read.
- `cmd/flotilla/watch.go:683-706` — `backlogStatusGate`: the per-tick fresh-read + fail-safe + alert-once
  pattern the per-recipient warrant read reuses (including the `!Found && non-empty-content` alert edge).
- `internal/backlog/backlog.go:54-56` — `Parse` sets `Found` ONLY on a `## Backlog` heading: the basis for
  the `!Found` warrant arm (a present, sectionless file ⇒ `Found=false, Unblocked=nil` ⇒ must warrant).
- `internal/watch/detector.go:795-848` — `synthEligibleLocked` / `runSynthesis`: the two-phase
  read-off-lock / decide-under-lock split the per-recipient warrant read mirrors (the load-bearing
  off-mutex invariant).
- `internal/dash/readmodel.go:256-291` — `BacklogInfo` / `BuildHistory`: the dash read-model that must be
  threaded with the new `AwaitingAuth` count for the authorizations ledger to be visible (§6a).
