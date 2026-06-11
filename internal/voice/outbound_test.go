package voice

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

var errTTS = errors.New("tts boom")

// fastOutbound keeps pacing/poll sub-second for tests.
func fastOutbound() OutboundConfig {
	return OutboundConfig{FrameInterval: time.Millisecond, PollInterval: 5 * time.Millisecond}
}

// bigMeter is a meter whose cap won't bite.
func bigMeter() *Meter { return NewMeter(1000, GrokVoiceCaps) }

// drainFrames reads frames from the fake session's send channel until it goes quiet for `quiet`.
func drainFrames(send chan []byte, quiet time.Duration) int {
	n := 0
	for {
		select {
		case <-send:
			n++
		case <-time.After(quiet):
			return n
		}
	}
}

func waitSpoolEmpty(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		names, _ := ListSpool()
		if len(names) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	names, _ := ListSpool()
	t.Fatalf("spool not drained within %v (%d entries remain)", d, len(names))
}

// A spooled reply is synthesized and transmitted as paced Opus frames, the spend is metered,
// and the entry is consumed. 1920 samples of PCM = exactly 2 full 960-sample frames.
func TestOutboundSpeaksSpooledReply(t *testing.T) {
	isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsPCM: make([]byte, DiscordFrameSize*2*2)} // 2 frames × 960 samples × 2 bytes
	meter := bigMeter()
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, meter, nil, fastOutbound())

	if _, err := WriteSpeak("status nominal"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	if got := drainFrames(sess.send, 150*time.Millisecond); got != 2 {
		t.Errorf("transmitted %d Opus frames, want 2 (1920 samples ÷ 960)", got)
	}
	waitSpoolEmpty(t, time.Second)
	if meter.SpentUSD() <= 0 {
		t.Errorf("meter did not record TTS spend: %v", meter.SpentUSD())
	}
}

// A partial final frame is zero-padded to a full frame: 1000 samples → 2 frames (960 + 40 padded).
func TestOutboundPadsFinalFrame(t *testing.T) {
	isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsPCM: make([]byte, 1000*2)} // 1000 samples
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, bigMeter(), nil, fastOutbound())

	if _, err := WriteSpeak("hi"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	if got := drainFrames(sess.send, 150*time.Millisecond); got != 2 {
		t.Errorf("transmitted %d frames, want 2 (1000 samples → 960 + a 40-sample padded frame)", got)
	}
}

// On the cost cap the pipeline goes quiet: no TTS, no frames, the entry is left (meter stopped),
// and the operator gets one notice.
func TestOutboundCostCapMutes(t *testing.T) {
	isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsPCM: make([]byte, DiscordFrameSize*2), ttsText: make(chan string, 1)}
	meter := NewMeter(0, GrokVoiceCaps) // $0 cap → any synthesis trips it
	notices := make(chan string, 1)
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, meter, func(s string) { notices <- s }, fastOutbound())

	if _, err := WriteSpeak("this will not be spoken"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	msg, ok := recvWithin(t, notices, time.Second)
	if !ok {
		t.Fatal("no cost-cap notice")
	}
	if !strings.Contains(msg, "cost cap") {
		t.Errorf("notice = %q, want a cost-cap notice", msg)
	}
	if !meter.Stopped() {
		t.Error("meter should be stopped after the cap trips")
	}
	select {
	case <-prov.ttsText:
		t.Fatal("TTS was called despite the cap (reserve must precede synth)")
	case <-sess.send:
		t.Fatal("a frame was transmitted despite the cap")
	default:
	}
}

// A TTS failure surfaces a notice and drops the entry (no infinite retry of a failing synth).
func TestOutboundTTSErrorDropsEntry(t *testing.T) {
	isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsErr: errTTS}
	notices := make(chan string, 1)
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, bigMeter(), func(s string) { notices <- s }, fastOutbound())

	if _, err := WriteSpeak("boom"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	if _, ok := recvWithin(t, notices, time.Second); !ok {
		t.Fatal("TTS error was not surfaced")
	}
	waitSpoolEmpty(t, time.Second) // the failing entry is dropped, not retried forever
}

