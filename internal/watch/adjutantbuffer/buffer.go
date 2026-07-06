package adjutantbuffer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item is one buffered interrupt for a coordinator layer.
type Item struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// File is the durable layer queue sidecar (flotilla-<xo>-buffer.json).
type File struct {
	Leader string `json:"leader"`
	Items  []Item `json:"items"`
}

// Append adds reasons to the buffer file, creating it when absent.
func Append(path, leader string, reasons []string) error {
	if path == "" || leader == "" || len(reasons) == 0 {
		return nil
	}
	f, err := load(path)
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
		f.Items = append(f.Items, Item{At: now, Reason: r})
	}
	return save(path, f)
}

// Drain reads and clears the buffer. ok is false when empty or absent.
func Drain(path string) (File, bool, error) {
	f, err := load(path)
	if err != nil {
		return File{}, false, err
	}
	if len(f.Items) == 0 {
		return File{}, false, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return File{}, false, fmt.Errorf("remove buffer %q: %w", path, err)
	}
	return f, true, nil
}

// Len reports buffered item count (0 when absent or empty).
func Len(path string) int {
	f, err := load(path)
	if err != nil {
		return 0
	}
	return len(f.Items)
}

// FormatBrief composes the consolidated leader inject at a seam (#439 phase 1b).
func FormatBrief(leader string, f File, charterMissing bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[adjutant brief — %s layer]\n\n", leader)
	if charterMissing {
		b.WriteString("Charter: not yet established — run first-presentation negotiation and write ")
		b.WriteString(rosterCharterName(leader))
		b.WriteString(" (liveness ack is required minimum).\n\n")
	}
	since := time.Since(oldest(f.Items))
	fmt.Fprintf(&b, "Since your last seam (%s ago): %d item(s) buffered.\n\nNeeds you:\n", humanSince(since), len(f.Items))
	for _, it := range f.Items {
		fmt.Fprintf(&b, "  • %s\n", it.Reason)
	}
	b.WriteString("\nEscalation (0).")
	return b.String()
}

func load(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, fmt.Errorf("read buffer %q: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("parse buffer %q: %w", path, err)
	}
	return f, nil
}

func save(path string, f File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir buffer dir: %w", err)
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
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
