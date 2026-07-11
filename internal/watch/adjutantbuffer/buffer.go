package adjutantbuffer

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item is one buffered interrupt for a coordinator layer.
type Item struct {
	At        time.Time `json:"at"`
	Reason    string    `json:"reason"`
	Key       string    `json:"key,omitempty"`
	StateHash string    `json:"state_hash,omitempty"`
	// Arc metadata (adjutant-buffer-v2 B1 mechanical coalesce). Optional on legacy items.
	ArcID      string    `json:"arc_id,omitempty"`
	OpenedAt   time.Time `json:"opened_at,omitempty"`
	MessageIDs []string  `json:"message_ids,omitempty"`
	ChannelID  string    `json:"channel_id,omitempty"`
	OperatorID string    `json:"operator_id,omitempty"`
}

// File is the durable layer queue sidecar (flotilla-<xo>-buffer.json).
type File struct {
	Leader string `json:"leader"`
	Items  []Item `json:"items"`
}

// Append adds reasons to the buffer file, creating it when absent.
//
// Single-writer contract: the watch daemon's detector calls Append synchronously from one
// goroutine per buffer path (one material-change wake at a time). Append is read-modify-write
// and is not safe under concurrent writers — callers must serialize.
func Append(path, leader string, reasons []string) error {
	if path == "" || leader == "" || len(reasons) == 0 {
		return nil
	}
	f, _, err := load(path)
	if err != nil {
		return err
	}
	if f.Leader == "" {
		f.Leader = leader
	}
	now := time.Now().UTC()
	for _, r := range reasons {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		f.Items = append(f.Items, Item{
			At:        now,
			Reason:    r,
			Key:       itemKey(r),
			StateHash: itemStateHash(r, now),
		})
	}
	return save(path, f)
}

// Peek reads pending buffer items without clearing the sidecar. quarantined is true when a
// corrupt file was renamed to a .corrupt-<timestamp> sidecar on this read (items empty).
func Peek(path string) (f File, ok bool, quarantined bool, err error) {
	f, quarantined, err = load(path)
	if err != nil {
		return File{}, false, false, err
	}
	return f, len(f.Items) > 0, quarantined, nil
}

// RemoveConfirmedItems rewrites the buffer minus exactly the confirmed seam items (#488 P1).
// Items appended after Peek but before confirm are retained.
func RemoveConfirmedItems(path, leader string, delivered []Item) error {
	if path == "" || len(delivered) == 0 {
		return nil
	}
	f, _, err := load(path)
	if err != nil {
		return err
	}
	remove := make(map[string]bool, len(delivered))
	for _, it := range delivered {
		norm, ok := normalizeItem(it)
		if !ok {
			continue
		}
		remove[norm.Key+"\x00"+norm.StateHash] = true
	}
	remaining := make([]Item, 0, len(f.Items))
	for _, it := range f.Items {
		norm, ok := normalizeItem(it)
		if !ok {
			continue
		}
		if remove[norm.Key+"\x00"+norm.StateHash] {
			continue
		}
		remaining = append(remaining, norm)
	}
	if len(remaining) == 0 {
		return Clear(path)
	}
	if f.Leader == "" {
		f.Leader = leader
	}
	f.Items = remaining
	return save(path, f)
}

// Clear removes the buffer sidecar after a successful enqueue (enqueue-then-delete).
func Clear(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove buffer %q: %w", path, err)
	}
	return nil
}

// Drain reads and clears the buffer. ok is false when empty or absent. Prefer Peek + Clear in
// production so the sidecar survives until after enqueue.
func Drain(path string) (File, bool, error) {
	f, ok, _, err := Peek(path)
	if err != nil || !ok {
		return f, ok, err
	}
	if err := Clear(path); err != nil {
		return File{}, false, err
	}
	return f, true, nil
}

// OldestItemAge returns how long the oldest buffered item has been waiting. ok is false when the
// buffer is absent or empty.
func OldestItemAge(path string, now time.Time) (time.Duration, bool) {
	f, has, _, err := Peek(path)
	if err != nil || !has || len(f.Items) == 0 {
		return 0, false
	}
	return now.Sub(oldest(f.Items)), true
}

// Len reports buffered item count (0 when absent or empty).
func Len(path string) int {
	f, _, err := load(path)
	if err != nil {
		return 0
	}
	return len(f.Items)
}

// FormatBrief composes the consolidated leader inject at a seam (#439 phase 1b).
func FormatBrief(leader string, f File, charterMissing, corruptQuarantined bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[adjutant brief — %s layer]\n\n", leader)
	if corruptQuarantined {
		b.WriteString("WARNING: the buffer sidecar was corrupt and has been quarantined as ")
		b.WriteString(filepath.Base(corruptSidecarName(leader)))
		b.WriteString(" — inspect the preserved bytes and fix the underlying bug; continuing with an empty buffer.\n\n")
	}
	if charterMissing {
		b.WriteString("Charter: not yet established — run first-presentation negotiation and write ")
		b.WriteString(rosterCharterName(leader))
		b.WriteString(" (evaluation-tick ack is required minimum).\n\n")
	}
	if len(f.Items) == 0 {
		return b.String()
	}
	since := time.Since(oldest(f.Items))
	fmt.Fprintf(&b, "Since your last seam (%s ago): %d buffered item(s) need your judgment.\n", humanSince(since), len(f.Items))
	b.WriteString("(Mechanical handling by the adjutant is prompt-contract only in this increment — not yet fully automated.)\n\nNeeds you:\n")
	for _, it := range f.Items {
		fmt.Fprintf(&b, "  • %s\n", it.Reason)
	}
	return b.String()
}

func load(path string) (File, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, false, nil
		}
		return File{}, false, fmt.Errorf("read buffer %q: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := quarantineSidecarPath(path)
		if renameErr := os.Rename(path, sidecar); renameErr != nil {
			log.Printf("flotilla watch: adjutant buffer at %q is corrupt (%v) and rename to sidecar failed: %v", path, err, renameErr)
			return File{}, false, fmt.Errorf("corrupt buffer %q: %w (quarantine rename failed: %v)", path, err, renameErr)
		}
		log.Printf("flotilla watch: adjutant buffer at %q is corrupt (%v); preserved as %q", path, err, sidecar)
		return File{}, true, nil
	}
	f.Items = normalizeItems(f.Items)
	return f, false, nil
}

func save(path string, f File) error {
	return atomicWriteJSON(path, f)
}

func oldest(items []Item) time.Time {
	if len(items) == 0 {
		return time.Now().UTC()
	}
	t := items[0].At
	for _, it := range items[1:] {
		if it.At.Before(t) {
			t = it.At
		}
	}
	return t
}

func humanSince(d time.Duration) string {
	if d < time.Minute {
		return "under 1m"
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func rosterCharterName(leader string) string {
	return "flotilla-" + leader + "-adjutant-charter.md"
}

// corruptSidecarName is the glob pattern stem for quarantined buffers (for brief text only).
func corruptSidecarName(leader string) string {
	return "flotilla-" + leader + "-buffer.json.corrupt-<timestamp>"
}
