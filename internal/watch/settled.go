package watch

import (
	"log"
	"os"
	"path/filepath"
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

// SettledMarkerSet is the per-agent analogue of SettledMarker (#183 recursive desk heartbeat).
// Each desk the heartbeat re-engages signals "nothing to advance" by touching its OWN marker
// (<dir>/flotilla-<agent>-settled) and replying idle; the detector Consume()s that marker to record
// the desk as settled and suppress further heartbeats to it until it is re-armed. The paths are
// per-agent so one desk's settle never consumes another desk's — or the XO's (whose marker is a
// separately-configured path). An empty dir means the per-agent fast settle is unconfigured.
type SettledMarkerSet struct {
	dir string
}

// NewSettledMarkerSet builds a per-agent settle-marker set rooted at dir (the roster directory, the
// same place the XO marker lives). An empty dir disables the per-agent fast settle (Consume is
// always false), so settling relies on the per-agent cap backstop alone.
func NewSettledMarkerSet(dir string) *SettledMarkerSet {
	return &SettledMarkerSet{dir: dir}
}

// Path returns agent's settle-marker path — the exact path injected into that desk's continuation
// prompt so the desk touches it to signal idle. Empty when the set is unconfigured or agent is
// empty (no path to advertise).
func (s *SettledMarkerSet) Path(agent string) string {
	if s.dir == "" || agent == "" {
		return ""
	}
	return filepath.Join(s.dir, "flotilla-"+agent+"-settled")
}

// Consume reports+removes agent's settle marker (delegating to SettledMarker for the fail-safe
// stat+remove). Absent / unreadable / unconfigured → false (fail toward NOT-settled, keeping the
// desk responsive — the same safe direction as the XO marker).
func (s *SettledMarkerSet) Consume(agent string) bool {
	return NewSettledMarker(s.Path(agent)).Consume()
}
