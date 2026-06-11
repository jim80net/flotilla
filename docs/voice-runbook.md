# `flotilla voice` runbook

`flotilla voice` is the operator↔XO **Discord voice** process: it joins a voice channel,
transcribes the operator's speech (Grok STT) and injects it into the XO's tmux pane, and
speaks the XO's short replies back (Grok TTS). It is **push-to-talk / async** — the operator
speaks a request, the XO takes its normal Claude turn, then the reply is spoken; there is no
real-time duplex (Phase 2).

It is an **opt-in, non-safety-critical** process, deliberately **isolated from
`flotilla watch`** (the heartbeat clock): a separate binary, a separate systemd unit, no
ordering/dependency between them. A voice crash can never perturb the clock.

> **Live activation is metered spend on a new audio surface — the operator's call.** Building
> and installing the unit does NOT make voice live; you enable/start it explicitly.

## How it works

```
operator speaks ─▶ OpusRecv ─▶ [operator-SSRC gate] ─▶ silence-endpoint ─▶ Opus→PCM
                                                                              │
                                                                         STT (Grok)
                                                                              │
                              inject "[voice] <transcript>" into the XO pane ◀┘
                                                                              │
                                                            (XO takes its Claude turn)
                                                                              │
operator hears ◀─ OpusSend ◀─ PCM→Opus ◀─ TTS (Grok, 48kHz PCM) ◀─ `flotilla speak "<reply>"`
```

- **Inbound** is gated **fail-closed to the operator**: only audio whose SSRC is positively
  mapped (via Discord "speaking" events) to `VOICE_OPERATOR_USER_ID` is transcribed and
  injected. Unmapped / non-operator / ambiguous audio is **dropped, never injected**.
- **Outbound** is the XO's *dedicated concise reply*: the XO runs `flotilla speak "<short
  text>"` (NOT every `notify`), which drops a file on a spool the voice process speaks. Plain
  `notify` / briefs are never spoken.
- A single **session cost cap** (`VOICE_COST_CAP_USD`) meters STT + TTS; on the cap the
  session goes quiet until restarted.

## Prerequisites

1. **libopus** on the host (the Opus codec links it via CGO):
   ```
   sudo apt install libopus-dev      # Debian/Ubuntu; provides opus.h + pkg-config opus
   ```
2. A **Discord bot** already in your server (the voice process reuses the relay's bot token).
   The bot needs permission to **View Channel** + **Connect** + **Speak** on the target voice
   channel. The process requests the `Guilds` + `GuildVoiceStates` gateway intents.
3. A **roster** (`flotilla.json`) containing the XO agent named in `VOICE_XO_AGENT`.
4. A **secrets** file (`flotilla-secrets.env`, chmod 600) with `FLOTILLA_BOT_TOKEN`.
5. Your **Grok (xAI) API key** (`XAI_API_KEY`).
6. The three Discord IDs (enable **Developer Mode** in Discord, then right-click → Copy ID):
   the **guild** (server), the **voice channel**, and your **operator user** id.

## 1. Build the voice binary (`-tags voiceopus`)

The core `flotilla` binary is built `CGO_ENABLED=0` and has **no** libopus; the voice process
is a separate build that links it:

```
CGO_ENABLED=1 go build -tags voiceopus -o ~/go/bin/flotilla-voice ./cmd/flotilla
```

A core (non-voiceopus) binary still has the `voice` subcommand, but it exits immediately with
`flotilla voice requires a build with -tags voiceopus (CGO + libopus-dev)` — so you can't
accidentally run voice without libopus.

## 2. Configure — two files

**(a) The runtime config** (the secret file the process loads via `--config`):

```
cp deploy/voice.env.example ~/.config/flotilla/voice.env
$EDITOR ~/.config/flotilla/voice.env     # fill XAI_API_KEY + the 3 IDs + XO agent + cap
chmod 600 ~/.config/flotilla/voice.env   # holds XAI_API_KEY — never world-readable/committed
```

Keys: `XAI_API_KEY`, `VOICE_GUILD_ID`, `VOICE_CHANNEL_ID`, `VOICE_OPERATOR_USER_ID`,
`VOICE_XO_AGENT`, `VOICE_COST_CAP_USD` (a positive number).

**(b) The host-path config** (for the installer to generate the systemd unit):

```
cp deploy/flotilla-voice.env.example deploy/flotilla-voice.env
$EDITOR deploy/flotilla-voice.env        # set FLOTILLA_BIN to the -tags voiceopus binary + the paths
```

## 3. Install the service

```
bash deploy/flotilla-voice-install.sh --dry-run    # preview the generated unit + diff
bash deploy/flotilla-voice-install.sh              # generate + daemon-reload (does NOT start it)
```

The installer generates `~/.config/systemd/user/flotilla-voice.service` from the template +
your `deploy/flotilla-voice.env`; never hand-edit the installed unit — edit the env and
re-run. It never auto-starts/auto-restarts voice (a live audio surface is your call).

## 4. Enable + start (opt-in)

```
systemctl --user enable --now flotilla-voice.service   # start now + on login
journalctl --user -u flotilla-voice -f                 # follow logs
```

## 5. Verify / smoke test

1. `journalctl --user -u flotilla-voice -f` should show `flotilla voice: ready (guild=… channel=… XO-pane=… cap=$…)`.
2. In Discord, confirm the bot has **joined** the voice channel.
3. **Speak** a short request in the channel. Within a moment you should see
   `[voice] <your words>` injected into the XO's pane (the XO then answers).
4. Have the XO emit a spoken reply: `flotilla speak "hello, I can hear you"` — you should hear
   it played back in the channel.
5. Watch the log for one-line notices (`transcription failed`, `cost cap reached`, etc.).

## Operator-identity gate (fail-closed)

Discord audio packets carry an SSRC but no user id; the SSRC→user mapping arrives only on a
"speaking start" event. Until a speaker's SSRC is **positively** mapped to
`VOICE_OPERATOR_USER_ID`, its audio is **dropped** — including the first half-word before the
speaking event lands, and anyone who is already talking when the bot joins. If your first few
words are missed, pause and re-speak. This is intentional: a missed half-word is far better
than injecting an unidentified speaker's words as an XO command.

## Cost cap

`VOICE_COST_CAP_USD` is a hard per-session ceiling on STT + TTS spend (reserved *before* each
call, so it never overshoots). On the cap the session goes quiet and logs
`cost cap reached`; restart the process to begin a new session with a fresh budget. Grok
pricing at time of writing: STT $0.10/hr, TTS $4.20 / 1M chars — a real push-to-talk session
is cents.

## Known limitation — reconnect on a live drop (issue #42)

The recovery supervisor reconnects on initial-connect failure and shuts down cleanly on
SIGTERM. However, on **discordgo v0.29.0** a mid-session voice drop is **self-healed inside
discordgo** (it never closes `OpusRecv`), so the supervisor's own drop→reconnect branch does
not fire for ordinary drops — the session is kept alive by discordgo regardless. A genuine
drop-liveness signal (that won't false-positive on normal push-to-talk silence) is tracked in
**issue #42**. No live blast radius: voice survives drops today; this only hardens
supervisor-level recovery.

## Stop / restart

```
systemctl --user restart flotilla-voice.service   # apply a changed unit / fresh cost budget
systemctl --user stop flotilla-voice.service       # leave the channel + stop the audio surface
```
On stop (SIGTERM) the process leaves the voice channel and stops both pipelines cleanly.

## See also
- `docs/watch-runbook.md` — the heartbeat clock + relay (the process voice is isolated from).
- `openspec/changes/discord-voice/` — the design + spec + build plan for this feature.
