package roster

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRoster writes a roster JSON to a temp file and returns its path.
func writeRoster(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_FederationChannels_Valid(t *testing.T) {
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id": "U",
	  "agents": [
	    {"name":"meta-xo"},
	    {"name":"alpha-xo"}, {"name":"alpha-be"},
	    {"name":"beta-xo"}
	  ],
	  "channels": [
	    {"role":"fleet-command","channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo","beta-xo"]},
	    {"role":"project","channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]},
	    {"role":"project","channel_id":"C_BETA","xo_agent":"beta-xo"}
	  ],
	  "cos_agent": "meta-xo"
	}`))
	if err != nil {
		t.Fatalf("valid federated roster rejected: %v", err)
	}
	// alpha-xo is BOTH a member of fleet-command AND the xo of #fleet-alpha — the
	// recursion must be allowed.
	b, ok := cfg.BindingForChannel("C_ALPHA")
	if !ok || b.XOAgent != "alpha-xo" {
		t.Fatalf("BindingForChannel(C_ALPHA) = %+v ok=%v", b, ok)
	}
	cmd, _ := cfg.BindingForChannel("C_CMD")
	found := false
	for _, m := range cmd.Members {
		if m == "alpha-xo" {
			found = true
		}
	}
	if !found {
		t.Errorf("fleet-command should have alpha-xo as a member: %+v", cmd)
	}
	if _, ok := cfg.BindingForChannel("nope"); ok {
		t.Error("BindingForChannel(nope) should be ok=false")
	}
}

func TestLoad_FederationChannels_FailClosed(t *testing.T) {
	cases := map[string]string{
		"legacy channel_id and channels[] are mutually exclusive": `{
		  "operator_user_id":"U","channel_id":"C",
		  "agents":[{"name":"a"}],
		  "channels":[{"channel_id":"C2","xo_agent":"a"}]}`,
		"channel bound twice": `{
		  "operator_user_id":"U","agents":[{"name":"a"},{"name":"b"}],
		  "channels":[{"channel_id":"C","xo_agent":"a"},{"channel_id":"C","xo_agent":"b"}]}`,
		"xo not an agent": `{
		  "operator_user_id":"U","agents":[{"name":"a"}],
		  "channels":[{"channel_id":"C","xo_agent":"ghost"}]}`,
		"member not an agent": `{
		  "operator_user_id":"U","agents":[{"name":"a"}],
		  "channels":[{"channel_id":"C","xo_agent":"a","members":["ghost"]}]}`,
		"empty channel_id": `{
		  "operator_user_id":"U","agents":[{"name":"a"}],
		  "channels":[{"channel_id":"","xo_agent":"a"}]}`,
		"cos_agent not an agent": `{
		  "operator_user_id":"U","channel_id":"C","xo_agent":"a",
		  "agents":[{"name":"a"}],"cos_agent":"ghost"}`,
	}
	for name, body := range cases {
		if _, err := Load(writeRoster(t, body)); err == nil {
			t.Errorf("%s: expected load error, got nil", name)
		}
	}
}

func TestLoad_FederationChannels_XOHubsMultipleChannels(t *testing.T) {
	// An agent MAY be the XO (hub) of MULTIPLE channels (XO→channels is one-to-many) —
	// e.g. a flotilla XO is primary in both its C2-group channel and its own command
	// channel. Channel→XO stays one-to-one (each channel routes to exactly one XO).
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"meta"},{"name":"alpha"},{"name":"be"}],
	  "channels":[
	    {"channel_id":"C_HOME","xo_agent":"alpha","members":["meta"]},
	    {"channel_id":"C_CMD","xo_agent":"alpha","members":["be"]},
	    {"channel_id":"C_META","xo_agent":"meta","members":["alpha"]}]}`))
	if err != nil {
		t.Fatalf("an XO hubbing multiple channels should load, got: %v", err)
	}
	// Each channel still routes to exactly one XO (one relay per channel).
	if b, ok := cfg.BindingForChannel("C_CMD"); !ok || b.XOAgent != "alpha" {
		t.Errorf("C_CMD should route to alpha, got %+v ok=%v", b, ok)
	}
	if b, ok := cfg.BindingForChannel("C_HOME"); !ok || b.XOAgent != "alpha" {
		t.Errorf("C_HOME should route to alpha, got %+v ok=%v", b, ok)
	}
	// @-resolution / member scope is per-channel and unaffected: be is addressable in
	// C_CMD but not C_HOME.
	home, _ := cfg.BindingForChannel("C_HOME")
	cmd, _ := cfg.BindingForChannel("C_CMD")
	if contains(home.Members, "be") {
		t.Errorf("be must NOT be a member of C_HOME (per-channel member scope): %+v", home.Members)
	}
	if !contains(cmd.Members, "be") {
		t.Errorf("be must be a member of C_CMD: %+v", cmd.Members)
	}
	// ChannelForXO returns the XO's FIRST-listed (primary/home) binding.
	if ch, ok := cfg.ChannelForXO("alpha"); !ok || ch != "C_HOME" {
		t.Errorf("ChannelForXO(alpha) = %q ok=%v, want C_HOME (first-listed)", ch, ok)
	}
	// A channel bound twice (same channel → two XOs) is STILL rejected (the preserved
	// one-relay-per-channel invariant).
	if _, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","agents":[{"name":"a"},{"name":"b"}],
	  "channels":[{"channel_id":"C","xo_agent":"a"},{"channel_id":"C","xo_agent":"b"}]}`)); err == nil {
		t.Error("a channel bound to two XOs must still fail (one relay per channel)")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestLoad_FederationChannels_WithPrimaryXO(t *testing.T) {
	// channels[] MAY carry a top-level xo_agent — it is this daemon's primary/clock
	// XO (heartbeat/status/voice target), orthogonal to the bindings, NOT a mutual-
	// exclusion error. It picks which XO a federated relay daemon clocks (the meta-XO)
	// instead of silently defaulting to Agents[0]. Bindings() still routes on channels[],
	// ignoring xo_agent for routing.
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","xo_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["alpha-be"]}]}`))
	if err != nil {
		t.Fatalf("channels[] + primary xo_agent should load, got: %v", err)
	}
	if cfg.XOAgent != "meta-xo" {
		t.Errorf("primary XOAgent = %q, want meta-xo (the clock target)", cfg.XOAgent)
	}
	// Routing is by channels[], unaffected by the primary xo_agent.
	if bs := cfg.Bindings(); len(bs) != 2 || bs[0].ChannelID != "C_CMD" {
		t.Errorf("Bindings should route on channels[] (2 bindings), got %+v", bs)
	}
}

