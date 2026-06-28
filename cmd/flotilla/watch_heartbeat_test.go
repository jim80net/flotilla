package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// G5 — the desk-continuation prompt (#183). With NO workspace, deskHeartbeatBody resolves the
// package builtin and substitutes {{settle}} with the desk's OWN per-agent settle path. It MUST be:
// (a) NON-AUTHORIZING (advance only already-authorized work; never approve a pending prompt),
// (b) DISTINCT from the XO's continuation prompt (it drops the "context is rotated between steps" and
//     the {{tracker}} read-source the XO prompt carries), and
// (c) carry the agent's settle path + the ack instruction.
func TestDeskContinuationBuiltinNoWorkspace(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir()) // no workspace dir for "backend"

	settle := "/abs/state/flotilla-backend-settled"
	ack := "\n(To ack you are alive, run: touch /x/alive)"
	got, err := deskHeartbeatBody("backend", func(a string) string {
		if a == "backend" {
			return settle
		}
		return ""
	}, ack)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unsubstituted placeholder remains: %q", got)
	}
	// NON-AUTHORIZING: never approve a pending tool/permission/approval prompt on a heartbeat.
	for _, want := range []string{
		"ALREADY-AUTHORIZED",
		"do NOT approve",
		"touch " + settle,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("desk prompt missing non-authorizing fragment %q\nfull: %q", want, got)
		}
	}
	if !strings.HasSuffix(got, ack) {
		t.Error("desk prompt MUST append the ack instruction (else a beaten desk never acks)")
	}
	// DISTINCT from the XO's prompt: the desk is NOT context-rotated by this design, and it has no
	// {{tracker}} read-source. Those XO-only fragments must be ABSENT.
	for _, absent := range []string{
		"rotated between steps",
		"goal+task tracker",
	} {
		if strings.Contains(got, absent) {
			t.Errorf("desk prompt must NOT carry the XO-only fragment %q\nfull: %q", absent, got)
		}
	}
}

// G5 — the wakeAgent dispatcher (today it rejects every non-synthesis kind) must handle the new
// WakeDeskHeartbeat kind by enqueuing an audit-suppressed Kind:"detector" job to the named desk with
// the desk-continuation body. (WakeSynthesis still works; an unknown kind is still rejected.)
func TestWakeAgentDispatchesDeskHeartbeat(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())

	var enq []watch.Job
	settle := func(a string) string { return "/abs/state/flotilla-" + a + "-settled" }
	dispatch := newDeskHeartbeatDispatch(func(j watch.Job) { enq = append(enq, j) }, settle, "\n(ack)")

	dispatch("backend")

	if len(enq) != 1 {
		t.Fatalf("WakeDeskHeartbeat must enqueue exactly one job, got %d", len(enq))
	}
	j := enq[0]
	if j.Agent != "backend" {
		t.Errorf("job must target the desk, got %q", j.Agent)
	}
	if j.Kind != "detector" {
		t.Errorf("desk-heartbeat job MUST be Kind:%q (audit-suppressed), got %q", "detector", j.Kind)
	}
	if !strings.Contains(j.Message, "ALREADY-AUTHORIZED") || !strings.Contains(j.Message, settle("backend")) {
		t.Errorf("job body must be the non-authorizing desk prompt carrying backend's settle path; got %q", j.Message)
	}
}

// G6 — the cap-escalation (#183 §8e) raises ONE loud alert that NAMES the wedged desk + routes to its
// OWNING XO (the channel it is a member of / its parent), via the loud Alert path — NOT a quiet wake.
func TestDeskEscalateRoutesToOwningXOViaLoudAlert(t *testing.T) {
	// Legacy star: the leaf "backend" is owned by "xo" (the channel it is a member of). AgentsAbove is
	// empty for the leaf, so this proves the §8e channel-membership fallback (not AgentsAbove).
	rosterPath := writeRosterFile(t, `{
	  "operator_user_id":"U","channel_id":"C1","xo_agent":"xo",
	  "agents":[{"name":"xo"},{"name":"backend"},{"name":"frontend"}]}`)
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}

	var alerts []string
	escalate := newDeskEscalate(cfg, "xo", func(m string) { alerts = append(alerts, m) })

	escalate("backend")

	if len(alerts) != 1 {
		t.Fatalf("a cap-escalation must raise exactly one loud alert, got %d", len(alerts))
	}
	if !strings.Contains(alerts[0], "backend") {
		t.Errorf("the escalation must NAME the wedged desk; got %q", alerts[0])
	}
	if !strings.Contains(alerts[0], "xo") {
		t.Errorf("the escalation must route to the owning XO (xo); got %q", alerts[0])
	}
}

// G7.2 — federation double-drive invariant (#183 §8i): a federated sub-XO that runs its OWN daemon is
// opted OUT of THIS (parent) daemon's desk heartbeat via the roster `heartbeat: false` flag, so it is
// driven by exactly one clock (its own), never both. This daemon cannot introspect another daemon, so
// the opt-out is the roster flag — and HeartbeatEnabled (the seam the detector gates on) honors it.
func TestDeskHeartbeatFederationDoubleDriveOptOut(t *testing.T) {
	rosterPath := writeRosterFile(t, `{
	  "xo_agent":"meta","operator_user_id":"U","heartbeat_interval":"20m",
	  "agents":[{"name":"meta"},{"name":"sub-xo","heartbeat":false},{"name":"leaf"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta","role":"fleet-command","members":["meta","sub-xo","leaf"]},
	    {"channel_id":"C_SUB","xo_agent":"sub-xo","members":["meta"]},
	    {"channel_id":"C_LEAF","xo_agent":"leaf","members":["sub-xo"]}]}`)
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	// The sub-XO that runs its own daemon is opted OUT of the parent's heartbeat (no double-drive).
	if cfg.HeartbeatEnabled("sub-xo") {
		t.Error("a federated sub-XO marked heartbeat:false must be opted OUT of the parent's desk heartbeat (§8i double-drive)")
	}
	// A general leaf below it is still heartbeated by this daemon (belt-and-suspenders recursion).
	if !cfg.HeartbeatEnabled("leaf") {
		t.Error("a general leaf desk must still be heartbeated default-ON")
	}
	// The primary XO is never desk-heartbeated (its own clock).
	if cfg.HeartbeatEnabled("meta") {
		t.Error("the primary XO must never be desk-heartbeated")
	}
}
