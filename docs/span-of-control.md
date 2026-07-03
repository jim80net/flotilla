# Span of control — the Rule of Three (a guideline)

How any coordinating agent (the **XO**, a **flotilla** lead, a **boat**) divides
its attention across the work it owns. This is *operating doctrine* for the agent
in a coordinating seat — not flotilla software behavior. flotilla moves the
messages and clocks the turns; this page says how a lead should *structure* the
work below it so that no single seat is ever managing more than it can hold, and
so that independent work runs concurrently instead of one-at-a-time.

> **The Rule of Three is a GUIDELINE, not a hard rule.** "Three active charges" is a
> soft ceiling to design toward — a default that keeps attention coherent — not a
> limit strictly enforced. Use judgment; exceed it briefly when the situation
> genuinely warrants, and grow a layer when standing charges start to fray your
> attention rather than the instant a fourth appears.

> **Who this is for / how to use it.** flotilla does not own a lead's prompt — a
> lead is an ordinary agent session (e.g. Claude Code) that *you* run. So this
> doctrine becomes the default the same way every other agent behavior does: it
> is wired into the lead's standing instructions (its system prompt / `CLAUDE.md` /
> skill set). flotilla ships it for you: `flotilla workspace init` seeds the
> distilled Rule of Three into a new agent's identity file, and `flotilla doctrine
> install <agent>` (re)installs it idempotently into an existing workspace. The
> [Wiring it in](#wiring-it-in) section at the bottom is the two-minute setup (and
> what the installer does for you). Everything above it is the *why* and the
> *exact contract*.

## The guideline: keep each seat to about three active charges

> **A coordinating node — XO, flotilla lead, or boat — aims to keep its DIRECT
> charges to about THREE active subordinates or workstreams. When a fourth active
> charge would crowd you, prefer to grow a layer rather than carry it directly:
> cluster the charges into a few coherent groups, designate an owning lead for each,
> and apply the same guideline inside every group — so every layer keeps a
> manageable span. This is a default to design toward, not a tripwire that fires on
> the exact count of four; use judgment.**

Three is the anchor, and the reasoning is not folklore: a coordinating agent holds
each subordinate's state, last report, blockers, and next step in working memory, and
that memory is finite (and, under the change-detector, *rotated* — see the
[state-externalization contract](./xo-doctrine.md#the-state-externalization-contract-non-negotiable-when-this-is-on)).
Past about three active charges a lead tends to stop coordinating and start
*thrashing*: reports go uncollected, a blocked desk sits unsurfaced, the
operator-decision queue rots. A growing pile of charges is the signal — not to push
harder, but to grow a layer.

**The guideline SHAPES the hierarchy.** You do not design the org chart up front and
hope the work fits it. The work arrives, and as charges accumulate the structure grows
to fit: the lead clusters and delegates, a new intermediate lead is born, and the tree
deepens by roughly the amount the load demands. A flat fleet that stays within a few
charges never grows a layer; a fleet that takes on twenty workstreams grows about the
depth that keeps every seat manageable. The shape is an output of the load.

### Why a number at all (and why it's still a guideline)

Three is concrete enough to design toward — a useful anchor, the way the
self-continuation cap (`--max-self-continuations`, default 3) and the missed-ack
threshold (`--max-missed-acks`, default 3) are useful defaults. "Manage a reasonable
number" is too vague for an overloaded agent to act on; "around three, and grow a layer
when you start to fray" gives a usable shape without pretending the load respects a
bright line. But it is a GUIDELINE, not a hard gate: exceed it briefly when the
situation genuinely warrants, and let judgment — not a tripwire — decide when the
reorganization is worth its cost.

## How the layers map to flotilla terms

flotilla already *is* a recursive hub-and-spoke (see the
[federation tier](./quickstart.md#federated-fleets--per-project-channels--fleet-command) —
a meta-XO whose members are project-XOs whose members are desks). The Rule of
Three is the *governing invariant* for how deep and how wide that recursion grows.
The hierarchy, top to bottom:

| Layer | flotilla term | What it owns | Span obeys ≤ 3 |
|---|---|---|---|
| **Objectives** | the multivariate goal set | the operator's top-level objectives | a meta-XO covers ≤ 3 objective-clusters directly, else groups them |
| **Flotillas** | a **flotilla** (a project-XO + its channel) | one coherent objective domain | a meta-XO coordinates ≤ 3 project-XOs directly |
| **Boats** | a **boat** / desk (a domain-owning agent) | one domain workstream | a project-XO coordinates ≤ 3 boats directly |
| **Sub-agents** | Claude Code **sub-agents** (the `Task` tool) | one bounded task, fresh context | a boat fans out sub-agents (≈ unbounded — see below) |

**Every layer obeys the rule, with one deliberate floor.** A meta-XO with a fourth
project-XO clusters the flotillas under intermediate meta-XOs. A project-XO with a
fourth boat clusters the domains under sub-leads. A boat with a fourth concurrent
sub-task clusters them under coordinating sub-agents.

**The sub-agent layer is the pressure-relief valve, not an exception.** Claude
Code sub-agents are ephemeral, single-task, fresh-context workers that **report
once and exit** — they are not *managed* across time the way a desk is. A boat can
fan out many sub-agents in one turn because it is not holding twenty live
relationships; it is dispatching twenty bounded tasks and collecting their
returns. The rule governs *standing* coordination relationships (a thing you must
keep checking on), not *transient* fan-out. So: ≤ 3 standing charges per seat; a
boat's sub-agent fan-out is bounded by task independence and token budget, not by
three. **But a sub-agent you RE-DISPATCH every heartbeat is functionally a
STANDING charge — you must remember its state across rotations — so it COUNTS
against the three; only truly transient report-and-exit fan-out remains the
unbounded floor.** (The discrimination test: *does this charge require me to
remember its state across my next rotation?* If yes, it counts against the three.
If it reports once and is gone, it does not.)

## Reporting: each lead aggregates its ≤ 3 reports upward

A manageable span only buys you a readable fleet if reporting respects the same tree.
**Each lead AGGREGATES the reports from its handful of charges into one rolled-up
summary and passes THAT upward. The layer above sees a few group summaries — never N
raw node reports.**

This is the upward dual of the downward span limit, and it is what keeps the
operator's channel readable as the fleet scales. The operator (or meta-XO) reading
the top sees three flotilla-level summaries, not forty desk reports. Each flotilla
summary is itself an aggregate of ≤ 3 boat reports; each boat report is an
aggregate of its sub-agent returns. The same discrimination the XO already applies
to operator-facing traffic — *would the operator want to read this?* (see
[xo-doctrine §What counts as a genuine operator message](./xo-doctrine.md#what-counts-as-a-genuine-operator-message))
— applies at every tier: a lead forwards the *signal* (a decision, a blocker, a
completion the layer above is waiting on), not the raw plumbing. Aggregation is not
hiding information; it is the lead doing its job — turning N noisy streams into one
coherent picture for the seat above, exactly as the hub-and-spoke topology promises
"one coherent picture and one accountable router" (see the
[README → hub and spoke](../README.md)).

## Parallelism: independent work runs concurrently, never serially

The span limit says *how many* charges a seat holds. This says *how* it works
them: **independent workstreams are handled CONCURRENTLY — dispatch all of them,
then collect — never one-at-a-time. Serial ordering of independent work is the
failure mode the rule exists to prevent.**

A lead that owns three independent boats does not instruct boat A, wait for A to
finish, then instruct boat B. It dispatches A, B, and C in the same turn (three
`flotilla send` deliveries, or three sub-agent `Task` dispatches in one message)
and collects their reports as they land. The wall-clock cost of serializing
independent work is paid by the operator; the cost of parallelizing it is only the
lead's own context (cheap, recoverable) — the same asymmetry the project already
encodes in "proceed in parallel on independent clear paths."

**Concretely, at each layer:**

- **A boat fans out sub-agents in one turn** — all independent sub-tasks
  dispatched together (one assistant message with multiple `Task` tool calls),
  not a serial chain. Claude Code runs them concurrently.
- **A project-XO dispatches its boats in parallel** — independent desk
  instructions go out in the same turn via multiple `flotilla send` calls, and the
  XO collects on its heartbeat / change-detector wakes as each desk settles.
- **A meta-XO drives its project-XOs in parallel** — independent flotillas advance
  simultaneously; the meta-XO does not gate flotilla B's progress on flotilla A's.

The only thing that serializes is a genuine *dependency*: if B needs A's output, B
waits for A — that is correct ordering, not the failure mode. The failure mode is
serializing work that has **no** dependency between the streams. (Discrimination
test: *can I name the next concrete action on stream B without knowing stream A's
result?* If yes, B is independent — dispatch it now, do not wait.)

## Spawn discipline: grow the layer when charges start to pile up

The natural moment to act is **when charges accumulate past what you can hold —
typically around a fourth standing charge.** When you feel a seat fraying, grow the
layer rather than carrying more directly. Don't keep accepting "for now" and promising
to reorganize later — "for now" is how a seat ends up holding twelve. When you do grow
the layer, the reorganization is one act with accepting the work:

1. **Notice the crowding.** Charges are piling up past what the seat can coordinate
   well (around a fourth standing charge is the usual cue — use judgment).
2. **Cluster into a few groups.** Partition the charges into a handful of coherent
   groups — by domain, by objective, by dependency affinity (aim for about three).
3. **Designate a lead per group.** For each group, promote one charge to lead (or
   spawn a fresh coordinating agent / project-XO / boat) that owns the group and
   reports up as one aggregate.
4. **Recurse.** If any group is itself crowded, apply the guideline inside it.
5. **Then accept the work**, dispatched into the correct group.

The mechanism flotilla already ships supports this directly: a new boat is a new
roster entry + a `flotilla workspace init`; a new project-XO is a new
clock-only `watch` + a fleet-command channel binding (see the
[federation wiring](./quickstart.md#federated-fleets--per-project-channels--fleet-command)).
The doctrine just says *when* to reach for them: on the fourth.

## A worked example — an XO with four workstreams grows two leads

An XO (call it `infra-xo`) is coordinating a release and is, over a morning,
handed four standing workstreams by the operator:

1. **Ship the cache PR** (owned by desk `cache`).
2. **Migrate the auth schema** (owned by desk `auth`).
3. **Cut the v2 API** (owned by desk `api`).
4. **Stand up the new metrics pipeline** (a fresh objective — no desk yet).

The XO already holds three (`cache`, `auth`, `api`). The metrics pipeline is the
**fourth charge** — the tripwire. Instead of directly managing four desks
(thrashing: it would start dropping `cache`'s status while babysitting `metrics`),
the XO applies the rule **before** taking on metrics:

- **Cluster into ≤ 3 groups by affinity.** Two natural clusters emerge:
  - **`platform`** — `cache` + `auth` + `api` (the existing release-train desks,
    one coherent domain).
  - **`observability`** — the metrics pipeline (a new domain), which itself
    decomposes into `metrics-collector` + `metrics-store` + `metrics-dash` — three
    sub-tasks, already at the limit.
- **Designate a lead per group** — spawn two intermediate leads (two project-XOs
  under `infra-xo`'s meta-XO seat, or two boats, depending on weight):
  - **`platform-lead`** owns `cache`, `auth`, `api` (three boats — at the limit,
    obeys the rule).
  - **`observability-lead`** owns the three metrics sub-desks (three boats — at the
    limit, obeys the rule).
- **`infra-xo` now directly manages exactly TWO charges** — `platform-lead` and
  `observability-lead` — well under three, with headroom for a third objective.

Now reporting flows up the tree: each metrics sub-desk reports to
`observability-lead`, which aggregates into one observability summary;
`cache`/`auth`/`api` report to `platform-lead`, which aggregates into one platform
summary; `infra-xo` sees **two** group summaries and forwards to the operator only
what rises to operator attention. And work runs in parallel: `platform-lead`
dispatches `cache`/`auth`/`api` concurrently, `observability-lead` dispatches its
three sub-desks concurrently, and `infra-xo` drives both leads concurrently — no
independent stream waits on another. One fourth charge forced exactly one layer of
depth, and the fleet stayed readable.

## Wiring it in

You do not have to do this by hand — **flotilla ships the Rule of Three as a
constitutional member.** `flotilla workspace init <agent>` seeds the distilled rule
into the new agent's identity file automatically, and `flotilla doctrine install
<agent>` (re)installs it idempotently into an existing workspace (it appends the
rule once, under a marker fence, and detects-and-skips on a re-run, so your edits
survive). The identity file loads into the agent's system prompt at launch via
`--append-system-prompt-file`, so the rule is in force from the first turn. The
steps below are what that install wires in for you (and what to do if you are
hand-rolling a prompt the installer does not own):

1. **Add the rule to every coordinating agent's standing instructions.** Put a line
   in the XO's / lead's system prompt / `CLAUDE.md` / skills to the effect of:

   > As a guideline, keep to about THREE active charges (desks / workstreams)
   > directly. When a fourth would crowd you, prefer to grow a layer: cluster your
   > charges into a few groups, designate an owning lead per group, and delegate —
   > recursively, so every seat keeps a manageable span. Aggregate your charges'
   > reports into one rolled-up summary upward. Run independent workstreams
   > CONCURRENTLY (dispatch all, then collect) — never serialize independent work.
   > Use judgment; this is a default to design toward, not a hard limit.

2. **Apply it at every tier, not just the top.** The same guideline belongs in a
   project-XO's instructions (~3 boats), a boat's instructions (~3 standing
   sub-tasks; fan out transient sub-agents freely), and a meta-XO's instructions
   (~3 project-XOs). It is scale-invariant by design.

3. **Reach for the existing mechanism on the fourth.** A new lead is a
   `flotilla workspace init <lead>` + a roster entry (a boat) or a clock-only
   `watch` + a fleet-command channel binding (a project-XO under a meta-XO). The
   doctrine says *when*; the [federation
   wiring](./quickstart.md#federated-fleets--per-project-channels--fleet-command) is
   the *how*.

That is the whole setup. With it in place, every flotilla deployment grows
precisely the hierarchy its load demands, keeps every seat inside its span of
control, surfaces one aggregated picture upward, and works independent streams in
parallel.

## The constitutional set — how flotilla ships doctrine

The Rule of Three is not a one-off. It is the first member of flotilla's
**constitutional set** — the default operating doctrine a fleet needs to run well,
embedded into the binary so it travels with the product rather than living as
host-local assets. `flotilla workspace init` seeds the set into a fresh agent and
`flotilla doctrine install <agent>` (re)installs it idempotently into an existing one;
the install loop is **member-count-agnostic** and dispatches each member by its delivery
**mechanism**.

The set ships **nine members today** (`doctrine.Members()`): seven `identity-append`
and two `heartbeat-skill`, delivered by **two mechanisms** — the "vocabulary extends
with each new member kind" the set was designed to grow into. (`xo-outbound` is
coordinator-only, so execution desks receive eight.)

| Member | Mechanism | Delivery | Loads |
|---|---|---|---|
| **[operating-principles](./OPERATING-PRINCIPLES.md)** (the twelve standing principles, distilled) | `identity-append` | distilled text appended (under a marker fence) into the agent's standing **identity file** | once at launch, via `--append-system-prompt-file` |
| **Rule of Three** (span of control, a guideline) | `identity-append` | distilled text appended (under a marker fence) into the agent's **identity file** | once at launch |
| **no-self-merge** (a desk never merges its own work; the level above reviews + merges — the merge IS the independent review) | `identity-append` | distilled text appended (under a marker fence) into the agent's **identity file** | once at launch |
| **act-dont-idle-hold** (execute authorized reversible work; never stall on a non-decision) | `identity-append` | distilled text appended (under a marker fence) into the agent's **identity file** | once at launch |
| **executive-mini-brief** (operator turn-finals: bottom line, plain-language streams, detail footer, explicit needs-you line) | `identity-append` | distilled text appended (under a marker fence) into the agent's **identity file** | once at launch |
| **xo-outbound** (coordinator notify discipline — reply to the operator with `flotilla notify`) | `identity-append` | distilled text appended (under a marker fence) into the coordinator's **identity file** | once at launch; **coordinator-only** |
| **operator-direct-tasking** (operator-direct tasking is first-class authorization — execute and report) | `identity-append` | distilled text appended (under a marker fence) into the agent's **identity file** | once at launch |
| **[visibility-synthesis](./visibility.md)** (Tiers 2/3) | `heartbeat-skill` | a **whole-file** skill written into the agent's **workspace** (`skills/visibility-synthesis.md`) | when the daemon emits a synthesis wake |
| **[parade-formation](./parade.md)** (accomplishments parade) | `heartbeat-skill` | a **whole-file** skill written into the agent's **workspace** (`skills/parade-formation.md`) | when the operator runs `flotilla parade` |

The two mechanisms encode a real distinction. `identity-append` is for a **structural
identity rule** — *who the agent IS* (its standing organization, like the Rule of
Three) — so it loads once into the system prompt and is fenced by a marker the installer
keys idempotency on (re-runs detect the marker and skip, so your edits survive).
`heartbeat-skill` is for a **tick-time discipline** — *what the agent DOES on a wake* (a
skill the wake prompt references, like visibility synthesis) — so it is written as a
whole file into the workspace, and its idempotency is **stat-based**: a missing file is
created, an existing one is kept unchanged (operator edits survive a re-install). A
whole-file member carries no marker fence and never routes through the identity-append
path.

Adding a member is adding a registry entry plus its embedded asset; a member that
introduces a **new** mechanism also lands that mechanism's install behavior in the same
change (the mechanism-coupling contract). Which further behaviors join the default set is
the operator's strategic lever — the set is built to grow one member at a time.

## See also

- [xo-doctrine.md](./xo-doctrine.md) — the operator ↔ XO contract and the
  narrow-answer / state-externalization disciplines this rule composes with; the
  "would the operator want to read this?" discrimination is the per-tier test for
  what an aggregating lead forwards upward.
- [visibility.md](./visibility.md) — the stratified-visibility doctrine (Tiers 1/2/3);
  visibility-synthesis is a `heartbeat-skill` member of this set.
- [parade.md](./parade.md) — the accomplishments-parade doctrine (operator-triggered v1);
  parade-formation is the second `heartbeat-skill` member.
- [quickstart.md → Federated fleets](./quickstart.md#federated-fleets--per-project-channels--fleet-command)
  — the recursive meta-XO → project-XO → desk topology this rule governs.
- [README.md](../README.md) — the hub-and-spoke premise ("one coherent picture and
  one accountable router") that aggregation-upward delivers at every tier.
- [inter-harness.md](./inter-harness.md) — a lead's ≤ 3 charges may be mixed
  harnesses; the span limit and parallel-dispatch discipline are surface-agnostic
  (they govern coordination relationships, which every driver exposes the same way).
