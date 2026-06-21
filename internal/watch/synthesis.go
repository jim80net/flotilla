package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// SynthState is the DURABLE last-seen materiality snapshot for visibility synthesis (B2): per
// synthesizing agent, a hash of each subordinate's last-synthesized turn text. It is a DISK
// SIDECAR — separate from the detector's diff Snapshot — because it must survive BOTH context
// rotation (/clear wipes the skill's own context) AND a daemon restart (an in-memory-only
// snapshot would re-post every subordinate as "new" on the first post-restart wake — a synthesis
// restart-storm). It is keyed by synthesizing agent, then by subordinate agent, to the SHA-256 of
// that subordinate's full latest turn text (Q-C: the materiality unit is the full latest turn).
type SynthState struct {
	// LastSeen[synthesizer][subordinate] = hash of the subordinate's last-synthesized turn text.
	LastSeen map[string]map[string]string `json:"last_seen"`
}

// hashTurn returns the materiality hash of a subordinate's full latest turn text.
func hashTurn(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// materialSubordinates returns the subset of an agent's subordinates whose latest turn text has
// CHANGED since the synthesizer's last-seen snapshot, and the FRESH hash set to record IFF the
// synthesis fires. read resolves a subordinate's latest text (ok=false ⇒ UNREADABLE: pane won't
// resolve / no ResultReader). An unreadable subordinate is EXCLUDED from the computation entirely
// (re-trio P2-4): it is neither counted as changed nor folded into the fresh hash set, so a
// transient resolve failure never flaps the wake (no "changed to empty" then "changed back"). A
// subordinate absent from last-seen is NEW (material) — which makes a missing/corrupt sidecar fail
// SAFE toward "all changed" (synthesize once), never silent-never-fire.
//
// It is PURE over its inputs (no mutation of state); the caller commits freshHashes only when it
// decides to fire. changed is the list of subordinates whose state is material (named in the wake
// reasons); fresh is the hash map to persist for this synthesizer on a fire.
func materialSubordinates(lastSeen map[string]string, subordinates []string, read func(string) (string, bool)) (changed []string, fresh map[string]string) {
	fresh = map[string]string{}
	for _, sub := range subordinates {
		text, ok := read(sub)
		if !ok {
			// UNREADABLE — exclude from materiality (never hashed as empty). Carry the prior hash
			// forward UNCHANGED if we had one, so the next readable tick compares against the real
			// last-seen state rather than treating recovery as a spurious change.
			if prev, had := lastSeen[sub]; had {
				fresh[sub] = prev
			}
			continue
		}
		h := hashTurn(text)
		fresh[sub] = h
		if lastSeen[sub] != h {
			changed = append(changed, sub)
		}
	}
	return changed, fresh
}

// LoadSynthState reads the synthesis materiality sidecar fail-safe. A missing or unparseable file
// returns ok=false (a brand-new / corrupt sidecar) so the caller fails SAFE toward "all changed"
// (the empty LastSeen makes every subordinate NEW ⇒ synthesize once), never silent-never-fire. A
// read/parse error is logged but never crashes the detector.
func LoadSynthState(path string) (SynthState, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: synthesis sidecar read failed for %q: %v (treating as all-changed)", path, err)
		}
		return SynthState{LastSeen: map[string]map[string]string{}}, false
	}
	var s SynthState
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Printf("flotilla watch: synthesis sidecar at %q is corrupt: %v (treating as all-changed)", path, err)
		return SynthState{LastSeen: map[string]map[string]string{}}, false
	}
	if s.LastSeen == nil {
		s.LastSeen = map[string]map[string]string{}
	}
	return s, true
}

// Save writes the synthesis sidecar atomically (temp file + rename), modeled on Snapshot.Save, so a
// crash mid-write never leaves a torn, unparseable sidecar (which would itself fail-safe to
// all-changed — at worst one extra synthesis, never a silent miss).
func (s SynthState) Save(path string) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal synthesis sidecar: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create synthesis sidecar temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write synthesis sidecar temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close synthesis sidecar temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename synthesis sidecar into place: %w", err)
	}
	return nil
}
