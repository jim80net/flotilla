## 0. Prerequisites (verify before starting)

- [x] 0.1 `send` capability merged (delivery + audit + roster) — `flotilla watch` reuses `deliver.Send` and `roster`
- [ ] 0.2 The C2 bot has the **Message Content** privileged intent enabled in the Discord developer portal
- [ ] 0.3 `FLOTILLA_BOT_TOKEN` present in the secrets file (chmod 600) and the bot is a member of the channel

## 1. Gateway reader (discordgo)

- [ ] 1.1 Add `github.com/bwmarrin/discordgo`; `go mod tidy`
- [ ] 1.2 Open one `Session` with `IntentsGuildMessages | IntentsMessageContent`; register a MESSAGE_CREATE handler; graceful `Session.Close()` on SIGTERM (D7)
- [ ] 1.3 Log reconnects; document the disconnect-window message-loss caveat (D7)

## 2. Message filter (the most important tests — D1)

- [ ] 2.1 `accept(m)`: early-return drop on `m.WebhookID != ""` (author-agnostic feedback guard); then require `m.Author.ID == operator_user_id`
- [ ] 2.2 Test: a synthetic mirror message (non-empty WebhookID, body `→ v12-dev: …`) is DROPPED even if author looked like the operator
- [ ] 2.3 Test: a non-operator author is dropped; the operator is accepted

## 3. Routing (D5)

- [ ] 3.1 Parse `@<agent> <rest>` multi-line-safe (split on first whitespace run, take remainder verbatim); case-insensitive agent normalize; `@@` escape → literal `@…` to XO; bare message → XO
- [ ] 3.2 Unknown `@agent` → post a one-line "no agent <x>; sent to XO" reply, route to XO
- [ ] 3.3 Tests: multi-line `@agent` keeps all lines; `@Unknown` replies + routes to XO; `@@literal` → XO; case-insensitive match

## 4. Serialized injector (D4)

- [ ] 4.1 One worker goroutine draining a job channel; both relay and heartbeat enqueue; calls `deliver.Send` strictly sequentially
- [ ] 4.2 Test: two concurrent enqueues do not interleave (deliveries are serialized)

## 5. Idle-gated heartbeat (D2, D6)

- [ ] 5.1 Inactivity timer (`heartbeat_interval`); reset on every real relayed delivery (D4)
- [ ] 5.2 Idle gate: skip the tick if the XO pane title shows a working/spinner glyph (busy)
- [ ] 5.3 Tick prompt = the idempotent check-then-noop default (D6); test the idle gate skips when busy and fires when idle

## 6. Liveness watchdog (tick→ack — D3, D9)

- [ ] 6.1 The tick instructs the XO to emit a one-line ack (state file touch or terse channel post); watcher records last-ack time
- [ ] 6.2 Alert after K consecutive missed acks within a window; cheap fast-path: `#{pane_current_command}` is a shell → immediate crash alert
- [ ] 6.3 Alert on the down-transition only, with cool-down; clear on recovery (D9)
- [ ] 6.4 `ResolvePane` failures (0 or >1) are caught per-tick → watchdog state, never fatal (D9)
- [ ] 6.5 Tests: K-missed-acks alert; recovery clears; alert debounced (no per-cycle spam)

## 7. Config (D8)

- [ ] 7.1 Add `roster.Config` fields `xo_agent`, `heartbeat_interval`, `heartbeat_message`
- [ ] 7.2 Validate at load: `heartbeat_interval` parses (Go duration); `xo_agent` exists in `agents`; tests for both rejection paths

## 8. Command + deploy

- [ ] 8.1 `cmd/flotilla watch` wiring (flags: `--roster`, `--secrets`); compose reader + filter + router + injector + heartbeat + watchdog
- [ ] 8.2 `deploy/flotilla-watch.service` (systemd user unit): `EnvironmentFile` for secrets, `Restart=on-failure`, `RestartPreventExitStatus` for auth failure, `ExecStart=flotilla watch --roster … --secrets …`
- [ ] 8.3 Runbook: install/enable/verify; require operator Discord 2FA (security boundary)

## 9. Review + ship

- [ ] 9.1 `gofmt`/`go vet`/`go build`/`go test` green
- [ ] 9.2 e2e validation: live gateway → relay injects into a throwaway pane; idle-gate skips a busy XO; watchdog alerts on missed acks
- [ ] 9.3 systems-review the diff; cubic; address all findings
- [ ] 9.4 PR → merge → deploy as `flotilla-watch.service`; archive this change
