package voice

// Discord's voice gateway speaks Opus at a fixed format: 48 kHz, mono for our use, in
// 20 ms frames. These constants are the contract the codec and the pipelines share.
const (
	DiscordSampleRate = 48000 // Hz — Discord voice is always 48 kHz
	DiscordChannels   = 1     // mono: the XO speaks/listens as a single stream
	DiscordFrameSize  = 960   // samples per channel in one 20 ms frame at 48 kHz
)

// OpusCodec encodes/decodes single 20 ms Discord voice frames. discordgo transports Opus
// bytes but does NOT encode or decode them — that is libopus's job, and libopus is CGO.
//
// The codec is therefore the ONE place CGO enters the voice path, and it is isolated
// behind a build tag (design "CGO isolation"): the real libopus implementation compiles
// only under `-tags voiceopus` (which also needs CGO_ENABLED=1 + libopus-dev). The
// default build gets the stub in opus_stub.go, so the core flotilla binary builds
// CGO_ENABLED=0 with no libopus dependency. NewOpusCodec is the seam: the voice process
// (built with the tag) gets a working codec; everything else gets a clear error if it
// ever tries to construct one.
type OpusCodec interface {
	// Encode turns one frame of mono 48 kHz PCM (DiscordFrameSize samples) into an Opus
	// packet for VoiceConnection.OpusSend.
	Encode(pcm []int16) ([]byte, error)
	// Decode turns one received Opus packet (Packet.Opus) back into mono 48 kHz PCM.
	Decode(opus []byte) ([]int16, error)
	// Close releases the underlying libopus encoder/decoder. Callers MUST Close every codec
	// they construct: the libopus implementation holds C state the Go GC never reclaims, so a
	// dropped-without-Close codec leaks. Safe to call more than once.
	Close() error
}
