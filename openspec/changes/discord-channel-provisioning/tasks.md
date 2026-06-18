# Tasks — Discord channel provisioning

## 1. Provisioner core + seam (`internal/discord/provision.go`)

- [ ] 1.1 Define the `guildAPI` seam interface (`selfMember`, `guild`, `channels`,
      `createChannel`, `deleteChannel`), the flotilla-owned `Channel`, `guildInfo`
      (`ownerID` + `rolePerms map[string]int64`), and `CreateSpec` value types (no discordgo
      types leak past the adapter). `selfMember` returns bot id + roles in ONE call.
- [ ] 1.2 Implement `effectivePermissions(g, botID, botRoles) int64` — MIRROR discordgo's
      `memberPermissions` (`restapi.go:514`, cite it in a comment) using the EXPORTED
      `discordgo.PermissionAdministrator`/`PermissionManageChannels` constants (no open-coded
      bits). TEST FIRST: owner, Administrator-only (short-circuit), @everyone-grant (role
      id==guild id), role-grant, none, unknown-role-id-ignored.
- [ ] 1.3 Implement `hasManageChannels(perms) bool` and the name `key(name, ctype)` — text →
      lowercase + space→'-' + trim '-' (documented transform ONLY, no speculative `_`/`-`
      collapse); category → lowercase + trim-space. TEST FIRST: case/space drift, category
      case-insensitivity, no-over-match (`team_a` ≠ `team-a`).
- [ ] 1.4 Implement `findExisting(chans, name, ctype, parentID) (Channel, bool)`. TEST FIRST:
      match on normalized name, type mismatch, parent mismatch, same-name-different-parent.
- [ ] 1.5 Implement `Provisioner.Preflight(guildID)` over the seam (selfMember + guild →
      effectivePermissions). TEST FIRST with a fake `guildAPI`: pass (owner/admin/grant),
      fail (no Manage Channels) → clear error.
- [ ] 1.6 Implement `Provisioner.Create(guildID, spec) (Channel, created bool, err)` — list →
      findExisting → skip-or-create, read id from the returned object, empty-id → error,
      403 → clear permission error, 429 `*RateLimitError` → rate-limit error, non-RESTError →
      verbatim passthrough. TEST FIRST with the fake (incl. skip-message discloses non-reconcile).
- [ ] 1.7 Implement `Provisioner.List` and `Provisioner.Delete` over the seam (Delete maps a
      404 to a clear "no such channel" error). TEST FIRST.
- [ ] 1.8 Implement `bindingSnippet(channelID, xo, members, role)` → indented `roster.Channel`
      JSON. TEST FIRST: valid JSON, correct fields, empty members/role omitted.
- [ ] 1.9 Implement the `discordgoAPI` adapter (wraps `*discordgo.Session`) and
      `NewProvisioner(botToken)` (`discordgo.New("Bot "+token)`, no `Open()`, default log
      level, keep default `ShouldRetryOnRateLimit`). The adapter UNWRAPS a
      `*discordgo.RESTError` to `(status int, apiMessage string)` and NEVER returns the raw
      RESTError past the seam (its embedded Request carries the Authorization header). Map
      `*discordgo.RateLimitError` to a rate-limit sentinel. (Adapter is the one untested unit.)
- [ ] 1.10 Secret-discipline test: the returned error chain for a simulated REST failure
      contains (a) no token / no `Authorization` substring AND (b) no `*discordgo.RESTError`
      in the chain (the `%+v` struct-dump leak vector).

## 2. Command surface (`cmd/flotilla/channel.go`)

- [ ] 2.1 `parseChannelCreateArgs` / `parseChannelDeleteArgs` (pure). TEST FIRST: name
      positional, `--type` validation (text|category, reject others), flag-before-positional
      guard, delete-requires-`--yes`, delete-rejects-non-snowflake, `--category` id-vs-name
      passthrough, `--member` repeatable.
- [ ] 2.2 `cmdChannel(args)` dispatch (create|list|delete; unknown sub → clear error).
- [ ] 2.3 `cmdChannelCreate`: precedence-ordered checks (token → guild_id), load roster
      (guild_id + agent validation for `--xo`/`--member`), load secrets (bot token), resolve
      `--category` (id-or-name via `List`; ambiguous/unresolvable → error), `NewProvisioner`,
      `Preflight`, `Create`; print `created`/`exists … skipped (topic/parent NOT updated)` +
      binding block when `--xo` set.
- [ ] 2.4 `cmdChannelList`: load roster+secrets, `List`, print table or `--json`.
- [ ] 2.5 `cmdChannelDelete`: require `--yes` + snowflake, load roster+secrets, `Delete(id)`,
      print `deleted <id>` (clear error on 404).

## 3. Wiring + docs

- [ ] 3.1 Add `case "channel": return cmdChannel(args[1:])` to `cmd/flotilla/main.go` `run()`.
- [ ] 3.2 Add the `channel` usage block (create/list/delete + flags) to `usage()`.
- [ ] 3.3 Update `README.md` (and any command reference) with the `flotilla channel` surface
      and the provision→wire-binding flow.
- [ ] 3.4 `gofmt`, `go vet ./...`, `go build ./...`, `go test ./...` all green.

## 4. Gate

- [ ] 4.1 `openspec validate discord-channel-provisioning --strict`.
- [ ] 4.2 Trio on the implementation diff: `/systems-review` + `/open-code-review` + STORM;
      iterate to clean.
- [ ] 4.3 PR; CI green; report trio-clean for operator merge.
