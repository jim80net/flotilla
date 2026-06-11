//go:build voiceopus

package voice

import (
	"math"
	"testing"
)

// rms is the root-mean-square amplitude of a PCM frame — a level/energy measure that lets
// us assert a lossy codec carried the signal without demanding bit-exact output.
func rms(pcm []int16) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

// sineFrame fills one DiscordFrameSize mono frame with a tone of the given freq/amplitude.
func sineFrame(freqHz, amp float64) []int16 {
	f := make([]int16, DiscordFrameSize)
	for i := range f {
		f[i] = int16(amp * math.Sin(2*math.Pi*freqHz*float64(i)/float64(DiscordSampleRate)))
	}
	return f
}

// The §3.2 requirement: PCM→Opus→PCM round-trips within tolerance. Opus is lossy and has
// encoder warmup, so we push several frames and assert on a steady-state frame: the packet
// is valid and bounded, the decoded frame is full-length, and its energy tracks the input
// within a generous band (proving signal is carried, not silence or garbage).
func TestOpusCodecRoundTrip(t *testing.T) {
	c, err := NewOpusCodec()
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()

	in := sineFrame(440, 8000)
	var lastOut []int16
	for f := 0; f < 6; f++ { // 6 × 20 ms gets well past Opus's ~6.5 ms encoder delay
		pkt, err := c.Encode(in)
		if err != nil {
			t.Fatalf("encode frame %d: %v", f, err)
		}
		if len(pkt) == 0 || len(pkt) > maxEncodedFrameBytes {
			t.Fatalf("frame %d packet len=%d, want 0<len<=%d", f, len(pkt), maxEncodedFrameBytes)
		}
		out, err := c.Decode(pkt)
		if err != nil {
			t.Fatalf("decode frame %d: %v", f, err)
		}
		if len(out) != DiscordFrameSize {
			t.Fatalf("frame %d decoded len=%d, want %d", f, len(out), DiscordFrameSize)
		}
		lastOut = out
	}

	inRMS, outRMS := rms(in), rms(lastOut)
	if outRMS < inRMS*0.4 || outRMS > inRMS*1.8 {
		t.Errorf("steady-state round-trip energy off: in=%.0f out=%.0f (want within 0.4×–1.8×)", inRMS, outRMS)
	}
}

// A lossy codec must still distinguish silence from signal: silence decodes to ~silence,
// a tone decodes to clearly-present energy. This guards against a codec that returns a
// fixed buffer or drops the signal entirely.
//
// Each stream gets its OWN codec: an Opus encoder/decoder is stateful across frames, so a
// real continuous stream feeds one codec one consistent signal. (Interleaving silence and
// tone through a single decoder leaves it ringing from the previous frame — a test artifact,
// not a codec property.) drainSteadyState returns the decoded frame after encoder warmup.
func TestOpusCodecSilenceVsSignal(t *testing.T) {
	silRMS := rms(drainSteadyState(t, make([]int16, DiscordFrameSize)))
	sigRMS := rms(drainSteadyState(t, sineFrame(440, 8000)))
	if sigRMS < silRMS*4 {
		t.Errorf("codec did not distinguish signal from silence: silence=%.1f tone=%.1f", silRMS, sigRMS)
	}
}

// drainSteadyState round-trips the same frame 6× through a fresh codec and returns the
// last decoded frame (past Opus's encoder warmup), so callers measure steady-state energy.
func drainSteadyState(t *testing.T, frame []int16) []int16 {
	t.Helper()
	c, err := NewOpusCodec()
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()
	var out []int16
	for f := 0; f < 6; f++ {
		pkt, err := c.Encode(frame)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if out, err = c.Decode(pkt); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return out
}

// Encode must reject a frame that is not exactly one full mono frame: libopus would read
// DiscordFrameSize samples from the slice regardless of its Go length, so a short slice is
// a C-side over-read. The wrapper turns that into a clear error instead.
func TestOpusCodecEncodeRejectsWrongFrameSize(t *testing.T) {
	c, err := NewOpusCodec()
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()

	for _, n := range []int{0, DiscordFrameSize - 1, DiscordFrameSize + 1, 2 * DiscordFrameSize} {
		if _, err := c.Encode(make([]int16, n)); err == nil {
			t.Errorf("Encode(%d samples) = nil error, want a wrong-frame-size error", n)
		}
	}
}

// Decode must reject an empty packet rather than let libopus synthesize a fabricated
// packet-loss-concealment frame (which v1 does not use) — an empty/lost packet should
// surface, not become invented audio fed to STT.
func TestOpusCodecDecodeRejectsEmpty(t *testing.T) {
	c, err := NewOpusCodec()
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	defer c.Close()

	if _, err := c.Decode(nil); err == nil {
		t.Error("Decode(nil) = nil error, want an empty-packet error")
	}
	if _, err := c.Decode([]byte{}); err == nil {
		t.Error("Decode(empty) = nil error, want an empty-packet error")
	}
}

// Close frees real C state, so it must be safe to call more than once (e.g. an explicit
// Close plus a deferred Close) without a double-free crash.
func TestOpusCodecCloseIdempotent(t *testing.T) {
	c, err := NewOpusCodec()
	if err != nil {
		t.Fatalf("NewOpusCodec: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close (must be a safe no-op): %v", err)
	}

	// After Close the C state is freed; Encode/Decode must return a clear error rather than
	// dereference a nil encoder/decoder (a use-after-close should never reach into C).
	if _, err := c.Encode(make([]int16, DiscordFrameSize)); err == nil {
		t.Error("Encode after Close = nil error, want a closed-codec error")
	}
	if _, err := c.Decode([]byte{0x01, 0x02}); err == nil {
		t.Error("Decode after Close = nil error, want a closed-codec error")
	}
}
