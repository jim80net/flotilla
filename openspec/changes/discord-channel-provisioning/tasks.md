# Tasks — Discord channel provisioning

## 1. Provisioner core + seam (`internal/discord/provision.go`)

- [ ] 1.1 Define the `guildAPI` seam interface, the flotilla-owned `Channel`, `guildInfo`,
      and `CreateSpec` value types (no discordgo types leak past the adapter).
- [ ] 1.2 Implement `effectivePermissions(g, botID, botRoles) int64` (owner → all; @everyone
      + bot roles; Administrator → all). TEST FIRST: owner, admin, @everyone-grant,
      role-grant, none, unknown-role-id-ignored.
- [ ] 1.3 Implement `hasManageChannels(perms) bool` and the name normalization `key(name, ctype)`
      (text → lowercase + collapse [space/_/-] → '-' + trim; category → lowercase + trim-space).
      TEST FIRST: case/space drift, category case-insensitivity.
- [ ] 1.4 Implement `findExisting(chans, name, ctype, parentID) (Channel, bool)`. TEST FIRST:
      match on normalized name, type mismatch, parent mismatch, same-name-different-parent.
- [ ] 1.5 Implement `Provisioner.Preflight(guildID)` over the seam. TEST FIRST with a fake
      `guildAPI`: pass (owner/admin/grant), fail (no Manage Channels) → clear error.
- [ ] 1.6 Implement `Provisioner.Create(guildID, spec) (Channel, created bool, err)` — list →
      findExisting → skip-or-create, read id from the returned object, empty-id → error,
      403 → clear permission error (backstop). TEST FIRST with the fake.
- [ ] 1.7 Implement `Provisioner.List` and `Provisioner.Delete` over the seam. TEST FIRST.
- [ ] 1.8 Implement `bindingSnippet(channelID, xo, members, role)` → indented `roster.Channel`
      JSON. TEST FIRST: valid JSON, correct fields, empty members/role omitted.
- [ ] 1.9 Implement the `discordgoAPI` adapter (wraps `*discordgo.Session`) and
      `NewProvisioner(botToken)` (`discordgo.New("Bot "+token)`, no `Open()`, default log
      level). Map a `*discordgo.RESTError` to a status code for 403 detection. (Adapter is
      the one untested unit — exercised live.)
- [ ] 1.10 Secret-discipline test: a simulated REST error rendered through the provisioner's
      error path contains neither the token nor an Authorization header.

## 2. Command surface (`cmd/flotilla/channel.go`)

- [ ] 2.1 `parseChannelCreateArgs` / `parseChannelDeleteArgs` (pure). TEST FIRST: name
      positional, `--type` validation (text|category), flag-before-positional guard,
      delete-requires-explicit-id, `--category` id-vs-name passthrough, `--member` repeatable.
- [ ] 2.2 `cmdChannel(args)` dispatch (create|list|delete; unknown sub → clear error).
- [ ] 2.3 `cmdChannelCreate`: load roster (guild_id + agent validation for `--xo`/`--member`),
      load secrets (bot token), resolve `--category` (id-or-name via `List`), `NewProvisioner`,
      `Preflight`, `Create`; print `created`/`exists … skipped` + binding block when `--xo` set.
- [ ] 2.4 `cmdChannelList`: load roster+secrets, `List`, print table or `--json`.
- [ ] 2.5 `cmdChannelDelete`: load roster+secrets, `Delete(id)`, print `deleted <id>`.

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
