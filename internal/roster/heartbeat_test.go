package roster

import (
	"testing"

	"github.com/jim80net/flotilla/internal/backlog"
)

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

// TestHeartbeatWarranted covers the #189 per-recipient JUDGMENT composed ON TOP of the #183 HARD
// eligibility gate. The judgment is I/O-free: the parsed backlog Status is INJECTED. It can only
// SUPPRESS a beat the recipient would otherwise receive — the HARD gate (XO-excl / approval-sensitive
// / explicit heartbeat:false) is checked FIRST and is NEVER overridden by a warrant-true Status.
func TestHeartbeatWarranted(t *testing.T) {
	cfg, err := Load(writeTemp(t, `{
	  "xo_agent":"xo","operator_user_id":"U","channel_id":"C","heartbeat_interval":"20m",
	  "agents":[
	    {"name":"xo"},
	    {"name":"backend"},
	    {"name":"frontend","heartbeat":false},
	    {"name":"data","approval_sensitive":true}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	// A backlog with live actionable work — warrant-true for an ELIGIBLE desk.
	actionable := backlog.Status{Found: true, Unblocked: []string{"- [in-flight] x"}}
	// A cleanly-parsed backlog with NO actionable work (everything blocked/awaiting-auth/done).
	allParked := backlog.Status{Found: true, Blocked: 1, AwaitingAuth: 1, Done: 2}
	// A present-but-sectionless backlog: Found=false, no Unblocked — the !Found fail-safe arm.
	sectionless := backlog.Status{Found: false}
	// A malformed item lands in Unblocked (the parser's fail-safe) — actionable ⇒ warranted.
	malformed := backlog.Status{Found: true, Malformed: 1, Unblocked: []string{"- [whoknows] y"}}

	cases := []struct {
		name  string
		agent string
		st    backlog.Status
		want  bool
		why   string
	}{
		// HARD gate FIRST — the judgment NEVER overrides it, even with actionable work.
		{"xo-excluded-even-with-work", "xo", actionable, false, "primary XO is HARD-excluded; warrant cannot resurrect"},
		{"opt-out-even-with-work", "frontend", actionable, false, "explicit heartbeat:false is HARD; warrant cannot resurrect"},
		{"approval-sensitive-even-with-work", "data", actionable, false, "#184 approval-sensitive HARD carve-out; warrant cannot resurrect"},
		{"unknown-agent", "unknown", actionable, false, "not a roster agent — HARD gate false"},
		// ELIGIBLE desk — now the judgment decides.
		{"eligible-actionable", "backend", actionable, true, "live actionable work ⇒ warranted"},
		{"eligible-all-parked", "backend", allParked, false, "Found && empty actionable set ⇒ NOT warranted"},
		{"eligible-sectionless", "backend", sectionless, true, "!Found fail-safe arm ⇒ warranted (cannot prove no work)"},
		{"eligible-malformed", "backend", malformed, true, "malformed item is in Unblocked (fail-safe) ⇒ warranted"},
		{"eligible-empty-found", "backend", backlog.Status{Found: true}, false, "Found && nothing actionable ⇒ NOT warranted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cfg.HeartbeatWarranted(c.agent, c.st); got != c.want {
				t.Errorf("HeartbeatWarranted(%q, %+v) = %v, want %v (%s)", c.agent, c.st, got, c.want, c.why)
			}
		})
	}

	// HeartbeatEnabled is UNCHANGED by the addition: re-assert a representative resolution.
	if cfg.HeartbeatEnabled("backend") != true || cfg.HeartbeatEnabled("data") != false {
		t.Error("HeartbeatEnabled resolution drifted — the #189 addition must not alter the HARD gate")
	}
}
