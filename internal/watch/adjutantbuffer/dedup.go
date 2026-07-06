package adjutantbuffer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// maxDeliveredLedgerEntries caps delivered-ledger growth per layer (#469 F3).
const maxDeliveredLedgerEntries = 512

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

// LoadDelivered reads the consumed-item ledger. Missing file is an empty ledger. A corrupt
// file is quarantined to a .corrupt-<timestamp> sidecar and an empty ledger is returned
// (fail-open dedup — #488 P2).
func LoadDelivered(path string) (f DeliveredFile, quarantined bool, err error) {
	if path == "" {
		return DeliveredFile{}, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DeliveredFile{}, false, nil
		}
		return DeliveredFile{}, false, fmt.Errorf("read delivered ledger %q: %w", path, err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
		if renameErr := os.Rename(path, sidecar); renameErr != nil {
			log.Printf("flotilla watch: adjutant delivered ledger at %q is corrupt (%v) and rename failed: %v", path, err, renameErr)
			return DeliveredFile{}, false, fmt.Errorf("corrupt delivered ledger %q: %w (quarantine rename failed: %v)", path, err, renameErr)
		}
		log.Printf("flotilla watch: adjutant delivered ledger at %q is corrupt (%v); preserved as %q", path, err, sidecar)
		return DeliveredFile{}, true, nil
	}
	return f, false, nil
}

// RecordDelivered appends delivered item identities after confirmed seam delivery (#469).
func RecordDelivered(path, leader string, items []Item) error {
	if path == "" || leader == "" || len(items) == 0 {
		return nil
	}
	f, _, err := LoadDelivered(path)
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
	f.Entries = pruneDeliveredEntries(f.Entries)
	return atomicWriteJSON(path, f)
}

func pruneDeliveredEntries(entries []DeliveredEntry) []DeliveredEntry {
	if len(entries) <= maxDeliveredLedgerEntries {
		return entries
	}
	return entries[len(entries)-maxDeliveredLedgerEntries:]
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
