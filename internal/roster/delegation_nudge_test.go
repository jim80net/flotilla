package roster

import "testing"

func TestParseDelegationNudge(t *testing.T) {
	for _, tc := range []struct {
		in    string
		want  DelegationNudgePolicy
		isErr bool
	}{
		{"", DelegationNudgePolicy(""), false},
		{"on", DelegationNudgeOn, false},
		{"OFF", DelegationNudgeOff, false},
		{"nope", "", true},
	} {
		got, err := ParseDelegationNudge(tc.in)
		if tc.isErr {
			if err == nil {
				t.Fatalf("ParseDelegationNudge(%q) = nil error, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseDelegationNudge(%q) err = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseDelegationNudge(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveDelegationNudge(t *testing.T) {
	got, err := ResolveDelegationNudge("", "")
	if err != nil || got != DelegationNudgeOn {
		t.Fatalf("default = %q, err = %v", got, err)
	}
	got, err = ResolveDelegationNudge("off", "")
	if err != nil || got != DelegationNudgeOff {
		t.Fatalf("roster off = %q, err = %v", got, err)
	}
	got, err = ResolveDelegationNudge("on", "off")
	if err != nil || got != DelegationNudgeOff {
		t.Fatalf("env override = %q, err = %v", got, err)
	}
}

func TestLoadDelegationNudgeValidation(t *testing.T) {
	if _, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"xo","heartbeat_interval":"20m","delegation_nudge":"nope"}`)); err == nil {
		t.Fatal("invalid delegation_nudge should fail load")
	}
	cfg, err := Load(writeTemp(t, `{"agents":[{"name":"xo"}],"xo_agent":"xo","heartbeat_interval":"20m","delegation_nudge":"off"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DelegationNudge != "off" {
		t.Fatalf("delegation_nudge = %q", cfg.DelegationNudge)
	}
}
