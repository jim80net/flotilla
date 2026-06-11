package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"time"
)

// outbound.go is the XO→operator pipeline: it drains the `flotilla speak` spool, synthesizes
// each reply to speech, and transmits it to Discord. Because Grok TTS returns Discord-native
// audio directly (raw 48 kHz mono 16-bit PCM — probe-verified, see GrokProvider.TTS), the
// path is simply spool → TTS → frame → Opus-encode → OpusSend: NO MP3 decode and NO resample.
// It is pure-Go over the Session / OpusCodec / SpeechProvider seams + the cost Meter, so it
// unit-tests with fakes; the real codec/session are wired by the `flotilla voice` command.
//
// The spend gate is the Meter (§2.3): every synthesis is reserved BEFORE the TTS call, and on
// the cap the pipeline goes quiet (the meter hard-stops the session). There is no SSRC gate
// here — that guards INBOUND operator identity; outbound is the XO speaking out.

// OutboundConfig tunes pacing. Zero values get the documented defaults.
type OutboundConfig struct {
	// FrameInterval paces OpusSend so audio plays in real time (Discord expects ~20 ms
	// frames). Default 20ms. Tests set it tiny.
	FrameInterval time.Duration
	// PollInterval is how often the spool is checked when idle. Default 250ms.
	PollInterval time.Duration
}

func (c OutboundConfig) withDefaults() OutboundConfig {
	if c.FrameInterval <= 0 {
		c.FrameInterval = 20 * time.Millisecond
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 250 * time.Millisecond
	}
	return c
}

// OutboundPipeline synthesizes spooled replies and transmits them to Discord voice.
type OutboundPipeline struct {
	session Session
	codec   OpusCodec
	tts     SpeechProvider
	meter   *Meter
	notice  func(string)
	cfg     OutboundConfig
	// done records entries already synthesized+transmitted whose spool file could NOT be
	// deleted (a rare non-IsNotExist delete failure). Without it, the next drain would
	// re-read, RE-RESERVE (charge again), re-synthesize, and re-play the same line — burning
	// real money on a loop. Consulted at the top of speakOne so an undeletable entry is
	// spoken at most once. Run is single-goroutine, so no lock is needed.
	done map[string]bool
}

// NewOutboundPipeline wires the pipeline. meter enforces the session cost cap; notice posts a
// one-line operator notice on a failure or the cap (pass nil to discard).
func NewOutboundPipeline(s Session, codec OpusCodec, tts SpeechProvider, meter *Meter, notice func(string), cfg OutboundConfig) *OutboundPipeline {
	if notice == nil {
		notice = func(string) {}
	}
	return &OutboundPipeline{session: s, codec: codec, tts: tts, meter: meter, notice: notice, cfg: cfg.withDefaults(), done: map[string]bool{}}
}

// Run drains-and-speaks the spool on each poll tick until ctx is cancelled.
func (p *OutboundPipeline) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.drain(ctx)
		}
	}
}

// drain speaks every spooled reply oldest-first. Once the meter has hard-stopped (cost cap),
// it speaks nothing more — entries simply remain (bounded by the spool's own drop-oldest cap)
// rather than being synthesized.
func (p *OutboundPipeline) drain(ctx context.Context) {
	names, err := ListSpool()
	if err != nil {
		p.notice("voice: could not read the outbound spool.")
		return
	}
	for _, name := range names {
		if ctx.Err() != nil || p.meter.Stopped() {
			return
		}
		p.speakOne(ctx, name)
	}
}

// speakOne synthesizes and transmits one spool entry, then deletes it (read-then-delete, so a
// crash mid-speak re-delivers rather than silently drops). Failures produce a one-line notice.
func (p *OutboundPipeline) speakOne(ctx context.Context, name string) {
	if p.done[name] {
		return // already spoken; its file lingers only because a prior delete failed
	}
	text, err := ReadSpool(name)
	if err != nil {
		// A racing drop-oldest trim may have removed it — that's fine, nothing to say.
		// Any other read failure: surface once and drop the poison entry (don't loop on it).
		if !errors.Is(err, os.ErrNotExist) {
			p.notice("voice: could not read a spooled reply.")
			p.consume(name)
		}
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		p.consume(name) // nothing to speak; consume it
		return
	}
	// Spend gate: reserve BEFORE synthesizing. On the cap, go quiet and leave the entry (the
	// meter is now stopped; subsequent drains skip). One notice, not a spam.
	if err := p.meter.ReserveTTS(len(text)); err != nil {
		p.notice("voice: cost cap reached — outbound voice muted for this session.")
		return
	}
	au, err := p.tts.TTS(ctx, text)
	if err != nil {
		p.notice("voice: speech synthesis failed for a reply.")
		p.consume(name) // drop — don't retry a failing synth forever (already charged once)
		return
	}
	if err := p.transmit(ctx, pcmBytesToInt16(au.Data)); err != nil {
		// ctx cancelled (shutdown) → leave the entry to re-deliver next session.
		if ctx.Err() != nil {
			return
		}
		// An encode failure mid-stream is unexpected (every frame is a fixed 960-sample
		// shape); some audio may already have played, so consume rather than re-speak the
		// whole reply on the next tick.
		p.notice("voice: could not encode a reply for playback.")
		p.consume(name)
		return
	}
	p.consume(name) // spoken → consume
}

// consume removes a spool entry after it has been handled. If the delete fails for a real
// reason (not already-absent — DeleteSpool swallows that), the entry is marked done so it is
// never re-read → re-charged → re-spoken; the failure is surfaced once.
func (p *OutboundPipeline) consume(name string) {
	if err := DeleteSpool(name); err != nil {
		p.done[name] = true
		p.notice("voice: a spoken reply could not be cleared from the spool (won't repeat it).")
	}
}

// transmit frames PCM into 20 ms Opus packets and sends them to the voice connection, paced
// at FrameInterval so playback is real-time. ctx cancellation aborts mid-stream.
func (p *OutboundPipeline) transmit(ctx context.Context, pcm []int16) error {
	frames := frameInt16(pcm, DiscordFrameSize)
	timer := time.NewTimer(p.cfg.FrameInterval)
	defer timer.Stop()
	for i, fr := range frames {
		enc, err := p.codec.Encode(fr)
		if err != nil {
			return err
		}
		select {
		case p.session.OpusSend() <- enc:
		case <-ctx.Done():
			return ctx.Err()
		}
		if i < len(frames)-1 { // pace between frames, not after the last
			rearm(timer, p.cfg.FrameInterval)
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// pcmBytesToInt16 reinterprets a raw little-endian signed-16-bit mono PCM byte stream (Grok
// TTS's audio/pcm body) as samples. A trailing odd byte (never expected from 16-bit PCM) is
// ignored.
func pcmBytesToInt16(b []byte) []int16 {
	n := len(b) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

// frameInt16 splits PCM into fixed-size frames, zero-padding the final partial frame to a
// full frame (libopus requires exactly one frame's worth of samples per Encode). Empty PCM
// yields no frames.
func frameInt16(pcm []int16, size int) [][]int16 {
	if len(pcm) == 0 {
		return nil
	}
	var frames [][]int16
	for off := 0; off < len(pcm); off += size {
		if off+size <= len(pcm) {
			frames = append(frames, pcm[off:off+size])
			continue
		}
		last := make([]int16, size) // zero-padded tail
		copy(last, pcm[off:])
		frames = append(frames, last)
	}
	return frames
}
