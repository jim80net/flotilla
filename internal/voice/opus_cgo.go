//go:build voiceopus

package voice

// #cgo pkg-config: opus
// #include <opus.h>
import "C"

import (
	"fmt"
	"unsafe"
)

// maxEncodedFrameBytes caps the Opus output buffer for one 20 ms frame. The Opus bitstream
// bounds a single-frame packet at 1275 bytes; 4000 is libopus's documented safe encode
// buffer. opus_encode returns the ACTUAL encoded length, so over-sizing the cap costs only
// a transient per-frame allocation, never a wrong-length packet.
const maxEncodedFrameBytes = 4000

// opusCodec is a first-party, ~one-screen binding over the system libopus C API. It is the
// real OpusCodec, built ONLY in the `voiceopus` build (CGO + libopus); the core gets the
// opus_stub.go fail-closed stub instead, so the core binary stays CGO_ENABLED=0 (see
// codec.go for the isolation rationale).
//
// Why first-party rather than a third-party wrapper (ruling 2026-06-11): libopus's
// encode/decode surface is 7 stable, frozen C functions — small enough to own and audit in
// one file. `#cgo pkg-config: opus` binds the SYSTEM libopus on EVERY architecture (the
// design's declared `libopus-dev` host dependency, nothing extra). A surveyed third-party
// wrapper (layeh.com/gopus) was rejected because it silently vendors a 2015 opus-1.1.2 on
// amd64 while pkg-config'ing system libopus only on other arches — a standing audit
// liability for a frozen API we can bind directly.
//
// NOT SAFE FOR CONCURRENT USE: opus_encode and opus_decode mutate the encoder/decoder C
// state across frames, so a single codec must be driven by ONE goroutine. Use one codec
// per stream (the voice process holds one for inbound decode and one for outbound encode).
type opusCodec struct {
	enc *C.OpusEncoder
	dec *C.OpusDecoder
}

// NewOpusCodec (voiceopus build) creates a libopus codec for Discord's fixed 48 kHz mono
// 20 ms-frame format, tuned for voice (the VoIP application profile).
func NewOpusCodec() (OpusCodec, error) {
	var cErr C.int
	enc := C.opus_encoder_create(C.opus_int32(DiscordSampleRate), C.int(DiscordChannels), C.OPUS_APPLICATION_VOIP, &cErr)
	if enc == nil || cErr != C.OPUS_OK {
		return nil, fmt.Errorf("voice: opus_encoder_create: %s", C.GoString(C.opus_strerror(cErr)))
	}
	dec := C.opus_decoder_create(C.opus_int32(DiscordSampleRate), C.int(DiscordChannels), &cErr)
	if dec == nil || cErr != C.OPUS_OK {
		C.opus_encoder_destroy(enc) // don't leak the encoder we already created
		return nil, fmt.Errorf("voice: opus_decoder_create: %s", C.GoString(C.opus_strerror(cErr)))
	}
	return &opusCodec{enc: enc, dec: dec}, nil
}

// Encode turns exactly one frame of mono 48 kHz PCM into an Opus packet. The frame MUST be
// DiscordFrameSize samples: opus_encode reads that many samples from the slice's backing
// array regardless of its Go length, so a short slice would be a C-side over-read — we
// reject it instead (this also rejects an empty slice, whose &pcm[0] would panic). The
// pipeline pads a trailing partial frame to a full frame before calling this.
func (c *opusCodec) Encode(pcm []int16) ([]byte, error) {
	if c.enc == nil {
		return nil, fmt.Errorf("voice: Encode on a closed codec")
	}
	if len(pcm) != DiscordFrameSize*DiscordChannels {
		return nil, fmt.Errorf("voice: Encode needs exactly %d samples (one %d-sample mono frame), got %d",
			DiscordFrameSize*DiscordChannels, DiscordFrameSize, len(pcm))
	}
	out := make([]byte, maxEncodedFrameBytes)
	n := C.opus_encode(c.enc,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(DiscordFrameSize),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.opus_int32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("voice: opus_encode: %s", C.GoString(C.opus_strerror(C.int(n))))
	}
	return out[:int(n)], nil
}

// Decode turns one received Opus packet back into mono 48 kHz PCM (up to DiscordFrameSize
// samples). An empty packet is rejected: opus_decode would treat nil/empty data as a
// packet-loss-concealment request and synthesize a fabricated frame, which v1 does not use
// — surfacing the error is more honest than silently inventing audio for the STT path.
// decode_fec=0: v1 uses no in-band forward-error-correction.
func (c *opusCodec) Decode(opus []byte) ([]int16, error) {
	if c.dec == nil {
		return nil, fmt.Errorf("voice: Decode on a closed codec")
	}
	if len(opus) == 0 {
		return nil, fmt.Errorf("voice: Decode requires a non-empty Opus packet (v1 does not use PLC)")
	}
	out := make([]int16, DiscordFrameSize*DiscordChannels)
	n := C.opus_decode(c.dec,
		(*C.uchar)(unsafe.Pointer(&opus[0])),
		C.opus_int32(len(opus)),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		C.int(DiscordFrameSize),
		0)
	if n < 0 {
		return nil, fmt.Errorf("voice: opus_decode: %s", C.GoString(C.opus_strerror(C.int(n))))
	}
	// opus_decode cannot write past the buffer cap we passed (it errors if undersized), so
	// n ≤ DiscordFrameSize already — but bound it explicitly before slicing so any future
	// libopus drift or API misuse surfaces as an error, never an out-of-range slice panic.
	if int(n) > DiscordFrameSize {
		return nil, fmt.Errorf("voice: opus_decode returned %d samples, exceeds the %d-sample frame", int(n), DiscordFrameSize)
	}
	return out[:int(n)*DiscordChannels], nil
}

// Close frees the libopus encoder and decoder. Unlike a no-op, this is a real release:
// opus_encoder_create/opus_decoder_create allocate C state that the GC never reclaims, so
// Close MUST be called when the codec is retired. Idempotent — a double Close is safe.
func (c *opusCodec) Close() error {
	if c.enc != nil {
		C.opus_encoder_destroy(c.enc)
		c.enc = nil
	}
	if c.dec != nil {
		C.opus_decoder_destroy(c.dec)
		c.dec = nil
	}
	return nil
}
