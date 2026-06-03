## 0. Prerequisites (verify before starting)

- [x] 0.1 `send` capability merged (delivery + audit + roster) — `flotilla watch` reuses `deliver.Send` and `roster`
- [ ] 0.2 The C2 bot has the **Message Content** privileged intent enabled in the Discord developer portal
- [ ] 0.3 `FLOTILLA_BOT_TOKEN` present in the secrets file (chmod 600) and the bot is a member of the channel

## 1. Gateway reader (discordgo)

- [ ] 1.1 Add `github.com/bwmarrin/discordgo`; `go mod tidy`
- [ ] 1.2 Open one `Session` with `IntentsGuildMessages | IntentsMessageContent`; register a MESSAGE_CREATE handler; graceful `Session.Close()` on SIGTERM (D7)
- [ ] 1.3 Log reconnects; document the disconnect-window message-loss caveat (D7)

## 2. Message filter (the most important tests — D1)

- [x] 2.1 `accept(m)`: early-return drop on `m.WebhookID != ""` (author-agnostic feedback guard); then require `m.Author.ID == operator_user_id` (`internal/relay.Accept`)
- [x] 2.2 Test: a synthetic mirror message (non-empty WebhookID) is DROPPED even if author looked like the operator
- [x] 2.3 Test: a non-operator author is dropped; the operator is accepted

## 3. Routing (D5)

- [x] 3.1 Parse `@<agent> <rest>` multi-line-safe (split on first whitespace run, take remainder verbatim); case-insensitive agent normalize; `@@` escape → literal `@…` to XO; bare message → XO (`internal/relay.Route`)
- [x] 3.2 Unknown `@agent` → `Decision.Notice` "no agent <x>; sent to XO", route to XO (channel post wired in §8)
- [x] 3.3 Tests: multi-line `@agent` keeps all lines; `@Unknown` notice + routes to XO; `@@literal` → XO; case-insensitive match

## 4. Serialized injector (D4)

- [x] 4.1 One worker goroutine draining a job channel; both relay and heartbeat enqueue; calls a `SendFunc` (wired to `deliver`) strictly sequentially (`internal/watch.Injector`)
- [x] 4.2 Test: 20 concurrent enqueues do not interleave (verified with `-race`); worker survives a send error

## 5. Idle-gated heartbeat (D2, D6)

- [x] 5.1 Inactivity timer (`heartbeat_interval`); `Reset()` on every real delivery (`internal/watch.Heartbeat`)
- [x] 5.2 Idle gate: heartbeat takes a `busy(agent)` predicate and skips the tick when true (the spinner-glyph detector is wired in §8)
- [x] 5.3 Tick prompt = `DefaultHeartbeatPrompt` (the autonomous-continuation self-clock, D6); tests cover disabled / idle-fires / busy-skips / reset-suppresses

## 6. Liveness watchdog (tick→ack — D3, D9)

- [x] 6.1 The tick asks the XO for a one-line ack (in `DefaultHeartbeatPrompt`); the ack SOURCE (channel post / state-file touch) is consumed in §8 and passed to `Observe(acked, ...)`
- [x] 6.2 `Watchdog.Observe`: alert after K consecutive missed acks; `crashed` argument is the immediate fast-path (cmd supplies it from `#{pane_current_command}` is-a-shell)
- [x] 6.3 Alert on the down-transition only (debounced); clears on recovery, can re-trip (`internal/watch.Watchdog`)
- [ ] 6.4 `ResolvePane` failures (0 or >1) caught per-tick → watchdog state, never fatal — wired in §8
- [x] 6.5 Tests: K-missed alert; debounce while down; recovery clears + re-trips; crash fast-path

## 7. Config (D8)

- [x] 7.1 Add `roster.Config` fields `xo_agent`, `heartbeat_interval`, `heartbeat_message`
- [x] 7.2 Validate at load: `heartbeat_interval` parses (Go duration); `xo_agent` exists in `agents`; tests for both rejection paths

## 8. Command + deploy

- [ ] 8.1 `cmd/flotilla watch` wiring (flags: `--roster`, `--secrets`); compose reader + filter + router + injector + heartbeat + watchdog
- [ ] 8.2 `deploy/flotilla-watch.service` (systemd user unit): `EnvironmentFile` for secrets, `Restart=on-failure`, `RestartPreventExitStatus` for auth failure, `ExecStart=flotilla watch --roster … --secrets …`
- [ ] 8.3 Runbook: install/enable/verify; require operator Discord 2FA (security boundary)

## 9. Review + ship

- [ ] 9.1 `gofmt`/`go vet`/`go build`/`go test` green
- [ ] 9.2 e2e validation: live gateway → relay injects into a throwaway pane; idle-gate skips a busy XO; watchdog alerts on missed acks
- [ ] 9.3 systems-review the diff; cubic; address all findings
- [ ] 9.4 PR → merge → deploy as `flotilla-watch.service`; archive this change
