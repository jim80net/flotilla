package decisionbrief

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type claimsFile struct {
	Claimed []string `json:"claimed"`
}

// LoadTracker restores dispatched gap keys from disk (survives watch restarts — #365).
func LoadTracker(path string) *Tracker {
	t := NewTracker()
	if path == "" {
		return t
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: decision-brief claims read failed for %q: %v (starting empty)", path, err)
		}
		return t
	}
	var f claimsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("flotilla watch: decision-brief claims at %q corrupt: %v (starting empty)", path, err)
		return t
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range f.Claimed {
		if k != "" {
			t.dispatched[k] = true
		}
	}
	return t
}

// Save persists claimed gap keys to disk.
func (t *Tracker) Save(path string) error {
	if path == "" {
		return nil
	}
	t.mu.Lock()
	keys := make([]string, 0, len(t.dispatched))
	for k := range t.dispatched {
		keys = append(keys, k)
	}
	t.mu.Unlock()

	raw, err := json.Marshal(claimsFile{Claimed: keys})
	if err != nil {
		return fmt.Errorf("marshal decision-brief claims: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir decision-brief claims dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create decision-brief claims temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write decision-brief claims temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close decision-brief claims temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename decision-brief claims: %w", err)
	}
	return nil
}
