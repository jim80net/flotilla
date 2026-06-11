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
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3-fake-mp3-bytes"))
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
	if gotBody != (ttsRequest{Text: "hello voice", VoiceID: "eve", Language: "en"}) {
		t.Errorf("tts body = %+v, want the verified {text,voice_id,language}", gotBody)
	}
	if au.ContentType != "audio/mpeg" || au.SampleRate != 24000 || string(au.Data) != "ID3-fake-mp3-bytes" {
		t.Errorf("audio = %+v (want 24kHz audio/mpeg passthrough)", au)
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
	if _, err := g.TTS(context.Background(), "x"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("tts error must be non-nil AND key-free: %v", err)
	}
	if _, err := g.STT(context.Background(), []byte("a"), "a.mp3"); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("stt error must be non-nil AND key-free: %v", err)
	}
}
