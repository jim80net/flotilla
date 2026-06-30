package roster

import "testing"

func TestAutoSwitchEligible(t *testing.T) {
	cfg, err := Load(writeRoster(t, `{
	  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"cos",
	  "agents":[
	    {"name":"meta-xo"},
	    {"name":"cos"},
	    {"name":"alpha-xo"},
	    {"name":"backend"},
	    {"name":"trader","approval_sensitive":true}
	  ],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","members":["alpha-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["backend"]}
	  ]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := []struct {
		agent string
		want  bool
	}{
		{"meta-xo", false},
		{"alpha-xo", false},
		{"cos", false},
		{"trader", false},
		{"backend", true},
	}
	for _, tc := range cases {
		if got := cfg.AutoSwitchEligible(tc.agent); got != tc.want {
			t.Errorf("AutoSwitchEligible(%q) = %v, want %v", tc.agent, got, tc.want)
		}
	}
}
