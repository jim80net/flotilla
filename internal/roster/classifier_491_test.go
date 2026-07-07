package roster

import "testing"

// fleetShapeFixture mirrors the live federation shape implicated in #491 (generic roles only).
const fleetShapeFixture = `{
  "operator_user_id":"U","xo_agent":"cos","cos_agent":"cos",
  "agents":[
    {"name":"cos"},{"name":"family-office"},{"name":"memex"},
    {"name":"inventrise-xo"},{"name":"inventrise-build"},
    {"name":"flotilla-dev"},{"name":"flotilla-dash"},
    {"name":"macro-desk-dev"},{"name":"grok-research"},{"name":"codex-harness-dev"},{"name":"codex-memex-dev"},
    {"name":"opencode-trial-xo"}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"cos","role":"fleet-command","members":["flotilla-dev","flotilla-dash","family-office","inventrise-xo"]},
    {"channel_id":"C_FO","xo_agent":"family-office","members":["cos"]},
    {"channel_id":"C_FDEV","xo_agent":"flotilla-dev","members":["cos"]},
    {"channel_id":"C_FDEV_SOLO","xo_agent":"flotilla-dev"},
    {"channel_id":"C_FDASH","xo_agent":"flotilla-dash","members":["flotilla-dev"]},
    {"channel_id":"C_MACRO","xo_agent":"macro-desk-dev","members":["family-office"]},
    {"channel_id":"C_GROK","xo_agent":"grok-research","members":["family-office"]},
    {"channel_id":"C_MEMEX","xo_agent":"memex","members":["cos"]},
    {"channel_id":"C_HARNESS","xo_agent":"codex-harness-dev","members":["cos"]},
    {"channel_id":"C_CMEMEX","xo_agent":"codex-memex-dev","members":["cos","memex"]},
    {"channel_id":"C_INV","xo_agent":"inventrise-xo","members":["cos","inventrise-xo","inventrise-build"]},
    {"channel_id":"C_OTRIAL","xo_agent":"opencode-trial-xo","members":["cos","flotilla-dev"]}
  ]
}`

func TestCoordinatorSet_FleetShape491(t *testing.T) {
	cfg, err := Load(writeRoster(t, fleetShapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	want := map[string]bool{"cos": true, "family-office": true, "memex": true, "inventrise-xo": true}
	for coord := range want {
		if !set[coord] {
			t.Errorf("CoordinatorSet missing coordinator %q (set=%v)", coord, set)
		}
	}
	for _, desk := range []string{"flotilla-dev", "codex-harness-dev", "codex-memex-dev", "flotilla-dash", "macro-desk-dev"} {
		if cfg.IsCoordinator(desk) {
			t.Errorf("execution desk %q must NOT be coordinator (span=%v)", desk, cfg.hasSpanOfControl(desk))
		}
	}
	if len(set) != len(want) {
		t.Errorf("CoordinatorSet count = %d, want %d (%v)", len(set), len(want), set)
	}
	for _, a := range cfg.Agents {
		if set[a.Name] != cfg.IsCoordinator(a.Name) {
			t.Errorf("CoordinatorSet[%q]=%v disagrees with IsCoordinator=%v", a.Name, set[a.Name], cfg.IsCoordinator(a.Name))
		}
	}
}