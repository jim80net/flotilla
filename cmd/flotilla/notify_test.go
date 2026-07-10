package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
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

func TestCmdNotifyAttachPostsMultipart(t *testing.T) {
	const agent = "xo"
	var (
		gotContent string
		gotFile    string
		gotData    []byte
		gotCT      string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(gotCT)
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("Content-Type = %q, want multipart/form-data", gotCT)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "payload_json":
				var p struct {
					Content string `json:"content"`
				}
				_ = json.Unmarshal(data, &p)
				gotContent = p.Content
			case "files[0]":
				gotFile = part.FileName()
				gotData = data
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	secrets := writeSecrets(t, agent, srv.URL)
	attach := filepath.Join(t.TempDir(), "proto.html")
	if err := os.WriteFile(attach, []byte("<html>dash</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--attach", attach, "here"}); err != nil {
		t.Fatalf("cmdNotify --attach: %v", err)
	}
	if gotContent != "here" {
		t.Errorf("content = %q, want %q", gotContent, "here")
	}
	if gotFile != "proto.html" || string(gotData) != "<html>dash</html>" {
		t.Errorf("attachment = %q %q, want proto.html with body", gotFile, gotData)
	}
}

func TestCmdNotifyAttachOnlyAllowed(t *testing.T) {
	const agent = "xo"
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	secrets := writeSecrets(t, agent, srv.URL)
	attach := filepath.Join(t.TempDir(), "only.bin")
	if err := os.WriteFile(attach, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--attach", attach}); err != nil {
		t.Fatalf("cmdNotify attach-only: %v", err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1", hits)
	}
}

func TestCmdNotifyAttachMissingFailsClosed(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "xo"
	secrets := writeSecrets(t, agent, srv.URL)
	err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--attach", filepath.Join(t.TempDir(), "nope.txt"), "hi"})
	if err == nil {
		t.Fatal("cmdNotify(bad attach) = nil, want error")
	}
	if hits != 0 {
		t.Errorf("server received %d requests; bad attach must post NOTHING", hits)
	}
}

func TestCmdNotifyAttachAndChunkRejected(t *testing.T) {
	secrets := writeSecrets(t, "xo", "https://example.test/hook")
	attach := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(attach, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := cmdNotify([]string{"--from", "xo", "--secrets", secrets, "--chunk", "--attach", attach, "hi"})
	if err == nil {
		t.Fatal("cmdNotify --chunk --attach = nil, want error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q should reject --chunk with --attach", err.Error())
	}
}

func TestCmdNotifyStampsRecentNotify595(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	const agent = "cos"
	secrets := writeSecrets(t, agent, srv.URL)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"cos"}]}`)
	rosterDir := filepath.Dir(rosterPath)

	if err := cmdNotify([]string{"--from", agent, "--secrets", secrets, "--roster", rosterPath, "deploy brief"}); err != nil {
		t.Fatalf("cmdNotify: %v", err)
	}
	stampPath := roster.LayerLastNotifyPath(rosterDir, agent)
	if !watch.RecentNotifyWithinTTL(stampPath, watch.DefaultRecentNotifySuppressTTL, time.Now()) {
		t.Fatalf("recent notify stamp missing or expired at %s", stampPath)
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
