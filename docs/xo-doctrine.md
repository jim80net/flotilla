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
  "Deploy is green and live. Positions reconciled, nothing pending — your call on the next batch."
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

## See also

- [quickstart.md §4 → "Reach the operator directly"](./quickstart.md#reach-the-operator-directly-flotilla-notify)
  — the `flotilla notify` command surface (webhook identity, `--file`/stdin, the
  2000-character limit).
- [watch-runbook.md](./watch-runbook.md) — the clock + relay daemon that
  delivers operator messages to the XO and the heartbeat ticks the XO must
  *not* mirror.
- [README.md](../README.md) — the hub-and-spoke topology and the durable-audit
  premise this doctrine serves.
