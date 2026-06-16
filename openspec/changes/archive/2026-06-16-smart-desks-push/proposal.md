## Why

The pull-only inter-harness increment (archived `2026-06-15-inter-harness-pull-only`)
established that non-Claude desks are **pull-participants**: the XO collects their
results by reading their panes (cued by the driver `Assess`); they cannot push. This
change — pillar B's follow-on (#63) — makes a non-Claude desk a **first-class
push-capable peer**: on finishing a delegated task (or getting blocked), it proactively
**reports to the XO**, instead of waiting for the XO to poll.

## The security crux (the XO's flagged concern, answered by design)

"Inject conventions into AGENTS.md" undersells the real design surface: **how** does a
non-Claude desk push, securely? The naive answer — give it the flotilla CLI + the
secrets file so it can `flotilla notify` the operator's Discord — is a real
**identity/secrets-exposure** hazard: the secrets file holds the Discord **bot token**
+ **every agent's webhook URL** (`internal/roster/secrets.go:13-17`). A desk with it
could impersonate any agent and use the bot. A non-Claude desk must NOT gain that.

**The design avoids the exposure entirely.** A desk pushes via **`flotilla send
--from <desk> <xo> "<report>"`**, which is **pure tmux injection into the XO's pane**
— it needs NO secrets (the delivery is the operation that must succeed; the Discord
mirror is optional and default-off, `cmd/flotilla/main.go:240-249`). The XO — the only
holder of the secrets — relays to Discord if warranted. So a smart desk gains only the
**flotilla binary** + the **committable, secret-free roster** + its own `--from`
identity; it never touches the secrets file, the bot token, or any webhook. And it gains
**no privilege beyond what a desk in a tmux pane already has** (it can already
`tmux send-keys` to other panes; `flotilla send` is the structured, roster-resolved
form of that).

- **Push channel = desk → XO via `flotilla send` (tmux, no secrets).** ✅
- **Push channel = desk → Discord via `flotilla notify` (webhook, needs secrets).** ❌
  This is the exposure the XO flagged; it is an explicit **Non-Goal** — desks never get
  the secrets file. The XO remains the sole Discord-facing identity.

## What Changes (design-gate scope)

- **The smart-push convention** — a documented, recommended snippet for a desk's native
  identity file (`AGENTS.md` for opencode/grok/cursor, `CONVENTIONS.md` for aider,
  resolved by `workspace.IdentityFileName`): WHEN to report (finished a delegated task /
  blocked / errored) and HOW (`flotilla send --from <self> <xo> "<one-line status +
  where the result is>"`). The desk reports a POINTER (status + where its output lives),
  not its whole transcript — the XO still reads the pane/files for detail.
- **Provisioning** — the desk needs the `flotilla` binary on PATH + the roster
  (`$FLOTILLA_ROSTER`/default) + its `--from` identity (`$FLOTILLA_SELF`). This is
  launch-recipe/workspace config (the same place the desk is launched), NOT secrets.
- **Opt-in** — smart-push is per-desk opt-in (a desk without the convention stays a
  pull-participant; nothing changes for it). Surfaced at the design gate: whether opt-in
  is a roster flag the workspace honors when seeding the identity file, or purely the
  operator writing the snippet — recommended below, decided at the checkpoint.
- **Docs** — extend `docs/inter-harness.md` (the smart-desk section) with the secure
  push model + the security boundary; note in the spec.

## Capabilities

### Modified Capabilities
- `surface`: a non-Claude desk MAY be provisioned as a push-capable peer that reports to
  the XO via `flotilla send` (tmux, no secrets) — turning the pull-only model into a
  two-way protocol WITHOUT exposing the fleet secrets to the desk.

## Impact

- **Code**: minimal — the push uses the EXISTING `flotilla send`; the increment is the
  documented convention + (optionally) a small provisioning helper to seed the snippet
  consistently. No new secret-handling, no new network surface in any desk.
- **Security**: a smart desk gains ZERO new privilege beyond its existing tmux access +
  the secret-free roster; the secrets stay with the XO/daemon. The desk→Discord-direct
  path is an explicit Non-Goal.
- **Out of scope (Non-Goals)**: desk→Discord-direct push (`flotilla notify` from a desk
  — the secrets-exposure surface); authenticating `--from` (the fleet is a trusted-host
  model — `--from` is a self-declared identity today, unchanged); a full per-desk
  scoped-credential system (over-engineering for the trusted-host model).
