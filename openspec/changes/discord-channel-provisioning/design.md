# Design — Discord channel provisioning (`flotilla channel`)

## Context

F#105 gave flotilla a routing mechanism: a roster `channels[]` of channel→XO bindings,
and `flotilla watch` opening a gateway over that SET of channels
(`internal/discord/gateway.go`, `internal/roster/roster.go:48`). What it did NOT give is
a way to *make* the channels — those are created by hand in Discord, their snowflake ids
copied into `flotilla.json` manually. This change closes that gap with a `flotilla
channel` command that provisions channels mechanically via the bot token.

The bot token already exists in the system: the inbound gateway authenticates with `Bot
<FLOTILLA_BOT_TOKEN>` (`internal/discord/gateway.go:32`). Channel creation reuses that
same credential against Discord's REST API. Crucially, **REST calls do not need an open
gateway** — a `discordgo.Session` performs REST with just the token; `Open()` (which
establishes the streaming websocket) is only for the relay. So `flotilla channel` is a
one-shot REST command, structurally like `flotilla notify` (a single Discord HTTP call,
then exit), not like `flotilla watch` (a long-lived daemon).

## Goals / Non-goals

**Goals**
- A `flotilla channel create|list|delete` surface that provisions channels via the bot.
- Idempotent create so re-running a provisioning plan never duplicates channels.
- A Manage-Channels preflight with a clear error, plus a 403 backstop.
- Emit the F#105 roster binding for a created channel (one-flow create→wire).
- Bot token never logged; REST errors carry no credentials.
- Policy (idempotency, permissions, error taxonomy, flag parsing, binding emission) fully
  unit-tested behind a seam; only the thin discordgo adapter stays unverified.

