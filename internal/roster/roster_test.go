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
		"agents": [{"name": "xo"}, {"name": "frontend", "tmux_title": "fe"}]
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
	if got := (Agent{Name: "xo"}).Title(); got != "xo" {
		t.Errorf("Title fallback = %q, want xo", got)
	}
	if got := (Agent{Name: "frontend", TmuxTitle: "fe"}).Title(); got != "fe" {
		t.Errorf("Title explicit = %q, want fe", got)
	}
}

func TestLoadRejectsEmptyAndDup(t *testing.T) {
	cases := map[string]string{
		"no agents":                      `{"agents": []}`,
		"empty name":                     `{"agents": [{"name": ""}]}`,
		"duplicate":                      `{"agents": [{"name": "a"}, {"name": "a"}]}`,
		"shared title":                   `{"agents": [{"name": "a", "tmux_title": "x"}, {"name": "b", "tmux_title": "x"}]}`,
		"title collides with other name": `{"agents": [{"name": "x"}, {"name": "b", "tmux_title": "x"}]}`,
		"tab in name":                    `{"agents": [{"name": "a\tb"}]}`,
		"newline in name":                `{"agents": [{"name": "a\nb"}]}`,
		"tab in tmux_title":              `{"agents": [{"name": "a", "tmux_title": "x\ty"}]}`,
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
	body := "# comment\nFLOTILLA_BOT_TOKEN=tok\nFLOTILLA_WEBHOOK_XO=https://example/h\n\nFLOTILLA_WEBHOOK_FRONTEND=https://example/v\n"
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
	if got := WebhookKey("frontend"); got != "FLOTILLA_WEBHOOK_FRONTEND" {
		t.Errorf("WebhookKey = %q", got)
	}
	url, err := s.Webhook("xo")
	if err != nil || url != "https://example/h" {
		t.Errorf("Webhook(xo) = %q, %v", url, err)
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

func TestLoadAgentSurface(t *testing.T) {
	// surface is optional (default applied at resolve time) and carried verbatim.
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"a"},{"name":"b","surface":"grok"}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, _ := cfg.Agent("a")
	if a.Surface != "" {
		t.Errorf("agent a surface = %q, want \"\" (default applied at resolve)", a.Surface)
	}
	b, _ := cfg.Agent("b")
	if b.Surface != "grok" {
		t.Errorf("agent b surface = %q, want grok", b.Surface)
	}
}

func TestLoadWatchConfigValidation(t *testing.T) {
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"},{"name":"frontend"}],"xo_agent":"xo","heartbeat_interval":"20m"}`)); err != nil {
		t.Fatalf("valid watch config rejected: %v", err)
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"nope"}`)); err == nil {
		t.Error("xo_agent not in agents = nil error, want error")
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"heartbeat_interval":"20mins"}`)); err == nil {
		t.Error("bad heartbeat_interval = nil error, want error")
	}
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"heartbeat_interval":"0"}`)); err != nil {
		t.Errorf("heartbeat_interval \"0\" (disabled) should be valid: %v", err)
	}
}

func TestLoadChangeDetectorValidation(t *testing.T) {
	// change_detector with a positive interval + valid ping mode is accepted.
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"xo","heartbeat_interval":"20m","change_detector":true,"liveness_ping_mode":"none"}`))
	if err != nil {
		t.Fatalf("valid change_detector config rejected: %v", err)
	}
	if !cfg.ChangeDetector || cfg.LivenessPingMode != "none" {
		t.Errorf("change_detector fields not loaded: %+v", cfg)
	}
	// change_detector with no interval → error (it would never tick).
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"change_detector":true}`)); err == nil {
		t.Error("change_detector without heartbeat_interval = nil error, want error")
	}
	// change_detector with disabled interval ("0") → error too.
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"change_detector":true,"heartbeat_interval":"0"}`)); err == nil {
		t.Error("change_detector with heartbeat_interval \"0\" = nil error, want error")
	}
	// invalid liveness_ping_mode → error.
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"heartbeat_interval":"20m","liveness_ping_mode":"sometimes"}`)); err == nil {
		t.Error("invalid liveness_ping_mode = nil error, want error")
	}
	// empty liveness_ping_mode is valid (defaults to none at use).
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"heartbeat_interval":"20m"}`)); err != nil {
		t.Errorf("empty liveness_ping_mode should be valid: %v", err)
	}
}

