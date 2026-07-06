package adjutantbuffer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeliveredEntry records one item identity delivered in a prior seam brief (#469).
type DeliveredEntry struct {
	Key       string `json:"key"`
	StateHash string `json:"state_hash"`
}

// DeliveredFile is the durable consumed-item ledger (flotilla-<xo>-buffer-delivered.json).
type DeliveredFile struct {
	Leader  string           `json:"leader"`
	Entries []DeliveredEntry `json:"entries"`
}

// itemKey is the stable detector-edge identity for a buffered reason.
func itemKey(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if i := strings.Index(reason, ": "); i > 0 {
		return reason[:i] + ":" + strings.TrimSpace(reason[i+2:])
	}
	return reason
}

// itemStateHash fingerprints one append occurrence so delta-only re-injection can distinguish
// a fresh edge from an already-delivered one (#469).
func itemStateHash(reason string, at time.Time) string {
	sum := sha256.Sum256([]byte(reason + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:8])
}

func normalizeItem(it Item) (Item, bool) {
	r := strings.TrimSpace(it.Reason)
	if r == "" {
		return Item{}, false
	}
	it.Reason = r
	if it.Key == "" {
		it.Key = itemKey(r)
	}
	if it.StateHash == "" {
		it.StateHash = itemStateHash(r, it.At)
	}
	return it, true
}

func normalizeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if norm, ok := normalizeItem(it); ok {
			out = append(out, norm)
		}
	}
	return out
}

// Has reports whether key+stateHash was already delivered in a prior seam brief.
func (d DeliveredFile) Has(key, stateHash string) bool {
	for _, e := range d.Entries {
		if e.Key == key && e.StateHash == stateHash {
			return true
		}
	}
	return false
}

// FilterUndelivered returns items not yet delivered at inject time (delta-only semantics).
func FilterUndelivered(items []Item, delivered DeliveredFile) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if !delivered.Has(it.Key, it.StateHash) {
			out = append(out, it)
		}
	}
	return out
}

// LoadDelivered reads the consumed-item ledger. Missing file is an empty ledger.
func LoadDelivered(path string) (DeliveredFile, error) {
	if path == "" {
		return DeliveredFile{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DeliveredFile{}, nil
		}
		return DeliveredFile{}, fmt.Errorf("read delivered ledger %q: %w", path, err)
	}
	var f DeliveredFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return DeliveredFile{}, fmt.Errorf("corrupt delivered ledger %q: %w", path, err)
	}
	return f, nil
}

// RecordDelivered appends delivered item identities after a successful seam enqueue (#469).
func RecordDelivered(path, leader string, items []Item) error {
	if path == "" || leader == "" || len(items) == 0 {
		return nil
	}
	f, err := LoadDelivered(path)
	if err != nil {
		return err
	}
	if f.Leader == "" {
		f.Leader = leader
	}
	seen := make(map[string]bool, len(f.Entries))
	for _, e := range f.Entries {
		seen[e.Key+"\x00"+e.StateHash] = true
	}
	for _, it := range items {
		if it.Key == "" || it.StateHash == "" {
			continue
		}
		id := it.Key + "\x00" + it.StateHash
		if seen[id] {
			continue
		}
		seen[id] = true
		f.Entries = append(f.Entries, DeliveredEntry{Key: it.Key, StateHash: it.StateHash})
	}
	return saveDelivered(path, f)
}

func saveDelivered(path string, f DeliveredFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir delivered ledger dir: %w", err)
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create delivered temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write delivered temp %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close delivered temp %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename delivered ledger %q: %w", path, err)
	}
	return nil
}

// PrepareInject applies consumed-item dedup and composes the leader seam brief (#469).
// injectItems is the post-dedup list; ok is false when nothing should be injected.
// Charter-missing banners are included only when fresh items inject — charter pairing is
// handled on evaluation ticks, not as a null seam interrupt.
func PrepareInject(leader string, f File, delivered DeliveredFile, charterMissing, corruptQuarantined bool) (brief string, injectItems []Item, ok bool) {
	f.Items = normalizeItems(f.Items)
	injectItems = FilterUndelivered(f.Items, delivered)
	if len(injectItems) == 0 {
		if corruptQuarantined {
			return FormatBrief(leader, File{Leader: f.Leader}, false, true), nil, true
		}
		return "", nil, false
	}
	render := File{Leader: f.Leader, Items: injectItems}
	return FormatBrief(leader, render, charterMissing, corruptQuarantined), injectItems, true
}
