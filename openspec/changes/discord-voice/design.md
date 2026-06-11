# Design: Discord live voice chat (Phase 1 — Grok Voice v1)

**Status:** design (awaiting XO checkpoint; DESIGN ONLY — no build until operator greenlight) · **Date:** 2026-06-11 · **Ratified params (XO):** Grok Voice STT/TTS v1; separate `flotilla voice` process; pluggable SpeechProvider; CGO+libopus isolated to the voice binary; push-to-talk v1; `XAI_API_KEY` from `~/.hermes/.env` → `state/voice.env`; dedicated concise spoken-reply path (not all-notify-spoken).

## Context

The operator can read/type to the XO over Discord (relay in, `notify` out) but cannot
**talk** to it. Goal: a real-time spoken back-and-forth. Two facts make this tractable
without new heavy infrastructure:

- **flotilla already depends on `discordgo`** (the relay gateway). discordgo natively
  supports voice: `Session.ChannelVoiceJoin(guild, channel, mute, deaf)` returns a
  `*VoiceConnection` exposing `OpusSend chan []byte` and `OpusRecv chan *Packet`
  (48 kHz Opus, per-SSRC). The Discord audio transport is already in our dep tree.
- **Grok ships standalone STT + TTS APIs** (verified from xAI docs): `POST
  https://api.x.ai/v1/stt` (multipart file or WS stream; ~$0.10/hr batch, $0.20/hr
  streaming; 25+ langs), `POST https://api.x.ai/v1/tts` (JSON `{text, voice_id,
  language}` → MP3 or telephony μ-law; $4.20/1M chars; WS streaming), bearer
  `XAI_API_KEY`, sub-second. (A speech-to-speech Voice Agent API also exists — see
  "Why not speech-to-speech" — we use the discrete STT/TTS pair so the XO's CLAUDE
  reasoning stays in the loop.)

## Architecture — Grok = ears + mouth, the XO's Claude = brain

A NEW, separate **`flotilla voice`** process, isolated from `watch` (the
relay-non-fatal lesson: audio must never be able to take down the safety-critical
clock). It owns one Discord voice connection and runs two pipelines:

```
 operator speaks ──▶ OpusRecv ──▶ endpoint (VAD/silence) ──▶ Opus→PCM ──▶ STT ──▶ text
        ▲                                                                          │
        │                                                          inject into the XO pane
   OpusSend ◀── PCM→Opus ◀── TTS ◀── concise spoken reply ◀── XO Claude turn ◀──────┘
        (the XO emits the spoken reply via `flotilla speak`/--voice — see Outbound)
```

1. **Inbound (operator → XO):** capture the operator's utterance from `OpusRecv`,
   detect end-of-utterance (silence-timeout / VAD), decode Opus→PCM, run STT, and
   **inject the transcript into the XO's tmux pane** via the existing
   `internal/deliver` path — exactly how the text relay wakes the XO — tagged
   voice-originated so the XO knows to answer concisely-for-voice.
2. **XO turn:** the XO (Claude) reasons as today — tools, audit trail, the relay — and
   emits a SHORT spoken reply through the dedicated outbound path.
3. **Outbound (XO → operator):** that reply text → TTS → PCM → encode→Opus → `OpusSend`.

The XO's Claude is never replaced; Grok is only STT/TTS.

### Why NOT the Grok speech-to-speech Voice Agent API
It makes **Grok** the reasoning agent (it speaks, calls tools, searches) — bypassing
the XO's Claude turn loop, its tool access, and its audit trail. Wrong layer for "the
XO joins voice." (A future "voice front-end that calls a relay-to-XO tool" is a Phase-2
possibility, not v1.)

## The SpeechProvider abstraction (Grok now, Nemotron later)

Mirror the `internal/surface` driver pattern: a small interface so the speech backend
is swappable without touching the Discord/pipeline code.

```go
type SpeechProvider interface {
    // STT transcribes a complete PCM utterance (16-bit mono, sample rate per Caps).
    STT(ctx context.Context, pcm []byte) (string, error)
    // TTS synthesizes speech for text, returning PCM at Caps().SampleRate.
    TTS(ctx context.Context, text string) ([]byte, error)
    Caps() ProviderCaps // sample rate, max utterance, cost-per-unit (for the meter)
}
```

- **v1 driver = Grok** (`grokProvider`): STT POSTs PCM/wav to `/v1/stt`; TTS POSTs
  `{text, voice_id, language}` to `/v1/tts` and decodes the returned audio to PCM.
  `XAI_API_KEY` from `state/voice.env`.
