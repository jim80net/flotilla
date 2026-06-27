package discord

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostSendsPayloadAndSucceedsOn204(t *testing.T) {
	var gotUser, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p webhookPayload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("server: bad json: %v", err)
		}
		gotUser, gotContent = p.Username, p.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Post(srv.URL, "xo", "→ frontend: ship it"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotUser != "xo" {
		t.Errorf("username = %q, want xo", gotUser)
	}
	if gotContent != "→ frontend: ship it" {
		t.Errorf("content = %q", gotContent)
	}
}

func TestPostSetsExplicitUserAgent(t *testing.T) {
	// Discord's Cloudflare edge 403s the Go default User-Agent (error 1010), so
	// every webhook POST must carry an explicit, non-default UA. This is the
	// regression guard for that empirically-verified requirement.
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Post(srv.URL, "agent", "hi"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotUA != UserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, UserAgent)
	}
	if strings.HasPrefix(gotUA, "Go-http-client") {
		t.Errorf("User-Agent is the Go default %q — Discord would 403 this", gotUA)
	}
}

func TestPostContentTypeIsJSON(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Post(srv.URL, "agent", "hi"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
}

func TestPostErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()

	if err := Post(srv.URL, "x", "y"); err == nil {
		t.Error("Post = nil error on 403, want error")
	}
}

func TestPostRedactsWebhookOnError(t *testing.T) {
	// An unreachable URL carrying a secret token must not leak the token.
	const secret = "SUPERSECRETWEBHOOKTOKEN"
	err := Post("http://127.0.0.1:1/webhooks/123/"+secret, "x", "y")
	if err == nil {
		t.Fatal("Post to dead address = nil error, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked the webhook secret: %v", err)
	}
}

func TestPostInvalidURLNoLeak(t *testing.T) {
	const secret = "SUPERSECRETWEBHOOKTOKEN"
	// A control char makes url.Parse fail; the secret must not appear in the error.
	err := Post("http://discord.test/\x01webhooks/123/"+secret, "x", "y")
	if err == nil {
		t.Fatal("Post(malformed url) = nil error, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked the secret on a malformed URL: %v", err)
	}
}

func TestClampContent(t *testing.T) {
	if got := clampContent("short"); got != "short" {
		t.Errorf("clampContent(short) = %q", got)
	}
	long := strings.Repeat("a", MaxContentRunes+50)
	got := clampContent(long)
	if n := len([]rune(got)); n != MaxContentRunes {
		t.Errorf("clamped rune length = %d, want %d", n, MaxContentRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("clamped content missing ellipsis marker")
	}
	// Multi-byte runes must not be split mid-rune.
	multi := strings.Repeat("é", MaxContentRunes+50)
	if !utf8ValidClamp(clampContent(multi)) {
		t.Error("clampContent split a multi-byte rune")
	}
}

func utf8ValidClamp(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
