package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fakes for the four seams (Session / OpusCodec / SpeechProvider / PaneInjector) ---

// fakeSession uses UNBUFFERED channels: because InboundPipeline.Run is single-goroutine, a
// channel send returns only after Run has received it, so a Speaking event sent before a
// Packet is guaranteed to be applied to the gate before that Packet is processed — making
// the tests deterministic without sleeps.
type fakeSession struct {
	recv     chan Packet
	speaking chan SpeakingEvent
	send     chan []byte
}

func newFakeSession() *fakeSession {
	return &fakeSession{recv: make(chan Packet), speaking: make(chan SpeakingEvent), send: make(chan []byte, 8)}
}

func (f *fakeSession) OpusRecv() <-chan Packet        { return f.recv }
func (f *fakeSession) Speaking() <-chan SpeakingEvent { return f.speaking }
func (f *fakeSession) OpusSend() chan<- []byte        { return f.send }
func (f *fakeSession) Close() error                   { return nil }

// fakeCodec decodes any Opus packet to a fixed-length PCM block (samplesPerFrame), so the
// inbound assembly can be driven without real audio.
type fakeCodec struct{ samplesPerFrame int }

func (c fakeCodec) Decode(opus []byte) ([]int16, error) { return make([]int16, c.samplesPerFrame), nil }

// Encode validates the pipeline framed to exactly one Discord frame (the real codec requires
// it), then returns a non-empty marker packet so outbound tests can count transmitted frames.
func (c fakeCodec) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) != DiscordFrameSize {
		return nil, fmt.Errorf("fakeCodec.Encode got %d samples, want a full %d-sample frame", len(pcm), DiscordFrameSize)
	}
	return []byte{0xAB}, nil
}
func (c fakeCodec) Close() error { return nil }

// fakeProvider records the audio handed to STT and returns a configured transcript/error
// (inbound) or a configured PCM clip (outbound TTS).
type fakeProvider struct {
	text    string
	err     error
	gotWAV  chan []byte
	ttsPCM  []byte // raw s16le PCM returned by TTS (outbound tests)
	ttsErr  error
	ttsText chan string // records text handed to TTS
}

func (p *fakeProvider) STT(ctx context.Context, audio []byte, filename string) (Transcript, error) {
	if p.gotWAV != nil {
		p.gotWAV <- audio
	}
	if p.err != nil {
		return Transcript{}, p.err
	}
	return Transcript{Text: p.text, Duration: 1}, nil
}
func (p *fakeProvider) TTS(ctx context.Context, text string) (Audio, error) {
	if p.ttsText != nil {
		p.ttsText <- text
	}
	if p.ttsErr != nil {
		return Audio{}, p.ttsErr
	}
	return Audio{Data: p.ttsPCM, ContentType: "audio/pcm", SampleRate: DiscordSampleRate}, nil
}
func (p *fakeProvider) Caps() Caps { return GrokVoiceCaps }

// fakeInjector reports busy for the first busyFor checks, then idle; it records injected text.
type fakeInjector struct {
	mu         sync.Mutex
	busyCalls  int
	busyFor    int // number of leading Busy() calls that report true
	alwaysBusy bool
	injected   chan string
}

func (i *fakeInjector) Busy() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.alwaysBusy {
		return true
	}
	i.busyCalls++
	return i.busyCalls <= i.busyFor
}
func (i *fakeInjector) Inject(text string) error {
	i.injected <- text
	return nil
}

// fastConfig keeps the timing tests sub-second.
func fastConfig() InboundConfig {
	return InboundConfig{QuietGap: 30 * time.Millisecond, BusyRetryInterval: 5 * time.Millisecond, BusyMaxRetries: 5}
}

func recvWithin(t *testing.T, ch chan string, d time.Duration) (string, bool) {
	t.Helper()
	select {
	case s := <-ch:
		return s, true
	case <-time.After(d):
		return "", false
	}
}

// The happy path: the operator's mapped SSRC is accumulated, transcribed (STT receives a
// real WAV), and injected tagged voice-originated.
func TestInboundOperatorUtteranceInjected(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "status please", gotWAV: make(chan []byte, 1)}
	inj := &fakeInjector{injected: make(chan string, 1)}
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"} // map operator (applied before packets)
	for i := 0; i < 3; i++ {
		sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}
	}

	got, ok := recvWithin(t, inj.injected, time.Second)
	if !ok {
		t.Fatal("operator utterance was never injected")
	}
	if got != voiceTagPrefix+"status please" {
		t.Errorf("injected = %q, want %q", got, voiceTagPrefix+"status please")
	}
	wav := <-prov.gotWAV
	if len(wav) < 44 || string(wav[0:4]) != "RIFF" {
		t.Errorf("STT did not receive a WAV clip (first bytes %q)", wav[:min(8, len(wav))])
	}
}

// Fail-closed: audio from an SSRC that was never mapped (join-time / first-packet race) is
// dropped — never transcribed, never injected.
func TestInboundUnattributedDropped(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "should not happen", gotWAV: make(chan []byte, 1)}
	inj := &fakeInjector{injected: make(chan string, 1)}
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	for i := 0; i < 3; i++ {
		sess.recv <- Packet{SSRC: 99, Opus: []byte{0x1}} // never mapped
	}
	if _, ok := recvWithin(t, inj.injected, 200*time.Millisecond); ok {
		t.Fatal("unattributed audio was injected — gate is not fail-closed")
	}
	select {
	case <-prov.gotWAV:
		t.Fatal("unattributed audio reached STT")
	default:
	}
}

