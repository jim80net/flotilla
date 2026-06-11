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
clock). It opens its OWN discordgo session carrying voice intents
(`IntentsGuildVoiceStates` — the existing relay gateway sets only Guild-Messages, and
`ChannelVoiceJoin`'s connect handshake needs voice-state events; P2-1), and owns one
Discord voice connection running two pipelines:

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
`watch`/`relay`/core stay pure-Go. This is **enforced explicitly** (P3-1): the non-voice
build + CI set `CGO_ENABLED=0` (not merely pure-by-absence-of-cgo), so "the clock binary
needs no libopus" is a tested guarantee; only `flotilla voice` builds with cgo. The voice
install documents the `libopus-dev` host dependency. (TTS MP3→PCM decode is a separate,
pure-Go-capable step;
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

## Trust boundary — POSITIVE SSRC→operator mapping, fail-closed (systems-review P1-1)

The earlier "only the operator is trusted" framing was unsound: it failed OPEN on
unattributed audio. discordgo's `OpusRecv` `*Packet` carries an **SSRC but NO UserID**
(`voice.go:799`); the only SSRC→UserID source is the op-5 Speaking event
(`VoiceSpeakingUpdate`, `voice.go:215-226 / 486-499`), which the **caller maintains** and
which Discord emits only on "speaking start." So a speaker already talking at join, or
the first packets of an utterance arriving before the op-5 event lands, have an **UNKNOWN
SSRC** — and a naive "inject unless known-non-operator" build would inject unattributed
audio as an XO command. **Trust-boundary bypass.**

The gate SHALL be a POSITIVE allow-list, mirroring the relay's (`relay.go:18`):

- The process maintains an **SSRC→UserID table** seeded from `VoiceSpeakingUpdate` events.
- Audio is transcribed-and-injected **ONLY** when its SSRC is positively mapped to
  `operator_user_id`. **ANY unmapped, ambiguous, or non-operator SSRC is DROPPED**
  (fail-closed) — never injected.
- **The join-time / first-packet race is explicit:** until an SSRC appears in the table,
  its audio is dropped, not buffered-and-injected-later. The operator simply re-speaks
  after the speaking event registers (≤ a few hundred ms); a missed first half-word is a
  far better failure than injecting an unidentified speaker's command.

## Secrets & cost

- **Secret hygiene (P2-4):** `XAI_API_KEY` lives in `state/voice.env` (the operator copies
  it from `~/.hermes/.env`, decoupling voice from Hermes; chmod 600, never committed). It
  SHALL NEVER appear in logs, errors, or the audit mirror (URL/secret-free errors, as
  `internal/discord` already does for webhook URLs).
- **Cost (operator's $, metered on the shared xAI account):** STT ¢/hr, TTS $4.20/1M chars
  — a real session is cents. v1 carries a **cost cap** (max session minutes / max
  synthesized chars) and a running meter; on the cap it stops + alerts. The meter's
  **reserve→commit SHALL be atomic** (P2-3): concurrent synthesis must not overshoot the
  cap via a check-then-spend race.
- **Grok API shape is NOT yet code-grounded (P3-3):** the `/v1/stt` `/v1/tts` request
  shapes + pricing are from xAI docs, not a verified probe. The build's provider task MUST
  validate the real API (live docs / a $0-or-cheap probe) BEFORE wiring — treated as
  to-confirm, not assumed.

## Pane-injection serialization — a GLOBAL per-pane lock (systems-review P1-2)

`internal/watch/inject.go` serializes deliveries so "two deliveries never interleave into
a pane's composer" — but that `Injector` is owned by the **watch** process, and `voice`
is a separate process. `internal/deliver.Send` (`tmux.go:263-291`) is a non-atomic
load-buffer→paste→Enter with **no cross-process lock** (the per-PID buffer prevents
buffer collisions, NOT composer interleave). So a voice transcript can interleave with a
heartbeat/relay paste into the SAME XO composer, corrupting both. This race **already
exists for `send`/`cmdSend` today**; voice adds a second high-frequency autonomous
injector and makes it likely.

**Ratified fix (XO): a per-pane advisory lock inside `internal/deliver`.** Every writer —
`send`, the watch `Injector`, and `voice` — acquires an `flock` on a per-pane lockfile
around the paste-sequence (load-buffer→paste→settle→Enter), releasing after. It is
**symmetric** (no process coupling, unlike routing voice through watch's Injector, which
would add a listener surface to the safety-critical clock) and it **closes the
pre-existing `send` race for real** (per fix-preexisting-errors, not just for voice). The
lock is advisory + per-pane so it never blocks unrelated panes; a held lock just serializes
writers to one composer.

## Voice-session recovery — isolated, and self-healing (systems-review P2-2)

Isolation from the clock is verified (a voice failure can't touch `watch`). But the voice
SESSION needs its own recovery: discordgo's voice support is explicitly WIP
(`voice.go:887` "reconnect ugly shit WIP"), so a mid-utterance gateway drop is expected.
On a voice disconnect the process SHALL **drop the in-flight utterance** (no stale-audio
replay — a half-captured command must never be injected late), re-establish the voice
connection, and if it cannot, emit a **one-line operator notice** and idle rather than
spin. **discordgo voice maturity is a documented build risk** (the build may need a thin
reconnect wrapper or a fallback).

## XO-pane-busy at inject time (systems-review P2-5)

The XO pane may be `StateWorking` when a transcript is ready. Injecting into a busy
composer races the XO's own output. v1 SHALL check pane state (the same `surface.Assess`
the watchdog uses) and, when the XO is working, **defer the injection** (brief retry/queue)
rather than paste mid-turn — and this defer composes with the P1-2 per-pane lock (the lock
serializes; the busy-check avoids interrupting an active turn).

## Ratified decisions (XO checkpoint 2026-06-11)

1. **Endpointing = silence-timeout** (configurable quiet-gap, ~1.5–2 s). Push-to-talk
   single-operator needs no VAD lib; VAD is a Phase-2 refinement if endpointing is janky.
2. **`speak` transport = a dedicated `flotilla speak "<short text>"` command** (NOT
   `notify --voice`) — keeps the spoken-reply path semantically separate from
   operator-text `notify`, which must NEVER be auto-spoken.
3. **`speak` → `voice` transport = a FILE-DROP SPOOL (outbound, XO→voice — distinct from
   P1-2's inbound serialization).** `flotilla speak` writes a small timestamped file to a
   spool dir (e.g. `state/voice/outbound/`) and **returns IMMEDIATELY** (non-blocking,
   never fails the XO turn on the voice process's state); the `voice` process
   watches→consumes→deletes. Bounded with a TTL / max-files so a down voice process never
   grows the spool unbounded. Rationale: maximal decoupling — `speak` must never block or
   fail on voice being up (the same fail-open/isolation spine as the whole design); a
   socket would couple the XO turn to voice's liveness, the spool does not.
4. **Opus lib = deferred to build** (build-detail); lean `hraban/opus` (more actively
   maintained than `layeh.com/gopus`); final pick is empirical at build (builds +
   round-trips cleanly on the GB10 host).

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
