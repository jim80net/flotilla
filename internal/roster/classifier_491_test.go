package roster

import "testing"

// fleetShapeFixture mirrors the federation topology implicated in #491 using generic roles.
// product-skill-dev carries coordinator:false — the explicit declaration that ends the
// supervisor-as-member inference hole for execution desks on fleet-command.
const fleetShapeFixture = `{
  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
  "agents":[
    {"name":"meta-xo"},{"name":"venture-a-xo"},{"name":"venture-b-xo"},
    {"name":"venture-c-xo"},{"name":"venture-c-build"},
    {"name":"product-skill-dev","coordinator":false},
    {"name":"dash-desk"},{"name":"macro-desk"},{"name":"research-desk"},
    {"name":"harness-desk"},{"name":"memex-desk"},{"name":"trial-xo"}
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

// bareNoChannelFixture is a clock-only roster: no channel_id, no channels[]. Agents[0]
// must NOT be treated as the implicit primary XO (Bindings returns nil; effectiveXOAgent
// must mirror that).
const bareNoChannelFixture = `{
  "operator_user_id":"U",
  "agents":[{"name":"infra"},{"name":"data"}]
}`

// agents0ExecutionDeskFixture is federated with Agents[0] a plain execution desk while
// the real primary is explicit xo_agent — the legacy Agents[0] fallback must not apply.
const agents0ExecutionDeskFixture = `{
  "operator_user_id":"U","xo_agent":"meta-xo",
  "agents":[{"name":"backend"},{"name":"meta-xo"},{"name":"frontend"}],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","backend","frontend"]}
  ]
}`

// xoObserverShapeFixture pins the #502 rail-regression shape: xo-observer is a genuine
// coordinator via inferred span (sole cos+XO supervisor on trial-xo's home, both on
// fleet-command) and must NOT be excluded by execution-desk overrides.
const xoObserverShapeFixture = `{
  "xo_agent": "cos",
  "agents": [{"name": "cos"}, {"name": "xo-fleet"}, {"name": "xo-proj"}, {"name": "xo-observer"},
    {"name": "trial-xo"}, {"name": "backend"}, {"name": "frontend"}, {"name": "data"}, {"name": "builder"}],
  "channels": [
    {"channel_id": "Ccmd", "xo_agent": "cos", "members": ["cos", "xo-fleet", "xo-proj", "xo-observer", "trial-xo", "backend", "frontend", "data", "builder"], "role": "fleet-command"},
    {"channel_id": "Cxf", "xo_agent": "xo-fleet", "members": []},
    {"channel_id": "Cxo", "xo_agent": "xo-observer", "members": []},
    {"channel_id": "Cbe", "xo_agent": "backend", "members": ["xo-fleet"]},
    {"channel_id": "Cfe", "xo_agent": "frontend", "members": ["xo-fleet"]},
    {"channel_id": "Cda", "xo_agent": "data", "members": []},
    {"channel_id": "Cpr", "xo_agent": "xo-proj", "members": ["cos", "xo-proj", "builder"]},
    {"channel_id": "Ctr", "xo_agent": "trial-xo", "members": ["cos", "xo-observer"]}
  ]
}`

func TestCoordinatorSet_BareNoChannelAgents0NotPrimary491(t *testing.T) {
	cfg, err := Load(writeRoster(t, bareNoChannelFixture))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bindings() != nil {
		t.Fatalf("clock-only fixture must have no bindings, got %+v", cfg.Bindings())
	}
	if cfg.effectiveXOAgent() != "" {
		t.Errorf("effectiveXOAgent = %q, want empty on bare no-channel roster", cfg.effectiveXOAgent())
	}
	for _, desk := range []string{"infra", "data"} {
		if cfg.IsCoordinator(desk) {
			t.Errorf("%q must NOT be coordinator on clock-only roster (no implicit Agents[0] primary)", desk)
		}
	}
	if len(cfg.CoordinatorSet()) != 0 {
		t.Errorf("CoordinatorSet = %v, want empty", cfg.CoordinatorSet())
	}
}

func TestCoordinatorSet_Agents0ExecutionDeskNotCoordinator491(t *testing.T) {
	cfg, err := Load(writeRoster(t, agents0ExecutionDeskFixture))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.effectiveXOAgent() != "meta-xo" {
		t.Fatalf("effectiveXOAgent = %q, want meta-xo", cfg.effectiveXOAgent())
	}
	if cfg.IsCoordinator("backend") {
		t.Error("Agents[0] backend must NOT be coordinator when xo_agent names meta-xo")
	}
	if !cfg.IsCoordinator("meta-xo") {
		t.Error("meta-xo must remain coordinator as explicit primary")
	}
}

func TestCoordinatorSet_FleetShape491(t *testing.T) {
	cfg, err := Load(writeRoster(t, fleetShapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.hasSpanOfControl("product-skill-dev") {
		t.Fatal("fixture must retain inferred span on product-skill-dev — explicit false is what opts it out")
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

func TestCoordinatorSet_XoObserverShape502(t *testing.T) {
	cfg, err := Load(writeRoster(t, xoObserverShapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	want := []string{"cos", "xo-fleet", "xo-observer", "xo-proj"}
	if len(set) != len(want) {
		t.Fatalf("CoordinatorSet count = %d, want %d (%v)", len(set), len(want), set)
	}
	for _, coord := range want {
		if !set[coord] {
			t.Errorf("CoordinatorSet missing coordinator %q (set=%v)", coord, set)
		}
	}
	for _, excluded := range []string{"backend", "frontend", "data", "builder", "trial-xo"} {
		if set[excluded] {
			t.Errorf("%q must NOT be a coordinator (#502)", excluded)
		}
	}
}

func TestCoordinatorExplicitFalseOverridesInferredSpan(t *testing.T) {
	cfg, err := Load(writeRoster(t, fleetShapeFixture))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cfg.Agent("product-skill-dev")
	if err != nil {
		t.Fatal(err)
	}
	if a.Coordinator == nil || *a.Coordinator {
		t.Fatal("product-skill-dev must declare coordinator:false")
	}
	if cfg.IsCoordinator("product-skill-dev") {
		t.Error("explicit coordinator:false must override inferred span")
	}
}

func TestLoad_RejectsCoordinatorFalseOnPrimaryXO(t *testing.T) {
	_, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","xo_agent":"meta-xo",
	  "agents":[{"name":"meta-xo","coordinator":false}]}`))
	if err == nil {
		t.Fatal("coordinator:false on primary xo_agent must fail load")
	}
}

// Legacy single-fleet rosters omit xo_agent and default the primary to Agents[0].
func TestLoad_RejectsCoordinatorFalseOnLegacyImplicitPrimary(t *testing.T) {
	_, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C1",
	  "agents":[{"name":"meta-xo","coordinator":false},{"name":"backend"}]}`))
	if err == nil {
		t.Fatal("coordinator:false on legacy implicit primary (Agents[0]) must fail load")
	}
}
