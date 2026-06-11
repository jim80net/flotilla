package voice

import "testing"

func TestResampleUpsample24to48(t *testing.T) {
	// A 3-sample ramp at 24 kHz lifted to 48 kHz (2×) interpolates the midpoints and
	// clamps the right neighbour at the tail.
	got := Resample([]int16{0, 100, 200}, 24000, 48000)
	want := []int16{0, 50, 100, 150, 200, 200}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestResamplePassthrough(t *testing.T) {
	in := []int16{1, -2, 3, -4}
	got := Resample(in, 48000, 48000)
	if len(got) != len(in) {
		t.Fatalf("len = %d, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("sample[%d] = %d, want %d (passthrough)", i, got[i], in[i])
		}
	}
	// Must be a copy, not the same backing array — mutating the output must not touch input.
	got[0] = 99
	if in[0] != 1 {
		t.Error("passthrough returned an alias of the input, not a copy")
	}
}

func TestResampleDownsample(t *testing.T) {
	// 48→24 kHz halves the length and keeps every other sample. This pins the raw
	// decimation MATH (no panic, correct length/indices) — NOT audio quality: downsampling
	// is not anti-aliased (see Resample's doc) and the voice path never downsamples.
	got := Resample([]int16{0, 50, 100, 150}, 48000, 24000)
	want := []int16{0, 100}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestResampleEdgeCases(t *testing.T) {
	if got := Resample(nil, 24000, 48000); len(got) != 0 {
		t.Errorf("nil input → %v, want empty", got)
	}
	if got := Resample([]int16{1, 2}, 0, 48000); len(got) != 0 {
		t.Errorf("zero fromHz → %v, want empty", got)
	}
	if got := Resample([]int16{1, 2}, 24000, 0); len(got) != 0 {
		t.Errorf("zero toHz → %v, want empty", got)
	}
}
