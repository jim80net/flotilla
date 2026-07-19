package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type recordingSynthesisBot struct {
	verified []string
	channels []string
	bodies   []string
	err      error
}

func (b *recordingSynthesisBot) Verify(channelID string) error {
	b.verified = append(b.verified, channelID)
	return b.err
}

func (b *recordingSynthesisBot) Post(channelID, content string) error {
	b.channels = append(b.channels, channelID)
	b.bodies = append(b.bodies, content)
	return b.err
}

func writeSynthesisFixture(t *testing.T, withBot bool) (rosterPath, secretsPath string) {
	t.Helper()
	dir := t.TempDir()
	rosterPath = filepath.Join(dir, "flotilla.json")
	rosterBody := `{
  "agents":[{"name":"cos"},{"name":"memex"}],
  "channels":[
    {"channel_id":"C_HOME","xo_agent":"memex","members":["cos"]},
    {"channel_id":"C_SECONDARY","xo_agent":"memex","members":["cos"]}
  ]
}`
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	secretsPath = filepath.Join(dir, "secrets.env")
	secretsBody := "FLOTILLA_WEBHOOK_MEMEX=https://example.invalid/home\n"
	if withBot {
		secretsBody += "FLOTILLA_BOT_TOKEN=relay-token\n"
	}
	if err := os.WriteFile(secretsPath, []byte(secretsBody), 0o600); err != nil {
		t.Fatal(err)
	}
	return rosterPath, secretsPath
}

