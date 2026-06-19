package control

import (
	"go/build"
	"strings"
	"testing"
)

// TestNoPaneDrivingImports enforces the design §5 safety invariant BY
// CONSTRUCTION: while Route/Resume are spec-gated on the cross-process pane lock,
// the control package MUST NOT link any pane-driving code — so a dash route can
// never interleave with watch's detector rotate (the hazard the lock closes).
// The fail-closed is then a fact (the capability is absent from the binary), not
// a flippable runtime flag.
//
// When Phase 3b lands the lock and wires the REAL Route/Resume, they will
// intentionally import internal/surface + internal/deliver — at that point this
// test MUST be REPLACED with one asserting the cross-process lock is acquired
// around the whole confirmed-delivery transaction (spec "Cross-process pane
// serialization for control"). The import appearing is the signal to make that
// switch deliberately, not silently.
func TestNoPaneDrivingImports(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("import control dir: %v", err)
	}
	forbidden := []string{
		"internal/surface", // Confirm.Submit (pane composer writes)
		"internal/deliver", // tmux send/paste/Enter
		"internal/relay",   // routing into panes
	}
	for _, imp := range pkg.Imports {
		for _, f := range forbidden {
			if strings.Contains(imp, f) {
				t.Errorf("control imports %q — pane-driving code MUST NOT be linked while route/resume are gated on the cross-process pane lock (design §5). "+
					"If this is Phase 3b wiring the real Route/Resume, replace this test with a lock-acquisition assertion.", imp)
			}
		}
	}
}
