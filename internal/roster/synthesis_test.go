package roster

import (
	"slices"
	"strings"
	"testing"
)

// A small federation modeled on the LIVE roster shape that exposed the implement-gate
// P0: a fleet-command BROADCAST channel (role="fleet-command", members = everyone) PLUS
// per-XO home channels (members = parent) PLUS a two-tier project/meta structure. The
// legacy-star example rosters never exercised this; these tests are the regression guard.
//
//	meta            (root; owns the fleet-command broadcast + an empty interaction channel)
//	├── alpha-xo    (project-XO; home members=[meta])
//	│   ├── alpha-be   (boat; home members=[alpha-xo])
//	│   └── alpha-fe   (boat; home members=[alpha-xo])
//	└── beta-xo     (project-XO; home members=[meta])
//	    └── beta-be    (boat; home members=[beta-xo])
const liveShapeRoster = `{
  "operator_user_id":"U",
  "agents":[{"name":"meta"},{"name":"alpha-xo"},{"name":"alpha-be"},{"name":"alpha-fe"},
            {"name":"beta-xo"},{"name":"beta-be"}],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"meta","role":"fleet-command",
     "members":["meta","alpha-xo","alpha-be","alpha-fe","beta-xo","beta-be"]},
    {"channel_id":"C_META_INT","xo_agent":"meta","members":[]},
    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["meta"]},
    {"channel_id":"C_BETA","xo_agent":"beta-xo","members":["meta"]},
    {"channel_id":"C_ALPHA_BE","xo_agent":"alpha-be","members":["alpha-xo"]},
    {"channel_id":"C_ALPHA_FE","xo_agent":"alpha-fe","members":["alpha-xo"]},
    {"channel_id":"C_BETA_BE","xo_agent":"beta-be","members":["beta-xo"]}]}`

func loadLiveShape(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load(writeRoster(t, liveShapeRoster))
	if err != nil {
		t.Fatalf("the live federated shape must load (fleet-command excluded), got: %v", err)
	}
	return cfg
}

func sortedEqual(got, want []string) bool {
	g := slices.Clone(got)
	w := slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	return slices.Equal(g, w)
}

func TestIsFleetCommand(t *testing.T) {
	if !(Channel{Role: "fleet-command"}).IsFleetCommand() {
		t.Error("a role=fleet-command channel must report IsFleetCommand()=true")
	}
	if (Channel{Role: "project"}).IsFleetCommand() || (Channel{}).IsFleetCommand() {
		t.Error("a non-fleet-command channel must report IsFleetCommand()=false")
	}
}

func TestOwnedChannels_IncludesFleetCommand(t *testing.T) {
	cfg := loadLiveShape(t)
	// The meta owns BOTH its fleet-command broadcast and its empty interaction channel —
	// OwnedChannels is the POST target and INCLUDES fleet-command (the meta posts Tier-3
	// into it).
	if got := cfg.OwnedChannels("meta"); !sortedEqual(got, []string{"C_CMD", "C_META_INT"}) {
		t.Errorf("OwnedChannels(meta) = %v; want [C_CMD C_META_INT]", got)
	}
	if got := cfg.OwnedChannels("alpha-xo"); !sortedEqual(got, []string{"C_ALPHA"}) {
		t.Errorf("OwnedChannels(alpha-xo) = %v; want [C_ALPHA]", got)
	}
	if got := cfg.OwnedChannels("alpha-be"); !sortedEqual(got, []string{"C_ALPHA_BE"}) {
		t.Errorf("OwnedChannels(alpha-be) = %v; want [C_ALPHA_BE]", got)
	}
}

