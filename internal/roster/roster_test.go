package roster

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roster.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp roster: %v", err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `{
		"guild_id": "g", "channel_id": "c", "operator_user_id": "op",
		"agents": [{"name": "hydra-ops"}, {"name": "v12-dev", "tmux_title": "v12"}]
	}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OperatorUserID != "op" {
		t.Errorf("OperatorUserID = %q, want op", cfg.OperatorUserID)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("len(Agents) = %d, want 2", len(cfg.Agents))
	}
}

func TestAgentTitleFallback(t *testing.T) {
	if got := (Agent{Name: "hydra-ops"}).Title(); got != "hydra-ops" {
		t.Errorf("Title fallback = %q, want hydra-ops", got)
	}
	if got := (Agent{Name: "v12-dev", TmuxTitle: "v12"}).Title(); got != "v12" {
		t.Errorf("Title explicit = %q, want v12", got)
	}
}

func TestLoadRejectsEmptyAndDup(t *testing.T) {
	cases := map[string]string{
		"no agents":                      `{"agents": []}`,
		"empty name":                     `{"agents": [{"name": ""}]}`,
		"duplicate":                      `{"agents": [{"name": "a"}, {"name": "a"}]}`,
		"shared title":                   `{"agents": [{"name": "a", "tmux_title": "x"}, {"name": "b", "tmux_title": "x"}]}`,
		"title collides with other name": `{"agents": [{"name": "x"}, {"name": "b", "tmux_title": "x"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestAgentLookup(t *testing.T) {
	cfg := &Config{Agents: []Agent{{Name: "a"}, {Name: "b"}}}
	if _, err := cfg.Agent("b"); err != nil {
		t.Errorf("Agent(b): %v", err)
	}
	if _, err := cfg.Agent("missing"); err == nil {
		t.Error("Agent(missing) = nil error, want error")
	}
}

func TestSecrets(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.env")
	body := "# comment\nFLOTILLA_BOT_TOKEN=tok\nFLOTILLA_WEBHOOK_HYDRA_OPS=https://example/h\n\nFLOTILLA_WEBHOOK_V12_DEV=https://example/v\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}
	s, err := LoadSecrets(p)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	if s.BotToken() != "tok" {
		t.Errorf("BotToken = %q, want tok", s.BotToken())
	}
	if got := WebhookKey("v12-dev"); got != "FLOTILLA_WEBHOOK_V12_DEV" {
		t.Errorf("WebhookKey = %q", got)
	}
	url, err := s.Webhook("hydra-ops")
	if err != nil || url != "https://example/h" {
		t.Errorf("Webhook(hydra-ops) = %q, %v", url, err)
	}
	if _, err := s.Webhook("nope"); err == nil {
		t.Error("Webhook(nope) = nil error, want error")
	}
}

func TestSecretsRejectsMalformedLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets.env")
	// A non-blank, non-comment line with no '=' must be rejected, not skipped.
	body := "FLOTILLA_BOT_TOKEN=tok\nGARBAGE_NO_EQUALS\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}
	if _, err := LoadSecrets(p); err == nil {
		t.Error("LoadSecrets(malformed) = nil error, want error")
	}
}

func TestLoadWatchConfigValidation(t *testing.T) {
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"},{"name":"v12-dev"}],"xo_agent":"hydra-ops","heartbeat_interval":"20m"}`)); err != nil {
		t.Fatalf("valid watch config rejected: %v", err)
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"}],"xo_agent":"nope"}`)); err == nil {
		t.Error("xo_agent not in agents = nil error, want error")
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"}],"heartbeat_interval":"20mins"}`)); err == nil {
		t.Error("bad heartbeat_interval = nil error, want error")
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"}],"heartbeat_interval":"0"}`)); err != nil {
		t.Errorf("heartbeat_interval \"0\" (disabled) should be valid: %v", err)
	}
}

func TestLoadIdleContextReset(t *testing.T) {
	// Opt-in (default false): absent ⇒ false; explicit true ⇒ true.
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"}],"xo_agent":"hydra-ops"}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.IdleContextReset {
		t.Error("idle_context_reset absent should default to false (opt-in)")
	}
	cfg, err = Load(writeTemp(t, `{"agents":[{"name":"hydra-ops"}],"xo_agent":"hydra-ops","idle_context_reset":true}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.IdleContextReset {
		t.Error("idle_context_reset:true should parse as true")
	}
}