- **Future driver = local Nemotron** (documented, not built): same interface, no API
  spend, GPU-resident — the box has capacity (GB10 GPU idle, 107 GB RAM free), but v1
  ships the API to avoid local-model integration risk and ship faster.

## The hard technical decision — Opus codec (CGO + libopus, isolated)

Discord voice is **Opus 48 kHz**; Grok STT wants PCM/wav, TTS emits MP3/μ-law. So the
process must decode Opus→PCM (inbound) and encode PCM→Opus (outbound). Mature Go Opus
is **CGO over libopus** (e.g. `layeh.com/gopus` / `hraban/opus`); pure-Go Opus *encode*
is the gap. **Decision (ratified):** CGO + libopus in the **`voice` binary ONLY** —
`watch`/`relay`/core stay pure-Go (`CGO_ENABLED=0`). The voice install documents the
`libopus-dev` host dependency. (TTS MP3→PCM decode is a separate, pure-Go-capable step;
only the Opus edge needs libopus.)

## Outbound — a dedicated CONCISE spoken reply (ratified contract)

NOT "speak every `notify`." A 6881-char executive brief must never be read aloud, and
the XO's text post (full depth) is distinct from what it should *say*. The contract:

- The operator's voice utterance is injected **tagged voice-originated**, instructing
  the XO to additionally produce a SHORT spoken reply.
- The XO emits that spoken reply via a dedicated path — `flotilla speak "<short text>"`
  (or a `--voice` tag on a notify) — which the `voice` process consumes for TTS.
- Plain `notify` (briefs, audit) is NOT spoken. The spoken reply is crafted by the XO
  to be voice-appropriate (a sentence or two), separate from any text it also posts.

This keeps voice terse and on-channel while text stays the medium for depth.

## Push-to-talk v1 — honest latency expectation

The conversation latency floor is the **XO's Claude turn** (seconds→minutes), NOT the
sub-second voice APIs. So v1 is **push-to-talk / async**: the operator speaks a request,
the XO takes its turn, then the reply is spoken. True duplex (barge-in, interruption,
streaming overlap) is impossible while a turn is in flight and is **Phase 2**. The
design sets this expectation explicitly rather than implying fluid real-time chat.

## Security & cost

- **Trust boundary = the text relay's:** only the operator's Discord identity
  (`operator_user_id`) is trusted; mere voice presence is NOT authorization. The injected
  transcript is an operator command exactly as a relayed text message is.
- **Secrets:** `XAI_API_KEY` lives in `state/voice.env` (the operator copies it from
  `~/.hermes/.env`, decoupling voice from Hermes; chmod 600, never committed).
- **Cost (operator's $, metered on the shared xAI account):** STT ¢/hr, TTS $4.20/1M
  chars — a real session is cents. v1 carries a **cost cap** (a max session minutes /
  max TTS chars) and surfaces a running meter; on the cap it stops + alerts.

## Open questions for the checkpoint

1. **Endpointing:** silence-timeout (simple, a fixed quiet gap ends the utterance) vs a
   VAD library? Recommend silence-timeout for v1 (push-to-talk; the operator pauses).
2. **`speak` transport:** a new `flotilla speak` command the XO calls (clean, explicit)
   vs a `--voice` flag on `notify` (fewer surfaces)? Recommend `flotilla speak`
   (distinct from the operator-text `notify`; the `voice` process tails its own channel).
3. **How the `voice` process receives the XO's `speak` output:** a local socket/FIFO,
   a file the process watches, or a Discord webhook the process consumes? Recommend a
   small local mechanism (the voice process + the XO are co-host) — to settle at the
   checkpoint.
4. **Opus lib choice** (`layeh.com/gopus` vs `hraban/opus`) — a build-phase detail,
   noted not decided.

## Non-goals (v1)

- Duplex / barge-in / streaming overlap (Phase 2).
- The speech-to-speech Voice Agent API (wrong layer — keeps Claude as the brain).
- A local Nemotron provider (future SpeechProvider driver; API-only in v1).
- Speaking arbitrary `notify` traffic (only the dedicated concise reply is spoken).
- Multi-operator / multi-channel voice (single operator, single channel).

## Phasing

- **Phase 1 (this design → build on operator greenlight):** `flotilla voice` process;
  Grok STT inbound→XO pane; dedicated `speak`→TTS outbound; SpeechProvider iface +
  Grok driver; CGO+libopus isolated; push-to-talk; operator-identity gate; cost cap.
- **Phase 2 (future):** streaming/low-latency, barge-in, local Nemotron driver,
  optional Grok Voice-Agent front-end.
