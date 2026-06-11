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

### Requirement: The operator-identity trust boundary is preserved

Voice input SHALL be trusted only from the operator's Discord identity
(`operator_user_id`) — mere presence in the voice channel SHALL NOT confer authorization,
mirroring the text relay's allow-list. A transcript from a non-operator speaker SHALL NOT
be injected as an XO command.

#### Scenario: A non-operator voice is not an XO command
- **WHEN** someone other than the operator speaks in the channel
- **THEN** their speech is not injected into the XO's pane as a command

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
meter; on reaching the cap it SHALL stop and alert rather than spend unbounded.

#### Scenario: The cost cap stops unbounded spend
- **WHEN** a voice session reaches the configured cost/duration cap
- **THEN** the process stops synthesizing/transcribing and alerts, rather than continuing to spend
