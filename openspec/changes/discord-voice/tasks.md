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
- [ ] 0.4 RE-checkpoint the revised design → XO re-runs systems-review (+OCR); iterate to clean.
- [ ] 0.5 OPERATOR greenlight of the build phase. (BLOCKS 1+.)

## 1. Per-pane injection serialization (internal/deliver — P1-2; PREREQUISITE, closes a pre-existing race)

- [ ] 1.1 Add a per-pane advisory `flock` (per-pane lockfile) around the
      load-buffer→paste→settle→Enter sequence in `internal/deliver.Send`; EVERY writer
      (`send`, watch `Injector`, `voice`) acquires/releases it. Per-pane (never blocks
      unrelated panes). Test: two concurrent Sends to one pane serialize; different panes
      don't block; the pre-existing `send` race is closed.

## 2. SpeechProvider interface + Grok driver (internal/voice)

- [ ] 2.0 **VALIDATE the real Grok STT/TTS API shape FIRST (P3-3)** — confirm `/v1/stt`,
      `/v1/tts` request/response + auth against live xAI docs (and a $0-or-cheap probe)
      BEFORE wiring; record the verified shape. Do NOT build on the doc-summarized shape.
- [ ] 2.1 `SpeechProvider` interface (STT/TTS/Caps). Test: a fake provider satisfies it.
- [ ] 2.2 `grokProvider` per the validated shape; `XAI_API_KEY` from `state/voice.env`;
      key NEVER in logs/errors/audit (P2-4). Test: httptest round-trip; key-free errors.
- [ ] 2.3 Cost meter + cap with **atomic reserve→commit** (P2-3) — concurrent synthesis
      cannot overshoot. On cap → stop + alert. Test: the cap holds under concurrency.

## 3. Discord voice I/O + codec (internal/voice; cgo build tag)

- [ ] 3.1 Own discordgo session w/ `IntentsGuildVoiceStates` (P2-1); `ChannelVoiceJoin`;
      `OpusRecv`/`OpusSend`; maintain the **SSRC→UserID table** from `VoiceSpeakingUpdate`.
- [ ] 3.2 Opus↔PCM via libopus (cgo, build-tagged; lean `hraban/opus`, empirical pick).
      Test: PCM→Opus→PCM round-trip within tolerance.
- [ ] 3.3 Endpointer: configurable silence-timeout (~1.5–2 s) end-of-utterance. Test.
- [ ] 3.4 Voice-session recovery (P2-2): a gateway drop discards the in-flight utterance
      (no late inject), re-establishes or emits a one-line notice. Test the drop-stale path.

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

- [ ] 6.1 `flotilla-voice.service` (own unit via the installer pattern); **enforce
      `CGO_ENABLED=0` on the non-voice build + CI (P3-1)** so "core is pure-Go" is tested;
      document `libopus-dev` + `state/voice.env`.
- [ ] 6.2 Voice docs: push-to-talk expectation, the `speak` contract, cost cap, the
      operator-SSRC gate, discordgo-voice-maturity build risk.

## 7. Review + PR

- [ ] 6.1 `/systems-review` + OCR on the implementation diff; fold findings.
- [ ] 6.2 PR(s); CI green; merge-ready → XO reviews+merges. (Live-capture / activation is
      a further operator decision — metered spend on a new audio surface.)
