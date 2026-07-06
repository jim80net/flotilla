package roster

import "testing"

func TestAdjutantFor_ResolvesBinding(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{Name: "xo"},
			{Name: "xo-adj", AdjutantFor: "xo"},
			{Name: "alpha-xo"},
			{Name: "alpha-adj", AssistantFor: "alpha-xo"},
		},
	}
	if got := cfg.AdjutantFor("xo"); got != "xo-adj" {
		t.Errorf("AdjutantFor(xo) = %q, want xo-adj", got)
	}
	if got := cfg.AdjutantFor("alpha-xo"); got != "alpha-adj" {
		t.Errorf("AdjutantFor(alpha-xo) = %q, want alpha-adj", got)
	}
	if got := cfg.AdjutantFor("missing"); got != "" {
		t.Errorf("AdjutantFor(missing) = %q, want empty", got)
	}
}

func TestLoadRejectsInvalidAdjutantBindings(t *testing.T) {
	cases := map[string]string{
		"self adjutant": `{
			"agents": [{"name": "xo", "adjutant_for": "xo"}]
		}`,
		"non-coordinator target": `{
			"agents": [{"name": "xo"}, {"name": "backend"}, {"name": "adj", "adjutant_for": "backend"}]
		}`,
		"duplicate adjutant": `{
			"agents": [
				{"name": "xo"},
				{"name": "a-adj", "adjutant_for": "xo"},
				{"name": "b-adj", "adjutant_for": "xo"}
			]
		}`,
		"conflicting aliases": `{
			"agents": [{"name": "xo"}, {"name": "adj", "adjutant_for": "xo", "assistant_for": "alpha-xo"}]
		}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Fatalf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadAdjutantBindingsFederated(t *testing.T) {
	body := `{
		"guild_id": "g", "xo_agent": "xo",
		"agents": [
			{"name": "xo"},
			{"name": "xo-adj", "adjutant_for": "xo"},
			{"name": "alpha-xo"},
			{"name": "alpha-adj", "adjutant_for": "alpha-xo"},
			{"name": "backend"}
		],
		"channels": [
			{"channel_id": "fc", "xo_agent": "xo", "role": "fleet-command", "members": ["xo"]},
			{"channel_id": "a", "xo_agent": "alpha-xo", "members": ["xo"]},
			{"channel_id": "b", "xo_agent": "backend", "members": ["alpha-xo"]}
		]
	}`
	cfg, err := Load(writeTemp(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdjutantFor("xo") != "xo-adj" {
		t.Errorf("xo adjutant = %q", cfg.AdjutantFor("xo"))
	}
	if cfg.StackableWakes {
		t.Error("stackable_wakes should default false")
	}
}

func TestUrgentMaterialMatch(t *testing.T) {
	cfg := &Config{UrgentWindows: []UrgentWindow{{Match: "approval_sensitive"}}}
	if !cfg.UrgentMaterial([]string{"frontend approval_sensitive throttle"}) {
		t.Fatal("expected urgent match")
	}
	if cfg.UrgentMaterial([]string{"backend Working→Idle"}) {
		t.Fatal("expected no urgent match")
	}
}

func TestLoadRejectsUnsafeAgentNames(t *testing.T) {
	cases := map[string]string{
		"slash in name":  `{"agents": [{"name": "foo/bar"}]}`,
		"dotdot in name": `{"agents": [{"name": "../xo"}]}`,
		"slash adjutant target": `{
			"agents": [{"name": "xo"}, {"name": "adj", "adjutant_for": "alpha/xo"}]
		}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Fatalf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadRejectsAdjutantWithoutPingMode(t *testing.T) {
	body := `{
		"change_detector": true,
		"heartbeat_interval": "20m",
		"liveness_ping_mode": "none",
		"agents": [{"name": "xo"}, {"name": "xo-adj", "adjutant_for": "xo"}]
	}`
	if _, err := Load(writeTemp(t, body)); err == nil {
		t.Fatal("expected error when adjutant configured with ping mode none")
	}
}

func TestLayerAckPath(t *testing.T) {
	got := LayerAckPath("/state", "alpha-xo")
	want := "/state/flotilla-alpha-xo-alive"
	if got != want {
		t.Errorf("LayerAckPath = %q, want %q", got, want)
	}
}
