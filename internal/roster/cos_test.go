package roster

import (
	"path/filepath"
	"testing"
)

func TestCosLedger_DefaultResolvedAtLoad(t *testing.T) {
	// cos_agent set, cos_ledger unset → CosLedger defaults to <roster-dir>/context-ledger.md.
	p := writeRoster(t, `{
	  "operator_user_id":"U","cos_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"}]}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := filepath.Join(filepath.Dir(p), "context-ledger.md")
	if cfg.CosLedger != want {
		t.Errorf("CosLedger = %q, want default %q", cfg.CosLedger, want)
	}
}

func TestCosLedger_ExplicitPathPreserved(t *testing.T) {
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","cos_agent":"meta-xo","cos_ledger":"/var/lib/flotilla/ledger.md",
	  "agents":[{"name":"meta-xo"}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CosLedger != "/var/lib/flotilla/ledger.md" {
		t.Errorf("CosLedger = %q, want the explicit path", cfg.CosLedger)
	}
}

func TestCosLedger_InertWhenCosAgentUnset(t *testing.T) {
	// No cos_agent → the mirror is inert: CosLedger is forced EMPTY even though a
	// cos_ledger value is present, so the single gate (cfg.CosLedger != "") correctly
	// reports "inactive" and a stray cos_ledger can never activate the feature.
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","cos_ledger":"/should/be/ignored.md",
	  "agents":[{"name":"a"}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CosAgent != "" {
		t.Errorf("CosAgent = %q, want empty (inert)", cfg.CosAgent)
	}
	if cfg.CosLedger != "" {
		t.Errorf("CosLedger = %q, want empty when cos_agent is unset (the feature must be inert)", cfg.CosLedger)
	}
}

func TestIsXO(t *testing.T) {
	// Legacy: the top-level xo_agent is the XO.
	legacy, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C","xo_agent":"xo",
	  "agents":[{"name":"xo"},{"name":"backend"}]}`))
	if !legacy.IsXO("xo") {
		t.Error("legacy xo_agent should be an XO")
	}
	if legacy.IsXO("backend") {
		t.Error("a desk is not an XO")
	}
	if legacy.IsXO("") {
		t.Error("empty name is not an XO")
	}

	// Federated: every binding's xo_agent is an XO; members are not.
	fed, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`))
	if !fed.IsXO("meta-xo") || !fed.IsXO("alpha-xo") {
		t.Error("federated binding xo_agents should be XOs")
	}
	if fed.IsXO("alpha-be") {
		t.Error("a desk member is not an XO")
	}
}

func TestIsCoordinator(t *testing.T) {
	fed, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"cos",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"},{"name":"cos"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo","cos"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`))
	for _, coord := range []string{"meta-xo", "alpha-xo", "cos"} {
		if !fed.IsCoordinator(coord) {
			t.Errorf("IsCoordinator(%q) = false, want true", coord)
		}
	}
	if fed.IsCoordinator("alpha-be") {
		t.Error("a desk member is not a coordinator")
	}
	// cos_agent without IsXO overlap still counts.
	onlyCos, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U","cos_agent":"cos",
	  "agents":[{"name":"xo"},{"name":"cos"}]}`))
	if !onlyCos.IsCoordinator("cos") {
		t.Error("cos_agent alone should be a coordinator")
	}
}

func TestChannelForXO(t *testing.T) {
	fed, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`))
	if ch, ok := fed.ChannelForXO("alpha-xo"); !ok || ch != "C_ALPHA" {
		t.Errorf("ChannelForXO(alpha-xo) = %q,%v; want C_ALPHA,true", ch, ok)
	}
	if ch, ok := fed.ChannelForXO("meta-xo"); !ok || ch != "C_CMD" {
		t.Errorf("ChannelForXO(meta-xo) = %q,%v; want C_CMD,true", ch, ok)
	}
	if _, ok := fed.ChannelForXO("alpha-be"); ok {
		t.Error("ChannelForXO(a desk) should be ok=false")
	}

	// Legacy single-fleet: the synthesized binding's channel.
	legacy, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C1","xo_agent":"xo",
	  "agents":[{"name":"xo"}]}`))
	if ch, ok := legacy.ChannelForXO("xo"); !ok || ch != "C1" {
		t.Errorf("legacy ChannelForXO(xo) = %q,%v; want C1,true", ch, ok)
	}
}

func TestChannelForAgent(t *testing.T) {
	fed, _ := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`))
	// An owner resolves to the channel it owns.
	if ch, ok := fed.ChannelForAgent("alpha-xo"); !ok || ch != "C_ALPHA" {
		t.Errorf("ChannelForAgent(alpha-xo) = %q,%v; want C_ALPHA,true (owner)", ch, ok)
	}
	// A pure DESK owns no channel but is a MEMBER of its parent's — resolved via membership
	// (this is the cubic #362 P2 fix; without it a desk relay tags no channel).
	if ch, ok := fed.ChannelForAgent("alpha-be"); !ok || ch != "C_ALPHA" {
		t.Errorf("ChannelForAgent(alpha-be) = %q,%v; want C_ALPHA,true (member)", ch, ok)
	}
	// An agent in no binding at all resolves to ok=false.
	if _, ok := fed.ChannelForAgent("ghost"); ok {
		t.Error("ChannelForAgent(unknown) should be ok=false")
	}
}
