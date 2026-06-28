package roster

import "testing"

// TestHeartbeatEnabled covers #183 per-agent desk-heartbeat opt-out resolution: the recursive
// detector re-engages an Idle desk on the clock cadence, default-ON for general desks. The primary
// XO is excluded (it has its own clock); an explicit per-agent flag wins; an approval-sensitive
// desk (one that places orders / spends) defaults OFF — the #184 carve-out — until an explicit
// heartbeat:true flips it on, because the claude driver's binary Idle assessment can't yet tell an
// approval-blocked desk from an idle one.
func TestHeartbeatEnabled(t *testing.T) {
	cfg, err := Load(writeTemp(t, `{
	  "xo_agent":"xo","operator_user_id":"U","channel_id":"C","heartbeat_interval":"20m",
	  "agents":[
	    {"name":"xo"},
	    {"name":"backend"},
	    {"name":"frontend","heartbeat":false},
	    {"name":"data","approval_sensitive":true},
	    {"name":"grok-desk","approval_sensitive":true,"heartbeat":true}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		agent string
		want  bool
	}{
		{"xo", false},       // the primary XO is excluded — its own clock drives it
		{"backend", true},   // default-ON (no flag)
		{"frontend", false}, // explicit opt-out
		{"data", false},     // approval-sensitive → default-OFF carve-out (#184)
		{"grok-desk", true}, // approval-sensitive BUT an explicit heartbeat:true wins
		{"unknown", false},  // not a roster agent → nothing to heartbeat
	}
	for _, c := range cases {
		if got := cfg.HeartbeatEnabled(c.agent); got != c.want {
			t.Errorf("HeartbeatEnabled(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
}
