package watch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// cursorStore persists the relay catch-up reconciler's per-channel cursor — the
// highest message snowflake the reconciler has processed in each bound channel.
// It is the ONLY catch-up state that survives a daemon restart (the seen-set is
// rebuilt from the live path + poll). It is written fail-safe and atomically,
// exactly like the detector snapshot (Snapshot.Save): a missing or corrupt file
// is treated as "no cursors yet" (every channel first-boots / tail-inits), never
// a crash — so a cursor-file outage can at worst replay-or-tail, never wedge the
// daemon.
type cursorStore struct {
	path string
}

// load reads the persisted cursor map fail-safe. A missing or unparseable file
// yields an empty map (all channels cold-start → tail-init), logged but never an
// error — mirroring LoadSnapshot's cold-start contract.
func (c cursorStore) load() map[string]uint64 {
	out := map[string]uint64{}
	if c.path == "" {
		return out
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: relay cursor read failed for %q: %v (cold-starting)", c.path, err)
		}
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Printf("flotilla watch: relay cursor at %q is corrupt: %v (cold-starting)", c.path, err)
		return map[string]uint64{}
	}
	return out
}

// save writes the cursor map atomically (temp in the same dir, then rename) so a
// crash mid-write never leaves a torn file — the reader sees the old or the new
// map, never a half-written one. A no-op when no path is configured.
func (c cursorStore) save(cursor map[string]uint64) error {
	if c.path == "" {
		return nil
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return fmt.Errorf("marshal relay cursor: %w", err)
	}
	dir := filepath.Dir(c.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(c.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create relay cursor temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write relay cursor temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close relay cursor temp: %w", err)
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		cleanup()
		return fmt.Errorf("rename relay cursor into place: %w", err)
	}
	return nil
}
