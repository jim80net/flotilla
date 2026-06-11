## ADDED Requirements

### Requirement: Voice runs in an isolated process, never coupling to the clock

The `voice` capability SHALL run as a separate `flotilla voice` process, NOT folded
into `flotilla watch`. A voice-side failure (Discord voice disconnect, a speech-provider
error, the Opus codec, a crash) SHALL NOT affect the safety-critical heartbeat clock or
the inbound text relay — the same isolation the relay-non-fatal design established.

#### Scenario: A voice failure does not touch the clock
- **WHEN** the `flotilla voice` process errors or exits (provider down, codec failure, voice gateway drop)
- **THEN** `flotilla watch` (the heartbeat clock + relay) keeps running unaffected

### Requirement: Inbound — operator speech becomes an XO pane injection

The system SHALL capture the operator's spoken utterance from the Discord voice channel,
detect its end (silence/endpoint), transcribe it via the speech provider's STT, and
inject the transcript into the XO's tmux pane via the existing delivery path — tagged as
voice-originated so the XO answers concisely-for-voice. The injected transcript is an
operator command exactly as a relayed text message is.

#### Scenario: A spoken request reaches the XO as a turn
- **WHEN** the operator finishes speaking a request in the voice channel
- **THEN** the utterance is transcribed and delivered into the XO's pane (tagged voice-originated), waking the XO's turn

### Requirement: Outbound — only a dedicated, concise reply is spoken

The system SHALL speak ONLY the XO's dedicated spoken-reply output (via `flotilla speak`
/ a `--voice` path), never arbitrary `flotilla notify` traffic. The XO SHALL craft a
short, voice-appropriate reply distinct from any full-depth text it also posts; that
reply is synthesized via the provider's TTS and played into the channel. A long
operator brief (e.g. thousands of characters) SHALL NEVER be read aloud.

#### Scenario: The spoken reply is the dedicated terse output, not a brief
- **WHEN** the XO answers a voice request and also posts a long text brief via `notify`
- **THEN** only the dedicated concise `speak` reply is synthesized and played; the `notify` brief is not spoken

### Requirement: Speech backend is a pluggable provider

The speech backend SHALL be a pluggable `SpeechProvider` (STT + TTS + capabilities),
so the backend is swappable without touching the Discord/pipeline code. v1 SHALL ship a
Grok Voice driver (`/v1/stt`, `/v1/tts`, bearer `XAI_API_KEY` from `state/voice.env`); a
local model driver is a future addition behind the same interface.

#### Scenario: The Grok driver satisfies the interface
- **WHEN** the voice process is configured with the Grok provider
- **THEN** STT and TTS route through Grok's APIs, and swapping to another provider requires no pipeline change

### Requirement: Inbound audio is gated by a POSITIVE operator-SSRC mapping, fail-closed

Voice input SHALL be injected ONLY when its Discord audio SSRC is POSITIVELY mapped to
`operator_user_id`. Discord's received audio packets carry an SSRC but no user id; the
process SHALL maintain an SSRC→UserID table seeded from the voice "speaking" events
(`VoiceSpeakingUpdate`). Audio whose SSRC is **unmapped, ambiguous, or mapped to a
non-operator** SHALL be DROPPED (fail-closed) — never injected — mirroring the text
relay's positive allow-list. This explicitly covers the join-time / first-packet race: an
SSRC not yet in the table is dropped (the operator re-speaks once the speaking event
registers), NOT buffered and injected later.

#### Scenario: Unattributed audio is dropped, not injected
- **WHEN** audio arrives from an SSRC not yet (or never) mapped to the operator — e.g. a speaker already talking at join, or the first packets before the speaking event lands
- **THEN** that audio is dropped and never injected into the XO's pane (fail-closed)

#### Scenario: A non-operator voice is not an XO command
- **WHEN** an SSRC mapped to a non-operator user speaks
- **THEN** their speech is not injected into the XO's pane as a command

### Requirement: Pane injection is serialized by a per-pane lock across all writers

`internal/deliver` SHALL serialize the pane paste-sequence (load-buffer → paste → settle
→ Enter) with a **per-pane advisory lock** that EVERY writer (`send`, the watch injector,
`voice`) acquires and releases. This is required because `voice` is a separate process
from `watch`, so the watch `Injector`'s in-process serialization does NOT protect against
a voice transcript interleaving with a heartbeat/relay paste into the same composer. The
lock SHALL be per-pane (never blocking unrelated panes), and SHALL also close the
pre-existing `send` interleave race, not only the voice case. The lock SHALL be a
kernel-advisory `flock` (auto-released on holder death, so a crashed writer never wedges
the pane) with a BOUNDED acquire timeout; on timeout the writer SHALL log and DROP the
delivery rather than block — critical because the watch `Injector` (the heartbeat clock)
acquires this same lock and must never be wedged by a stuck holder or a non-self-releasing
lockfile.

