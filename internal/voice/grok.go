package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	defaultSTTURL = "https://api.x.ai/v1/stt"
	defaultTTSURL = "https://api.x.ai/v1/tts"
)

// GrokVoiceCaps are the §2-live-probe-VERIFIED Grok Voice characteristics + pricing
// (TTS returns 24 kHz mono MP3 — the codec stage resamples to Discord's 48 kHz; batch
// STT is $0.10/hr, TTS $4.20/1M chars).
var GrokVoiceCaps = Caps{
	Name:        "grok",
	TTSVoice:    "eve",
	Language:    "en",
	SampleRate:  24000,
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

// ttsRequest is the verified POST /v1/tts JSON body.
type ttsRequest struct {
	Text     string `json:"text"`
	VoiceID  string `json:"voice_id"`
	Language string `json:"language"`
}

// TTS synthesizes text → audio (verified: JSON in, audio/mpeg 24 kHz mono out).
func (g *GrokProvider) TTS(ctx context.Context, text string) (Audio, error) {
	body, err := json.Marshal(ttsRequest{Text: text, VoiceID: g.caps.TTSVoice, Language: g.caps.Language})
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
	return Audio{Data: data, ContentType: resp.Header.Get("Content-Type"), SampleRate: g.caps.SampleRate}, nil
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
