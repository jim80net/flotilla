package main

import (
	"strings"
	"testing"

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