// An empty/whitespace spool entry is consumed without a TTS call.
func TestOutboundEmptyEntrySkipped(t *testing.T) {
	isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsText: make(chan string, 1), ttsPCM: make([]byte, DiscordFrameSize*2)}
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, bigMeter(), nil, fastOutbound())

	if _, err := WriteSpeak("   "); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	waitSpoolEmpty(t, time.Second)
	select {
	case <-prov.ttsText:
		t.Fatal("TTS called for an empty entry")
	default:
	}
}

// The money-critical guard (review P2-1): if a spool entry's file cannot be deleted after it
// was synthesized+transmitted (a real, non-absent delete failure), it must be spoken AT MOST
// ONCE — never re-read → re-charged → re-played on the next drain. We force the failure by
// making the spool dir non-writable so os.Remove fails, then assert TTS is called exactly once.
func TestOutboundUndeletableEntrySpokenOnce(t *testing.T) {
	dir := isolateSpool(t)
	sess := newFakeSession()
	prov := &fakeProvider{ttsPCM: make([]byte, DiscordFrameSize*2), ttsText: make(chan string, 4)}
	meter := bigMeter()
	notices := make(chan string, 4)
	p := NewOutboundPipeline(sess, fakeCodec{}, prov, meter, func(s string) { notices <- s }, fastOutbound())

	if _, err := WriteSpeak("speak me once"); err != nil {
		t.Fatal(err)
	}
	// Make the spool dir non-writable so DeleteSpool (os.Remove) fails with EACCES, not
	// IsNotExist. Restore on cleanup so t.TempDir can remove the tree.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	// First synthesis happens (the reply is spoken once).
	if _, ok := recvWithin(t, prov.ttsText, time.Second); !ok {
		t.Fatal("entry was never synthesized")
	}
	spentAfterFirst := meter.SpentUSD()
	// Give several more drain ticks a chance to (wrongly) re-synthesize the lingering file.
	if _, ok := recvWithin(t, prov.ttsText, 120*time.Millisecond); ok {
		t.Fatal("entry was synthesized a SECOND time — undeletable entry re-charged/re-spoken")
	}
	if meter.SpentUSD() != spentAfterFirst {
		t.Errorf("spend changed after the first synth (%.6f → %.6f): re-charged", spentAfterFirst, meter.SpentUSD())
	}
}

func TestPCMBytesToInt16(t *testing.T) {
	// 0x0100 LE = 1, 0xFFFF LE = -1, 0x0080 LE = -32768... check a couple.
	b := []byte{0x01, 0x00, 0xFF, 0xFF, 0x00, 0x80, 0x07} // trailing odd byte ignored
	got := pcmBytesToInt16(b)
	want := []int16{1, -1, -32768}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (odd trailing byte dropped)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFrameInt16(t *testing.T) {
	if frameInt16(nil, 960) != nil {
		t.Error("empty PCM should yield no frames")
	}
	// 960 samples → exactly 1 frame, no padding.
	if got := frameInt16(make([]int16, 960), 960); len(got) != 1 || len(got[0]) != 960 {
		t.Errorf("960 samples → %d frames", len(got))
	}
	// 961 samples → 2 frames, the second zero-padded to 960.
	in := make([]int16, 961)
	in[960] = 42
	got := frameInt16(in, 960)
	if len(got) != 2 || len(got[1]) != 960 {
		t.Fatalf("961 samples → %d frames (last len %d)", len(got), len(got[1]))
	}
	if got[1][0] != 42 {
		t.Errorf("padded frame[0] = %d, want the carried sample 42", got[1][0])
	}
	for i := 1; i < 960; i++ {
		if got[1][i] != 0 {
			t.Fatalf("padded frame not zero at %d: %d", i, got[1][i])
		}
	}
}
