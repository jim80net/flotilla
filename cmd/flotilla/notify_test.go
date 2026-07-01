package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/discord"
)

// writeSecrets writes a one-key secrets file mapping the given agent to the
// given webhook URL and returns its path. The agent key is derived the same way
// the production loader derives it (FLOTILLA_WEBHOOK_<AGENT_UPPER>, '-' -> '_').
func writeSecrets(t *testing.T, agent, webhookURL string) string {
	t.Helper()
	key := "FLOTILLA_WEBHOOK_" + strings.ToUpper(strings.ReplaceAll(agent, "-", "_"))
	p := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(p, []byte(key+"="+webhookURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCmdNotifyPostsToAgentWebhookWithCorrectRequest(t *testing.T) {
	const agent = "xo"
	var (
		gotPath        string
		gotUA          string
		gotContentType string
		gotBody        []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// The webhook URL carries a distinctive path so we can prove the request
	// went to the URL resolved from THIS agent's secrets key (not some default).
	webhook := srv.URL + "/webhooks/123/abc-token"
	secrets := writeSecrets(t, agent, webhook)

	args := []string{"--from", agent, "--secrets", secrets, "ping the operator"}
	if err := cmdNotify(args); err != nil {
		t.Fatalf("cmdNotify: %v", err)
	}

	if gotPath != "/webhooks/123/abc-token" {
		t.Errorf("request path = %q, want the agent's webhook path", gotPath)
	}
	if gotUA != discord.UserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, discord.UserAgent)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	var payload struct {
		Username string `json:"username"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("body is not the expected JSON shape: %v (body=%s)", err, gotBody)
	}
	if payload.Username != agent {
		t.Errorf("username = %q, want %q (posts under the agent's own identity)", payload.Username, agent)
	}
	if payload.Content != "ping the operator" {
		t.Errorf("content = %q, want %q", payload.Content, "ping the operator")
	}
}

func TestCmdNotifyRejectsOverLimitMessageWithoutPosting(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "backend"
	secrets := writeSecrets(t, agent, srv.URL)

	// One rune over Discord's 2000-rune content limit.
	long := strings.Repeat("a", discord.MaxContentRunes+1)
	err := cmdNotify([]string{"--from", agent, "--secrets", secrets, long})
	if err == nil {
		t.Fatal("cmdNotify(over-limit) = nil error, want a clean rejection")
	}
	if !strings.Contains(err.Error(), "2000") {
		t.Errorf("error %q should cite the 2000-char limit", err.Error())
	}
	if hits != 0 {
		t.Errorf("server received %d requests; an over-limit message must post NOTHING", hits)
	}
}

func TestCmdNotifyAtLimitMessagePosts(t *testing.T) {
	// Exactly at the limit must succeed (boundary: <= MaxContentRunes is allowed).
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	atLimit := strings.Repeat("a", discord.MaxContentRunes)
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, atLimit}); err != nil {
		t.Fatalf("cmdNotify(at-limit) = %v, want success", err)
	}
	if hits != 1 {
		t.Errorf("server received %d requests, want 1", hits)
	}
}

func TestCmdNotifyMultibyteLimitCountedInRunes(t *testing.T) {
	// 2000 multi-byte runes is within the limit even though it is > 2000 bytes —
	// the limit is rune-based, matching discord.MaxContentRunes semantics.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	runes := strings.Repeat("é", discord.MaxContentRunes) // 2000 runes, 4000 bytes
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, runes}); err != nil {
		t.Fatalf("cmdNotify(2000 multibyte runes) = %v, want success", err)
	}
	if hits != 1 {
		t.Errorf("server received %d requests, want 1", hits)
	}
}

func TestCmdNotifyReadsBodyFromFile(t *testing.T) {
	const agent = "xo"
	var gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(body, &p)
		gotContent = p.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	secrets := writeSecrets(t, agent, srv.URL)
	msgFile := filepath.Join(t.TempDir(), "msg.txt")
	if err := os.WriteFile(msgFile, []byte("line one\nline two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--file", msgFile}); err != nil {
		t.Fatalf("cmdNotify --file: %v", err)
	}
	if gotContent != "line one\nline two" {
		t.Errorf("content = %q, want multi-line body with trailing newline trimmed", gotContent)
	}
}

func TestCmdNotifyMissingWebhookForAgentErrors(t *testing.T) {
	// Secrets file exists but has no key for this agent → clean error, no panic.
	secrets := writeSecrets(t, "someone-else", "https://example.test/hook")
	err := cmdNotify([]string{"--from", "xo", "--secrets", secrets, "hi"})
	if err == nil {
		t.Fatal("cmdNotify(no webhook for agent) = nil error, want error")
	}
	if !strings.Contains(err.Error(), "xo") {
		t.Errorf("error %q should name the agent with no webhook", err.Error())
	}
}

func TestCmdNotifyFirewallRefuseBouncesWithoutPosting(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	// A deployment denylist term (supplied via env) trips the firewall. (We use a
	// denylist term rather than a generic home-path shape so this committed test file
	// stays clean under the static tree-scan guard.)
	clearFirewallEnv(t)
	t.Setenv(denylistEnv, "acme-desk")
	err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "ping from the acme-desk"})
	if err == nil {
		t.Fatal("a leaking notify must be REFUSED (bounced), not posted")
	}
	if !strings.Contains(err.Error(), "REFUSED") {
		t.Errorf("error %q should bounce with a REFUSED message + the abstraction", err.Error())
	}
	if hits != 0 {
		t.Errorf("server received %d requests; a refused notify must post NOTHING", hits)
	}
}

func TestCmdNotifyFirewallWarnStillPosts(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	clearFirewallEnv(t)
	t.Setenv(warnlistEnv, "flatten(ed|s|ing)?")
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "we flattened the book"}); err != nil {
		t.Fatalf("a warnlist hit is advisory — notify must still succeed; got %v", err)
	}
	if hits != 1 {
		t.Errorf("a warn-tier message must still post once; got %d requests", hits)
	}
}

func TestCmdNotifyRequiresFrom(t *testing.T) {
	t.Setenv("FLOTILLA_SELF", "")
	if err := cmdNotify([]string{"hi"}); err == nil {
		t.Error("cmdNotify(no --from, no $FLOTILLA_SELF) = nil error, want error")
	}
}

func TestCmdNotifyEmptyMessageRejected(t *testing.T) {
	secrets := writeSecrets(t, "xo", "https://example.test/hook")
	if err := cmdNotify([]string{"--from", "xo", "--secrets", secrets, "   "}); err == nil {
		t.Error("cmdNotify(whitespace-only) = nil error, want empty-message error")
	}
}

func TestCmdNotifyMissingSecretsPathErrors(t *testing.T) {
	t.Setenv("FLOTILLA_SECRETS", "")
	if err := cmdNotify([]string{"--from", "xo", "hi"}); err == nil {
		t.Error("cmdNotify(no secrets path) = nil error, want error")
	}
}

func TestCmdNotifyChunkSplitsOverLimitBody(t *testing.T) {
	const agent = "xo"
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(body, &p)
		bodies = append(bodies, p.Content)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	secrets := writeSecrets(t, agent, srv.URL)
	long := strings.Repeat("a", mirrorChunkLimit*2+10) // forces 3 chunks
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--chunk", long}); err != nil {
		t.Fatalf("cmdNotify --chunk: %v", err)
	}
	if len(bodies) != 3 {
		t.Fatalf("posted %d chunks, want 3", len(bodies))
	}
	for i, b := range bodies {
		if !strings.HasPrefix(b, fmt.Sprintf("(%d/3)\n", i+1)) {
			t.Errorf("chunk %d missing (i/3) prefix: %q", i+1, b[:min(12, len(b))])
		}
	}
}

func TestCmdNotifyChunkSinglePartHasNoPrefix(t *testing.T) {
	const agent = "xo"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(body, &p)
		got = p.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	secrets := writeSecrets(t, agent, srv.URL)
	short := "short mirror body"
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--chunk", short}); err != nil {
		t.Fatalf("cmdNotify --chunk(short): %v", err)
	}
	if got != short {
		t.Errorf("single-chunk body = %q, want unprefixed %q", got, short)
	}
}

func TestCmdNotifyWithoutChunkStillRejectsOverLimit(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	long := strings.Repeat("z", discord.MaxContentRunes+1)
	err := cmdNotify([]string{"--from", agent, "--secrets", secrets, long})
	if err == nil {
		t.Fatal("cmdNotify(over-limit, no --chunk) = nil error, want rejection")
	}
	if hits != 0 {
		t.Errorf("server received %d requests; over-limit without --chunk must post nothing", hits)
	}
}

func TestCmdNotifyFlagAfterMessageRejected(t *testing.T) {
	secrets := writeSecrets(t, "xo", "https://example.test/hook")
	// A flag after the positional message is silently swallowed by Go's flag
	// parser; cmdNotify must catch it rather than post a partial message.
	err := cmdNotify([]string{"--from", "xo", "hello", "--secrets", secrets})
	if err == nil {
		t.Fatal("cmdNotify(flag after message) = nil error, want a clear error")
	}
	if !strings.Contains(err.Error(), "--secrets") {
		t.Errorf("error %q should name the swallowed flag", err.Error())
	}
}
