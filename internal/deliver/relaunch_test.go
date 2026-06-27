package deliver

import "testing"

// classifyPanes is the shared lower scan both parsePane and Resolve use; these
// tests lock the marker-vs-title precedence at the classification layer (Resolve
// itself drives tmux and is exercised by the live smoke).

func TestClassifyPanesMarkerWins(t *testing.T) {
	// Pane A carries the marker (drifted title); pane B has a coincidental title.
	out := "0:0.1\t✳ drifted\tfrontend\n0:0.9\tfrontend\t\n"
	mk, ti := classifyPanes(out, "frontend")
	if len(mk) != 1 || mk[0] != "0:0.1" {
		t.Errorf("markerMatches = %v, want [0:0.1]", mk)
	}
	// The untagged title-coincidence pane is still a title match; Resolve/parsePane
	// give the marker tier precedence, so this does not cause ambiguity downstream.
	if len(ti) != 1 || ti[0] != "0:0.9" {
		t.Errorf("titleMatches = %v, want [0:0.9]", ti)
	}
}

func TestClassifyPanesNoMatch(t *testing.T) {
	out := "0:0.0\tsomething\t\n0:0.1\tother\t\n"
	mk, ti := classifyPanes(out, "data")
	if len(mk) != 0 || len(ti) != 0 {
		t.Errorf("classifyPanes(no match) = (%v,%v), want empty/empty", mk, ti)
	}
}

func TestClassifyPanesDuplicateMarker(t *testing.T) {
	out := "0:0.1\tfoo\tfrontend\n1:0.0\tbar\tfrontend\n"
	mk, _ := classifyPanes(out, "frontend")
	if len(mk) != 2 {
		t.Errorf("markerMatches = %v, want 2 (ambiguous)", mk)
	}
}

func TestClassifyPanesTitleFallback(t *testing.T) {
	// Untagged fleet (empty marker fields) — only title matches.
	out := "0:0.0\txo\t\n0:0.3\t✳ frontend\t\n"
	mk, ti := classifyPanes(out, "frontend")
	if len(mk) != 0 {
		t.Errorf("markerMatches = %v, want empty (untagged fleet)", mk)
	}
	if len(ti) != 1 || ti[0] != "0:0.3" {
		t.Errorf("titleMatches = %v, want [0:0.3]", ti)
	}
}
