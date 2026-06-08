package watch

import (
	"log"
	"os"
)

// AwaitingMarker is the awaiting-operator veto: a file the XO touches the moment
// it poses a question to the operator (one discipline with its operator-decision
// queue) and removes the moment that question is resolved. While the marker is
// present, the change-detector MUST NOT rotate the XO's context — wiping the
// session would erase the outstanding-question thread out from under both the XO
// and the operator.
//
// This is built fresh here: it was proposed in the never-merged PR #18 and does
// not exist in the tree. The set/clear lifecycle is documented in
// docs/xo-doctrine.md.
//
// The read is fail-safe toward NOT rotating: an unreadable marker (a permission
// or I/O error where we cannot tell whether it exists) is treated as PRESENT, so
// an ambiguous state degrades to "skip the rotate" — never to a wrongful rotate.
// A forgotten (stale) marker therefore costs only lost token savings until the
// XO clears it, which the doctrine makes explicit is the safe direction to err.
type AwaitingMarker struct {
	path string
}

// NewAwaitingMarker builds a marker reader for the given path. An empty path
// means the veto is unconfigured — Present() is always false (rotate proceeds).
func NewAwaitingMarker(path string) *AwaitingMarker {
	return &AwaitingMarker{path: path}
}

// Present reports whether the awaiting-operator veto marker is set. Absent →
// false (rotate allowed). Present → true (rotate vetoed). Any other stat error →
// true (fail-safe: skip the rotate rather than risk wiping an outstanding
// operator conversation).
func (m *AwaitingMarker) Present() bool {
	if m.path == "" {
		return false
	}
	_, err := os.Stat(m.path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	log.Printf("flotilla watch: awaiting marker %q unreadable: %v (vetoing rotate to be safe)", m.path, err)
	return true
}
