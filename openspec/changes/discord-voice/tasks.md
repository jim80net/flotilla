# Tasks — discord-voice (Phase 1)

> **GATE:** this is the build plan. It is unblocked only after (a) the XO ratifies the
> design at the checkpoint, (b) the XO's systems-review + OCR on the design are clean,
> and (c) **the OPERATOR greenlights the build phase** (this is metered-spend + a new
> outward-facing audio surface). DESIGN ONLY until then — no code.

## 0. Design gate (current phase)

- [x] 0.1 Draft proposal + design.md + voice spec delta + this plan.
- [x] 0.2 `openspec validate discord-voice --strict` passes.
- [x] 0.3 Checkpoint → XO design-review round 1: 2 P1 (fail-open SSRC gate; cross-process
      injection interleave) + 5 P2 + 3 P3 + ruled the 4 forks. ALL folded (P1-1 positive
      fail-closed SSRC gate; P1-2 per-pane deliver lock; P2-1..5; P3-1/3; rulings recorded).
- [x] 0.4 RE-checkpoint the revised design → XO re-runs systems-review (+OCR); iterate to clean.
      (Design merged #34 after the XO review came back clean.)
- [x] 0.5 OPERATOR greenlight of the build phase. (BLOCKS 1+.) (Greenlit — modest voice spend.)

## 1. Per-pane injection serialization (internal/deliver — P1-2; PREREQUISITE, closes a pre-existing race)

- [x] 1.1 Add a per-pane advisory `flock` (per-pane lockfile) around the
      load-buffer→paste→settle→Enter sequence in `internal/deliver.Send`; EVERY writer
      (`send`, watch `Injector`, `voice`) acquires/releases it. Per-pane (never blocks
      unrelated panes). Test: two concurrent Sends to one pane serialize; different panes
      don't block; the pre-existing `send` race is closed. (Merged #35.)

## 2. SpeechProvider interface + Grok driver (internal/voice)

- [x] 2.0 **VALIDATE the real Grok STT/TTS API shape FIRST (P3-3)** — confirm `/v1/stt`,
      `/v1/tts` request/response + auth against live xAI docs (and a $0-or-cheap probe)
      BEFORE wiring; record the verified shape. Do NOT build on the doc-summarized shape.
      (Probe found two doc-vs-reality gaps: TTS is 24 kHz mono MP3 → needs the 24→48 kHz
      resample; STT returns the richer `{text,language,duration,words[]}` → `.duration`
      drives the cost meter. Both encoded.)
- [x] 2.1 `SpeechProvider` interface (STT/TTS/Caps). Test: a fake provider satisfies it. (Merged #36.)
- [x] 2.2 `grokProvider` per the validated shape; `XAI_API_KEY` from `state/voice.env`;
      key NEVER in logs/errors/audit (P2-4). Test: httptest round-trip; key-free errors. (Merged #36.)
- [x] 2.3 Cost meter + cap with **atomic reserve→commit** (P2-3) — concurrent synthesis
      cannot overshoot. On cap → stop + alert. Test: the cap holds under concurrency. (Merged #36.)

## 3. Discord voice I/O + codec (internal/voice; cgo build tag)

> **§3a FOUNDATION (merged #37 — pure-Go, no libopus):** the SSRC→UserID gate, the
> 24→48 kHz resample stage, and the CGO-isolated codec SEAM (interface + build-tagged stub
> + the CI proof that the core builds `CGO_ENABLED=0`). Unit-tested under `-race`, no CGO.
>
> **§3b-codec (this PR — the real libopus codec):** `libopus-dev` is now installed
> (operator, 2026-06-11; libopus 1.4 verified). `opus_cgo.go` (`//go:build voiceopus`) +
> the PCM→Opus→PCM round-trip test land, plus the `-tags voiceopus` CI build/test job. The
> remaining §3 items (live discordgo voice session, endpointer, recovery) move into the
> §3b-session / §4 / §5 stream and still need a live voice channel for integration.

- [x] 3.1 Own discordgo session w/ `IntentsGuildVoiceStates` (P2-1); `ChannelVoiceJoin`;
      `OpusRecv`/`OpusSend`; maintain the **SSRC→UserID table** from `VoiceSpeakingUpdate`.
      — SSRC→UserID table (`SpeakerTable`) was §3a; PR-C adds the live adapter
      (`discord_session.go`/`JoinVoice`, voiceopus-tagged): own discordgo session w/
      `IntentsGuilds|IntentsGuildVoiceStates`, join deaf=false, pump `OpusRecv`/`OpusSend`
      (Speaking toggle), feed `VoiceSpeakingUpdate`→the gate. Thin (discordgo voice is WIP);
      live-channel integration is the remaining verification.
- [x] 3.2 Opus↔PCM via libopus (cgo, build-tagged; **first-party binding over system
      libopus** — ruling 2026-06-11). Test: PCM→Opus→PCM round-trip within tolerance. — §3a
      shipped the build-tagged seam (`OpusCodec` in codec.go; `//go:build !voiceopus`
      fail-closed stub; CI `CGO_ENABLED=0` proof) + the 24→48 kHz `Resample` stage. §3b
      (this PR) ships the real `opus_cgo.go` (`//go:build voiceopus`) — a ~one-screen
      `#cgo pkg-config: opus` binding over the 7-function libopus C API (create/encode/
      decode/destroy/strerror) — plus round-trip / silence-vs-signal / frame-size-guard /
      empty-packet-guard / idempotent-Close tests + the `-tags voiceopus` CI job.
      **Library decision:** both design candidates were disqualified for "system libopus on
      ALL arches" — `hraban/opus` pins `pkg-config: opus opusfile` package-wide (needs the
      unused libopusfile); `layeh.com/gopus` silently VENDORS opus-1.1.2 on amd64 (the CI/
      prod arch) and only pkg-config's system libopus on other arches (caught by
      systems-review — a false "links libopus 1.4" claim on amd64). A first-party binding
      `#cgo pkg-config: opus` uses system libopus on every arch (the design's declared
      `libopus-dev` dep, nothing extra), gives a real `opus_*_destroy` `Close()`, and shrinks
      the supply chain to a frozen 7-function surface we fully audit.
- [x] 3.3 Endpointer: configurable silence-timeout (~1.5–2 s) end-of-utterance. Test.
      (`InboundConfig.QuietGap`, default 1500ms; silence timer in `InboundPipeline.Run` +
      a `MaxUtteranceSamples` forced-finalize cap. Tested.)
- [x] 3.4 Voice-session recovery (P2-2): a gateway drop discards the in-flight utterance
      (no late inject), re-establishes or emits a one-line notice. Test the drop-stale path.
      — `Supervise` (recovery.go): connect→run→on-drop tear down (fresh session = empty
      buffer = no late inject)→reconnect, bounded by MaxAttempts then a give-up notice; clean
      shutdown on ctx. Fully unit-tested via the `Connector` seam (reconnect, clean-shutdown,
      give-up, failure-count-reset) — the untestable discordgo transport is behind the seam.

## 4. The pipelines

- [~] 4.1 Inbound: utterance → endpoint → STT → inject via `internal/deliver` (under the
      per-pane lock), tagged voice-originated. **Gate = POSITIVE SSRC→operator mapping,
      fail-closed (P1-1)**: unmapped/ambiguous/non-operator SSRC → DROPPED. **Busy-defer
      (P2-5)**: if the XO pane is `Working`, defer/retry. STT error/timeout → drop + one-line
      notice, never silent (P2-3). Tests: unattributed-dropped, non-operator-dropped,
      busy-deferred, STT-error-surfaced.
      — **ENGINE DONE (this PR):** `InboundPipeline` over the `Session`/`OpusCodec`/
      `SpeechProvider`/`PaneInjector` seams — fail-closed gate, silence endpoint, WAV-wrap →
      STT → voice-tagged inject, bounded busy-defer, STT-error notice. All six behaviors
      unit-tested with fakes (pure-Go, `CGO_ENABLED=0`). **DEFERRED (§3.1/PR-C):** the real
      `PaneInjector` (wraps `surface.Assess` busy-check + `deliver.Send` under the per-pane
      lock) + the real discordgo `Session` adapter — both need the live session / a channel.
- [~] 4.2 Outbound: consume the `speak` spool → TTS → Opus → `OpusSend`. Test.
      — **ENGINE DONE (this PR):** `OutboundPipeline` over the `Session`/`OpusCodec`/
      `SpeechProvider` seams + the cost `Meter`: drains the spool oldest-first, reserves
      spend BEFORE synth (cap → quiet + one notice), synthesizes, frames PCM into 960-sample
      Opus packets (zero-padding the tail), and transmits paced at ~20 ms; read-then-delete
      consume. Unit-tested with fakes. **SIMPLIFICATION (probe-verified 2026-06-11):** Grok
      `/v1/tts` with `output_format.codec=pcm, sample_rate=48000` returns raw 48 kHz mono PCM
      directly (HTTP 200, `audio/pcm`) — so the outbound path needs **NO MP3 decoder (no new
      dependency) and NO 24→48 resample**; `GrokProvider.TTS` now requests that shape. (The
      §3b `Resample` remains for any future provider whose native rate ≠ 48 kHz.) **DEFERRED
      (§5.2/PR-C):** the real discordgo `Session` (OpusSend) wiring + the live channel.

## 5. `flotilla speak` (file-drop spool) + the `voice` command

- [x] 5.1 `flotilla speak "<short text>"` — writes a timestamped file to `state/voice/outbound/`
      and returns IMMEDIATELY (non-blocking; never fails the XO turn on voice's state).
      Bounded (TTL / max-files); **overflow action = DROP-OLDEST, never refuse-new** (a
      refuse-new would fail the XO turn, violating the never-blocks-the-turn ruling). The
      `voice` process watches→consumes→deletes. Test: speak writes + returns with voice
      down; the spool is bounded; overflow drops the oldest, not the new write.
      (Bound = **max-files (SpoolMaxFiles=64)**; TTL intentionally deferred — the file-count
      cap already bounds a permanently-down voice process, and an age sweep, if wanted, fits
      naturally in §5.2's consumer loop. trim never evicts the just-written entry even under
      clock skew; consume API + path-traversal guard included.)
- [x] 5.2 `flotilla voice` command: load roster + `state/voice.env`, join channel, run both
      pipelines; dispatch + usage in `main.go`. — `cmdVoice` (voice.go, voiceopus): loads the
      voice.env config (tested loader) + roster + secrets, builds the deps (two codecs, Grok
      provider, one shared **Meter capping STT+TTS**, fail-closed gate, real `paneInjector`
      over surface-busy + locked `deliver.Send`), opens its own voice-intent discordgo session,
      and runs `Supervise`. A `//go:build !voiceopus` stub makes `flotilla voice` fail clearly
      in the core CGO0 binary. **Closed a #40 gap:** inbound STT is now metered too (reserve by
      clip duration before the call), so the cost cap covers the whole session.

## 6. Deploy + docs

- [x] 6.1a **enforce `CGO_ENABLED=0` on the non-voice build + CI (P3-1)** so "core is
      pure-Go" is tested — DONE in §3a (ci.yml "Core builds without CGO" step + the
      build-tagged stub) AND in §3b the matching **`voice-opus-codec` CI job** (installs
      libopus-dev; `go build`/`go test -race -tags voiceopus`) compiles + tests the REAL
      codec path, so neither side can silently rot.
- [x] 6.1b `flotilla-voice.service` (own unit via the installer pattern);
      document `libopus-dev` + `state/voice.env`. — `deploy/flotilla-voice.service.in`
      (clock-isolated: no dependency on flotilla-watch; requires a `-tags voiceopus` binary)
      + `deploy/flotilla-voice-install.sh` (mirrors the watch installer: env-driven, pure-bash
      substitution, placeholder guards, operator-controlled restart — voice is a live metered
      surface) + `deploy/flotilla-voice.env.example` (host paths) + `deploy/voice.env.example`
      (runtime XAI+VOICE_* config). Tested: `voice_install_test.go` (functional-unit lock,
      example substitutes, incomplete/placeholder/whitespace guards) + the `voice.env.example`
      cold-test (parses via `loadVoiceConfig`).
- [x] 6.2 Voice docs: push-to-talk expectation, the `speak` contract, cost cap, the
      operator-SSRC gate, discordgo-voice-maturity build risk. — `docs/voice-runbook.md`:
      build (`-tags voiceopus` + `libopus-dev`), Discord bot/intents setup, the two config
      files, install/enable, smoke test, the fail-closed operator gate, the cost cap, and the
      #42 discordgo-reconnect caveat. The build command is cold-tested.

## 7. Review + PR

- [ ] 7.1 `/systems-review` + OCR on the implementation diff; fold findings.
- [ ] 7.2 PR(s); CI green; merge-ready → XO reviews+merges. (Live-capture / activation is
      a further operator decision — metered spend on a new audio surface.)
