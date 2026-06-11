package voice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGrokTTS(t *testing.T) {
	const key = "xai-test-SECRETKEY-123"
	var gotAuth, gotCT string
	var gotBody ttsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCT = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte("RAWPCMBYTES"))
	}))
	defer srv.Close()

	g := NewGrokProvider(key)
	g.ttsURL = srv.URL
	au, err := g.TTS(context.Background(), "hello voice")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+key {
		t.Errorf("auth header = %q, want the bearer key", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	// We request Discord-native PCM @ 48 kHz (probe-verified) so the outbound path needs no
	// transcode — assert the request carries output_format.codec=pcm + sample_rate=48000.
	want := ttsRequest{Text: "hello voice", VoiceID: "eve", Language: "en", OutputFormat: ttsOutputFormat{Codec: "pcm"}, SampleRate: 48000}
	if gotBody != want {
		t.Errorf("tts body = %+v, want %+v", gotBody, want)
	}
	if au.ContentType != "audio/pcm" || au.SampleRate != 48000 || string(au.Data) != "RAWPCMBYTES" {
		t.Errorf("audio = %+v (want 48kHz audio/pcm passthrough)", au)
	}
}

// A 200 response whose body is NOT audio (a JSON error envelope, an HTML interstitial) must
// be rejected — never returned as bytes that the outbound path would play as noise after
// already charging the synthesis.
func TestGrokTTSRejectsNonAudio200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"quota"}`))
	}))
	defer srv.Close()
	g := NewGrokProvider("xai-test")
	g.ttsURL = srv.URL
	if _, err := g.TTS(context.Background(), "hi"); err == nil {
		t.Fatal("TTS accepted a non-audio 200 body; want a content-type error")
	}
}

func TestGrokSTTRicherShapeAndNormalize(t *testing.T) {
	var gotFileName string
	var gotAudio []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, hdr, err := r.FormFile("file"); err == nil {
			gotFileName = hdr.Filename
			gotAudio, _ = io.ReadAll(f)
		}
		// the live-verified richer shape, including the observed leading-quote artifact
		_, _ = w.Write([]byte(`{"text":"\"hello there.","language":"en","duration":1.5,"words":[{"text":"hello","start":0.1,"end":0.4}]}`))
	}))
	defer srv.Close()

	g := NewGrokProvider("xai-test")
	g.sttURL = srv.URL
	tr, err := g.STT(context.Background(), []byte("AUDIODATA"), "utt.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if gotFileName != "utt.mp3" || string(gotAudio) != "AUDIODATA" {
		t.Errorf("multipart file: name=%q audio=%q (want the posted clip)", gotFileName, gotAudio)
	}
	if tr.Text != "hello there." { // normalize stripped the leading quote
		t.Errorf("transcript = %q, want the normalized text", tr.Text)
	}
	if tr.Duration != 1.5 {
		t.Errorf("duration = %v, want 1.5 (drives the cost meter)", tr.Duration)
	}
}

func TestGrokErrorNeverLeaksKey(t *testing.T) {
	const key = "xai-VERYSECRET-shouldNeverAppear"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad auth"}`))
	}))
	defer srv.Close()

	g := NewGrokProvider(key)
	g.ttsURL, g.sttURL = srv.URL, srv.URL
	// Non-200 (server-error) path: the snippet is the server body, never the key.
	if _, err := g.TTS(context.Background(), "x"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("tts (http error) must be non-nil AND key-free: %v", err)
	}
	if _, err := g.STT(context.Background(), []byte("a"), "a.mp3"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("stt (http error) must be non-nil AND key-free: %v", err)
	}

	// Transport-error path: client.Do fails (*url.Error). The error embeds the URL
	// (key-free — the key is only an Authorization header, never in the URL), so it must
	// still be key-free. (This is the path with the theoretical URL-embedding risk.)
	g.ttsURL, g.sttURL = "http://127.0.0.1:1/v1/tts", "http://127.0.0.1:1/v1/stt"
	if _, err := g.TTS(context.Background(), "x"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("tts (transport error) must be non-nil AND key-free: %v", err)
	}
	if _, err := g.STT(context.Background(), []byte("a"), "a.mp3"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("stt (transport error) must be non-nil AND key-free: %v", err)
	}
}
