package voice

import (
	"encoding/binary"
	"testing"
)

func TestEncodeWAVHeaderAndData(t *testing.T) {
	pcm := []int16{0, 1, -1, 32767, -32768}
	b := encodeWAV(pcm, 48000, 1)

	if len(b) != 44+len(pcm)*2 {
		t.Fatalf("len = %d, want %d (44-byte header + %d sample bytes)", len(b), 44+len(pcm)*2, len(pcm)*2)
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " || string(b[36:40]) != "data" {
		t.Fatalf("bad chunk tags: %q/%q/%q/%q", b[0:4], b[8:12], b[12:16], b[36:40])
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != uint32(36+len(pcm)*2) {
		t.Errorf("RIFF chunk size = %d, want %d", got, 36+len(pcm)*2)
	}
	if got := binary.LittleEndian.Uint16(b[20:22]); got != 1 {
		t.Errorf("AudioFormat = %d, want 1 (PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(b[22:24]); got != 1 {
		t.Errorf("NumChannels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(b[24:28]); got != 48000 {
		t.Errorf("SampleRate = %d, want 48000", got)
	}
	if got := binary.LittleEndian.Uint16(b[34:36]); got != 16 {
		t.Errorf("BitsPerSample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(b[40:44]); got != uint32(len(pcm)*2) {
		t.Errorf("data size = %d, want %d", got, len(pcm)*2)
	}
	// Samples round-trip as little-endian int16.
	for i, want := range pcm {
		got := int16(binary.LittleEndian.Uint16(b[44+i*2 : 44+i*2+2]))
		if got != want {
			t.Errorf("sample[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestEncodeWAVEmpty(t *testing.T) {
	b := encodeWAV(nil, 48000, 1)
	if len(b) != 44 {
		t.Fatalf("empty PCM → %d bytes, want a bare 44-byte header", len(b))
	}
	if got := binary.LittleEndian.Uint32(b[40:44]); got != 0 {
		t.Errorf("data size = %d, want 0", got)
	}
}
