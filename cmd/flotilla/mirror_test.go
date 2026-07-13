package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// recordLogf captures the decision lines a deskMirror emits, so each test can assert EXACTLY ONE
// line of the expected shape (the one-line-per-decision audit contract).
func recordLogf(lines *[]string) func(string, ...any) {
	return func(format string, args ...any) { *lines = append(*lines, fmt.Sprintf(format, args...)) }
}

func TestDeskMirrorSkipsWhenNoWebhook(t *testing.T) {
	var lines []string
	posted := 0
	// No rosterDir and no webhook → nothing to do after WARN (no session-mirror target).
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "", false },
		turnFinal: func(string) (string, bool, error) {
			t.Fatal("turnFinal must not be read when no webhook and no rosterDir")
			return "", false, nil
		},
		post: func(string, string, string) error { posted++; return nil },
		logf: recordLogf(&lines),
	}
	m.run("backend")
	if posted != 0 {
		t.Errorf("posted %d chunks, want 0", posted)
	}
	// #506: missing webhook is a LOUD WARN (not a quiet SKIP).
	if len(lines) != 1 || !strings.Contains(lines[0], "WARN backend: no webhook") {
		t.Errorf("decision lines = %v, want exactly one WARN-no-webhook (#506)", lines)
	}
	if !strings.Contains(lines[0], "FLOTILLA_WEBHOOK_BACKEND") {
		t.Errorf("WARN must name the expected secrets key, got %v", lines)
	}
}

// #572: missing Discord webhook still writes session-mirror so dash conversations work.
func TestDeskMirrorNoWebhookStillSessionMirrors572(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	posted := 0
	m := deskMirror{
		allowDiscord: true,
		rosterDir:    dir,
		webhook:      func(string) (string, bool) { return "", false },
		turnFinal:    func(string) (string, bool, error) { return "coordinator turn without webhook", true, nil },
		post:         func(string, string, string) error { posted++; return nil },
		logf:         recordLogf(&lines),
	}
	m.run("cos")
	if posted != 0 {
		t.Errorf("posted %d, want 0 without webhook", posted)
	}
	warn := false
	ledger := false
	for _, line := range lines {
		if strings.Contains(line, "WARN cos: no webhook") {
			warn = true
		}
		if strings.Contains(line, "LEDGER cos") {
			ledger = true
		}
	}
	if !warn {
		t.Errorf("want WARN no webhook, got %v", lines)
	}
	if !ledger {
		t.Errorf("want LEDGER success after session-mirror, got %v", lines)
	}
	path, err := sessionmirror.LedgerPath(dir, "cos")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session-mirror must exist without webhook: %v", err)
	}
	if !strings.Contains(string(raw), "coordinator turn without webhook") {
		t.Errorf("ledger body = %q", raw)
	}
}

func TestDeskMirrorSkipsWhenNotSubstantive(t *testing.T) {
	var lines []string
	posted := 0
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "", false, nil },
		post:         func(string, string, string) error { posted++; return nil },
		logf:         recordLogf(&lines),
	}
	m.run("backend")
	if posted != 0 {
		t.Errorf("posted %d, want 0 (nothing substantive)", posted)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "SKIP backend: no substantive") {
		t.Errorf("decision lines = %v, want exactly one SKIP-not-substantive", lines)
	}
}

func TestDeskMirrorSkipsOnReadError(t *testing.T) {
	var lines []string
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "", false, errors.New("tmux boom") },
		post:         func(string, string, string) error { t.Fatal("must not post on a read error"); return nil },
		logf:         recordLogf(&lines),
	}
	m.run("backend")
	if len(lines) != 1 || !strings.Contains(lines[0], "SKIP backend: read turn-final: tmux boom") {
		t.Errorf("decision lines = %v, want exactly one SKIP-read-error naming the cause", lines)
	}
}

