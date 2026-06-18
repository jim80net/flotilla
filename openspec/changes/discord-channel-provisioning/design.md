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
establishes the streaming websocket) is only for the relay (verified: every method used —
`Guild`, `GuildMember`, `GuildChannels`, `GuildChannelCreateComplex`, `ChannelDelete` —
is a plain REST GET/POST in `discordgo@v0.29.0/restapi.go`, none touch `State`/`Open()`).
So `flotilla channel` is a one-shot REST command, structurally like `flotilla notify` (a
single Discord HTTP call, then exit), not like `flotilla watch` (a long-lived daemon).

## Goals / Non-goals

**Goals**
- A `flotilla channel create|list|delete` surface that provisions channels via the bot.
- Idempotent create so re-running a provisioning plan never duplicates channels.
- A Manage-Channels preflight with a clear error, plus a 403 backstop.
- Emit the F#105 roster binding for a created channel (one-flow create→wire).
- Bot token never logged; REST errors carry no credentials.
- Policy (idempotency, permissions, error taxonomy, flag parsing, binding emission) fully
  unit-tested behind a seam; only the thin discordgo adapter stays unverified.

**North star (decided — closes the "accidental IaC" trap; STORM/Historian):** this is an
**imperative provisioning helper**. `create` provisions from its **argv**, never by reading
the roster as a desired-state plan. Convergence/reconciliation (re-topic an existing
channel, drift detection, a `flotilla channel sync` that reconciles `channels[]` against
the guild, a `--write-roster` that rewrites committed config) are **explicit non-goals**
for this change — named here so the imperative scope is a conscious choice, not a drift.
The roadmap home for declarative reconciliation, if ever wanted, is a separate `sync`
verb; it is out of scope.

**Other non-goals**
- Editing/rewriting the committable `flotilla.json` in place (see Rejected).
- Channel *editing* (rename/move/topic-change). Re-topic'ing an existing channel is a
  manual edit (or delete+create) for v1 — and `create`'s skip message says so explicitly
  (it does NOT silently "apply" a new `--topic` to an existing channel; idempotent ≠
  convergent, and the skip discloses that).
- Permission *granting* (giving the bot Manage Channels) — a Discord-side admin action;
  flotilla only DETECTS its absence and reports it.
- Batch/atomic multi-channel plans (`--plan`, all-or-nothing). The CoS provisions one
  channel per invocation; because `create` is idempotent, **recovering from a partial
  squadron stand-up is just re-running the sequence** — already-created channels skip and
  the run resumes. No batch primitive is needed for resume-safety.
- Threads, forum channels, voice-channel options (bitrate/user-limit).

## Architecture

### Transport seam (testability)

`discordgo` makes live HTTP calls, so per the isolate-untestable-transport-behind-seam
discipline the provisioner depends on a narrow interface, not the concrete session:

```go
// guildAPI is the slice of Discord REST flotilla provisioning needs. The real
// implementation wraps *discordgo.Session; tests inject a fake. One-shot use.
type guildAPI interface {
    selfMember(guildID string) (botUserID string, roleIDs []string, err error) // GuildMember(guildID, "@me")
    guild(guildID string) (*guildInfo, error)                 // owner id + role→perm bits
    channels(guildID string) ([]Channel, error)               // GET /guilds/{id}/channels
    createChannel(guildID string, in CreateSpec) (Channel, error)
    deleteChannel(channelID string) error
}
```

- `guildInfo` is `{ownerID string; rolePerms map[string]int64}` (role id → permission
  bits) — a flotilla-owned shape, so the seam does not leak discordgo types into the
  policy or the tests.