**Non-goals**
- Editing/rewriting the committable `flotilla.json` in place (see Rejected).
- Channel *editing* (rename/move/topic-change) — out of scope; create + delete + list is
  the provisioning surface. (Re-topic'ing is a `delete`+`create` or a manual edit for v1.)
- Permission *granting* (giving the bot Manage Channels) — that is a Discord-side admin
  action; flotilla only DETECTS its absence and reports it.
- Threads, forum channels, voice-channel-specific options (bitrate/user-limit).

## Architecture

### Transport seam (testability)

`discordgo` makes live HTTP calls, so per the isolate-untestable-transport-behind-seam
discipline the provisioner depends on a narrow interface, not the concrete session:

```go
// guildAPI is the slice of Discord REST flotilla provisioning needs. The real
// implementation wraps *discordgo.Session; tests inject a fake. Safe for one-shot use.
type guildAPI interface {
    botUserID() (string, error)                          // GET /users/@me
    guild(guildID string) (*guildInfo, error)            // owner id + roles
    member(guildID, userID string) ([]string, error)     // the bot's role ids in the guild
    channels(guildID string) ([]Channel, error)          // GET /guilds/{id}/channels
    createChannel(guildID string, in CreateSpec) (Channel, error)
    deleteChannel(channelID string) error
}
```

`guildInfo` is `{ownerID string; roles map[string]int64}` (role id → permission bits) —
a flotilla-owned shape, so the seam does not leak discordgo types into the policy or the
tests. `Channel` (the provisioner's value type) is `{ID, Name, Type, ParentID string/int}`
— deliberately NOT `roster.Channel` (that is the *binding*; this is the *Discord object*).

`type discordgoAPI struct { sess *discordgo.Session }` implements `guildAPI` by calling
`sess.User("@me")`, `sess.Guild`, `sess.GuildMember`, `sess.GuildChannels`,
`sess.GuildChannelCreateComplex`, `sess.ChannelDelete`. This adapter is the ONLY part
that talks to discordgo, and the only part not unit-tested (it is exercised live).

`NewProvisioner(botToken string) (*Provisioner, error)` builds the real session
(`discordgo.New("Bot "+token)`) and wraps it. It does **not** call `Open()`. It does not
raise the log level (default `LogError`); the token never reaches a log.

### The `Provisioner` and its pure helpers

`Provisioner{api guildAPI}` exposes:
- `Preflight(guildID) error` — fetch bot id + guild + bot member roles, compute effective
  permissions, return a clear error if Manage Channels is absent.
- `Create(guildID string, spec CreateSpec) (ch Channel, created bool, err error)` — list,
  idempotency-match, create-if-absent. `created=false` ⇒ skipped (already existed).
- `List(guildID) ([]Channel, error)`.
- `Delete(channelID string) error`.

Pure, table-tested package functions (no I/O):
- `effectivePermissions(g *guildInfo, botID string, botRoles []string) int64` — the
  documented Discord base-permission algorithm:
  ```
  if botID == g.ownerID            → return all bits (owner)
  perms |= g.roles[guildID]        // @everyone (its role id == the guild id)
  for r in botRoles: perms |= g.roles[r]
  if perms & Administrator != 0    → return all bits
  return perms
  ```
  Manage Channels = `perms & (1<<4) != 0`. Tested: owner, admin, @everyone-grants,
  role-grants, none-grant, unknown-role-id-ignored.
- `findExisting(chans []Channel, name string, ctype int, parentID string) (Channel, bool)`
  — idempotency match (see below).
- `bindingSnippet(channelID, xo string, members []string, role string) (string, error)` —
  marshals a `roster.Channel` to indented JSON for paste-in.
- argv/flag parsing for each subcommand (pure, tested like `parseRegisterArgs`).

### Idempotency match — Discord name normalization

Discord **lowercases text-channel names and replaces spaces with hyphens** on create
(`"Fleet Command"` → `fleet-command`); **categories keep their case and spaces**. So a
re-run that lists `fleet-command` and compares it to the requested `"Fleet Command"`
would miss and create a duplicate. `findExisting` compares a **normalization key** on
both sides:

```
key(s, ctype):
    if ctype == text: lowercase(s); collapse runs of [space _ -] → single '-'; trim '-'
    else (category):  lowercase(trim-space(s))   // case-insensitive, space-preserving
```

A channel matches when `key(existing.Name)==key(requested)` AND same `ctype` AND same
`ParentID`. This is robust to the common drift (case, spaces) without trying to perfectly
mirror Discord's full normalization. **Residual risk:** an exotic character Discord
strips differently could still produce a rare duplicate; this is acceptable because (a)
provisioning names are tame (`fleet-command`, `fleet-alpha`), and (b) `list` is the
authoritative read-back the operator uses to confirm. Documented, not hidden.

### Confirmed create (no stdout-scraping)

`POST /guilds/{id}/channels` **returns the created channel object** (id, normalized name,
type, parent). That returned object IS the confirmation — `Create` reads the id directly
from the response, never parses a human string (echoing `internal/surface/confirm.go`'s
"a successful exit can still be wrong" discipline: here the API gives us the authoritative
resource, so we trust the object, not an inferred success). An empty id in a 2xx response
is treated as an error (`created channel returned no id`).

### Error taxonomy (clear, distinct, secret-free)

| Condition | Error |
|---|---|
| `--secrets` unset / no `FLOTILLA_BOT_TOKEN` | `channel provisioning needs a bot token (set FLOTILLA_BOT_TOKEN in the secrets file)` |
| roster has no `guild_id` | `roster has no guild_id — set it so the bot knows which guild to provision in` |
| bot lacks Manage Channels (preflight) | `bot lacks Manage Channels in guild <id> — grant it in the Discord server settings (Roles → the bot's role → Manage Channels)` |
| create returns 403 (backstop) | same Manage-Channels message + `(denied at create — check category/channel permission overwrites)` |
| any other REST error | `discord: HTTP <status>: <api message>` — status + Discord's own message only, never the request/headers (no token leak) |

The 403 detection unwraps `*discordgo.RESTError` and reads `Response.StatusCode`
(`RESTError.Error()` prints only status + body, never headers, so the token cannot leak
through it).

### Guild id & agent validation source

The guild id comes from `roster.Config.GuildID` (committed config). When `--xo`/`--member`
are supplied for binding emission, they are validated against `roster.Config` agents
(reusing `cfg.Agent`) so an emitted binding can never name a non-existent agent — the same
fail-closed discipline the relay binding validation uses at load.

### Command surface (`cmd/flotilla/channel.go`)

```
flotilla channel create <name> [--type text|category] [--topic <t>] [--category <name|id>]
                               [--xo <agent>] [--member <agent>]... [--role <label>]
                               [--roster <p>] [--secrets <p>]
flotilla channel list   [--roster <p>] [--secrets <p>] [--json]
flotilla channel delete <channel-id> [--roster <p>] [--secrets <p>]
```

- `create` runs `Preflight` then `Create`; on success prints `created #<name> (<id>)` or
  `exists #<name> (<id>) — skipped`; if `--xo` given, prints the binding JSON block.
- `--category` resolves a category by snowflake id (if all-digits) else by name (matched
  among category-type channels via the same normalization key); an unresolvable category
  is a clear error before any create.
- `list` prints one line per channel `<id>  <type>  <name>  [parent <id>]`, or `--json`.
- `delete` takes the snowflake id positional ONLY (no name path → no fat-finger nuke),
  calls `Delete`, prints `deleted <id>`.
- Flag-after-positional is caught with the same guard `send`/`register` use.

## Rejected alternatives

- **Rewrite `flotilla.json` in place (true append of the binding).** Rejected for v1:
  `flotilla.json` is committable, hand-maintained config; a tool rewriting it risks
  key-order churn, a partial/concurrent write clobbering an operator edit, and surprising
  the operator who owns that file. Emitting the paste-ready binding block gives the
  self-service flow without the tool owning a destructive rewrite of committed config. (A
  future `--write-roster` could add it behind an explicit opt-in once the shape settles.)
- **Open a gateway to create channels.** Unnecessary — REST needs only the token; opening
  the websocket would add latency, a privileged-intent dependency, and a daemon shape to a
  one-shot command.
- **Compute channel-level permission overwrites in the preflight.** The preflight computes
  *guild-level* Manage Channels (what the operator asks about). Category-overwrite denials
  are caught by the 403 backstop with a message pointing at overwrites — simpler and
  sufficient, no need to fetch+merge per-category overwrites pre-emptively.
- **Match idempotency by snowflake id.** You don't have the id before creating; the only
  pre-create identity is the name. Hence the normalization-key name match + `list` as the
  authoritative read-back.

## Testing strategy

- **Pure functions, table-driven:** `effectivePermissions` (owner/admin/@everyone/roles/
  none/unknown-role), `findExisting` (case+space drift, type mismatch, parent mismatch,
  category case-insensitivity), `bindingSnippet` (shape + JSON validity), name `key`.
- **`Provisioner` with a fake `guildAPI`:** preflight pass/fail; create-fresh vs
  skip-existing; 403→clear-error translation; empty-id-in-2xx→error; create echoes id.
- **Command parsing:** subcommand dispatch, flag-before-positional, `--type` validation,
  delete-requires-id, `--category` id-vs-name resolution.
- **Secret discipline:** a test asserts a simulated REST error string carries neither the
  bot token nor an Authorization header.
- The thin `discordgoAPI` adapter is the only untested unit (exercised live by the operator
  on first provision); every behavior around it is covered.
