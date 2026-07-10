package roster

import (
	"slices"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/org"
)

func TestOrgDAG_ParityWithSynthesis(t *testing.T) {
	cfg := loadLiveShape(t)
	d := cfg.Org()
	if d == nil {
		t.Fatal("Org() nil after Load")
	}
	if d.Source != org.SourceDerived {
		t.Errorf("source=%q", d.Source)
	}
	if d.Root != "meta" && d.Root != cfg.effectiveXOAgent() {
		// live shape has no xo_agent set — effectiveXO may be empty
		t.Logf("root=%q effectiveXO=%q", d.Root, cfg.effectiveXOAgent())
	}
	for _, a := range cfg.Agents {
		name := a.Name
		gotP := d.Parents[name]
		wantP := cfg.AgentsAbove(name)
		if !sortedEqual(gotP, wantP) {
			t.Errorf("Org.Parents(%q)=%v AgentsAbove=%v", name, gotP, wantP)
		}
		gotC := d.Children[name]
		wantC := cfg.AgentsBelow(name)
		if !sortedEqual(gotC, wantC) {
			t.Errorf("Org.Children(%q)=%v AgentsBelow=%v", name, gotC, wantC)
		}
	}
}

func TestOrgDAG_PrimaryParentMatchesOwningXOFederated(t *testing.T) {
	cfg := loadLiveShape(t)
	// Federated home-channel: OwningXO uses AgentsAbove[0] == PrimaryParent
	for _, desk := range []string{"alpha-be", "alpha-fe", "beta-be", "alpha-xo", "beta-xo"} {
		want := cfg.OwningXO(desk, "meta")
		// When AgentsAbove non-empty, PrimaryParent must match OwningXO
		if above := cfg.AgentsAbove(desk); len(above) > 0 {
			if got := cfg.Org().PrimaryParent(desk); got != want {
				t.Errorf("desk %q PrimaryParent=%q OwningXO=%q", desk, got, want)
			}
		}
	}
}

func TestLoad_CycleErrorNamesAgentsAndChannels(t *testing.T) {
	_, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"x"},{"name":"y"}],
	  "channels":[{"channel_id":"CX","xo_agent":"x","members":["y"]},
	              {"channel_id":"CY","xo_agent":"y","members":["x"]}]}`))
	if err == nil {
		t.Fatal("expected cycle refuse")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cycle") {
		t.Errorf("want cycle in error: %v", err)
	}
	// Improved text names both agents and both channels
	for _, needle := range []string{"x", "y", "CX", "CY"} {
		if !strings.Contains(msg, needle) {
			t.Errorf("cycle error should mention %q; got: %v", needle, err)
		}
	}
}

func TestOrgDAG_LegacyStar(t *testing.T) {
	legacy, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C1","xo_agent":"xo",
	  "agents":[{"name":"xo"},{"name":"backend"},{"name":"frontend"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := legacy.Org()
	if d == nil {
		t.Fatal("nil org")
	}
	// Legacy star: AgentsAbove empty for leaves; Children of xo may be empty via synthesis
	// (single binding is xo's own — AgentsBelow(xo) is empty). Parity still holds.
	for _, a := range legacy.Agents {
		if !slices.Equal(sorted(d.Parents[a.Name]), sorted(legacy.AgentsAbove(a.Name))) {
			t.Errorf("parents %q: org=%v above=%v", a.Name, d.Parents[a.Name], legacy.AgentsAbove(a.Name))
		}
	}
}

func sorted(ss []string) []string {
	out := slices.Clone(ss)
	slices.Sort(out)
	return out
}
