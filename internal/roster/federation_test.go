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
		"mutually exclusive with legacy": `{
		  "operator_user_id":"U","channel_id":"C","xo_agent":"a",
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
		"agent is xo of two bindings": `{
		  "operator_user_id":"U","agents":[{"name":"a"}],
		  "channels":[{"channel_id":"C1","xo_agent":"a"},{"channel_id":"C2","xo_agent":"a"}]}`,
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
