package roster

import "testing"

// Inter-agent send mirroring is default-off: a roster without mirror_inter_agent
// loads to false, and an explicit true round-trips.
func TestLoadMirrorInterAgent(t *testing.T) {
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MirrorInterAgent {
		t.Error("absent mirror_inter_agent should default to false (off)")
	}

	cfg, err = Load(writeTemp(t, `{"agents":[{"name":"a"}],"mirror_inter_agent":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MirrorInterAgent {
		t.Error("mirror_inter_agent:true should load as true")
	}
}
