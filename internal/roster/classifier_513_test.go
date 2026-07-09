package roster

import (
	"sort"
	"testing"
)

// soleSupervisorAsMemberFixture pins the live #513 shape with generic roles:
// an execution desk owns a home channel whose only member is the primary supervisor
// (sole-supervisor-as-member), and is also listed as sole supervisor on a peer desk
// home while both sit on fleet-command. Post-#512's simplified path-2 treated that
// peer listing as span and classified the execution desk as a coordinator.
const soleSupervisorAsMemberFixture = `{
  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
  "agents":[
    {"name":"meta-xo"},
    {"name":"build-desk"},
    {"name":"dash-desk"}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command",
      "members":["meta-xo","build-desk","dash-desk"]},
    {"channel_id":"C_BUILD","xo_agent":"build-desk","members":["meta-xo"]},
    {"channel_id":"C_DASH","xo_agent":"dash-desk","members":["build-desk"]}
  ]
}`

// projectLeadDualDeskFixture: a genuine project lead (not primary) appears as sole
// supervisor on two desk homes and must remain a coordinator without an explicit
// coordinator field (#513 must not over-suppress multi-desk venture leads).
const projectLeadDualDeskFixture = `{
  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
  "agents":[
    {"name":"meta-xo"},{"name":"project-xo"},
    {"name":"backend"},{"name":"frontend"}
  ],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command",
      "members":["meta-xo","project-xo","backend","frontend"]},
    {"channel_id":"C_PROJ","xo_agent":"project-xo","members":["meta-xo"]},
    {"channel_id":"C_BE","xo_agent":"backend","members":["project-xo"]},
    {"channel_id":"C_FE","xo_agent":"frontend","members":["project-xo"]}
  ]
}`

func TestIsCoordinator_SoleSupervisorAsMemberNotCoordinator513(t *testing.T) {
	cfg, err := Load(writeRoster(t, soleSupervisorAsMemberFixture))
	if err != nil {
		t.Fatal(err)
	}
	// Desk home lists only the primary — path-1 must not fire (meta-xo is IsXO).
	if cfg.hasSpanOfControl("build-desk") {
		t.Errorf("build-desk hasSpanOfControl=true; own channel members=[meta-xo] must not confer path-1 span")
	}
	// Peer sole listing on dash-desk while both are fleet-command members must not
	// confer path-2 span (#513 root cause post-#512 simplification).
	if cfg.IsCoordinator("build-desk") {
		t.Error("build-desk must NOT be coordinator (sole-supervisor-as-member + fleet-command peer on dash)")
	}
	if cfg.IsCoordinator("dash-desk") {
		t.Error("dash-desk must NOT be coordinator")
	}
	if !cfg.IsCoordinator("meta-xo") {
		t.Error("meta-xo primary must remain coordinator")
	}
	set := cfg.CoordinatorSet()
	if set["build-desk"] || set["dash-desk"] {
		t.Errorf("CoordinatorSet leaked execution desks: %v", set)
	}
	if !set["meta-xo"] {
		t.Errorf("CoordinatorSet missing meta-xo: %v", set)
	}
}

func TestIsCoordinator_ProjectLeadDualDeskStillCoordinator513(t *testing.T) {
	cfg, err := Load(writeRoster(t, projectLeadDualDeskFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.hasSpanOfControl("project-xo") {
		t.Fatal("project-xo must have inferred span via 2+ sole-supervisor desk homes")
	}
	if !cfg.IsCoordinator("project-xo") {
		t.Error("project-xo with two subordinate desk homes must be coordinator without explicit field")
	}
	for _, desk := range []string{"backend", "frontend"} {
		if cfg.IsCoordinator(desk) {
			t.Errorf("%q must NOT be coordinator", desk)
		}
	}
}

func TestIsCoordinator_AbsentCoordinatorFieldNoFleetPeerSpan513(t *testing.T) {
	// Same topology as soleSupervisorAsMemberFixture but proves the fix does not
	// require coordinator:false on the execution desk — inference alone is correct.
	cfg, err := Load(writeRoster(t, soleSupervisorAsMemberFixture))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cfg.Agent("build-desk")
	if err != nil {
		t.Fatal(err)
	}
	if a.Coordinator != nil {
		t.Fatal("build-desk must leave coordinator field absent for this test")
	}
	if cfg.IsCoordinator("build-desk") {
		t.Error("absent coordinator field + sole-supervisor fleet-command peer shape must NOT classify as coordinator")
	}
}

func TestCoordinatorSet_SoleSupervisorShapeMatchesIsCoordinator513(t *testing.T) {
	cfg, err := Load(writeRoster(t, soleSupervisorAsMemberFixture))
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	names := []string{"meta-xo", "build-desk", "dash-desk"}
	for _, n := range names {
		if set[n] != cfg.IsCoordinator(n) {
			t.Errorf("CoordinatorSet[%q]=%v disagrees with IsCoordinator=%v", n, set[n], cfg.IsCoordinator(n))
		}
	}
	var got []string
	for n := range set {
		got = append(got, n)
	}
	sort.Strings(got)
	if len(got) != 1 || got[0] != "meta-xo" {
		t.Errorf("CoordinatorSet = %v, want [meta-xo]", got)
	}
}
