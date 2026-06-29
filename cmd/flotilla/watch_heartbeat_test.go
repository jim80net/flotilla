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
//
//	the {{tracker}} read-source the XO prompt carries), and
//
// (c) carry the agent's OWN settle path, and (d) carry NO liveness-ack instruction (a desk is not
//
//	the liveness-acked entity — see the no-ack-pollution regression below, G4 review P1 / #190).
func TestDeskContinuationBuiltinNoWorkspace(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir()) // no workspace dir for "backend"

	settle := "/abs/state/flotilla-backend-settled"
	got, err := deskHeartbeatBody("backend", func(a string) string {
		if a == "backend" {
			return settle
		}
		return ""
	})
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
	// REGRESSION (G4 P1): a desk beat must carry NO liveness-ack instruction. The AckAge wedge watches
	// the SINGLE XO ack file; instructing a beaten idle desk to touch it would let the desk mask a
	// genuinely-dead XO from its own watchdog. The desk's ONLY file-touch instruction is its OWN settle
	// marker (asserted above) — never the XO ack path.
	for _, banned := range []string{"ack you are alive", "flotilla-xo-alive", "-alive"} {
		if strings.Contains(got, banned) {
			t.Errorf("desk prompt must NOT carry a liveness-ack instruction (%q reached the body)\nfull: %q", banned, got)
		}
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

// TestDeskContinuationBuiltinJudgmentContract asserts the #189 refinements to the desk-continuation
// prompt: re-trigger-first (idle is usually a transient fault → resume the next authorized step),
// never-sit-idle / opportunistic-work-if-blocked, the TWO-LEDGER recording instructions, the settle-
// when-no-actionable clause, and the preserved non-authorizing clause. Load-bearing: the prompt MUST
// QUOTE the EXACT literal `[awaiting-auth]` token the parser accepts (the §4 brittleness fix — a
// near-miss spelling silently breaks the judgment), and MUST warn against the near-miss spellings.
func TestDeskContinuationBuiltinJudgmentContract(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())

	settle := "/abs/state/flotilla-backend-settled"
	got, err := deskHeartbeatBody("backend", func(a string) string {
		if a == "backend" {
			return settle
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unsubstituted placeholder remains: %q", got)
	}

	// (1) Re-trigger-first: idle is USUALLY a transient technical fault → resume the next authorized step.
	for _, want := range []string{"transient", "RESUME", "ALREADY-AUTHORIZED"} {
		if !strings.Contains(got, want) {
			t.Errorf("re-trigger-first fragment %q missing\nfull: %q", want, got)
		}
	}
	// (2) Never sit idle; do opportunistic work if genuinely blocked.
	for _, want := range []string{"GENUINELY blocked", "opportunistic", "Never sit idle"} {
		if !strings.Contains(got, want) {
			t.Errorf("never-sit-idle fragment %q missing\nfull: %q", want, got)
		}
	}
	// (3) Two-ledger recording, QUOTING the exact tokens. The open-questions ledger and the
	//     authorizations ledger must both be named with the literal markers the parser reads.
	for _, want := range []string{"[blocked]", "[needs-attention]", "[awaiting-auth]", "open-questions", "authorizations"} {
		if !strings.Contains(got, want) {
			t.Errorf("two-ledger fragment %q missing\nfull: %q", want, got)
		}
	}
	// (3a) Brittleness guard: the prompt warns against the near-miss spellings that silently break the
	//      judgment (the parser recognizes ONLY `[awaiting-auth]`).
	for _, want := range []string{"awaiting-authorization", "awaiting auth"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt must warn against the near-miss spelling %q (the parser recognizes only [awaiting-auth])\nfull: %q", want, got)
		}
	}
	// (4) Settle-when-no-actionable: once everything is done/blocked-and-tracked/awaiting-auth, reply
	//     idle and touch the settle marker.
	if !strings.Contains(got, "touch "+settle) {
		t.Errorf("settle instruction missing the desk's own marker path\nfull: %q", got)
	}
	// (5) Non-authorizing preserved (#184 defense-in-depth).
	if !strings.Contains(got, "do NOT approve") {
		t.Errorf("non-authorizing clause must be preserved\nfull: %q", got)
	}
	// The XO ack path is still NEVER instructed (the G4 P1 regression-lock).
	for _, banned := range []string{"flotilla-xo-alive", "-alive"} {
		if strings.Contains(got, banned) {
			t.Errorf("desk prompt must NOT carry a liveness-ack instruction (%q)\nfull: %q", banned, got)
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
	dispatch := newDeskHeartbeatDispatch(func(j watch.Job) { enq = append(enq, j) }, settle)

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
	// REGRESSION (G4 P1): the enqueued beat must carry no XO liveness-ack instruction.
	if strings.Contains(j.Message, "-alive") || strings.Contains(j.Message, "ack you are alive") {
		t.Errorf("desk beat must NOT instruct touching the XO ack file; got %q", j.Message)
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