// Track C slice 1 — authority-domains-org-chart: primary_repo + worktree_path.
func TestLoadPrimaryRepoAndWorktreePath(t *testing.T) {
	// Absent fields remain valid (backward compatible).
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"xo"},{"name":"backend"}]}`))
	if err != nil {
		t.Fatalf("absent primary_repo/worktree_path rejected: %v", err)
	}
	b, _ := cfg.Agent("backend")
	if b.PrimaryRepo != "" || b.WorktreePath != "" {
		t.Errorf("absent fields should stay empty, got primary_repo=%q worktree_path=%q", b.PrimaryRepo, b.WorktreePath)
	}

	// Valid owner/name + absolute worktree_path (generic fixture paths only).
	cfg, err = Load(writeTemp(t, `{
		"agents":[
			{"name":"xo","primary_repo":"acme/flotilla"},
			{"name":"backend","primary_repo":"acme/backend-api","worktree_path":"/srv/fleet/desks/backend"},
			{"name":"frontend","primary_repo":"Acme-Org/web.app","worktree_path":"/srv/fleet/desks/frontend"}
		]
	}`))
	if err != nil {
		t.Fatalf("valid primary_repo/worktree_path rejected: %v", err)
	}
	xo, _ := cfg.Agent("xo")
	if xo.PrimaryRepo != "acme/flotilla" {
		t.Errorf("xo.PrimaryRepo = %q, want acme/flotilla", xo.PrimaryRepo)
	}
	if xo.WorktreePath != "" {
		t.Errorf("xo.WorktreePath = %q, want empty", xo.WorktreePath)
	}
	be, _ := cfg.Agent("backend")
	if be.PrimaryRepo != "acme/backend-api" || be.WorktreePath != "/srv/fleet/desks/backend" {
		t.Errorf("backend fields = %q / %q", be.PrimaryRepo, be.WorktreePath)
	}
	fe, _ := cfg.Agent("frontend")
	if fe.PrimaryRepo != "Acme-Org/web.app" {
		t.Errorf("frontend.PrimaryRepo = %q", fe.PrimaryRepo)
	}
}

func TestLoadRejectsInvalidPrimaryRepo(t *testing.T) {
	cases := map[string]string{
		"filesystem path": `{"agents":[{"name":"a","primary_repo":"/srv/fleet/repos/flotilla"}]}`,
		"relative path":   `{"agents":[{"name":"a","primary_repo":"./local-repo"}]}`,
		"https url":       `{"agents":[{"name":"a","primary_repo":"https://github.com/acme/flotilla"}]}`,
		"git@ url":        `{"agents":[{"name":"a","primary_repo":"git@github.com:acme/flotilla"}]}`,
		"extra segment":   `{"agents":[{"name":"a","primary_repo":"acme/flotilla/extra"}]}`,
		"missing name":    `{"agents":[{"name":"a","primary_repo":"acme/"}]}`,
		"missing owner":   `{"agents":[{"name":"a","primary_repo":"/flotilla"}]}`,
		"no slash":        `{"agents":[{"name":"a","primary_repo":"flotilla-only"}]}`,
		"whitespace":      `{"agents":[{"name":"a","primary_repo":"acme/flo tilla"}]}`,
		"traversal":       `{"agents":[{"name":"a","primary_repo":"acme/../etc"}]}`,
		"backslash":       `{"agents":[{"name":"a","primary_repo":"acme\\flotilla"}]}`,
		"invalid char":    `{"agents":[{"name":"a","primary_repo":"acme/flo@tilla"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadRejectsInvalidWorktreePath(t *testing.T) {
	cases := map[string]string{
		"relative":           `{"agents":[{"name":"a","worktree_path":"desks/backend"}]}`,
		"empty-ish relative": `{"agents":[{"name":"a","worktree_path":"./backend"}]}`,
		"tab":                `{"agents":[{"name":"a","worktree_path":"/srv/fleet/\tdesks/backend"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
	// worktree_path alone (no primary_repo) is accepted when absolute.
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"a","worktree_path":"/srv/fleet/desks/backend"}]}`))
	if err != nil {
		t.Fatalf("absolute worktree_path alone rejected: %v", err)
	}
	a, _ := cfg.Agent("a")
	if a.WorktreePath != "/srv/fleet/desks/backend" {
		t.Errorf("WorktreePath = %q", a.WorktreePath)
	}
}

// flotilla.example.json is the committed reference for primary_repo / worktree_path.
func TestExampleRosterLoadsPrimaryRepo(t *testing.T) {
	p := filepath.Join("..", "..", "flotilla.example.json")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load(flotilla.example.json): %v", err)
	}
	be, err := cfg.Agent("backend")
	if err != nil {
		t.Fatalf("backend agent: %v", err)
	}
	if be.PrimaryRepo != "acme/backend-api" {
		t.Errorf("backend.PrimaryRepo = %q, want acme/backend-api", be.PrimaryRepo)
	}
	if be.WorktreePath != "/srv/fleet/desks/backend" {
		t.Errorf("backend.WorktreePath = %q, want /srv/fleet/desks/backend", be.WorktreePath)
	}
	xo, err := cfg.Agent("xo")
	if err != nil {
		t.Fatalf("xo agent: %v", err)
	}
	if xo.PrimaryRepo != "acme/flotilla" {
		t.Errorf("xo.PrimaryRepo = %q, want acme/flotilla", xo.PrimaryRepo)
	}
}
