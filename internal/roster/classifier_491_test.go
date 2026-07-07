package roster

import "testing"

// fleetShapeFixture mirrors the federation topology implicated in #491 using generic roles.
const fleetShapeFixture = `{
  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
  "agents":[
    {"name":"meta-xo"},{"name":"venture-a-xo"},{"name":"venture-b-xo"},
    {"name":"venture-c-xo"},{"name":"venture-c-build"},
    {"name":"product-skill-dev"},{"name":"dash-desk"},
    {"name":"macro-desk"},{"name":"research-desk"},{"name":"harness-desk"},{"name":"memex-desk"},
    {"name":"trial-xo"}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["product-skill-dev","dash-desk","venture-a-xo","venture-c-xo"]},
    {"channel_id":"C_VA","xo_agent":"venture-a-xo","members":["meta-xo"]},
    {"channel_id":"C_PSKILL","xo_agent":"product-skill-dev","members":["meta-xo"]},
    {"channel_id":"C_PSKILL_SOLO","xo_agent":"product-skill-dev"},
    {"channel_id":"C_DASH","xo_agent":"dash-desk","members":["product-skill-dev"]},
    {"channel_id":"C_MACRO","xo_agent":"macro-desk","members":["venture-a-xo"]},
    {"channel_id":"C_RESEARCH","xo_agent":"research-desk","members":["venture-a-xo"]},
    {"channel_id":"C_VB","xo_agent":"venture-b-xo","members":["meta-xo"]},
    {"channel_id":"C_HARNESS","xo_agent":"harness-desk","members":["meta-xo"]},
    {"channel_id":"C_MEMEX","xo_agent":"memex-desk","members":["meta-xo","venture-b-xo"]},
    {"channel_id":"C_VC","xo_agent":"venture-c-xo","members":["meta-xo","venture-c-xo","venture-c-build"]},
    {"channel_id":"C_TRIAL","xo_agent":"trial-xo","members":["meta-xo","product-skill-dev"]}
  ]
}`

func TestCoordinatorSet_FleetShape491(t *testing.T) {
	cfg, err := Load(writeRoster(t, fleetShapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	want := map[string]bool{"meta-xo": true, "venture-a-xo": true, "venture-b-xo": true, "venture-c-xo": true}
	for coord := range want {
		if !set[coord] {
			t.Errorf("CoordinatorSet missing coordinator %q (set=%v)", coord, set)
		}
	}
	for _, desk := range []string{"product-skill-dev", "harness-desk", "memex-desk", "dash-desk", "macro-desk"} {
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
