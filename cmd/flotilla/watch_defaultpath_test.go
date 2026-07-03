package main

import (
	"path/filepath"
	"testing"
)

// TestDefaultPath locks the unset-path defaulting extracted from cmdWatch: an
// empty target falls back to the roster-relative join; a caller-supplied value
// (flag/env) is preserved untouched. This is the behavior-preservation proof for
// factoring the eight repeated `if *p == "" { *p = filepath.Join(...) }` blocks
// into one helper.
func TestDefaultPath(t *testing.T) {
	dir := "/roster/dir"

	// Empty ⇒ filled with the roster-relative default.
	empty := ""
	defaultPath(&empty, dir, "flotilla-xo-alive")
	if want := filepath.Join(dir, "flotilla-xo-alive"); empty != want {
		t.Errorf("empty target = %q, want %q", empty, want)
	}

	// Non-empty ⇒ preserved (an operator-supplied value always wins).
	supplied := "/custom/ack-file"
	defaultPath(&supplied, dir, "flotilla-xo-alive")
	if supplied != "/custom/ack-file" {
		t.Errorf("supplied target = %q, want it preserved", supplied)
	}
}
