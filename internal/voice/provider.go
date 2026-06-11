// Package voice is the operator↔XO Discord voice path (design: openspec/changes/
// discord-voice). This file defines the pluggable speech backend; the Grok driver,
// the cost meter, and transcript normalization live alongside. The Discord voice I/O
// and the Opus/resample codec stage (which need CGO+libopus) are a SEPARATE,
// build-tagged concern — this file and its siblings are pure-Go and unit-testable.
package voice

import "context"

// SpeechProvider is the swappable speech backend: speech-to-text + text-to-speech.
// Grok Voice is the v1 driver (GrokProvider); a local model is a future driver behind
// the same interface, so the Discord/pipeline code never changes when the backend does.
type SpeechProvider interface {
	// STT transcribes a COMPLETE audio utterance (push-to-talk: the endpointed clip),
	// returning the transcript and the clip's duration (for exact cost metering). The
	// transcript is already normalized (provider-specific artifacts stripped).
	STT(ctx context.Context, audio []byte, filename string) (Transcript, error)
	// TTS synthesizes speech for text, returning the audio bytes + their format.
	TTS(ctx context.Context, text string) (Audio, error)
	// Caps reports the provider's audio + cost characteristics.
	Caps() Caps
}

// Transcript is a recognized utterance.
type Transcript struct {
	Text     string  // normalized transcript text
	Duration float64 // clip duration in seconds (provider-reported; drives STT cost)
}

// Audio is synthesized speech. SampleRate is the provider's native rate — the codec
// stage resamples to Discord's 48 kHz before Opus-encoding (Grok TTS is 24 kHz mono).
type Audio struct {
	Data        []byte
	ContentType string // e.g. "audio/mpeg"
	SampleRate  int    // e.g. 24000
}

// Caps describes a provider's fixed audio + pricing parameters (pricing drives the
// cost meter; rates are operator-configurable but default to the verified Grok rates).
type Caps struct {
	Name        string
	TTSVoice    string  // e.g. "eve"
	Language    string  // e.g. "en"
	SampleRate  int     // synthesized-audio native rate
	STTUSDPerHr float64 // speech-to-text $/hour
	TTSUSDPerM  float64 // text-to-speech $/1,000,000 characters
}
