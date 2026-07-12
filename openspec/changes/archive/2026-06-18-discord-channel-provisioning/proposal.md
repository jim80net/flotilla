## Why

The `federation` capability lets a fleet ROUTE across many Discord channels — each
channel bound to one XO, the inbound relay routing by origin channel
(`internal/roster/roster.go:48` `Channel`, `cmd/flotilla/watch.go`, F#105). But the
channels themselves still have to be **created by hand in the Discord UI** before any
binding can point at them. To stand up the squadron layout (an Alpha Group squadron +
the flotilla product squadron, with the CoS/meta-XO across both) the operator must
click through Discord to make every `#fleet-…` and `#fleet-command` channel, then
copy each channel's snowflake id back into `flotilla.json`. That manual step is the one
thing standing between "pick the structure" and "live."

Operator directive (2026-06-18): *"I would like for [the CoS] to be able to create the
discord channels — this should be a mechanical ability of flotilla.go."*

This change adds the channel-**provisioning** complement to the F#105 channel-**routing**
mechanism. Together they make federation self-service: **pick the structure → provision
the channels mechanically → wire the bindings → live**, with no manual Discord step for
the operator.

## What Changes

- **Add a `provision` capability + a `flotilla channel` command surface** that creates,
  lists, and deletes Discord channels via the bot token (the same `FLOTILLA_BOT_TOKEN`
  the inbound gateway already uses), calling Discord's REST API
  (`POST /guilds/{guild.id}/channels`, `GET`, `DELETE`) through the `discordgo` session
  **without opening a gateway** — these are one-shot REST calls, not the streaming relay.
  - `flotilla channel create <name>` — create a text channel (or a category with
    `--type category`), with optional `--topic` and `--category <name|id>` parent.
  - `flotilla channel list` — list the guild's channels (id, type, parent) so the
    operator/CoS can read back what exists and harvest snowflake ids.
  - `flotilla channel delete <channel-id> --yes` — tear down a channel **by explicit
    snowflake id only** (never by name; the id is validated as a snowflake before any REST
    call) and **only with the `--yes` confirmation flag** — the one destructive verb is
    never a one-keystroke fire. Intended for operator-driven teardown (least-privilege:
    discouraged for autonomous-CoS use against the live coordination guild).

This is an **imperative provisioning helper**: `create` provisions from its argv, never by
reading the roster as a desired-state plan. Declarative reconciliation (a `sync` verb,
`--write-roster`, drift detection) is an explicit non-goal here — named so the imperative
scope is a conscious choice, not a drift toward an accidental IaC engine.
- **Idempotent create (skip-if-exists).** Discord does NOT enforce channel-name
  uniqueness, so a naive re-run would silently create duplicates. `create` first lists
  the guild and skips (reporting the existing id) when a channel of the same type with a
  matching name already lives under the same parent — re-running the same provisioning
  plan is safe.
- **Manage Channels permission preflight.** Before attempting any create, compute the
  bot's effective guild permissions (owner / Administrator / @everyone + the bot's
  roles) and fail with a **clear, actionable error** if the bot lacks **Manage
  Channels** — mirroring the empty-allowlist / operator-only preflight discipline
  (`internal/relay`, the `_is_allowed_user` analogue). A create-time `403` is translated
  to the same clear error as a backstop (channel-level overwrites or a permission change
  between preflight and create).
- **One-flow binding emission.** When `create` is given `--xo <agent>` (and optional
  `--member`/`--role`), it prints the ready-to-paste F#105 `roster.Channel` binding JSON
  for the freshly-created channel (its real snowflake id filled in), so wiring routing is
  copy-one-block, not hunt-for-the-id. The committable `flotilla.json` is **never
  rewritten in place** by the tool (see design §Rejected — it is hand-maintained config).
- **Secret discipline.** The bot token is read from the secrets file
  (`roster.Secrets.BotToken()`) and is **never logged**; REST errors are reduced to
  status + Discord's API message (no request/header dump), echoing the webhook-URL
  scrubbing already in `internal/discord/discord.go`.

## Impact

- **Affected specs:** NEW capability `provision`. No change to `federation`, `watch`,
  `send`, `notify` behavior — provisioning is additive and lazy (only `flotilla channel`
  touches the bot-token REST path; fleets that never provision are unaffected, exactly
  as the gateway is only opened by `watch`).
- **Affected code:** NEW `internal/discord/provision.go` (the provisioner + a thin
  `discordgo` adapter behind a testable seam) and `cmd/flotilla/channel.go` (the command);
  `cmd/flotilla/main.go` gains the `channel` dispatch + usage. `internal/roster` is reused
  read-only (guild id, agent validation for `--xo`/`--member`, the `Channel` binding shape).
- **No new dependency** — `discordgo` is already required (`go.mod`), used by the gateway.
- **Risk:** LOW. Creation is confirmed by Discord's echoed channel object (the POST
  returns the created resource — self-confirming, no stdout-scraping). `delete` is the
  only destructive verb and is guarded by requiring the explicit snowflake id.
