package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "flotilla-launch.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp launch file: %v", err)
	}
	return p
}

// rosterAgents is the standard fixture roster set used across the table tests.
func rosterAgents() map[string]bool {
	return map[string]bool{"xo": true, "backend": true, "frontend": true}
}

func TestCommittedLaunchExampleValidates(t *testing.T) {
	// flotilla-launch.example.json is the committed #466 policy-shape reference;
	// it must load cleanly for the example agents (partition-safe generic paths).
	p := filepath.Join("..", "..", "flotilla-launch.example.json")
	agents := map[string]bool{"xo": true, "backend": true}
	cfg, err := Load(p, agents)
	if err != nil {
		t.Fatalf("Load committed example: %v", err)
	}
	xo, ok := cfg.Recipe("xo")
	if !ok {
		t.Fatal("example missing xo recipe")
	}
	slots := xo.Slots()
	if len(slots) < 3 {
		t.Fatalf("xo chain len = %d, want primary + >=2 fallbacks", len(slots))
	}
	if slots[0].Name != "primary" || slots[0].Model != "opus" {
		t.Errorf("xo primary = %+v, want primary/opus", slots[0])
	}
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `{
		"agents": {
			"xo": {
				"launch": "claude -w xo",
				"cwd": "/srv/fleet/main",
				"tmux": "flotilla:xo",
				"state": ".claude/handoffs/latest.md"
			},
			"backend": {
				"launch": "cd /tmp && claude --continue",
				"cwd": "/srv/fleet/secondary"
			}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := cfg.Recipe("xo")
	if !ok {
		t.Fatal("Recipe(xo) not found")
	}
	if r.Launch != "claude -w xo" {
		t.Errorf("Launch = %q", r.Launch)
	}
	if r.Cwd != "/srv/fleet/main" {
		t.Errorf("Cwd = %q", r.Cwd)
	}
	if r.Tmux != "flotilla:xo" {
		t.Errorf("Tmux = %q", r.Tmux)
	}
	if r.State != ".claude/handoffs/latest.md" {
		t.Errorf("State = %q", r.State)
	}
	// An agent in the roster but absent from the launch file is not resumable.
	if _, ok := cfg.Recipe("frontend"); ok {
		t.Error("Recipe(frontend) found, want absent (declared but not resumable)")
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"missing launch":     `{"agents": {"xo": {"cwd": "/abs"}}}`,
		"empty launch":       `{"agents": {"xo": {"launch": "", "cwd": "/abs"}}}`,
		"tab in launch":      `{"agents": {"xo": {"launch": "a\tb", "cwd": "/abs"}}}`,
		"newline in launch":  `{"agents": {"xo": {"launch": "a\nb", "cwd": "/abs"}}}`,
		"cr in launch":       `{"agents": {"xo": {"launch": "a\rb", "cwd": "/abs"}}}`,
		"missing cwd":        `{"agents": {"xo": {"launch": "claude"}}}`,
		"empty cwd":          `{"agents": {"xo": {"launch": "claude", "cwd": ""}}}`,
		"relative cwd":       `{"agents": {"xo": {"launch": "claude", "cwd": "relative/path"}}}`,
		"dot cwd":            `{"agents": {"xo": {"launch": "claude", "cwd": "."}}}`,
		"tab in cwd":         `{"agents": {"xo": {"launch": "claude", "cwd": "/a\tb"}}}`,
		"newline in cwd":     `{"agents": {"xo": {"launch": "claude", "cwd": "/a\nb"}}}`,
		"tab in tmux":        `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": "a\tb:w"}}}`,
		"newline in tmux":    `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": "a:w\nx"}}}`,
		"tmux no colon":      `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": "flotilla"}}}`,
		"tmux empty session": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": ":w"}}}`,
		"tmux empty window":  `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": "s:"}}}`,
		"tmux double colon":  `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "tmux": "a:b:c"}}}`,
		"tab in state":       `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "state": "a\tb"}}}`,
		"newline in state":   `{"agents": {"xo": {"launch": "claude", "cwd": "/abs", "state": "a\nb"}}}`,
		"unknown agent":      `{"agents": {"not-a-real-agent": {"launch": "claude", "cwd": "/abs"}}}`,
		"malformed json":     `{"agents": {`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body), rosterAgents()); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadRejectsDuplicateTmuxTarget(t *testing.T) {
	// Two distinct agents pointing at the same tmux target would resume into the
	// same window — rejected (mirrors roster's shared-title rejection).
	p := writeTemp(t, `{
		"agents": {
			"xo": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:shared"},
			"backend": {"launch": "claude", "cwd": "/b", "tmux": "flotilla:shared"}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err == nil {
		t.Error("Load(duplicate tmux target) = nil error, want error")
	}
}

func TestLoadAllowsDistinctTmuxAndEmptyTmux(t *testing.T) {
	// Distinct tmux targets, plus multiple recipes with NO tmux target (empty is
	// not a shared value), all load cleanly.
	p := writeTemp(t, `{
		"agents": {
			"xo": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:xo"},
			"backend": {"launch": "claude", "cwd": "/b"},
			"frontend": {"launch": "claude", "cwd": "/c"}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err != nil {
		t.Errorf("Load(distinct + empty tmux) = %v, want nil", err)
	}
}

func TestLoadAbsentFileErrors(t *testing.T) {
	// A genuinely absent launch file is an error (resume handles "no recipe"
	// distinctly, but Load itself surfaces the read failure).
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := Load(missing, rosterAgents()); err == nil {
		t.Error("Load(absent file) = nil error, want error")
	}
}

func TestLoadEmptyAgentsIsValid(t *testing.T) {
	// An empty agents map is not malformed — it just declares no recipes; every
	// resume then errors "no launch recipe" (a distinct, clear message).
	p := writeTemp(t, `{"agents": {}}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load(empty agents): %v", err)
	}
	if _, ok := cfg.Recipe("xo"); ok {
		t.Error("Recipe found in empty config, want absent")
	}
}

func TestDefaultPath(t *testing.T) {
	cases := map[string]string{
		"/etc/flotilla/flotilla.json": "/etc/flotilla/flotilla-launch.json",
		"flotilla.json":               "flotilla-launch.json",
		"./cfg/roster.json":           "cfg/flotilla-launch.json",
	}
	for in, want := range cases {
		if got := DefaultPath(in); got != want {
			t.Errorf("DefaultPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecipeWithNoChainResolvesFlatLaunchAsPrimary(t *testing.T) {
	// Backward-compat: a recipe with NO primary/fallbacks must resolve the flat
	// `launch` as the implied primary slot, so every existing launch file keeps
	// working byte-identically. The implied surface is empty here (the caller —
	// resume/switch — fills it from the roster Agent.surface, or the default).
	p := writeTemp(t, `{
		"agents": {
			"xo": {"launch": "claude -w xo", "cwd": "/srv/fleet/main"}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := cfg.Recipe("xo")
	if !ok {
		t.Fatal("Recipe(xo) not found")
	}
	slots := r.Slots()
	if len(slots) != 1 {
		t.Fatalf("Slots() len = %d, want 1 (flat launch is the implied primary)", len(slots))
	}
	if slots[0].Name != "primary" {
		t.Errorf("Slots()[0].Name = %q, want %q", slots[0].Name, "primary")
	}
	if slots[0].Launch != "claude -w xo" {
		t.Errorf("Slots()[0].Launch = %q, want the flat launch", slots[0].Launch)
	}
	if slots[0].Surface != "" {
		t.Errorf("Slots()[0].Surface = %q, want empty (caller supplies the roster/default surface)", slots[0].Surface)
	}
}

func TestRecipeWithChainResolvesPrimaryThenFallbacks(t *testing.T) {
	// An explicit chain: primary first, then fallbacks in declared order. Each
	// slot's launch + surface come from the slot, not the flat launch.
	p := writeTemp(t, `{
		"agents": {
			"xo": {
				"launch": "claude -w xo",
				"cwd": "/srv/fleet/main",
				"primary":   {"surface": "claude-code", "launch": "claude -w xo", "provider": "anthropic"},
				"fallbacks": [
					{"surface": "grok",     "launch": "grok -w xo",     "provider": "xai"},
					{"surface": "opencode", "launch": "opencode -w xo", "provider": "zai"}
				]
			}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, _ := cfg.Recipe("xo")
	slots := r.Slots()
	if len(slots) != 3 {
		t.Fatalf("Slots() len = %d, want 3 (primary + 2 fallbacks)", len(slots))
	}
	want := []struct{ name, surface, launch, provider string }{
		{"primary", "claude-code", "claude -w xo", "anthropic"},
		{"fallback-0", "grok", "grok -w xo", "xai"},
		{"fallback-1", "opencode", "opencode -w xo", "zai"},
	}
	for i, w := range want {
		if slots[i].Name != w.name {
			t.Errorf("Slots()[%d].Name = %q, want %q", i, slots[i].Name, w.name)
		}
		if slots[i].Surface != w.surface {
			t.Errorf("Slots()[%d].Surface = %q, want %q", i, slots[i].Surface, w.surface)
		}
		if slots[i].Launch != w.launch {
			t.Errorf("Slots()[%d].Launch = %q, want %q", i, slots[i].Launch, w.launch)
		}
		if slots[i].Provider != w.provider {
			t.Errorf("Slots()[%d].Provider = %q, want %q", i, slots[i].Provider, w.provider)
		}
	}
}

func TestChainPreservesProviderDistinctFromSubscriptionID(t *testing.T) {
	// Two slots may share a provider with DIFFERENT subscription_ids: the
	// provider is the failover-targeting key (poison the provider on a
	// server-side throttle), the subscription_id is a billing/account bucket
	// WITHIN it. They must be preserved distinctly — never collapsed.
	p := writeTemp(t, `{
		"agents": {
			"xo": {
				"launch": "claude -w xo",
				"cwd": "/srv/fleet/main",
				"primary": {"surface": "claude-code", "launch": "claude -w xo", "provider": "anthropic", "subscription_id": "anthropic-work"},
				"fallbacks": [
					{"surface": "claude-code", "launch": "claude --profile personal -w xo", "provider": "anthropic", "subscription_id": "anthropic-personal"}
				]
			}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, _ := cfg.Recipe("xo")
	slots := r.Slots()
	if len(slots) != 2 {
		t.Fatalf("Slots() len = %d, want 2", len(slots))
	}
	if slots[0].Provider != "anthropic" || slots[1].Provider != "anthropic" {
		t.Errorf("providers = %q, %q; want both %q", slots[0].Provider, slots[1].Provider, "anthropic")
	}
	if slots[0].SubscriptionID == slots[1].SubscriptionID {
		t.Errorf("subscription_ids both %q; want distinct buckets within the shared provider", slots[0].SubscriptionID)
	}
	if slots[0].SubscriptionID != "anthropic-work" || slots[1].SubscriptionID != "anthropic-personal" {
		t.Errorf("subscription_ids = %q, %q; want %q, %q", slots[0].SubscriptionID, slots[1].SubscriptionID, "anthropic-work", "anthropic-personal")
	}
}

func TestLoadRejectsBadChainSlot(t *testing.T) {
	// Per-slot validation runs at load (surface-agnostic — the surface
	// known-driver check is deferred to switch/resume time). An empty `launch`
	// or a control char in a primary OR fallback slot is rejected, never
	// resolving a half-valid chain.
	cases := map[string]string{
		"primary empty launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary": {"surface": "claude-code", "launch": "", "provider": "anthropic"}}}}`,
		"primary tab in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary": {"surface": "claude-code", "launch": "a\tb", "provider": "anthropic"}}}}`,
		"primary newline in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary": {"surface": "claude-code", "launch": "a\nb", "provider": "anthropic"}}}}`,
		"primary cr in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary": {"surface": "claude-code", "launch": "a\rb", "provider": "anthropic"}}}}`,
		"fallback empty launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary":   {"surface": "claude-code", "launch": "claude", "provider": "anthropic"},
			"fallbacks": [{"surface": "grok", "launch": "", "provider": "xai"}]}}}`,
		"fallback tab in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary":   {"surface": "claude-code", "launch": "claude", "provider": "anthropic"},
			"fallbacks": [{"surface": "grok", "launch": "a\tb", "provider": "xai"}]}}}`,
		"fallback newline in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary":   {"surface": "claude-code", "launch": "claude", "provider": "anthropic"},
			"fallbacks": [{"surface": "grok", "launch": "a\nb", "provider": "xai"}]}}}`,
		"fallback cr in launch": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary":   {"surface": "claude-code", "launch": "claude", "provider": "anthropic"},
			"fallbacks": [{"surface": "grok", "launch": "a\rb", "provider": "xai"}]}}}`,
		"primary bad subscription_id": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary": {"surface": "claude-code", "launch": "claude", "provider": "anthropic", "subscription_id": "Bad-ID"}}}`,
		"fallback bad subscription_id": `{"agents": {"xo": {"launch": "claude", "cwd": "/abs",
			"primary":   {"surface": "claude-code", "launch": "claude", "provider": "anthropic"},
			"fallbacks": [{"surface": "claude-code", "launch": "claude", "provider": "anthropic", "subscription_id": "../escape"}]}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body), rosterAgents()); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadAcceptsValidChain(t *testing.T) {
	// A well-formed chain (every slot has a non-empty, control-char-free launch)
	// loads cleanly — the positive companion to TestLoadRejectsBadChainSlot.
	p := writeTemp(t, `{
		"agents": {
			"xo": {
				"launch": "claude -w xo",
				"cwd": "/srv/fleet/main",
				"primary":   {"surface": "claude-code", "launch": "claude -w xo", "provider": "anthropic", "model": "opus"},
				"fallbacks": [{"surface": "grok", "launch": "grok -w xo", "provider": "xai"}]
			}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err != nil {
		t.Errorf("Load(valid chain) = %v, want nil", err)
	}
}

func TestValidTmuxTarget(t *testing.T) {
	cases := map[string]bool{
		"flotilla:xo": true,
		"s:w":         true,
		"s:w.0":       false, // trailing ".<digits>" = a tmux pane index, rejected
		"s:rel-1.2":   false, // also a trailing ".<digits>" → pane-index ambiguous
		"s:my.app":    true,  // a non-numeric dot is a legit window name
		"flotilla":    false,
		":w":          false,
		"s:":          false,
		"a:b:c":       false,
		"a b:w":       false, // space in session
		"s:w x":       false, // space in window
		"":            false,
	}
	for in, want := range cases {
		if got := validTmuxTarget(in); got != want {
			t.Errorf("validTmuxTarget(%q) = %v, want %v", in, got, want)
		}
	}
}
