package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	defaultSTTURL = "https://api.x.ai/v1/stt"
	defaultTTSURL = "https://api.x.ai/v1/tts"
)

// GrokVoiceCaps are the live-probe-VERIFIED Grok Voice characteristics + pricing. SampleRate
// is 48 kHz: we request output_format.codec=pcm + sample_rate=48000 and Grok returns
// Discord-native PCM directly (probe 2026-06-11), so the outbound path needs no resample —
// SampleRate is the rate of the audio TTS returns. (Defaults without output_format are 24 kHz
// MP3, but we never use that path.) Pricing: batch STT $0.10/hr, TTS $4.20/1M chars.
var GrokVoiceCaps = Caps{
	Name:        "grok",
	TTSVoice:    "eve",
	Language:    "en",
	SampleRate:  48000,
	STTUSDPerHr: 0.10,
	TTSUSDPerM:  4.20,
}

// GrokProvider is the v1 SpeechProvider driving xAI's Grok STT/TTS APIs.
type GrokProvider struct {
	apiKey string
	caps   Caps
	client *http.Client
	sttURL string
	ttsURL string
}

// NewGrokProvider builds the Grok driver. apiKey is XAI_API_KEY (from state/voice.env);
// it is held only in memory and never logged nor returned in an error (it rides only the
// Authorization header, never a request/response body or URL).
func NewGrokProvider(apiKey string) *GrokProvider {
	return &GrokProvider{
		apiKey: apiKey,
		caps:   GrokVoiceCaps,
		client: &http.Client{Timeout: 30 * time.Second},
		sttURL: defaultSTTURL,
		ttsURL: defaultTTSURL,
	}
}

func (g *GrokProvider) Caps() Caps { return g.caps }

// ttsOutputFormat selects the synthesized audio codec. We request raw PCM so the outbound
// pipeline transmits straight to Discord with no transcode (see TTS).
type ttsOutputFormat struct {
	Codec string `json:"codec"`
}

// ttsRequest is the POST /v1/tts JSON body. We pin output_format=pcm + sample_rate=48000 so
// Grok returns Discord-native audio directly. VERIFIED by a live probe (2026-06-11): that
// request returns HTTP 200 with Content-Type audio/pcm and a raw little-endian 16-bit mono
// PCM body at 48 kHz — eliminating the MP3-decode (and its dependency) AND the 24→48 kHz
// resample the outbound path would otherwise need. (The doc's `{audio: base64}` JSON
// response envelope does NOT match reality — the body is raw audio bytes.)
type ttsRequest struct {
	Text         string          `json:"text"`
	VoiceID      string          `json:"voice_id"`
	Language     string          `json:"language"`
	OutputFormat ttsOutputFormat `json:"output_format"`
	SampleRate   int             `json:"sample_rate"`
}

// TTS synthesizes text → raw 48 kHz mono 16-bit PCM (Discord-native; verified probe above).
func (g *GrokProvider) TTS(ctx context.Context, text string) (Audio, error) {
	body, err := json.Marshal(ttsRequest{
		Text:         text,
		VoiceID:      g.caps.TTSVoice,
		Language:     g.caps.Language,
		OutputFormat: ttsOutputFormat{Codec: "pcm"},
		SampleRate:   DiscordSampleRate,
	})
	if err != nil {
		return Audio{}, fmt.Errorf("voice: encode tts request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.ttsURL, bytes.NewReader(body))
	if err != nil {
		return Audio{}, fmt.Errorf("voice: build tts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return Audio{}, fmt.Errorf("voice: tts call: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Audio{}, fmt.Errorf("voice: read tts response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Audio{}, fmt.Errorf("voice: tts http %d: %s", resp.StatusCode, snippet(data))
	}
	// Guard the cost-for-audio contract: a 200 with a non-audio body (a JSON error envelope,
	// an HTML proxy interstitial, the doc's base64 shape) would be reinterpreted as raw PCM
	// and played as noise — having already charged the synthesis. Require an audio/* body.
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "audio/") {
		return Audio{}, fmt.Errorf("voice: tts returned non-audio content-type %q (%d bytes)", ct, len(data))
	}
	return Audio{Data: data, ContentType: ct, SampleRate: DiscordSampleRate}, nil
}

// sttResponse is the verified POST /v1/stt JSON. The live shape is richer than the docs
// ({text, language, duration, words[]}); v1 consumes .text + .duration (duration drives
// exact cost metering), words[] is reserved for future alignment.
type sttResponse struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
}

// STT transcribes a complete audio clip (verified: multipart `file` in, JSON out). The
// transcript is normalized (provider artifacts stripped) before return.
func (g *GrokProvider) STT(ctx context.Context, audio []byte, filename string) (Transcript, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: build stt form: %w", err)
	}
	if _, err := fw.Write(audio); err != nil {
		return Transcript{}, fmt.Errorf("voice: write stt audio: %w", err)
	}
	if err := mw.Close(); err != nil {
		return Transcript{}, fmt.Errorf("voice: close stt form: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.sttURL, &buf)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: build stt request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := g.client.Do(req)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: stt call: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Transcript{}, fmt.Errorf("voice: read stt response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Transcript{}, fmt.Errorf("voice: stt http %d: %s", resp.StatusCode, snippet(data))
	}
	var r sttResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return Transcript{}, fmt.Errorf("voice: parse stt response: %w", err)
	}
	return Transcript{Text: normalizeTranscript(r.Text), Duration: r.Duration}, nil
}

// snippet bounds an error-body excerpt. The API key never appears in a request/response
// BODY (only the Authorization header), so a body snippet is key-safe.
func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