func TestDeskMirrorPostsSingleChunk(t *testing.T) {
	var lines []string
	var gotURL, gotUser, gotBody string
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "a short report", true, nil },
		post: func(url, user, body string) error {
			gotURL, gotUser, gotBody = url, user, body
			return nil
		},
		logf: recordLogf(&lines),
	}
	m.run("backend")
	if gotURL != "https://wh" || gotUser != "backend" {
		t.Errorf("post got (url=%q, user=%q), want the desk's webhook + identity", gotURL, gotUser)
	}
	if gotBody != "a short report" {
		t.Errorf("single-chunk body = %q, want the unprefixed text", gotBody)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend 1 chunks") {
		t.Errorf("decision lines = %v, want exactly one POST 1 chunks", lines)
	}
}

func TestDeskMirrorChunksOversizeAndPrefixes(t *testing.T) {
	var lines []string
	var bodies []string
	big := strings.Repeat("z", mirrorChunkLimit*2+10) // forces 3 chunks
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return big, true, nil },
		post:         func(_, _, body string) error { bodies = append(bodies, body); return nil },
		logf:         recordLogf(&lines),
	}
	m.run("backend")
	if len(bodies) != 3 {
		t.Fatalf("posted %d chunks, want 3", len(bodies))
	}
	for i, b := range bodies {
		if !strings.HasPrefix(b, fmt.Sprintf("(%d/3)\n", i+1)) {
			t.Errorf("chunk %d missing the (i/N) prefix: %q", i+1, b[:10])
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "POST backend 3 chunks") {
		t.Errorf("decision lines = %v, want exactly one POST 3 chunks", lines)
	}
}

func TestDeskMirror_OnDiscordSuccessAfterPost(t *testing.T) {
	var lines []string
	var successAgent, successBody string
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "operator brief", true, nil },
		post:         func(_, _, _ string) error { return nil },
		onDiscordSuccess: func(agent, body string) {
			successAgent, successBody = agent, body
		},
		logf: recordLogf(&lines),
	}
	m.run("xo")
	if successAgent != "xo" || successBody != "operator brief" {
		t.Errorf("onDiscordSuccess got (%q, %q), want (xo, operator brief)", successAgent, successBody)
	}
}

func TestDeskMirror_OnDiscordSuccessSkippedOnPostFailure(t *testing.T) {
	called := false
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return "brief", true, nil },
		post:         func(_, _, _ string) error { return errors.New("discord down") },
		onDiscordSuccess: func(_, _ string) {
			called = true
		},
		logf: func(string, ...any) {},
	}
	m.run("xo")
	if called {
		t.Error("onDiscordSuccess must not run when Discord post fails")
	}
}

func TestDeskMirrorStopsAndLogsOnPostFailure(t *testing.T) {
	var lines []string
	calls := 0
	big := strings.Repeat("z", mirrorChunkLimit*2+10) // 3 chunks; fail on the 2nd
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return big, true, nil },
		post: func(_, _, _ string) error {
			calls++
			if calls == 2 {
				return errors.New("403 forbidden")
			}
			return nil
		},
		logf: recordLogf(&lines),
	}
	m.run("backend")
	if calls != 2 {
		t.Errorf("post calls = %d, want 2 (stop on the first failure)", calls)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "MIRROR-FAIL backend: chunk 2/3") {
		t.Errorf("decision lines = %v, want exactly one MIRROR-FAIL naming the chunk", lines)
	}
}

func TestDeskMirrorNeverExceedsChunkLimit(t *testing.T) {
	var bodies []string
	big := strings.Repeat("世", mirrorChunkLimit+500) // multi-byte, over the limit
	m := deskMirror{
		allowDiscord: true,
		webhook:      func(string) (string, bool) { return "https://wh", true },
		turnFinal:    func(string) (string, bool, error) { return big, true, nil },
		post:         func(_, _, body string) error { bodies = append(bodies, body); return nil },
		logf:         func(string, ...any) {},
	}
	m.run("backend")
	for i, b := range bodies {
		// Strip the "(i/N)\n" prefix before measuring the content chunk against the limit.
		content := b
		if idx := strings.IndexByte(b, '\n'); idx >= 0 && strings.HasPrefix(b, "(") {
			content = b[idx+1:]
		}
		if n := utf8.RuneCountInString(content); n > mirrorChunkLimit {
			t.Errorf("chunk %d content has %d runes, exceeds limit %d", i, n, mirrorChunkLimit)
		}
	}
}