func TestAgentsBelow_FleetCommandExcluded(t *testing.T) {
	cfg := loadLiveShape(t)
	// The meta reads the project-XOs (the XOs of their non-fleet-command home channels
	// that list the meta) — NOT the leaves, and NOT itself.
	if got := cfg.AgentsBelow("meta"); !sortedEqual(got, []string{"alpha-xo", "beta-xo"}) {
		t.Errorf("AgentsBelow(meta) = %v; want [alpha-xo beta-xo]", got)
	}
	// A project-XO reads its boats — with NO meta-XO leak (the P0: a member of the
	// broadcast channel must not pull the broadcaster in).
	if got := cfg.AgentsBelow("alpha-xo"); !sortedEqual(got, []string{"alpha-be", "alpha-fe"}) {
		t.Errorf("AgentsBelow(alpha-xo) = %v; want [alpha-be alpha-fe] (no meta leak)", got)
	}
	// A LEAF desk is a member only of the broadcast channel (excluded) — its read set is
	// EMPTY. Under the broken model this wrongly returned {meta}.
	if got := cfg.AgentsBelow("alpha-be"); len(got) != 0 {
		t.Errorf("AgentsBelow(alpha-be) = %v; want [] (a leaf synthesizes nobody)", got)
	}
}

func TestAgentsAbove_IsParentResolver(t *testing.T) {
	cfg := loadLiveShape(t)
	// A boat's parent = the members of its OWN (non-fleet-command) home channel, minus
	// self.
	if got := cfg.AgentsAbove("alpha-be"); !sortedEqual(got, []string{"alpha-xo"}) {
		t.Errorf("AgentsAbove(alpha-be) = %v; want [alpha-xo]", got)
	}
	// A project-XO's parent is the meta.
	if got := cfg.AgentsAbove("alpha-xo"); !sortedEqual(got, []string{"meta"}) {
		t.Errorf("AgentsAbove(alpha-xo) = %v; want [meta]", got)
	}
	// The ROOT (whose only owned channels are fleet-command + an empty one) has NO
	// parent — fleet-command is excluded and the empty channel lists nobody.
	if got := cfg.AgentsAbove("meta"); len(got) != 0 {
		t.Errorf("AgentsAbove(meta) = %v; want [] (the root has no parent)", got)
	}
}

func TestAgentsAbove_IsExactInverseOfAgentsBelow(t *testing.T) {
	cfg := loadLiveShape(t)
	agents := []string{"meta", "alpha-xo", "alpha-be", "alpha-fe", "beta-xo", "beta-be"}
	// C ∈ AgentsBelow(P)  ⟺  P ∈ AgentsAbove(C), for every ordered pair.
	for _, p := range agents {
		below := cfg.AgentsBelow(p)
		for _, c := range agents {
			cBelowP := slices.Contains(below, c)
			pAboveC := slices.Contains(cfg.AgentsAbove(c), p)
			if cBelowP != pAboveC {
				t.Errorf("inverse violated: %q∈AgentsBelow(%q)=%v but %q∈AgentsAbove(%q)=%v",
					c, p, cBelowP, p, c, pAboveC)
			}
		}
	}
}