func TestSynthesisPublishFansOutWithoutDuplicatingHome(t *testing.T) {
	rosterPath, secretsPath := writeSynthesisFixture(t, true)
	bot := &recordingSynthesisBot{}
	var webhookURLs, webhookBodies []string
	var stdout bytes.Buffer
	deps := synthesisPublishDeps{
		webhookChannel: func(string) (string, error) { return "C_HOME", nil },
		webhookPost: func(url, username, content string) error {
			if username != "memex" {
				t.Fatalf("webhook username = %q, want memex", username)
			}
			webhookURLs = append(webhookURLs, url)
			webhookBodies = append(webhookBodies, content)
			return nil
		},
		newBot: func(token string) (synthesisChannelPoster, error) {
			if token != "relay-token" {
				t.Fatalf("bot token = %q", token)
			}
			return bot, nil
		},
		stdout: &stdout,
	}
	err := cmdSynthesisPublish([]string{
		"--from", "memex", "--roster", rosterPath, "--secrets", secretsPath,
		"curated rollup",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(webhookURLs, []string{"https://example.invalid/home"}) {
		t.Fatalf("home webhook posts = %v, want exactly one", webhookURLs)
	}
	if !reflect.DeepEqual(webhookBodies, []string{"curated rollup"}) {
		t.Fatalf("home bodies = %v", webhookBodies)
	}
	if !reflect.DeepEqual(bot.channels, []string{"C_SECONDARY"}) {
		t.Fatalf("relay channels = %v, want only C_SECONDARY", bot.channels)
	}
	if !reflect.DeepEqual(bot.verified, []string{"C_SECONDARY"}) {
		t.Fatalf("verified channels = %v, want C_SECONDARY", bot.verified)
	}
	if len(bot.bodies) != 1 || !strings.Contains(bot.bodies[0], "Visibility synthesis · memex") || !strings.HasSuffix(bot.bodies[0], "curated rollup") {
		t.Fatalf("relay body = %q, want attributed synthesis", bot.bodies)
	}
	if !strings.Contains(stdout.String(), "2 unique channel(s)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSynthesisPublishMissingRelayFailsBeforeHomePost(t *testing.T) {
	rosterPath, secretsPath := writeSynthesisFixture(t, false)
	var homePosts int
	deps := synthesisPublishDeps{
		webhookChannel: func(string) (string, error) { return "C_HOME", nil },
		webhookPost:    func(string, string, string) error { homePosts++; return nil },
		newBot: func(string) (synthesisChannelPoster, error) {
			t.Fatal("bot factory must not run without a token")
			return nil, nil
		},
		stdout: &bytes.Buffer{},
	}
	err := cmdSynthesisPublish([]string{
		"--from", "memex", "--roster", rosterPath, "--secrets", secretsPath,
		"curated rollup",
	}, deps)
	if err == nil || !strings.Contains(err.Error(), "nothing was posted") {
		t.Fatalf("error = %v, want fail-closed missing relay", err)
	}
	if homePosts != 0 {
		t.Fatalf("home posts = %d, want 0", homePosts)
	}
}

func TestSynthesisPublishRelayPreflightFailsBeforeHomePost(t *testing.T) {
	rosterPath, secretsPath := writeSynthesisFixture(t, true)
	var homePosts int
	deps := synthesisPublishDeps{
		webhookChannel: func(string) (string, error) { return "C_HOME", nil },
		webhookPost:    func(string, string, string) error { homePosts++; return nil },
		newBot:         func(string) (synthesisChannelPoster, error) { return nil, errors.New("bad relay") },
		stdout:         &bytes.Buffer{},
	}
	err := cmdSynthesisPublish([]string{
		"--from", "memex", "--roster", rosterPath, "--secrets", secretsPath,
		"curated rollup",
	}, deps)
	if err == nil || !strings.Contains(err.Error(), "nothing was posted") {
		t.Fatalf("error = %v, want preflight failure", err)
	}
	if homePosts != 0 {
		t.Fatalf("home posts = %d, want 0", homePosts)
	}
}

func TestSynthesisPublishRejectsWebhookBoundOutsideOwnedChannels(t *testing.T) {
	rosterPath, secretsPath := writeSynthesisFixture(t, true)
	var posts int
	deps := synthesisPublishDeps{
		webhookChannel: func(string) (string, error) { return "C_OPERATOR", nil },
		webhookPost:    func(string, string, string) error { posts++; return nil },
		newBot: func(string) (synthesisChannelPoster, error) {
			t.Fatal("bot must not be built for a foreign webhook binding")
			return nil, nil
		},
		stdout: &bytes.Buffer{},
	}
	err := cmdSynthesisPublish([]string{
		"--from", "memex", "--roster", rosterPath, "--secrets", secretsPath,
		"curated rollup",
	}, deps)
	if err == nil || !strings.Contains(err.Error(), "does not own") || !strings.Contains(err.Error(), "nothing was posted") {
		t.Fatalf("error = %v, want foreign binding rejection", err)
	}
	if posts != 0 {
		t.Fatalf("posts = %d, want zero", posts)
	}
}

func TestSynthesisPublishDryRunShowsBothRoutesWithoutPosting(t *testing.T) {
	rosterPath, secretsPath := writeSynthesisFixture(t, true)
	var stdout bytes.Buffer
	deps := synthesisPublishDeps{
		webhookChannel: func(string) (string, error) { return "C_HOME", nil },
		webhookPost:    func(string, string, string) error { t.Fatal("dry-run posted webhook"); return nil },
		newBot: func(string) (synthesisChannelPoster, error) {
			return &recordingSynthesisBot{}, nil
		},
		stdout: &stdout,
	}
	if err := cmdSynthesisPublish([]string{
		"--from", "memex", "--roster", rosterPath, "--secrets", secretsPath,
		"--dry-run", "curated rollup",
	}, deps); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	for _, want := range []string{"C_HOME via seat webhook", "C_SECONDARY via relay bot", "2 unique channel(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestUniqueNonEmptyDeduplicatesOperatorChannel(t *testing.T) {
	got := uniqueNonEmpty([]string{" C_CMD ", "C_SECONDARY", "C_CMD", "", "C_SECONDARY"})
	want := []string{"C_CMD", "C_SECONDARY"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueNonEmpty = %v, want %v", got, want)
	}
}
