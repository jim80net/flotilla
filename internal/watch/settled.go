package watch

import (
	"log"
	"os"
)

// SettledMarker is the XO's "I have nothing to advance" signal. On a
// continuation wake the XO either advances the next authorized step OR — if
// nothing remains — touches this marker and replies idle. The detector consumes
// (removes) the marker when it observes the XO settle, records the settled state,
// and stops self-continuation waking until an external material change or an
// operator message.
//
// This is the FAST settle path (the spec's "the XO replies idle → not woken
// again"); the self-continuation hard cap is the deterministic BACKSTOP for a
// runaway XO that keeps claiming work without ever signalling idle.
//
// The read fails safe toward NOT settling: an unreadable marker (a stat error
// we cannot resolve) is treated as absent, so an ambiguous state keeps the XO
// responsive (continuation continues, bounded by the cap) rather than wrongly
// putting it to sleep — the OPPOSITE direction from the awaiting veto, because
// here "don't settle" is the safe error.
type SettledMarker struct {
	path string
}

// NewSettledMarker builds a settle-marker reader. An empty path means the fast
// settle signal is unconfigured — Consume() is always false and settling relies
// solely on the self-continuation cap.
func NewSettledMarker(path string) *SettledMarker {
	return &SettledMarker{path: path}
}

// Consume reports whether the settle marker was present and, if so, removes it
// (so each settle is a fresh, explicit signal). Absent or unreadable → false.
func (m *SettledMarker) Consume() bool {
	if m.path == "" {
		return false
	}
	if _, err := os.Stat(m.path); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: settle marker %q unreadable: %v (treating as not-settled)", m.path, err)
		}
		return false
	}
	if err := os.Remove(m.path); err != nil {
		// We observed the settle signal; a failed remove must not lose it. Log and
		// proceed as settled — a leftover marker would, at worst, settle the XO
		// again next cycle, which an external change or operator message re-engages.
		log.Printf("flotilla watch: settle marker %q observed but not removed: %v", m.path, err)
	}
	return true
}