func TestAgentsAbove_TwoParentsBothOwed(t *testing.T) {
	// A boat whose OWN channel lists two parents marks BOTH owed (the many-to-many case).
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"p1"},{"name":"p2"},{"name":"shared-boat"}],
	  "channels":[
	    {"channel_id":"C_SHARED","xo_agent":"shared-boat","members":["p1","p2"]},
	    {"channel_id":"C_P1","xo_agent":"p1","members":[]},
	    {"channel_id":"C_P2","xo_agent":"p2","members":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AgentsAbove("shared-boat"); !sortedEqual(got, []string{"p1", "p2"}) {
		t.Errorf("AgentsAbove(shared-boat) = %v; want [p1 p2] (both parents owed)", got)
	}
}

func TestSynthesisAccessors_DoNotMutateBindings(t *testing.T) {
	cfg := loadLiveShape(t)
	before := slices.Clone(cfg.Bindings()[0].Members)
	cfg.AgentsBelow("meta")
	cfg.AgentsAbove("alpha-be")
	cfg.OwnedChannels("meta")
	if !slices.Equal(cfg.Bindings()[0].Members, before) {
		t.Error("synthesis accessors mutated a binding's Members slice (read-only-slice contract)")
	}
}

func TestSynthesisAccessors_LegacyAndUnknownAgent(t *testing.T) {
	// Legacy single-binding star: the XO reads every other agent; the desks read nobody
	// (the XO's channel is their only membership and its XO is the XO, not them).
	legacy, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C1","xo_agent":"xo",
	  "agents":[{"name":"xo"},{"name":"a"},{"name":"b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	// In the legacy star the only binding's XO is "xo" and every agent is a member;
	// AgentsBelow(xo) excludes that binding (its XO == xo, self), so xo reads nobody.
	if got := legacy.AgentsBelow("xo"); len(got) != 0 {
		t.Errorf("legacy AgentsBelow(xo) = %v; want [] (the single binding is xo's own)", got)
	}
	// An unknown agent resolves to empty sets, never a panic.
	if got := legacy.AgentsBelow("ghost"); len(got) != 0 {
		t.Errorf("AgentsBelow(ghost) = %v; want []", got)
	}
	if got := legacy.AgentsAbove("ghost"); len(got) != 0 {
		t.Errorf("AgentsAbove(ghost) = %v; want []", got)
	}
}

func TestLoad_AcceptsLiveFleetCommandShape(t *testing.T) {
	// The whole point of the P0 fix: the live broadcast shape must LOAD. Without the
	// fleet-command exclusion the broadcast channel's meta→{everyone} edges close a cycle
	// with the per-XO home channels and Load refuses.
	if _, err := Load(writeRoster(t, liveShapeRoster)); err != nil {
		t.Fatalf("the live fleet-command shape must load; got: %v", err)
	}
}

func TestLoad_AcceptsHomeChannelSelfMembership(t *testing.T) {
	// A channel whose XO is also among its own members (the self-edge) is the normal
	// home-channel shape, not a cycle.
	// xo's channel lists xo itself (the self-membership — a self-edge, excluded); d is a
	// boat whose home channel lists its parent xo. No mutual cycle.
	if _, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"xo"},{"name":"d"}],
	  "channels":[{"channel_id":"C","xo_agent":"xo","members":["xo"]},
	              {"channel_id":"CD","xo_agent":"d","members":["xo"]}]}`)); err != nil {
		t.Fatalf("a home-channel self-membership must load (self-edge excluded); got: %v", err)
	}
}

func TestLoad_RefusesMutualCycleBetweenDistinctChannels(t *testing.T) {
	// Two DISTINCT non-fleet-command channels that each list the other's XO as a member
	// form a genuine synthesis cycle and must refuse to start.
	_, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"x"},{"name":"y"}],
	  "channels":[{"channel_id":"CX","xo_agent":"x","members":["y"]},
	              {"channel_id":"CY","xo_agent":"y","members":["x"]}]}`))
	if err == nil {
		t.Fatal("a mutual membership cycle between two distinct channels must refuse to start")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("cycle error should name the cycle; got: %v", err)
	}
}

func TestLoad_FleetCommandTagBreaksAnOtherwiseCyclicBroadcast(t *testing.T) {
	// The SAME broadcast-shaped roster: WITHOUT the fleet-command tag it cycles and
	// refuses; WITH the tag it loads. This pins the tag's load-bearing role.
	body := func(role string) string {
		return `{
		  "operator_user_id":"U",
		  "agents":[{"name":"meta"},{"name":"p"}],
		  "channels":[
		    {"channel_id":"C_CMD","xo_agent":"meta",` + role + `"members":["meta","p"]},
		    {"channel_id":"C_P","xo_agent":"p","members":["meta"]}]}`
	}
	if _, err := Load(writeRoster(t, body(`"role":"fleet-command",`))); err != nil {
		t.Fatalf("tagged fleet-command broadcast must load; got: %v", err)
	}
	if _, err := Load(writeRoster(t, body(``))); err == nil {
		t.Fatal("an UNTAGGED broadcast channel (meta↔p mutual membership) must refuse — proving role is load-bearing")
	}
}
