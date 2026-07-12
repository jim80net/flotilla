package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestDisplayName(t *testing.T) {
	if got := displayName("podcast-reader"); got != "Podcast Reader" {
		t.Fatalf("displayName = %q", got)
	}
}

func TestProvisionDiscordDryRunNeedsNoHostCredentials(t *testing.T) {
	if err := cmdProvisionDiscord([]string{"acceptance-canary", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchRosterChannelsAppliesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla.json")
	raw := `{"guild_id":"100","xo_agent":"canary-xo","agents":[{"name":"canary-xo"}],"custom_future_field":{"keep":true}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	add := []roster.Channel{{ChannelID: "10", XOAgent: "canary-xo", Role: "fleet-command"}, {ChannelID: "11", XOAgent: "canary-xo", Role: "project"}}
	if err := patchRosterChannels(path, add); err != nil {
		t.Fatal(err)
	}
	if err := patchRosterChannels(path, add); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("bindings = %d, want 2", len(cfg.Channels))
	}
	var doc map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["custom_future_field"]; !ok {
		t.Fatal("safe patch dropped an unknown field")
	}
}
