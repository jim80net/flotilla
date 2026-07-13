package watch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// Snapshot is the change-detector's persisted diff-state: the materiality
// signals as of the last tick, written next to the ack file. It is the ONLY
// detector state that survives a daemon restart — liveness (ack age, watchdog)
// and the runtime counters live in-memory, independent of this file (so a
// snapshot outage can never blind the watchdog; see the Detector).
//
// It is deliberately small and self-describing. The per-desk State is stored as
// its integer code; a drift in the surface.State iota would only misread a
// stale cache, which the fail-safe load degrades to a single conservative wake
// (treat-as-all-changed), never a crash.
type Snapshot struct {
	// DeskStates is the last assessed surface state per monitored desk (by agent
	// name), including the XO. The diff against the next tick's states is what
	// the materiality predicate consumes.
	DeskStates map[string]surface.State `json:"desk_states"`
	// SignalHash is the content hash of the OPTIONAL external signal file as of the
	// last tick; a change is a material wake signal. It is deliberately NOT the XO's
	// own state tracker (.flotilla-state.md): the XO writes that itself, so hashing
	// it would self-wake the XO on its own writes. Empty when no signal file is
	// configured (then it is never material). NOTE: the json tag was renamed
	// tracker_hash→signal_hash; a pre-rename on-disk snapshot loads this field empty,
	// which is harmless — empty ⇒ non-material, and the default config runs with no
	// signal file, so SignalHash starts empty regardless.
	SignalHash string `json:"signal_hash"`
	// XOSettled records that the XO replied idle (or hit the self-continuation
	// cap) and should not be self-continuation-woken until an external material
	// change or an operator message. Persisted so a daemon restart does not
	// re-engage a settled XO.
	XOSettled bool `json:"xo_settled"`
	// Usage holds the last authoritative per-desk observation. Missing probes do
	// not create entries and do not erase prior evidence; readers use StaleAfter
	// to distinguish old evidence from fresh coverage.
	Usage map[string]UsageObservation `json:"usage,omitempty"`
}

// UsageObservation is the acquisition-agnostic usage shape shared by watch,
// status, and dash. Provider/subscription come from the active launch slot,
// never from the surface chrome.
type UsageObservation struct {
	Provider         string    `json:"provider,omitempty"`
	SubscriptionID   string    `json:"subscription_id,omitempty"`
	RemainingPercent int       `json:"remaining_percent"`
	Window           string    `json:"window"`
	Scope            string    `json:"scope"`
	ObservedAt       time.Time `json:"observed_at"`
	StaleAfter       time.Time `json:"stale_after"`
}

// LoadSnapshot reads a persisted snapshot fail-safe. A missing or unparseable
// file is NOT an error — it returns ok=false so the caller cold-starts
// (treat-as-everything-changed → wake once, conservatively). A read/parse error
// is logged but never crashes the detector.
func LoadSnapshot(path string) (Snapshot, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: snapshot read failed for %q: %v (cold-starting)", path, err)
		}
		return Snapshot{}, false
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Printf("flotilla watch: snapshot at %q is corrupt: %v (cold-starting)", path, err)
		return Snapshot{}, false
	}
	if s.DeskStates == nil {
		s.DeskStates = map[string]surface.State{}
	}
	if s.Usage == nil {
		s.Usage = map[string]UsageObservation{}
	}
	return s, true
}

// Save writes the snapshot atomically (write a temp file in the same directory,
// then rename) so a crash mid-write never leaves a half-written, unparseable
// snapshot — the reader sees either the old file or the new one, never a torn
// one. The temp name is process- and path-scoped so concurrent writers (there
// are none today, but the discipline is cheap) do not collide.
func (s Snapshot) Save(path string) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create snapshot temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	// On any failure past this point, remove the temp so a failed write never
	// litters the directory.
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write snapshot temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close snapshot temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename snapshot into place: %w", err)
	}
	return nil
}
