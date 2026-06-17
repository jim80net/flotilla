# XO doctrine

How the hub agent (the **XO**) talks to the operator and the fleet. This is
*operating doctrine* for the agent in the XO seat — not flotilla software
behavior. flotilla moves the messages; this page says what the XO should *do*
with them so the operator can run the fleet from Discord, phone in hand, without
scrolling a terminal session.

> **Who this is for / how to use it.** flotilla does not own the XO's prompt —
> the XO is an ordinary agent session (e.g. Claude Code) that *you* run. So this
> doctrine becomes the default the same way every other agent behavior does: you
> wire it into the XO's standing instructions (its system prompt / `CLAUDE.md` /
> skill set). The [Wiring it in](#wiring-it-in) section at the bottom is the
> two-minute setup. Everything above it is the *why* and the *exact contract*.

## The operator ↔ XO conversation belongs in Discord

flotilla's whole point is that **every coordination message is mirrored to a
Discord channel you can read back from anywhere** (see the
[README](../README.md)). For agent-to-agent traffic that already happens
automatically — but there is one conversation flotilla cannot mirror for you,
and it is the most important one: **your conversation with the XO.**

Trace what flotilla mirrors today when you message the XO from Discord:

1. You post a bare message in the coordination channel.
2. `flotilla watch`'s relay delivers it verbatim into the XO's tmux pane (that
   delivery *is* the XO's wake) and posts an audit copy `→ <xo>: <your message>`
   back to the channel — so you see your own message land.
3. The XO takes its turn and **answers inside its pane.**

Step 3 is the gap. The XO's reply exists only in its Claude Code session; it is
**never posted to Discord.** From the channel you see your question echoed and
then silence — to read the answer you have to go back to the terminal. That
defeats the read-from-anywhere promise for the one conversation you most need to
follow.

flotilla deliberately does **not** auto-capture the XO's reply (that would mean
scraping the XO pane — fragile, and easy to mirror noise). Instead it gives the
XO an explicit, clean outbound path and asks the XO to use it. That path is
`flotilla notify`.

## The rule: reply to the operator with `flotilla notify`

> **When the XO receives a genuine operator message, it replies to the operator
> in Discord via `flotilla notify --from <xo>` — in addition to whatever it does
> in its pane.**

`flotilla notify` is the operator-facing outbound path: it posts a message
straight to the channel under the XO's own webhook identity, with **no tmux
injection** (distinct from `send`, which wakes another agent's pane). See
[quickstart §4 → "Reach the operator directly"](./quickstart.md#reach-the-operator-directly-flotilla-notify)
for the command surface.

So the XO's turn, on a genuine operator message, has two halves:

- **Do the work / think it through in the pane** — as it always has.
- **Post the operator-facing reply to Discord** with `flotilla notify` — so the
  operator sees the answer where they asked the question.

A minimal reply (with `FLOTILLA_SELF=<xo>` and `FLOTILLA_SECRETS=<path>`
exported in the XO's environment, the reply collapses to `flotilla notify
"<reply>"`):

```sh
flotilla notify --from <xo> --secrets ./flotilla-secrets.env \
  "Deploy is green and live. All checks pass, nothing pending — your call on the next step."
```

For a long or multi-line reply, use a file or stdin (no shell quoting), exactly
like `send`:

```sh
flotilla notify --from <xo> --secrets ./flotilla-secrets.env --file ./reply.md
echo "done — full report above" | flotilla notify --from <xo> --secrets ./flotilla-secrets.env --file -
```

The reply must be ≤ 2000 characters (Discord's hard limit); a longer body is
rejected cleanly and **nothing is posted** — split it or summarize and link.
Unlike the best-effort audit mirror, a `notify` failure is a command failure
(non-zero exit), because the post *is* the point — so the XO can tell whether
the operator actually received the reply.

### What counts as a "genuine operator message"

A real message **from the operator, routed to the XO** — i.e. a bare message in
the coordination channel (the relay routes bare messages to the XO; `@<agent>`
messages go to that desk). These are the turns that deserve an operator-facing
reply.

The following are **not** genuine operator messages, and the XO must **not**
`notify` for them:

- **Heartbeat ticks.** The self-continuing clock injects a tick that begins
  *"This is an automated heartbeat, not a new instruction."* It asks only for a
  one-line liveness ack (a `touch` of the ack file) and autonomous continuation
  of already-authorized work. Liveness is covered by the ack file plus the
  missed-ack down-alert — a per-tick Discord post is pure noise (this is exactly
  the noise removed in PR #13). **Ack the file; do not notify.** Notify on a
  heartbeat tick *only* if the autonomous work surfaced something the operator
  genuinely needs to see (a decision, a blocker, a completion they're waiting
  on) — i.e. notify for the *content*, never for the *tick*.
- **Routine inter-agent traffic.** A desk's status report the XO is merely
  collecting, a relayed handoff, an inter-agent ack — the everyday plumbing of
  hub-and-spoke coordination. The operator does not need a Discord ping every
  time the XO processes one. Surface to the operator only what rises to
  operator attention (a desk that is blocked, a sign-off that needs the
  operator's call, a finished deliverable).

The discrimination test: **would the operator want to read this in their
channel?** A direct reply to their message → yes, always. A decision/blocker/
completion they are waiting on → yes. A heartbeat ack or routine plumbing →
no. When the answer is "no," staying quiet *is* the doctrine — flotilla's value
is a readable channel, and a readable channel is one without noise.

## Wiring it in

1. **Add the rule to the XO's standing instructions.** Put a line in the XO's
   system prompt / `CLAUDE.md` / skills to the effect of:

   > When you receive a genuine operator message (a relayed message from the
   > coordination channel), post your operator-facing reply to Discord with
   > `flotilla notify --from <xo>`. Do **not** notify for heartbeat acks or
   > routine inter-agent traffic — only for direct replies, decisions, blockers,
   > and completions the operator is waiting on.

2. **Give the XO the environment so the command is one line.** Export in the XO
   session so `--from`/`--secrets` can be omitted:

   ```sh
   export FLOTILLA_SELF=<xo>
   export FLOTILLA_SECRETS=/path/to/flotilla-secrets.env
   ```

3. **Permit `flotilla notify` in the XO's allow-list.** The XO runs with a
   bounded permission posture (see the
   [watch-runbook → XO permission posture](./watch-runbook.md#prerequisites));
   add `flotilla notify` to the allowed operations so the reply goes out
   unattended, the same way the ack `touch` does.

That is the whole setup. With it in place, every flotilla deployment's XO
follows the operator ↔ XO conversation into Discord by default.

## Why `notify`, not auto-capture

Two designs reach the same end (the operator follows the conversation in
Discord):

- **A — the notify convention (this doctrine).** The XO explicitly posts its
  operator-facing replies. The operator's inbound message stays verbatim, the
  XO stays in control of exactly what is operator-facing (signal, not its raw
  scratch-work), and there is no pane-scraping to break. Costs one `notify`
  call per reply.
- **B — auto-capture.** `watch` mirrors the XO pane's post-relay output
  automatically. Zero XO effort, but it scrapes the pane (fragile to TUI
  rendering), and it mirrors *everything* the XO emits — including thinking and
  noise — unless filtered.

flotilla ships **A as the default.** B is deferred; if it lands later it will be
opt-in and additive, not a replacement for the XO knowing how to address its
operator directly.

## The change-detector (heartbeat v2) and the discipline it demands

When a deployment enables the **change-detector** (`change_detector: true` in the
roster — opt-in), `flotilla watch` stops waking the XO every interval with a
generic prompt. Instead it watches the fleet deterministically and wakes the XO
**only on a material change** (a desk finished a turn or crashed, or the state
tracker changed), and **rotates the XO's context after each settled handling**.
An idle fleet costs nothing; every handling runs in fresh context. This keeps the
self-continuing clock cheap and inference sharp — but, exactly as with any
fresh-context model, it is a **contract the XO must hold**, plus two markers it
maintains.

### Mixed-harness desks — lean on the driver `Assess`, and pull (don't expect push)

When the fleet mixes harnesses (Aider / OpenCode / Grok / Cursor desks alongside
Claude ones — see [inter-harness.md](./inter-harness.md)), the XO's desk-monitoring
duty changes in two ways:

- **Trust the driver-assessed state; don't eyeball a non-Claude render.** The
  change-detector already assesses each desk through *its own* surface driver and names
  the state in the wake reason (`aid: finished a turn`, `aid: entered awaiting-approval`).
  Lean on that — do not re-classify a non-Claude pane by eye (its working / idle /
  approval render differs from Claude's, and the XO may misread it).
- **Non-Claude desks are pull-participants by default.** An unprovisioned non-Claude
  desk has no flotilla skill set, so it does not push — **collect by reading its pane /
  files** (cued by the driver `Assess` for *when*); delegation is one-way (the XO
  submits; the desk reports via its state + what it writes). Rotate a non-Claude desk
  with its driver's reset (`/new`, `/new-chat`), and recover a dead one by relaunching
  its launch recipe — not Claude's `/clear` / resume.
- **A *provisioned smart desk* IS a push peer** (opt-in, via `flotilla push-snippet` —
  see [inter-harness.md](./inter-harness.md)). It reports to the XO with `flotilla send`
  (pane injection, **no secrets** — never `flotilla notify`, which the XO owns). When a
  smart desk pushes a report, that is the XO's cue to **collect that desk** (pull its
  detail), route as needed, and relay to the operator only if it needs them. A pushed
  desk report can never be an *operator* message — operator messages arrive only via the
  Discord relay's operator-id filter, which a desk's pane injection never transits.

### The state-externalization contract (non-negotiable when this is on)

Because the XO's context is rotated between handlings, **anything you will need
after the next rotation must be written to durable state — never held only in
context.** Keep `.flotilla-state.md` (the top-level goal + task tracker) current:
the goal, the open tasks, what you just did, what's blocked and why, and the
operator-decision queue. A rotated XO re-reads it and continues. If you hold
progress only in your head, the next rotation loses it. (`watch` never rotates you
mid-turn — only after you settle to idle — but it *will* wipe a quiescent task's
working memory, which is fine *if and only if* you externalized the progress.)

### Self-continuation — the narrow-answer discipline

When you finish a turn, the detector wakes you once with a **continuation
prompt**: advance the next clear, **already-authorized** step if one remains
(read your durable sources in order — the tracker, the active openspec change's
unchecked tasks, the roadmap) — and if nothing authorized remains, **reply idle,
do NOT manufacture work, and signal idle** (see the settle marker below). A task
blocked only from landing (a push gate, a pending review) is **NOT idle** —
advance it locally, then surface the blocker in one line. The detector rotates
your context between steps, so always reason from durable state, not the prior
turn's conversation.

A code-level cap (`--max-self-continuations`, default 3) bounds a runaway: after
that many consecutive continuations with no external change and no idle signal,
the detector forces you settled regardless. The cap is the backstop; the
narrow-answer discipline is what keeps you off it.

### The fleet backlog — the goal-driven loop (opt-in, `--backlog-file`)

When a backlog file is configured, the detector is **goal-driven**: it
**mechanically refuses to settle** (overriding BOTH your idle reply AND the
self-continuation cap) while the backlog has **unblocked** items. Each
continuation it instead wakes you to **advance the top unblocked item**. There is
no "if quiet, hold" — while unblocked work exists you are driven; you settle ONLY
when every item is done or operator-blocked (or while you are awaiting an operator
answer). This is the durable anti-passivity mechanism — it does not depend on you
choosing to keep going.

**The item-line CONTRACT.** Each backlog item is a list line in the `## Backlog`
section carrying a leading bracketed status marker:

```
## Backlog (prioritized — advance the top UNBLOCKED item each wake)
- [in-flight] <task> (<owner desk>)      # being driven        → UNBLOCKED
- [next] <task> (<owner desk>)           # not started          → UNBLOCKED
- [blocked] <task> — awaiting <operator decision>   # human-gated → operator-blocked
- [needs-attention] <task>               # deprioritized stuck   → operator-blocked
- [done] <task>                          # complete             → drained
```

`in-flight`/`next` are unblocked (they keep the loop driving); `blocked`/
`needs-attention` are operator-blocked (drive PREP on them, but they do not block
settle); `done` is drained. **Maintain this format** — a line with no recognized
`[status]` marker is FLAGGED (a loud alert) and treated as unblocked (the loop
keeps driving, never silently misclassifies), so a format slip is loud, not
silent. **Drain** by marking items `[done]` (or `[blocked]`) as they complete, and
by **dispatching** unblocked items across desks/harnesses (not doing everything
yourself).

**Per-item stuck handling.** If you are driven onto the same item repeatedly
without it progressing, the detector escalates a loud alert naming that item and
**deprioritizes** it (drives the rest of the backlog instead). When you get that
alert: advance the item, or durably mark it `[blocked]`/`[needs-attention]` so the
loop stops returning to it.

**Write the backlog ATOMICALLY** (write a temp file, then rename over the target),
the same discipline as the snapshot — the detector reads the file every tick, and
a torn mid-write read must never corrupt the gate. (A torn read self-heals on the
next tick — at worst it momentarily mis-reads the queue — but atomic writes avoid
it entirely.)

### The settle (idle) marker — exact lifecycle

The fast way to tell the detector you are idle is the **settle marker**
(`watch --settled-file`, default `<roster-dir>/flotilla-xo-settled`):

- **When a continuation wake finds nothing authorized to advance**, reply idle
  and `touch <settled-file>`. The detector consumes the marker, records you
  settled, and **stops self-continuation waking until an external material
  change** (a desk transition, a tracker change) **or an operator message**.
- You do **not** remove it yourself — the detector consumes it. An external
  change or an operator message also clears the settled state automatically, so
  you re-engage on real work without any bookkeeping.

If you forget to touch it, the cap still settles you within a few cheap cycles —
the marker just settles you on the *first* idle turn instead.

### The awaiting-operator marker — exact lifecycle

The context rotate is suppressed while you are **awaiting an operator reply**, so
an outstanding question is never wiped out from under you. This is the
awaiting-veto marker (`watch --awaiting-file`, default
`<roster-dir>/flotilla-xo-awaiting`) that **you maintain as one discipline with
your operator-decision queue** — there is no separate bookkeeping:

- **SET the marker at the exact moment you pose a question to the operator** —
  the same act as adding the item to the operator-decision queue in
  `.flotilla-state.md`. Posing the question and creating the marker are one step:
  `touch <awaiting-file>`. If multiple questions are open, the marker simply
  stays present.
- **REMOVE the marker the moment the question is resolved** — when the operator's
  answer arrives, OR when you have durably recorded the answer/decision into
  `.flotilla-state.md` (whichever comes first). When the last open question is
  resolved, `rm -f <awaiting-file>`.

While the marker exists, `watch` handles changes **without** rotating your
context (the outstanding-question thread is preserved). A forgotten (stale)
marker only degrades to "no rotation" — never to a wrongful rotate — so erring
toward leaving it set is safe; the cost is just lost token savings until you
clear it.

### Wiring (opt-in)

```jsonc
// roster (committable):
{ "xo_agent": "xo", "heartbeat_interval": "20m",
  "change_detector": true, "liveness_ping_mode": "none" }
```

Permit, in the XO's allow-list (alongside the ack `touch`): `tmux send-keys` (the
`/clear` rotate path) and the marker writes (`touch`/`rm` of the settle and
awaiting files). Add the lines to the XO's standing instructions:

> When you finish a turn under the change-detector, advance the next
> already-authorized step from durable state, or reply idle and
> `touch <settled-file>` — never manufacture work. The moment you ask the
> operator a question, `touch <awaiting-file>`; remove it when the question is
> resolved.

## See also

- [quickstart.md §4 → "Reach the operator directly"](./quickstart.md#reach-the-operator-directly-flotilla-notify)
  — the `flotilla notify` command surface (webhook identity, `--file`/stdin, the
  2000-character limit).
- [watch-runbook.md](./watch-runbook.md) — the clock + relay daemon that
  delivers operator messages to the XO and the heartbeat ticks the XO must
  *not* mirror.
- [README.md](../README.md) — the hub-and-spoke topology and the durable-audit
  premise this doctrine serves.
