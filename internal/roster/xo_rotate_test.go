package roster

import "testing"

func TestParseXORotate(t *testing.T) {
	cases := []struct {
		in    string
		want  XORotatePolicy
		valid bool
	}{
		{"", XORotatePolicy(""), true},
		{"always", XORotateAlways, true},
		{"NEVER", XORotateNever, true},
		{" handoff ", XORotateHandoff, true},
		{"sometimes", "", false},
	}
	for _, tc := range cases {
		got, err := ParseXORotate(tc.in)
		if tc.valid && err != nil {
			t.Fatalf("ParseXORotate(%q) err = %v", tc.in, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("ParseXORotate(%q) = nil error, want error", tc.in)
		}
		if tc.valid && got != tc.want {
			t.Fatalf("ParseXORotate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveXORotate(t *testing.T) {
	if got := ResolveXORotate("", ""); got != XORotateAlways {
		t.Fatalf("unset = %q, want always", got)
	}
	if got := ResolveXORotate("never", ""); got != XORotateNever {
		t.Fatalf("roster never = %q", got)
	}
	if got := ResolveXORotate("always", "never"); got != XORotateNever {
		t.Fatalf("env overrides roster: got %q, want never", got)
	}
}

func TestXORotatePolicyAllowsIdleEdgeRotate(t *testing.T) {
	if !XORotateAlways.AllowsIdleEdgeRotate() {
		t.Fatal("always must allow")
	}
	if XORotatePolicy("").AllowsIdleEdgeRotate() != true {
		t.Fatal("empty must allow (legacy default)")
	}
	for _, p := range []XORotatePolicy{XORotateNever, XORotateHandoff} {
		if p.AllowsIdleEdgeRotate() {
			t.Fatalf("%q must suppress idle-edge rotate", p)
		}
	}
}

func TestLoadXORotateValidation(t *testing.T) {
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"xo","heartbeat_interval":"20m","xo_rotate":"nope"}`)); err == nil {
		t.Fatal("invalid xo_rotate should fail load")
	}
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"xo","heartbeat_interval":"20m","xo_rotate":"never"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XORotate != "never" {
		t.Fatalf("xo_rotate = %q", cfg.XORotate)
	}
}
