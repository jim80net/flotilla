package voice

import "math"

// Resample converts mono 16-bit PCM from one sample rate to another by linear
// interpolation. The voice codec stage uses it to lift Grok TTS audio (24 kHz mono) to
// Discord's 48 kHz before Opus-encoding — an UPSAMPLE, which is the only direction the
// voice path needs (every provider's native rate is ≤ Discord's 48 kHz).
//
// Linear interpolation (not a windowed-sinc resampler) is the deliberate v1 choice: for an
// integer upsample of speech it is inaudible, has no ringing, and is trivially correct and
// testable. It is correct for upsampling (toHz ≥ fromHz) and the equal-rate no-op.
//
// DOWNSAMPLING (toHz < fromHz) is NOT anti-aliased: this is bare decimation with no
// low-pass pre-filter, so it violates Nyquist and aliases. The voice path never
// downsamples, so that's acceptable here; a caller that needs to downsample must add a
// low-pass pre-filter (or swap in a windowed-sinc resampler — the signature is the seam).
//
// fromHz==toHz is a no-op (returns a copy, so callers never alias the input). A
// non-positive rate or empty input returns an empty slice.
func Resample(pcm []int16, fromHz, toHz int) []int16 {
	if fromHz <= 0 || toHz <= 0 || len(pcm) == 0 {
		return []int16{}
	}
	if fromHz == toHz {
		out := make([]int16, len(pcm))
		copy(out, pcm)
		return out
	}

	// Output length scales by the rate ratio. int64 math avoids overflow on long clips.
	nOut := int(int64(len(pcm)) * int64(toHz) / int64(fromHz))
	if nOut <= 0 {
		return []int16{}
	}
	out := make([]int16, nOut)

	last := len(pcm) - 1
	ratio := float64(fromHz) / float64(toHz) // loop-invariant: hoisted out of the hot loop
	for j := range out {
		// Position in input-sample units of this output sample.
		pos := float64(j) * ratio
		i0 := int(pos)
		frac := pos - float64(i0)
		i1 := i0 + 1
		if i1 > last {
			i1 = last // clamp the right neighbour at the tail (no read past the end)
		}
		v := float64(pcm[i0])*(1-frac) + float64(pcm[i1])*frac
		out[j] = clampInt16(math.Round(v))
	}
	return out
}

// clampInt16 rounds a sample to the int16 range. Interpolation between two valid int16s
// can never exceed the range, but the clamp makes the conversion total and self-evidently
// safe rather than relying on that invariant.
func clampInt16(v float64) int16 {
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	if v < math.MinInt16 {
		return math.MinInt16
	}
	return int16(v)
}
