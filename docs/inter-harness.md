# Inter-harness fleets — running mixed harnesses through one interface

flotilla drives every desk through a per-agent **surface driver** (see
`internal/surface`). Because delivery, assessment, and wake all resolve that driver
*per desk*, a single fleet can mix harnesses: a Claude-Code XO can coordinate Aider,
OpenCode, Grok (and Cursor, once it ships) desks — each driven correctly by its own
driver. This is flotilla's "drop-in agentize ANY harness, then run an inter-harness
fleet" capability.

## What's surface-agnostic (the proven plumbing)

Every step of the loop resolves `surface.Get(agent.Surface)` and acts through that
driver — there is no hard-coded Claude assumption in the path:

- **send** — `cmd/flotilla/main.go` resolves the target agent's driver and calls
  `Submit`.
- **watch inject / wake** — the change-detector's injector resolves the target's
  driver and calls `Submit` (`cmd/flotilla/watch.go`).
- **assess** — the detector assesses each desk via *its* driver's `Assess`
  (`cmd/flotilla/watch.go`), and the materiality gate routes the resulting
  `surface.State` generically.
- **rotate** — `surface.RotateContext` injects each driver's reset (Claude `/clear`,
  Aider `/clear`, Grok `/new`, Cursor `/new-chat`).

This was **proven live** ($0, local ollama): a mixed Aider + OpenCode fleet with an
**OpenCode XO** — `flotilla send` delivered to each desk via its driver, and
`flotilla watch --change-detector` assessed each desk and woke the OpenCode XO via the
OpenCode driver (`detector delivered to "ocx"`). A regression test
(`internal/surface`: `TestMixedHarnessFleetRoutesPerDriver`) locks the per-driver
routing.

## Non-Claude desks are PULL-PARTICIPANTS (read this before you mix)

A **Claude-Code desk** has flotilla's skill set: it can `flotilla notify` / `flotilla
send` to **push** reports to the operator or the XO, and it understands flotilla-command
delegation. A **non-Claude desk** (Aider / OpenCode / Grok / Cursor) does *not* — it
just runs turns in its own harness. So in a mixed fleet:

- **Collect is pull.** The XO collects a non-Claude desk's result by **reading its
  pane / files** (a `tmux capture-pane`), *not* by expecting the desk to push a report.
  The desk's surface driver `Assess` (surfaced in the change-detector's material wake
  reason, e.g. `aid: finished a turn` / `aid: entered awaiting-approval`) tells the XO
  **WHEN** the turn finished or the desk is blocked; the pane content is the **WHAT**.
- **Delegation is one-way.** The XO `Submit`s a turn; the desk reports back only through
  its rendered state + whatever it writes to files. There is no `flotilla notify` push
  from a non-Claude desk.

> **Do not assume a non-Claude desk can push.** Treat it as a pull-participant: drive
> it with `send`, watch its state with the driver `Assess`, and collect by reading its
> pane. The XO's monitoring should lean on the driver-`Assess`'d state (which the
> detector already provides) rather than eyeballing a non-Claude render it may misread.

## Rotation & recovery are per-harness

- **Rotate** a non-Claude desk's context via its driver's reset (`/new`, `/new-chat`),
  NOT Claude's `/clear` — `surface.RotateContext` already does the right thing per
  driver.
- **Recover** a non-Claude dead desk by **relaunching it via its launch recipe** (the
  `flotilla resume` recipe runs an arbitrary command), NOT by a Claude-specific resume.
  (The `~/.claude` `fleet-session-rotation` / `flotilla-fleet-recovery` skills are being
  made harness-aware as a follow-on.)

## Multi-line submission is per-harness

A driver's `Submit` newline method is a per-driver choice: **bracketed paste**
(`deliver.Send` — literal newlines, requires the harness to enable bracketed-paste
mode) or **Ctrl+J keystrokes** (`deliver.SendCtrlJ` — for a harness without bracketed
paste, or whose tmux newline is Ctrl+J). Claude-Code, Aider, and OpenCode use bracketed
paste (confirmed live). Grok and Cursor's newline method is **not yet confirmed** — it
is deferred to their live-capture sessions (#58, #61); do not assume bracketed paste
works for them until then.

## Smart desks (opt-in push-capable peers)

A non-Claude desk can be **provisioned for push** so it proactively reports to the XO
instead of waiting to be polled. The push is **secret-free by design**:

- **A smart desk pushes via `flotilla send --from <desk> <xo> "<pointer>"`** — pure tmux
  injection into the XO's pane, which needs **no secrets**. The XO (the only holder of
  the fleet secrets) relays onward to the operator if warranted.
- **A smart desk NEVER pushes via `flotilla notify`** (which posts to Discord via the
  agent's webhook and so needs the secrets file — the Discord bot token + every agent's
  webhook). Giving a desk that file would let it impersonate any agent. So **desks never
  receive the secrets file**; the XO is the sole Discord-facing identity. A smart desk
  gains *zero* new privilege beyond its existing tmux access + the secret-free roster.

**Provision a desk for push** with `flotilla push-snippet <desk>` — it prints the
convention snippet (filled with the desk's + XO's roster names) to append to the desk's
identity file (`AGENTS.md` / `CONVENTIONS.md`), and names the file. The desk needs the
`flotilla` binary on PATH, the roster, and `$FLOTILLA_SELF=<desk>` in its launch
environment — all non-secret.

> **Security boundary (a provisioning contract, not a binary guarantee):** the desk's
> launch environment MUST NOT include `$FLOTILLA_SECRETS` or a readable secrets path. The
> binary cannot stop a desk that *is* handed the secrets from running `notify`; the
> boundary rests on not provisioning it the file. `--from` is an unauthenticated,
> self-declared identity (the trusted-host model, same as everywhere in flotilla) — but a
> pushed report can never be mistaken for an *operator* message (those arrive only via the
> Discord relay's operator-id filter, which a pane injection never transits), so the worst
> case is desk-spoofs-desk, never desk-spoofs-operator.

A desk **without** the smart-push convention stays a pull-participant exactly as
described above — push is strictly opt-in.