// A mapped-but-non-operator speaker is also dropped — and (the security-relevant guard)
// never reaches STT.
func TestInboundNonOperatorDropped(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "nope", gotWAV: make(chan []byte, 1)}
	inj := &fakeInjector{injected: make(chan string, 1)}
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "someone-else"}
	for i := 0; i < 3; i++ {
		sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}
	}
	if _, ok := recvWithin(t, inj.injected, 200*time.Millisecond); ok {
		t.Fatal("non-operator audio was injected")
	}
	select {
	case <-prov.gotWAV:
		t.Fatal("non-operator audio reached STT")
	default:
	}
}

// If the operator's SSRC changes mid-utterance (a rejoin assigns a new SSRC), the stale
// in-flight buffer is dropped — the two captures are NOT spliced into one transcript. Both
// SSRCs are the operator (gate-allowed); this is the "drop in-flight on disruption" guard.
func TestInboundSpeakerSSRCChangeDropsStaleUtterance(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "fresh start", gotWAV: make(chan []byte, 1)}
	inj := &fakeInjector{injected: make(chan string, 1)}
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// Operator on SSRC 7, two frames (200 samples) accumulate — then a rejoin: SSRC 8 also
	// maps to the operator, and one frame arrives on 8 before the silence gap.
	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}
	sess.speaking <- SpeakingEvent{SSRC: 8, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 8, Opus: []byte{0x1}} // different SSRC ⇒ drop the stale 200, start fresh

	// The finalized utterance must be ONLY the post-change capture (100 samples), not 300:
	// the WAV data size = 100*2 bytes.
	wav := <-prov.gotWAV
	const header = 44
	if len(wav) != header+100*2 {
		t.Fatalf("finalized WAV = %d bytes, want %d (only the post-SSRC-change 100 samples, stale buffer dropped)", len(wav), header+100*2)
	}
}

// Busy-defer: while the XO pane is working, injection is retried, then lands once idle.
func TestInboundBusyDeferThenInject(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "deferred reply"}
	inj := &fakeInjector{injected: make(chan string, 1), busyFor: 3} // busy for 3 checks, then idle
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}

	got, ok := recvWithin(t, inj.injected, time.Second)
	if !ok {
		t.Fatal("deferred injection never landed after the pane went idle")
	}
	if got != voiceTagPrefix+"deferred reply" {
		t.Errorf("injected = %q", got)
	}
}

// Busy-give-up: if the pane stays busy past the retry budget, do NOT paste mid-turn — emit
// a one-line notice and drop.
func TestInboundBusyGivesUpWithNotice(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "never lands"}
	inj := &fakeInjector{injected: make(chan string, 1), alwaysBusy: true}
	notices := make(chan string, 1)
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, func(s string) { notices <- s }, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}

	msg, ok := recvWithin(t, notices, time.Second)
	if !ok {
		t.Fatal("no operator notice when injection gave up")
	}
	if !strings.Contains(msg, "busy") {
		t.Errorf("notice = %q, want a busy/re-speak notice", msg)
	}
	select {
	case <-inj.injected:
		t.Fatal("pasted into a busy pane — must not inject mid-turn")
	default:
	}
}

// STT failure surfaces as a one-line notice (never a silent drop), and nothing is injected.
func TestInboundSTTErrorSurfaced(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{err: errors.New("stt http 503")}
	inj := &fakeInjector{injected: make(chan string, 1)}
	notices := make(chan string, 1)
	gate := NewSpeakerTable("op-1")
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, func(s string) { notices <- s }, fastConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}}

	msg, ok := recvWithin(t, notices, time.Second)
	if !ok {
		t.Fatal("STT error was not surfaced as a notice")
	}
	if !strings.Contains(msg, "transcription failed") {
		t.Errorf("notice = %q", msg)
	}
	if !strings.Contains(msg, "stt http 503") && strings.Contains(msg, "503") {
		// (we deliberately do NOT leak provider internals into the operator notice)
		t.Errorf("notice leaked provider internals: %q", msg)
	}
}

// MaxUtteranceSamples forces a finalize without waiting for the silence gap (bounds memory
// and cost). QuietGap is large here, so an injection proves the cap path fired.
func TestInboundMaxUtteranceForcesFinalize(t *testing.T) {
	sess := newFakeSession()
	prov := &fakeProvider{text: "long talk"}
	inj := &fakeInjector{injected: make(chan string, 1)}
	gate := NewSpeakerTable("op-1")
	cfg := InboundConfig{QuietGap: 10 * time.Second, MaxUtteranceSamples: 150, BusyRetryInterval: 5 * time.Millisecond, BusyMaxRetries: 5}
	p := NewInboundPipeline(sess, fakeCodec{samplesPerFrame: 100}, prov, gate, inj, nil, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	sess.speaking <- SpeakingEvent{SSRC: 7, UserID: "op-1"}
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}} // 100 samples
	sess.recv <- Packet{SSRC: 7, Opus: []byte{0x1}} // 200 ≥ 150 cap → forced finalize

	if _, ok := recvWithin(t, inj.injected, time.Second); !ok {
		t.Fatal("max-utterance cap did not force a finalize (only the 10s silence timer would, too slow)")
	}
}

// sanitizeForPane must collapse newlines/control chars so an injected transcript is one
// command — a multi-line STT result must not paste as multiple Enter-terminated commands.
func TestSanitizeForPane(t *testing.T) {
	cases := map[string]string{
		"plain text":          "plain text",
		"line one\nline two":  "line one line two",
		"with\r\ncrlf":        "with  crlf",
		"tab\tseparated":      "tab separated",
		"bell\x07and\x00null": "bell and null",
		"trailing\n":          "trailing ",
	}
	for in, want := range cases {
		if got := sanitizeForPane(in); got != want {
			t.Errorf("sanitizeForPane(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(sanitizeForPane(in), "\n\r\t\x00\x07") {
			t.Errorf("sanitizeForPane(%q) left a control char in %q", in, sanitizeForPane(in))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
