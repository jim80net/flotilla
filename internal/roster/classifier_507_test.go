package roster

import (
	"sort"
	"testing"
)

// supervisorNoOwnedChannelFixture is the #507 hole: shape-2 supervisors (xo-fleet,
// xo-observer) own NO channel of their own — supervision is expressed only via
// membership on desk homes. Pre-fix, IsXO-as-ownership inverted classification
// (desks → coordinators, supervisors → nothing). Generic roles only.
const supervisorNoOwnedChannelFixture = `{
  "operator_user_id":"U","xo_agent":"cos","cos_agent":"cos",
  "agents":[
    {"name":"cos"},{"name":"xo-fleet"},{"name":"xo-proj"},{"name":"xo-observer"},
    {"name":"trial-xo"},{"name":"backend"},{"name":"frontend"},{"name":"data"},{"name":"builder"}
  ],
  "channels":[
    {"channel_id":"Ccmd","xo_agent":"cos","role":"fleet-command",
      "members":["cos","xo-fleet","xo-proj","xo-observer","trial-xo","backend","frontend","data","builder"]},
    {"channel_id":"Cbe","xo_agent":"backend","members":["xo-fleet"]},
    {"channel_id":"Cfe","xo_agent":"frontend","members":["xo-fleet"]},
    {"channel_id":"Cda","xo_agent":"data","members":[]},
    {"channel_id":"Cpr","xo_agent":"xo-proj","members":["cos","xo-proj","builder"]},
    {"channel_id":"Ctr","xo_agent":"trial-xo","members":["cos","xo-observer"]}
  ]
}`

func TestIsCoordinator_SupervisorWithoutOwnedChannel507(t *testing.T) {
	cfg, err := Load(writeRoster(t, supervisorNoOwnedChannelFixture))
	if err != nil {
		t.Fatal(err)
	}

	// Ownership: supervisors own nothing; desks/owners do.
	for _, name := range []string{"xo-fleet", "xo-observer"} {
		if cfg.IsXO(name) {
			t.Errorf("%q must NOT be IsXO (owns no channel) — #507 premise", name)
		}
	}
	for _, name := range []string{"cos", "backend", "frontend", "data", "xo-proj", "trial-xo"} {
		if !cfg.IsXO(name) {
			t.Errorf("%q must be IsXO (channel owner)", name)
		}
	}

	// Coordinators: shape-2 membership supervisors + shape-1 project lead + primary.
	for _, name := range []string{"cos", "xo-fleet", "xo-observer", "xo-proj"} {
		if !cfg.IsCoordinator(name) {
			t.Errorf("%q must be coordinator without owned mirror (#507)", name)
		}
		if !cfg.hasSpanOfControl(name) && name != "cos" {
			// cos is primary (IsCoordinator via effectiveXOAgent); others need span.
			t.Errorf("%q must have span of control via membership shape", name)
		}
	}
	// cos is coordinator via primary seat even without path span checks.
	if !cfg.IsCoordinator("cos") {
		t.Error("cos primary must be coordinator")
	}

	// Execution tier must not invert into the coordinator set.
	for _, name := range []string{"backend", "frontend", "data", "builder", "trial-xo"} {
		if cfg.IsCoordinator(name) {
			t.Errorf("%q is execution-tier and must NOT be coordinator (#507 inversion)", name)
		}
		if cfg.hasSpanOfControl(name) {
			t.Errorf("%q must not have span of control", name)
		}
	}
}

func TestCoordinatorSet_SupervisorWithoutOwnedChannel507(t *testing.T) {
	cfg, err := Load(writeRoster(t, supervisorNoOwnedChannelFixture))
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	want := []string{"cos", "xo-fleet", "xo-observer", "xo-proj"}
	var got []string
	for n := range set {
		got = append(got, n)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("CoordinatorSet = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CoordinatorSet = %v, want %v", got, want)
		}
	}
	// IsCoordinator and CoordinatorSet must agree on every named agent.
	for _, a := range cfg.Agents {
		if set[a.Name] != cfg.IsCoordinator(a.Name) {
			t.Errorf("CoordinatorSet[%q]=%v disagrees with IsCoordinator=%v",
				a.Name, set[a.Name], cfg.IsCoordinator(a.Name))
		}
	}
}

func TestIsCoordinator_SoleSupervisorNoChannelStillCoordinator507(t *testing.T) {
	// Minimal sole-member shape: project-xo owns nothing; two desks list it alone.
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","xo_agent":"cos","cos_agent":"cos",
	  "agents":[
	    {"name":"cos"},{"name":"xo-fleet"},
	    {"name":"backend"},{"name":"frontend"}
	  ],
	  "channels":[
	    {"channel_id":"Ccmd","xo_agent":"cos","role":"fleet-command",
	      "members":["cos","xo-fleet","backend","frontend"]},
	    {"channel_id":"Cbe","xo_agent":"backend","members":["xo-fleet"]},
	    {"channel_id":"Cfe","xo_agent":"frontend","members":["xo-fleet"]}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsXO("xo-fleet") {
		t.Fatal("xo-fleet must own no channel")
	}
	if !cfg.hasSpanOfControl("xo-fleet") {
		t.Fatal("xo-fleet with 2+ sole desk homes must have span")
	}
	if !cfg.IsCoordinator("xo-fleet") {
		t.Error("xo-fleet must be coordinator via sole-supervisor membership only")
	}
	for _, desk := range []string{"backend", "frontend"} {
		if cfg.IsCoordinator(desk) {
			t.Errorf("%q must NOT be coordinator", desk)
		}
	}
}
