# Design: smart desks — secure push-to-XO for non-Claude desks (the inter-harness follow-on)

## The model in one line

A non-Claude desk becomes a push-capable peer by **reporting to the XO via `flotilla
send` (pure tmux injection, NO secrets)** when it finishes/blocks — never by talking to
Discord directly. The XO (sole secrets holder) relays onward. This removes the pull-only
limitation without exposing the fleet secrets to any desk.

## Why send-to-XO, not notify-to-Discord (the security design)

| Path | Mechanism | Needs | Exposure |
|---|---|---|---|
| `flotilla send --from <desk> <xo> "<report>"` | tmux injection into the XO's pane | flotilla binary + the **secret-free roster** + `--from` | **none new** — a desk in a pane can already `tmux send-keys`; this is its structured form |
| `flotilla notify --from <desk> <report>` | POST to the desk's Discord **webhook** | the **secrets file** (bot token + ALL webhooks) | **HIGH** — bot-token + every webhook → impersonate any agent, use the bot |

Verified against source:
- `flotilla send`'s delivery is the operation that must succeed; the Discord mirror is
  optional and default-off (`cmd/flotilla/main.go:240-249`; `shouldMirror` precedence,
  `send_test.go`). So `send` with no `--secrets` is pure tmux delivery — **no secrets**.
- `flotilla notify` POSTs to the agent's webhook from the secrets file
  (`cmd/flotilla/notify_test.go:48-56`); the secrets file holds `FLOTILLA_BOT_TOKEN` +
  `FLOTILLA_WEBHOOK_<AGENT>` for every agent (`internal/roster/secrets.go:13-17`).

**Therefore: smart desks push via `send` to the XO. Desks NEVER receive the secrets
file.** The XO is the only Discord-facing identity; it decides what (if anything) to
relay to the operator (`flotilla notify`) after receiving a desk's pushed report. This is
the same trust boundary the fleet already has — the daemon/XO hold secrets, desks don't.

## The smart-push convention (what goes in the desk's identity file)

Seeded into the desk's native instruction file (`workspace.IdentityFileName`:
`AGENTS.md` for opencode/grok/cursor, `CONVENTIONS.md` for aider). A recommended snippet:

> **Reporting to the fleet.** When you finish a delegated task, get blocked needing a
> decision, or hit an error you can't resolve, report to the XO by running:
> `flotilla send --from <YOUR_NAME> <XO_NAME> "<one line: status + where your output is>"`
> Report a POINTER, not your whole transcript — a one-line status and where the result
> lives (a file path, a branch, a PR). Do NOT run `flotilla notify` and do NOT touch any
> secrets/webhook — the XO relays to the operator. Report once per completion, not per
> step.

`<YOUR_NAME>`/`<XO_NAME>` are filled from the roster at provisioning time. The "pointer,
not transcript" rule keeps the XO's context cheap (it pulls detail from the pane/files
only when needed — the pull-participant model still underlies push).

## Provisioning (config, not secrets)

The desk needs, in its launch environment (the launch recipe / workspace — the same
place it is started): the `flotilla` binary on `PATH`, the roster
(`$FLOTILLA_ROSTER`/default), and `$FLOTILLA_SELF=<desk-name>` (so a bare `flotilla
send` knows its `--from`). All non-secret. No secrets file is provisioned to the desk.

## Opt-in mechanism (the one design decision for the checkpoint)

Smart-push is **per-desk opt-in** — a desk without the convention stays a pure
pull-participant (zero change). Two ways to seed the convention:

- **(rec) Documented snippet + launch/workspace seeds it.** flotilla provides the
  canonical snippet (a docs section + optionally a tiny `flotilla` helper that prints the
  filled snippet for a given agent, so the operator/launch can append it to the desk's
  identity file consistently). Lowest-code, honest, leverages the existing
  `IdentityFileName` mapping.
- **(alt) A roster flag** (e.g. `"push": true` per agent) the workspace honors by
  including the convention when it scaffolds the desk's identity file. More automation,
  more code + a workspace-scaffold seam.

Recommend (rec) for this increment; (alt) is a possible enhancement. **Decided at the
design-gate checkpoint.**

## The XO side (already mostly handled)

A desk's `flotilla send` to the XO injects a turn into the XO's pane — this is the
existing **peer-reports-to-XO** path (inter-agent traffic, NOT the Discord operator
relay). The XO's doctrine already covers routine inter-agent traffic (a desk's status
report — `docs/xo-doctrine.md`); it stays quiet on routine reports and surfaces only what
needs the operator. Under the change-detector, the desk's injection also **wakes** the XO
with the report (injection = wake), so the XO acts on a push promptly instead of on the
next poll. The XO distinguishes the pushed report by the `--from` identity + content (it
is not an operator message — operator messages arrive via the Discord relay's operator
filter). One doctrine note to add: a smart desk's pushed report is the XO's cue to
collect that desk (pull its detail), reply/route as needed, and relay to the operator
only if it needs them.

## Test plan (TDD)

1. If a provisioning helper is built (per the checkpoint): test it prints the correctly
   filled snippet (`--from`/XO names resolved from the roster) and never emits any secret.
2. A test/assertion that the documented push command is `flotilla send` (tmux, no
   secrets), and that nothing in the smart-push path loads or requires the secrets file.
3. Docs: `docs/inter-harness.md` smart-desk section is self-contained + states the
   security boundary (no secrets to desks; desk→Discord-direct is a Non-Goal).
4. `gofmt`/`go vet`/`go build`/`go test -race ./...`; `openspec validate --strict`.
5. `/systems-review` + `/open-code-review` on the design AND the implementation.

## Non-Goals

- **desk → Discord-direct push** (`flotilla notify` from a desk) — the secrets-exposure
  surface the XO flagged. Desks never get the secrets file; the XO is the sole Discord
  identity.
- **Authenticating `--from`** — the fleet is a trusted-host model; `--from` is a
  self-declared identity today and stays so (a per-desk scoped-credential system is
  over-engineering for one trusted host).
- **Auto-relaying every desk report to Discord** — the XO decides what the operator sees
  (interaction-priority doctrine), unchanged.