func TestBindings_LegacyBackwardCompat(t *testing.T) {
	// A single-fleet roster (legacy form) must synthesize ONE binding whose members
	// are ALL agents (pre-federation @name-resolves-against-all behavior).
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C_ONE","xo_agent":"xo",
	  "agents":[{"name":"xo"},{"name":"backend"},{"name":"data"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bs := cfg.Bindings()
	if len(bs) != 1 {
		t.Fatalf("legacy roster should yield 1 binding, got %d", len(bs))
	}
	if bs[0].ChannelID != "C_ONE" || bs[0].XOAgent != "xo" {
		t.Errorf("legacy binding = %+v, want {C_ONE, xo}", bs[0])
	}
	if len(bs[0].Members) != 3 {
		t.Errorf("legacy binding members = %v, want all 3 agents", bs[0].Members)
	}
}

func TestBindings_LegacyXODefaultsToFirstAgent(t *testing.T) {
	// No xo_agent → the legacy binding's XO defaults to the first agent (matching
	// watch's own rule).
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","channel_id":"C","agents":[{"name":"first"},{"name":"second"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if bs := cfg.Bindings(); len(bs) != 1 || bs[0].XOAgent != "first" {
		t.Errorf("legacy binding XO = %+v, want first", bs)
	}
}

func TestBindings_ClockOnlyNoChannel(t *testing.T) {
	// No channel_id and no channels[] (a clock-only daemon) → no bindings.
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","agents":[{"name":"xo"}],"xo_agent":"xo"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bs := cfg.Bindings(); bs != nil {
		t.Errorf("clock-only roster should yield no bindings, got %+v", bs)
	}
}