- `Channel` (the provisioner's value type) is `{ID, Name string; Type int; ParentID string}`
  — deliberately NOT `roster.Channel` (that is the *binding*; this is the *Discord object*).
- **`selfMember` uses two CANONICAL bot-token routes** — `User("@me")`
  (`GET /users/@me`, unambiguous) for the bot's id, then `GuildMember(guildID, <id>)` with
  the REAL snowflake for its roles. (The impl-trio split on whether
  `GET /guilds/{id}/members/@me` is valid for a bot token; rather than gamble on an
  endpoint not confirmable against canonical sources, we use the two routes that
  unambiguously are.)

`type discordgoAPI struct { sess *discordgo.Session }` implements `guildAPI` by calling
`sess.User("@me")` + `sess.GuildMember(guildID,<id>)`, `sess.Guild`, `sess.GuildChannels`,
`sess.GuildChannelCreateComplex`, `sess.ChannelDelete` (guarding nil returns — discordgo's
`Guild`/`User` do not all guard a null body). This adapter is the ONLY part that talks to
discordgo, and the only part not unit-tested (it is exercised live).

`NewProvisioner(botToken string) (*Provisioner, error)` builds the real session
(`discordgo.New("Bot "+token)`) and wraps it. It does **not** call `Open()`. It does not
raise the log level (default `LogError`); the token never reaches a log. It **disables
`ShouldRetryOnRateLimit`** (see Rate limits).

### The `Provisioner` and its pure helpers

`Provisioner{api guildAPI}` exposes:
- `Preflight(guildID) error` — fetch the bot's id+roles (`selfMember`) + guild
  (owner-id + role perm bits), compute effective permissions, return a clear error if
  Manage Channels is absent.
- `Create(guildID string, spec CreateSpec) (ch Channel, created bool, err error)` — list,
  idempotency-match, create-if-absent. `created=false` ⇒ skipped (already existed).
- `List(guildID) ([]Channel, error)`.
- `Delete(channelID string) error`.

Pure, table-tested package functions (no I/O):
- `effectivePermissions(g *guildInfo, botID string, botRoles []string) int64` — the
  documented Discord base-permission algorithm. **This MIRRORS discordgo's own unexported
  `memberPermissions` (`restapi.go:514`) — cite it in the code comment so a future reader
  re-checks it on a discordgo upgrade.** It uses the EXPORTED `discordgo.PermissionAdministrator`
  (`1<<3`) and `discordgo.PermissionManageChannels` (`1<<4`) constants, never open-coded
  bits, so the math can't drift from the library:
  ```
  if botID == g.ownerID                          → return discordgo.PermissionAll (owner)
  perms |= g.rolePerms[guildID]                   // @everyone — its role id EQUALS the guild id
  for r in botRoles: perms |= g.rolePerms[r]      // unknown role ids contribute 0 (skipped)
  if perms & discordgo.PermissionAdministrator != 0 → return discordgo.PermissionAll
  return perms
  ```
  `hasManageChannels(perms) = perms & discordgo.PermissionManageChannels != 0`. Tested:
  owner, Administrator-without-explicit-ManageChannels (the short-circuit), @everyone-grant,
  role-grant, none-grant, unknown-role-id-ignored, and the @everyone-id==guild-id invariant.
- `findExisting(chans []Channel, name string, ctype int, parentID string) (Channel, bool)`
  — idempotency match (see below).
- `bindingSnippet(channelID, xo string, members []string, role string) (string, error)` —
  marshals a `roster.Channel` to indented JSON; empty members/role are `omitempty` (the
  roster shape already marks them so, `roster.go:56-59`).
- argv/flag parsing for each subcommand (pure, tested like `parseRegisterArgs`).

### Idempotency match — conservative, no live-probe assumptions

Discord normalizes some channel names server-side on create. The **documented, low-risk**
transform is: text channels are **lowercased and spaces become hyphens** (`"Fleet
Command"` → `fleet-command`); categories keep their case. The idempotency `key` applies
**only that documented transform** — deliberately NOT a speculative `_`↔`-` collapse:

```
key(s, ctype):
    if ctype == text: lowercase(s); replace ' ' → '-'; trim leading/trailing '-'
    else (category):  lowercase(trim-space(s))    // case-insensitive
```

A channel matches when `key(existing.Name)==key(requested)` AND same `ctype` AND same
`ParentID`. **Why only the documented transform** (systems P2 / OCR #1): over-normalizing
is the *dangerous* direction — if the key collapsed `_`→`-` and Discord does not, a re-run
requesting `team_a` while `team-a` exists would falsely SKIP, so the operator never gets
`team_a`. Under-normalizing is the *safe* direction — at worst an exotic-character name
fails to match and a **visible duplicate** appears, which the operator catches via `list`.
We did NOT live-probe Discord's full normalization (that would mean creating throwaway
channels in the live coordination guild — an outward-facing action reserved for the
operator), so the key is pinned to what is documented and the residual risk is stated, not
hidden. `list` is the authoritative read-back.

**Idempotency is name+type+PARENT-scoped** (impl-trio): a re-run that resolves
`--category` to a *different* parent (e.g. a category name that became ambiguous between
runs) is a distinct key and creates a second channel. Ambiguity is caught at resolve time
(`ResolveParentCategory` errors), so the realistic path is closed; the residual cross-run
drift is the same "visible duplicate, caught by `list`" residual as the name key.

**Idempotency is sequential-single-actor** (systems P2): two concurrent
`flotilla channel create fleet-command`, or a manual UI create between this command's
`list` and its `POST`, can still race (Discord enforces no name uniqueness, and a
just-created channel may not be immediately visible to a subsequent `GET`). For the
single-CoS provisioning use case this is fine; `list` reconciles. The spec says
"idempotent under sequential use," not "concurrency-safe."

### Confirmed create (no stdout-scraping)

`POST /guilds/{id}/channels` **returns the created channel object** (id, normalized name,
type, parent — verified `restapi.go:1043-1051`). That returned object IS the confirmation
— `Create` reads the id directly from the response, never parses a human string (echoing
`internal/surface/confirm.go`'s "a successful exit can still be wrong" discipline: here the
API gives us the authoritative resource, so we trust the object). An empty id in a 2xx
response is treated as an error (`created channel returned no id`).

### Error taxonomy (clear, distinct, secret-free)

Checked in this PRECEDENCE order so the first error a user sees is deterministic (OCR #6):
**(1) bot token → (2) guild_id → (3) Manage-Channels preflight → (4) create**.

| Condition | Error |
|---|---|
| `--secrets` unset / no `FLOTILLA_BOT_TOKEN` | `channel provisioning needs a bot token (set FLOTILLA_BOT_TOKEN in the secrets file)` |
| roster has no `guild_id` | `roster has no guild_id — set it so the bot knows which guild to provision in` |
| bot not in the guild (preflight `selfMember` 404) | `bot is not a member of guild <id> (invite the bot to that guild, and check the roster guild_id)` |
| bot lacks Manage Channels (preflight) | `bot lacks Manage Channels in guild <id> — grant it in Server Settings → Roles → the bot's role → Manage Channels` |
| create returns **403** (backstop) | the same Manage-Channels message + `(denied at create — check the category's/channel's permission overwrites)` |
| create returns **429** (`*discordgo.RateLimitError`) and retry is exhausted/disabled | `discord: rate limited, retry after <d>` |
| **400** (e.g. 50-per-category / 500-per-guild limit) | `discord: HTTP 400: <Discord's message>` (Discord's own "Maximum number of channels reached" surfaces verbatim) |
| any other `*RESTError` | `discord: HTTP <status>: <Discord's api message>` |
| a non-`RESTError`/non-`RateLimitError` (502-after-max-retries, dial/network) | `discord: <err.Error()>` — passed through verbatim (the total catch-all; systems P2) |

**Secret discipline at the seam (systems P1, the load-bearing one):** a `*discordgo.RESTError`
**retains the original `*http.Request`, whose header carries `Authorization: Bot <token>`**.
`RESTError.Error()` happens to print only status+body (verified `restapi.go:81-83`), but a
`%+v` or struct-dump of it anywhere downstream would leak the token. Therefore the
`discordgoAPI` adapter **NEVER returns a raw `*discordgo.RESTError` past the seam** — it
unwraps it to `(statusCode int, apiMessage string)` and the provisioner builds a FRESH
flotilla error from those two strings, discarding the RESTError (and its embedded Request)
entirely. This mirrors `urlFreeCause` in `discord.go:98-104`, which drops the `*url.Error`
wrapper because its `Error()` embeds the secret URL. A test asserts the returned error
chain contains no `*discordgo.RESTError` (so the embedded Authorization can never leak via
`%+v`), in addition to the string-level "no token / no Authorization" assertion.

**Rate limits (systems P1 / STORM P0 / impl-trio):** a squadron stand-up is a burst of
creates — the canonical 429 trigger. discordgo's default `ShouldRetryOnRateLimit: true`
auto-retries a 429, BUT — confirmed by reading `restapi.go:287-295` — that retry re-issues
with the **same `sequence`** (unlike the 5xx path at `:272-275` which increments and is
capped by `MaxRestRetries`), so it is **UNBOUNDED**: a sustained/global 429 would sleep-and-
retry forever, hanging the command with no output. So `NewProvisioner` sets
`ShouldRetryOnRateLimit = false`: a 429 then surfaces immediately as discordgo's
`*RateLimitError` (`restapi.go:297`), which `mapErr` turns into the clear `*rateLimitError`
("rate limited, retry after <d>"). The command fails fast and the operator/CoS re-runs —
**safe because `Create` is idempotent** (already-created channels skip). This reverses the
design-gate "keep the default" note on concrete code evidence the default is unbounded.

### Guild id & agent validation source

The guild id comes from `roster.Config.GuildID` (committed config). When `--xo`/`--member`
are supplied for binding emission, they are validated against `roster.Config` agents
(reusing `cfg.Agent`) so an emitted binding can never name a non-existent agent — the same
fail-closed discipline the relay binding validation uses at load (`roster.go:236`).

### Command surface (`cmd/flotilla/channel.go`)

```
flotilla channel create <name> [--type text|category] [--topic <t>] [--category <name|id>]
                               [--xo <agent>] [--member <agent>]... [--role <label>]
                               [--roster <p>] [--secrets <p>]
flotilla channel list   [--roster <p>] [--secrets <p>] [--json]
flotilla channel delete <channel-id> --yes [--roster <p>] [--secrets <p>]
```

- `create` runs the precedence checks → `Preflight` → `Create`; on success prints
  `created #<name> (<id>)`, or on skip `exists #<name> (<id>) — skipped (topic/parent NOT
  updated)` (the skip discloses non-reconciliation; STORM P0). If `--xo` is given, it then
  prints the binding JSON block.
- `--type` accepts only `text` (default) or `category`; anything else is rejected by the
  pure parser BEFORE any REST call. The seam's `Channel.Type` is a flotilla int mapped to
  `discordgo.ChannelTypeGuildText`/`GuildCategory` only inside the adapter (systems P3).
- `--category` resolves a category by snowflake id (an all-digits value) else by name,
  matched among category-type channels via the SAME `key(name, category)` function (single
  source of truth). An **unresolvable** category is a clear error; an **ambiguous** name
  (two categories share it) is a clear error telling the operator to pass the id — both
  fail-closed before any create (OCR #2).
- `list` prints one line per channel `<id>  <type>  <name>  [parent <id>]`, or `--json`.
- `delete` takes the snowflake-id positional ONLY (no name path → no fat-finger nuke) and
  **requires `--yes`** (the one destructive verb is never a one-keystroke fire). The
  positional is **validated as a snowflake (all digits) before any REST call** (OCR #5); a
  404 from Discord (id well-formed but absent/already-deleted) is reported clearly, never a
  silent exit-0. **Least-privilege note:** `delete` exists for operator-driven teardown;
  driving it from an autonomous CoS against the live coordination guild is discouraged
  (the worst actor for a hallucinated id) — the `--yes` gate + id-only + snowflake
  validation are the guardrails a CLI can offer.
- Flag-after-positional is caught with the same guard `send`/`notify` use
  (`cmd/flotilla/main.go:330`).

## Rejected alternatives

- **Rewrite `flotilla.json` in place (true append of the binding).** Rejected for v1:
  `flotilla.json` is committable, hand-maintained config; a tool rewriting it risks
  key-order churn, a partial/concurrent write clobbering an operator edit, and surprising
  the operator who owns that file. Emitting the paste-ready binding block gives the
  self-service flow without the tool owning a destructive rewrite of committed config.
- **Open a gateway to create channels.** Unnecessary — REST needs only the token; opening
  the websocket would add latency, a privileged-intent dependency, and a daemon shape to a
  one-shot command.
- **Drop the permission preflight and rely only on the 403 backstop (STORM).** Rejected:
  the issue explicitly requires a Manage-Channels preflight, and a clear error BEFORE the
  first create in a sequence is genuinely more useful than a 403 mid-sequence. We address
  STORM's "don't reimplement Discord's permission model" concern by aligning the algorithm
  to discordgo's own `memberPermissions` (cited) and using its exported constants, so the
  copy can't silently rot — rather than by removing the preflight.
- **Compute channel-level permission overwrites in the preflight.** The preflight computes
  *guild-level* Manage Channels (what the operator asks about). Category-overwrite denials
  are caught by the 403 backstop with a message pointing at overwrites — simpler and
  sufficient.
- **Match idempotency by snowflake id, or live-probe Discord's full name normalization.**
  You don't have the id before creating; the only pre-create identity is the name. Probing
  normalization means creating throwaway channels in the live guild (operator-reserved). So
  the key uses only the documented transform and accepts the safe (under-match → visible
  duplicate) residual, with `list` as the read-back.

## Testing strategy

- **Pure functions, table-driven:** `effectivePermissions` (owner / Administrator-only /
  @everyone-grant / role-grant / none / unknown-role-id-ignored / @everyone-id==guild-id);
  `findExisting` (normalized name match, type mismatch, parent mismatch,
  same-name-different-parent, category case-insensitivity); `bindingSnippet` (shape, JSON
  validity, omit-empty members/role); name `key` (case+space for text, case for category).
- **`Provisioner` with a fake `guildAPI`:** preflight pass (owner/admin/grant) & fail
  (no Manage Channels → clear error); create-fresh vs skip-existing (+ skip message wording);
  403→clear permission error; 429 `*RateLimitError`→rate-limit error; 502/network
  non-RESTError→verbatim passthrough; empty-id-in-2xx→error; create reads id from object.
- **Command parsing:** subcommand dispatch, flag-before-positional, `--type` invalid
  rejection, delete-requires-`--yes`, delete-rejects-non-snowflake, `--category`
  id-vs-name resolution (+ ambiguous/unresolvable → error), `--member` repeatable.
- **Secret discipline:** the returned error chain for a simulated REST failure contains
  (a) no bot token / no `Authorization` substring AND (b) no `*discordgo.RESTError` in the
  chain (the struct-dump leak vector).
- The thin `discordgoAPI` adapter is the only untested unit (exercised live by the operator
  on first provision); every behavior around it is covered.
