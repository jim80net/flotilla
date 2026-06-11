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

- [~] 3.1 Own discordgo session w/ `IntentsGuildVoiceStates` (P2-1); `ChannelVoiceJoin`;
      `OpusRecv`/`OpusSend`; maintain the **SSRC→UserID table** from `VoiceSpeakingUpdate`.
      — **DONE: the SSRC→UserID table** (`SpeakerTable`, positive fail-closed operator gate,
      P1-1) + tests. **DEFERRED (§3b):** the live session/join/`OpusRecv`/`OpusSend` wiring
      (needs libopus + a live channel).
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
- [ ] 3.3 Endpointer: configurable silence-timeout (~1.5–2 s) end-of-utterance. Test. (§3b/§4.)
- [ ] 3.4 Voice-session recovery (P2-2): a gateway drop discards the in-flight utterance
      (no late inject), re-establishes or emits a one-line notice. Test the drop-stale path. (§3b.)

## 4. The pipelines

- [ ] 4.1 Inbound: utterance → endpoint → STT → inject via `internal/deliver` (under the
      per-pane lock), tagged voice-originated. **Gate = POSITIVE SSRC→operator mapping,
      fail-closed (P1-1)**: unmapped/ambiguous/non-operator SSRC → DROPPED. **Busy-defer
      (P2-5)**: if the XO pane is `Working`, defer/retry. STT error/timeout → drop + one-line
      notice, never silent (P2-3). Tests: unattributed-dropped, non-operator-dropped,
      busy-deferred, STT-error-surfaced.
- [ ] 4.2 Outbound: consume the `speak` spool → TTS → Opus → `OpusSend`. Test.

## 5. `flotilla speak` (file-drop spool) + the `voice` command

- [ ] 5.1 `flotilla speak "<short text>"` — writes a timestamped file to `state/voice/outbound/`
      and returns IMMEDIATELY (non-blocking; never fails the XO turn on voice's state).
      Bounded (TTL / max-files); **overflow action = DROP-OLDEST, never refuse-new** (a
      refuse-new would fail the XO turn, violating the never-blocks-the-turn ruling). The
      `voice` process watches→consumes→deletes. Test: speak writes + returns with voice
      down; the spool is bounded; overflow drops the oldest, not the new write.
- [ ] 5.2 `flotilla voice` command: load roster + `state/voice.env`, join channel, run both
      pipelines; dispatch + usage in `main.go`.

## 6. Deploy + docs

- [x] 6.1a **enforce `CGO_ENABLED=0` on the non-voice build + CI (P3-1)** so "core is
      pure-Go" is tested — DONE in §3a (ci.yml "Core builds without CGO" step + the
      build-tagged stub) AND in §3b the matching **`voice-opus-codec` CI job** (installs
      libopus-dev; `go build`/`go test -race -tags voiceopus`) compiles + tests the REAL
      codec path, so neither side can silently rot.
- [ ] 6.1b `flotilla-voice.service` (own unit via the installer pattern);
      document `libopus-dev` + `state/voice.env`.
- [ ] 6.2 Voice docs: push-to-talk expectation, the `speak` contract, cost cap, the
      operator-SSRC gate, discordgo-voice-maturity build risk.

## 7. Review + PR

- [ ] 7.1 `/systems-review` + OCR on the implementation diff; fold findings.
- [ ] 7.2 PR(s); CI green; merge-ready → XO reviews+merges. (Live-capture / activation is
      a further operator decision — metered spend on a new audio surface.)