#### Scenario: Concurrent writers do not interleave into one composer
- **WHEN** a voice transcript and a heartbeat tick target the same XO pane at the same moment
- **THEN** the two paste-sequences are serialized by the per-pane lock — neither corrupts the other's composer input

### Requirement: Inbound defers when the XO pane is busy

When a transcript is ready but the XO pane is in a working state, the process SHALL check
pane state (the same `surface.Assess` the watchdog uses) and DEFER the injection (brief
retry) rather than paste mid-turn into an active composer. The defer SHALL be bounded (up
to a configured N seconds); on exceeding the bound the process SHALL surface a one-line
operator notice (re-speak) rather than dropping silently or deferring unboundedly. This
composes with the per-pane lock (the lock serializes; the busy-check avoids interrupting an
in-flight turn).

#### Scenario: A transcript waits for a working XO
- **WHEN** a transcript is ready while the XO pane is `Working`
- **THEN** the injection is deferred and retried, not pasted into the active turn

### Requirement: Opus/CGO is isolated to the voice binary

The Opus codec (CGO + libopus) needed for Discord's 48 kHz Opus audio SHALL be confined
to the `voice` binary. `watch`, `relay`, and the core SHALL remain pure-Go
(`CGO_ENABLED=0`-buildable); the `libopus` host dependency SHALL be documented for the
voice install only.

#### Scenario: The clock binary stays pure-Go
- **WHEN** `flotilla` (watch/relay/send/notify) is built
- **THEN** it builds with `CGO_ENABLED=0` (no libopus dependency); only `flotilla voice` requires libopus

### Requirement: Push-to-talk v1 with a cost cap

v1 SHALL be push-to-talk (request → XO turn → spoken reply), NOT duplex/barge-in — the
latency floor is the XO's Claude turn. The process SHALL enforce a cost cap (max session
duration / max synthesized characters) on the metered speech APIs and surface a running
meter; on reaching the cap it SHALL stop and alert rather than spend unbounded. The
meter's reserve→commit SHALL be **atomic** so concurrent synthesis cannot overshoot the
cap via a check-then-spend race.

#### Scenario: The cost cap stops unbounded spend
- **WHEN** a voice session reaches the configured cost/duration cap
- **THEN** the process stops synthesizing/transcribing and alerts, rather than continuing to spend

### Requirement: A speech-provider error degrades gracefully, never silently

An STT/TTS provider error or timeout SHALL be handled gracefully — the affected utterance
or reply is dropped and a **one-line operator notice** is surfaced — NEVER silently
swallowed (silently dropping a transcribed operator command, or a reply, would leave the
operator believing the XO heard/answered when it did not).

#### Scenario: An STT timeout is surfaced, not swallowed
- **WHEN** the STT call errors or times out on the operator's utterance
- **THEN** the utterance is dropped AND a one-line notice tells the operator it failed (re-speak), rather than failing silently

### Requirement: Voice-session recovery is self-healing and drops stale audio

The process SHALL, on a voice-gateway disconnect, **drop the in-flight utterance** (a
half-captured command must never be injected late after reconnect), re-establish the voice
connection, and — if it cannot — emit a **one-line operator notice** and idle rather than
spin. Stale audio SHALL NEVER be replayed. discordgo's voice support is explicitly
work-in-progress, so a mid-utterance drop is expected; this recovery is independent of the
clock (a voice failure never touches `watch`).

#### Scenario: A mid-utterance disconnect drops the partial, does not inject it late
- **WHEN** the voice gateway drops while the operator is mid-utterance
- **THEN** the partial capture is discarded (never injected after reconnect) and the connection re-establishes or the operator is notified

### Requirement: The speech-provider key never leaks

`XAI_API_KEY` (in `state/voice.env`, chmod 600, never committed) SHALL NEVER appear in
logs, returned errors, or the Discord audit mirror — provider transport errors are reduced
to a key-free cause, mirroring how `internal/discord` keeps webhook URLs out of errors.

#### Scenario: A provider transport error carries no key
- **WHEN** an STT/TTS call fails (bad auth, network)
- **THEN** the surfaced error contains no part of `XAI_API_KEY`
