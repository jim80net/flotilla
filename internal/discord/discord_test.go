package discord

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	if err := Post(srv.URL, "hydra-ops", "→ v12-dev: ship it"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotUser != "hydra-ops" {
		t.Errorf("username = %q, want hydra-ops", gotUser)
	}
	if gotContent != "→ v12-dev: ship it" {
		t.Errorf("content = %q", gotContent)
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
