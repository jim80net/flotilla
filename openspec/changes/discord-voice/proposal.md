## Why

The operator's only real-time channel to the XO is **text** — Discord relay inbound,
`flotilla notify` outbound. The operator wants to **talk** to the XO: join a Discord
voice channel and have a spoken back-and-forth, hands-free. The pieces now exist:
flotilla already depends on `discordgo` (which carries Discord voice — `ChannelVoiceJoin`,
Opus send/recv), and xAI ships standalone **Grok Speech-to-Text + Text-to-Speech APIs**
(verified: `POST /v1/stt`, `POST /v1/tts`, bearer `XAI_API_KEY`, sub-second). So we can
give the XO ears and a mouth while its **Claude turn loop stays the brain** — Grok does
only STT/TTS, the XO reasons as it does today (tools, audit trail, the relay).

This is **Phase 1: DESIGN ONLY** (proposal + design + spec + tasks → XO checkpoint →
systems-review/OCR). No build until the operator greenlights the build phase.

## What Changes

- A NEW, separate **`flotilla voice`** process (NOT folded into `watch` — the same
  isolation lesson as the relay-non-fatal fix: never couple audio to the
  safety-critical clock) that:
  1. joins the operator's Discord voice channel, captures the operator's utterance
     (endpoint on silence), runs it through **STT**, and **injects the text into the
     XO's pane** via the existing deliver/relay path (tagged voice-originated);
  2. when the XO emits a **concise spoken reply** through a dedicated outbound path
     (`flotilla speak` / a `--voice` tag — NOT all-`notify`-spoken: a long brief must
     never be read aloud), runs it through **TTS** and plays it back into the channel.
- A pluggable **`SpeechProvider`** interface (STT + TTS) — Grok Voice is the v1 driver;
  a local Nemotron stays a documented future driver (no GPU load in v1).
- **Push-to-talk v1**: the conversation latency floor is the XO's Claude *turn*
  (seconds→minutes), so v1 is request→turn→spoken-reply, not duplex/barge-in (Phase 2).
- Opus codec handling (Discord is Opus 48 kHz; Grok wants PCM/mp3) is **CGO + libopus
  isolated to the `voice` binary only** — `watch`/`relay`/core stay pure-Go.

## Capabilities

### Added Capabilities
- `voice`: an operator↔XO Discord **voice** path — STT inbound to the XO pane, a
  dedicated concise spoken-reply outbound via TTS, behind a pluggable SpeechProvider,
  in an isolated process, gated by the operator's Discord identity.

## Impact

- **Code (build phase, gated):** new `cmd/flotilla voice` + `internal/voice` (the
  process, the SpeechProvider interface + a Grok driver, the Discord voice I/O, the
  endpointer); a `flotilla speak`/`--voice` outbound path; CGO+libopus in the voice
  build only. Reuses `internal/deliver` (inject) + `internal/discord` (gateway).
- **Config/secrets:** `XAI_API_KEY` (operator's existing xAI key from `~/.hermes/.env`
  → `state/voice.env`); the voice channel id in the roster. Metered spend (operator's $):
  STT ¢/hr, TTS $4.20/1M chars — a session is cents; v1 carries a cost cap + meter.
- **Security:** same trust boundary as the text relay — only the operator's Discord
  identity is trusted (voice presence ≠ authorization).
- **This change is DESIGN ONLY.** Nothing is built until the operator greenlights.
