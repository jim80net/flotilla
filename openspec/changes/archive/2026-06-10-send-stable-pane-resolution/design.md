# Design: stable, title-drift-immune pane resolution (#17)

## Problem

Pane resolution matches an agent name against the tmux **pane title** (exact, or
a single status-glyph prefix). Claude Code retitles its pane to a task summary
each turn, so the title stops matching the name and resolution fails — recurring
every turn, defeating the static `tmux_title` override. The XO worked around it
all session by re-pinning titles before every send.

## Mechanism — a tmux per-pane user-option marker

tmux supports per-pane **user options** (`@name`), settable with
`set-option -p -t <pane> @flotilla_agent <key>` and readable in a `list-panes`
format via `#{@flotilla_agent}` (empty string when unset). Verified on tmux 3.4:
the option **survives a pane retitle** (the bug's trigger) — exactly the stable
anchor resolution needs. It is also surface-agnostic (any TUI's pane carries it),
so it preps the drivable-surfaces lane.

## Resolution precedence (two tiers)

`parsePane` lists panes as `<target>\t<title>\t<marker>` and resolves:

1. **Marker (authoritative).** Pane(s) whose `@flotilla_agent == want`. Exactly
   one → resolved, regardless of title drift. More than one → ambiguity error
   (a mis-tagged fleet; never silently pick one).
2. **Title (fallback).** Only when NO pane carries the marker: the prior
   exact/single-glyph match. Zero → error; one → resolved; more than one →
   ambiguity. This keeps an **untagged fleet working exactly as before**
   (backward-compatible).

An empty marker never matches (an untagged pane is title-only). The marker value
is the agent's resolution **key** = `Agent.Title()` (its `tmux_title` override,
else its `name`) — the exact value every `ResolvePane` caller already passes, so
there is **zero call-site churn**.

## Tagging — `flotilla register`

`flotilla register <agent> [--pane <target>]` loads the roster, looks up the
agent, and `TagPane(pane, agent.Title())`. The pane defaults to `$TMUX_PANE` (the
pane the command runs in), so a desk's launch runs a bare `flotilla register
<name>`. To repair an already-drifted desk, the XO runs `flotilla register <name>
--pane <target>` from its own pane — no desk interruption, no title re-pin.

The agent positional is accepted **before or after** the flags: Go's flag parser
stops at the first positional, so the natural `register <name> --pane <target>`
would otherwise drop the flags. A pure `parseRegisterArgs` helper pulls a leading
positional out, parses the rest as flags, and also accepts a trailing positional
— unit-tested for both orderings.

## What this is NOT (deferred)

- **Auto-tag on first title-resolve.** Tempting (zero operator action), but it is
  a write-on-read side effect, has a name-vs-title key wrinkle, and — crucially —
  cannot help an already-drifted desk (its title-resolve already fails, so there
  is nothing to hang the auto-tag on). The `register` command + the launch recipe
  fully solve the issue; auto-tag is an easy follow-up if the pre-drift window
  ever matters.
- **A roster `pane_id` field.** Pane ids (`%4`) are brittle across server
  restarts/resumees and add config surface; the self-set marker is better.

## Test plan

- `parsePane` precedence matrix (pure): marker resolves a drifted title; marker
  wins over another pane's coincidental title; duplicate marker → ambiguous;
  untagged → title fallback; empty marker never matches; existing exact/glyph/
  substring/ambiguous title cases unchanged.
- `parseRegisterArgs` (pure): agent-before-flags, agent-after-flags, `=flag`
  form, default pane, and the error cases.
- Live end-to-end smoke: `register` → drift the title → `flotilla send` resolves
  by marker and delivers (verified on tmux 3.4).
