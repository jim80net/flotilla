package voice

import (
	"context"
	"strings"
	"time"
)

// inbound.go is the operator→XO pipeline: received Opus frames are gated to the operator,
// assembled into an utterance by a silence timeout, decoded to PCM, transcribed, and
// injected into the XO's pane tagged voice-originated. It is pure-Go over interfaces
// (Session, OpusCodec, SpeechProvider, PaneInjector) so it unit-tests with fakes and never
// imports discordgo or libopus; the real implementations are wired by the `flotilla voice`
// command (PR-C).
//
// Concurrency: Run is single-goroutine by design. Finalizing an utterance (STT + inject)
// runs inline, so received audio briefly backs up in the Session's buffered channel while a
// transcription/turn is in flight — acceptable for push-to-talk v1 (the operator is not
// speaking while awaiting the reply). Phase 2's duplex would move finalize off the hot loop.

// voiceTagPrefix marks an injected transcript as voice-originated so the XO answers
// concisely-for-voice (a short spoken reply via `flotilla speak`), distinct from its
// full-depth text. The design's outbound contract keeps voice terse; this tag is the inbound
// half of that signal.
const voiceTagPrefix = "[voice] "

// PaneInjector delivers a transcript into the XO's tmux pane. It is the seam over
// internal/deliver (which already takes the per-pane cross-process lock, P1-2) plus the
// busy check (surface.Assess, P2-5); the real impl is wired by the command, tests fake it.
type PaneInjector interface {
	// Busy reports whether the XO pane is mid-turn (StateWorking). When busy, injection is
	// deferred rather than pasted into an active composer.
	Busy() bool
	// Inject delivers the (already voice-tagged) text to the XO pane, under the per-pane lock.
	// The implementation MUST treat text as a SINGLE command — the inbound pipeline already
	// collapses control characters, but a tmux-paste impl must not let any residual newline
	// become a second Enter-terminated command.
	Inject(text string) error
}

// InboundConfig tunes the pipeline. Zero values are replaced with the documented defaults
// by Run, so a caller can pass InboundConfig{} for stock push-to-talk behavior.
type InboundConfig struct {
	// QuietGap is the silence timeout that ends an utterance (no operator packet for this
	// long ⇒ end-of-utterance). Default 1500ms (the ratified ~1.5–2 s push-to-talk gap).
	QuietGap time.Duration
	// MaxUtteranceSamples caps one utterance's accumulated PCM (a forced end-of-utterance),
	// bounding memory and STT cost if silence is never detected. Default 48000*60 (60 s mono);
	// clamped to maxUtteranceCeiling so the WAV size fields (uint32) can never overflow.
	MaxUtteranceSamples int
	// STTTimeout bounds a single transcription call. STT runs inline on the receive loop
	// (push-to-talk), so an unbounded call would stall audio intake; this caps that stall to
	// a per-utterance deadline well under the provider's transport timeout. Default 15s.
	STTTimeout time.Duration
	// BusyRetryInterval / BusyMaxRetries bound the busy-defer: while the XO pane is working,
	// retry this many times at this interval before giving up and asking the operator to
	// re-speak (never paste mid-turn). Defaults: 500ms × 20 (~10 s).
	BusyRetryInterval time.Duration
	BusyMaxRetries    int
}

// maxUtteranceCeiling caps MaxUtteranceSamples so encodeWAV's uint32 size fields cannot
// overflow (samples*2 bytes must stay well under 4 GB). 48 kHz mono × 600 s ≈ 28.8 M
// samples ≈ 57 MB — far above any real push-to-talk utterance, far below the uint32 limit.
const maxUtteranceCeiling = DiscordSampleRate * 600

func (c InboundConfig) withDefaults() InboundConfig {
	if c.QuietGap <= 0 {
		c.QuietGap = 1500 * time.Millisecond
	}
	if c.MaxUtteranceSamples <= 0 {
		c.MaxUtteranceSamples = DiscordSampleRate * 60 // 60 s mono
	}
	if c.MaxUtteranceSamples > maxUtteranceCeiling {
		c.MaxUtteranceSamples = maxUtteranceCeiling // keep WAV size fields within uint32
	}
	if c.STTTimeout <= 0 {
		c.STTTimeout = 15 * time.Second
	}
	if c.BusyRetryInterval <= 0 {
		c.BusyRetryInterval = 500 * time.Millisecond
	}
	if c.BusyMaxRetries <= 0 {
		c.BusyMaxRetries = 20
	}
	return c
}

// InboundPipeline assembles, transcribes, and injects the operator's spoken utterances.
type InboundPipeline struct {
	session  Session
	codec    OpusCodec
	stt      SpeechProvider
	gate     *SpeakerTable
	injector PaneInjector
	notice   func(string) // one-line operator notice on a dropped/failed utterance (never silent)
	cfg      InboundConfig
}

// NewInboundPipeline wires the pipeline. gate is the fail-closed operator SSRC gate
// (seeded from Speaking events at runtime); notice posts a one-line operator notice when an
// utterance is dropped or fails (so a failure is never silent) — pass a no-op to discard.
func NewInboundPipeline(s Session, codec OpusCodec, stt SpeechProvider, gate *SpeakerTable, inj PaneInjector, notice func(string), cfg InboundConfig) *InboundPipeline {
	if notice == nil {
		notice = func(string) {}
	}
	return &InboundPipeline{
		session:  s,
		codec:    codec,
		stt:      stt,
		gate:     gate,
		injector: inj,
		notice:   notice,
		cfg:      cfg.withDefaults(),
	}
}

