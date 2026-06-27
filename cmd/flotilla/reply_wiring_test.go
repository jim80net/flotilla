package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// A federated roster: primary XO = meta-xo on #cmd; a c2 channel #alpha whose XO is alpha-xo.
func fedRoster(t *testing.T) *roster.Config {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "roster.json")
	js := `{"agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "xo_agent":"meta-xo","operator_user_id":"U","heartbeat_interval":"20m",
	  "channels":[{"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	              {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`
	if err := os.WriteFile(p, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestIsHotlineToChannelXO(t *testing.T) {
	cfg := fedRoster(t)
	cases := []struct {
		name string
		j    watch.Job
		want bool
	}{
		{"federated c2-channel XO → arm", watch.Job{Kind: "relay", Agent: "alpha-xo", OriginChannel: "C_ALPHA"}, true},
		{"primary XO → arm (#177 unified: the watcher is the return leg for ALL XOs)", watch.Job{Kind: "relay", Agent: "meta-xo", OriginChannel: "C_CMD"}, true},
		{"a channel MEMBER (not the channel's XO) → no arm", watch.Job{Kind: "relay", Agent: "alpha-be", OriginChannel: "C_ALPHA"}, false},
		{"XO addressed from the wrong channel → no arm", watch.Job{Kind: "relay", Agent: "alpha-xo", OriginChannel: "C_CMD"}, false},
		{"non-relay (detector) → no arm", watch.Job{Kind: "detector", Agent: "alpha-xo", OriginChannel: "C_ALPHA"}, false},
		{"empty origin channel → no arm", watch.Job{Kind: "relay", Agent: "alpha-xo", OriginChannel: ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHotlineToChannelXO(cfg, tc.j); got != tc.want {
				t.Errorf("isHotlineToChannelXO = %v, want %v", got, tc.want)
			}
		})
	}
}

// #177: the LEGACY single-channel roster (channel_id + xo_agent, no channels[]) — the synthesized
// binding's XO is the primary, and an operator message to it now ARMS the watcher (the path #177
// changes for a single-fleet deployment; previously excluded).
func TestIsHotlineToChannelXO_LegacyPrimary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "roster.json")
	js := `{"agents":[{"name":"alpha-xo"},{"name":"desk-a"}],"xo_agent":"alpha-xo",
	  "operator_user_id":"U","channel_id":"C_MAIN","heartbeat_interval":"20m"}`
	if err := os.WriteFile(p, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !isHotlineToChannelXO(cfg, watch.Job{Kind: "relay", Agent: "alpha-xo", OriginChannel: "C_MAIN"}) {
		t.Error("legacy single-channel primary XO should ARM (#177 unified)")
	}
	// a heartbeat tick to the primary XO must NOT arm.
	if isHotlineToChannelXO(cfg, watch.Job{Kind: "heartbeat", Agent: "alpha-xo", OriginChannel: "C_MAIN"}) {
		t.Error("a heartbeat tick must not arm the watcher")
	}
}

func TestReplyDest(t *testing.T) {
	cfg := fedRoster(t)
	dir := t.TempDir()
	sp := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(sp, []byte("FLOTILLA_WEBHOOK_ALPHA_XO=https://wh/alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets, err := roster.LoadSecrets(sp)
	if err != nil {
		t.Fatal(err)
	}
	if url, ok := replyDest(cfg, secrets, "C_ALPHA"); !ok || url != "https://wh/alpha" {
		t.Errorf("replyDest(C_ALPHA) = (%q,%v), want the alpha-xo webhook", url, ok)
	}
	if _, ok := replyDest(cfg, secrets, "C_UNKNOWN"); ok {
		t.Error("replyDest(unknown channel) should miss")
	}
	// C_CMD's XO is meta-xo, which has NO webhook provisioned → miss (escalation path).
	if _, ok := replyDest(cfg, secrets, "C_CMD"); ok {
		t.Error("replyDest(C_CMD) should miss when meta-xo has no webhook")
	}
	if _, ok := replyDest(cfg, nil, "C_ALPHA"); ok {
		t.Error("replyDest with nil secrets should miss")
	}
}
