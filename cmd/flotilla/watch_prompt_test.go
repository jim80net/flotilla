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
