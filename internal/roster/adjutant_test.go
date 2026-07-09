package roster

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestLayerSidecarPaths(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ack", LayerAckPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-alive"},
		{"settled", LayerSettledPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-settled"},
		{"awaiting", LayerAwaitingPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-awaiting"},
		{"buffer", LayerBufferPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-buffer.json"},
		{"frontier", LayerFrontierPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-frontier.json"},
		{"arbitration-audit", LayerArbitrationAuditPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-arbitration-audit.jsonl"},
		{"delivered", LayerBufferDeliveredPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-buffer-delivered.json"},
		{"charter", LayerCharterPath("/state", "alpha-xo"), "/state/flotilla-alpha-xo-adjutant-charter.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestResolveLayerClockPath(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "flotilla-xo-alive")
	layer := filepath.Join(dir, "flotilla-cos-alive")

	if got := ResolveLayerClockPath(dir, "", "", "flotilla-xo-alive", "alive"); got != legacy {
		t.Fatalf("empty coordinator = %q, want legacy %q", got, legacy)
	}
	if got := ResolveLayerClockPath(dir, "cos", "/explicit", "flotilla-xo-alive", "alive"); got != "/explicit" {
		t.Fatalf("explicit = %q", got)
	}
	if got := ResolveLayerClockPath(dir, "cos", "", "flotilla-xo-alive", "alive"); got != layer {
		t.Fatalf("greenfield = %q, want %q", got, layer)
	}
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLayerClockPath(dir, "cos", "", "flotilla-xo-alive", "alive"); got != legacy {
		t.Fatalf("legacy fallback = %q, want %q", got, legacy)
	}
	if err := os.WriteFile(layer, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLayerClockPath(dir, "cos", "", "flotilla-xo-alive", "alive"); got != layer {
		t.Fatalf("layer preferred = %q, want %q", got, layer)
	}
}