// Run drives the pipeline until ctx is cancelled. It maintains the SSRC→user table from
// Speaking events, accumulates the OPERATOR's audio (everything else is dropped fail-closed),
// and finalizes an utterance when the operator goes quiet for QuietGap or the utterance hits
// MaxUtteranceSamples.
func (p *InboundPipeline) Run(ctx context.Context) error {
	var utter []int16
	var utterSSRC uint32 // the SSRC that owns the in-flight utterance (0 = none in progress)
	// A stopped timer; (re)armed to QuietGap on each operator packet. quiet fires only while
	// an utterance is in progress.
	timer := time.NewTimer(p.cfg.QuietGap)
	stopTimer(timer)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-p.session.Speaking():
			// Seed the gate. Note rejects ssrc 0 / empty user id itself.
			p.gate.Note(ev.SSRC, ev.UserID)

		case pkt := <-p.session.OpusRecv():
			// Fail-closed gate: only the operator's positively-mapped SSRC is accumulated;
			// unmapped / non-operator audio is dropped, never buffered for later injection.
			if !p.gate.IsOperator(pkt.SSRC) {
				continue
			}
			// If the operator's SSRC changed mid-utterance (a rejoin assigns a new SSRC),
			// the in-flight buffer is stale — DROP it rather than splice two captures into
			// one transcript (the design's "drop in-flight on disruption" stance). The gate
			// already guarantees both SSRCs are the operator, so this is a quality guard, not
			// a trust one.
			if len(utter) > 0 && pkt.SSRC != utterSSRC {
				utter = nil
			}
			pcm, err := p.codec.Decode(pkt.Opus)
			if err != nil {
				// A single undecodable frame is dropped silently (a click); the utterance
				// continues. A whole-utterance failure surfaces at STT, not here.
				continue
			}
			if len(utter) == 0 {
				utterSSRC = pkt.SSRC
			}
			utter = append(utter, pcm...)
			rearm(timer, p.cfg.QuietGap)
			if len(utter) >= p.cfg.MaxUtteranceSamples {
				stopTimer(timer)
				p.finalize(ctx, utter)
				utter = nil
			}

		case <-timer.C:
			if len(utter) > 0 {
				p.finalize(ctx, utter)
				utter = nil
			}
		}
	}
}

// finalize transcribes an assembled utterance and injects it. Any failure produces a
// one-line operator notice (never a silent drop).
func (p *InboundPipeline) finalize(ctx context.Context, pcm []int16) {
	wav := encodeWAV(pcm, DiscordSampleRate, DiscordChannels)
	// Bound the STT call: it runs inline on the receive loop, so an unbounded transcription
	// would stall audio intake (and drop frames) up to the provider's transport timeout.
	// A per-utterance deadline frees the loop and surfaces a notice promptly. ctx is still
	// honored (a shutdown cancels this too).
	sttCtx, cancel := context.WithTimeout(ctx, p.cfg.STTTimeout)
	defer cancel()
	tr, err := p.stt.STT(sttCtx, wav, "utterance.wav")
	if err != nil {
		// err is internal (HTTP status / transport) and key-free by the provider's contract.
		p.notice("voice: transcription failed — please re-speak.")
		return
	}
	if tr.Text == "" {
		// Endpointed noise / silence that transcribed to nothing — drop quietly (no inject),
		// but do not pretend it succeeded.
		return
	}
	p.inject(ctx, tr.Text)
}

// inject delivers the transcript to the XO pane, deferring while the pane is working (P2-5).
// It blocks the Run loop during the defer — intentional for push-to-talk (see file header).
func (p *InboundPipeline) inject(ctx context.Context, text string) {
	tagged := voiceTagPrefix + sanitizeForPane(text)
	// One reused timer for the retry waits (vs a fresh time.After per iteration).
	timer := time.NewTimer(p.cfg.BusyRetryInterval)
	defer timer.Stop()
	for attempt := 0; attempt <= p.cfg.BusyMaxRetries; attempt++ {
		if !p.injector.Busy() {
			if err := p.injector.Inject(tagged); err != nil {
				p.notice("voice: could not deliver the transcript to the XO pane.")
			}
			return
		}
		rearm(timer, p.cfg.BusyRetryInterval)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
	// Still busy after the bounded retries: never paste into an active turn — ask the
	// operator to re-speak once the XO is free.
	p.notice("voice: XO is busy — command not delivered, please re-speak shortly.")
}

// sanitizeForPane collapses newlines and other control characters in a transcript to single
// spaces, so an injected voice command is exactly ONE line = one XO command. STT output is
// free-form text and a tmux paste turns an embedded newline into an Enter (a second
// command); the `[voice]` tag must apply to the whole utterance. Defense in depth — the
// real PaneInjector (PR-C) MUST also treat its argument as a single command.
func sanitizeForPane(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f { // C0 controls (incl. \n, \r) and DEL → space
			return ' '
		}
		return r
	}, s)
}

// rearm safely resets timer to d (drains a pending fire first, per the time.Timer contract).
func rearm(t *time.Timer, d time.Duration) {
	stopTimer(t)
	t.Reset(d)
}

// stopTimer stops t and drains its channel if it had already fired, so a subsequent Reset
// never sees a stale tick.
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}
