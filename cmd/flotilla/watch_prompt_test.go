package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/workspace"
)

// With NO workspace, the detector continuation prompt the XO receives must be exactly
// what it was before the workspace feature: ResolvePrompt substitutes {{tracker}}/{{settle}}
// into the package builtin, leaving no placeholders and interpolating the paths at the
// same positions. This regression-locks the "additive on the no-workspace path" guarantee.
func TestDetectorContinuationBuiltinNoWorkspace(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir()) // no workspace dir for "xo"

	tracker := "/abs/state/.flotilla-state.md"
	settle := "/abs/state/flotilla-xo-settled"
	got, err := workspace.ResolvePrompt("xo", detectorContinuationBuiltin, tracker, settle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unsubstituted placeholder remains: %q", got)
	}
	for _, want := range []string{
		"[flotilla change-detector] You just finished a turn. Advance the next clear,",
		"the goal+task tracker " + tracker + "; (2) the active openspec change's unchecked tasks;",
		"signal idle by running: touch " + settle + ". (Your context is rotated between steps",
		"— rely on durable state, not this conversation.)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no-workspace prompt missing expected fragment %q\nfull: %q", want, got)
		}
	}
}

// The WakeBacklog prompt MUST name the driven item AND append the ack instruction — the latter is
// load-bearing: a continuously-driven XO that is never told to ack would falsely trip the AckAge
// wedge alert (the liveness backstop for an always-driving, never-settling XO).
func TestBacklogWakeBodyNamesItemAndAcks(t *testing.T) {
	ack := "\n(To ack you are alive, run: touch /x/alive)"
	body := backlogWakeBody([]string{"- [in-flight] ship the tactical PR"}, "/state/fleet-backlog.md", ack)
	if !strings.Contains(body, "ship the tactical PR") {
		t.Error("WakeBacklog body must NAME the driven item")
	}
	if !strings.HasSuffix(body, ack) {
		t.Error("WakeBacklog body MUST append the ack instruction (else a driven XO never acks → false wedge alert)")
	}
	if !strings.Contains(body, "/state/fleet-backlog.md") {
		t.Error("WakeBacklog body must point the XO at the backlog file (read durable state, not memory)")
	}
	if !strings.Contains(body, "NOT settle while unblocked work remains") {
		t.Error("WakeBacklog body must convey the mechanical no-settle contract")
	}
}
