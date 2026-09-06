// Package frontier implements the #530 return-to-frontier sidecar and turn-final guard.
// When a non-urgent seam interrupt preempts an active coordinator warrant, watch records a
// durable return_to pointer; coordinator turn-finals after the side item must resume it,
// reassign, or name a blocking gate.
package frontier

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/backlog"
)

// Priority classifies seam interrupt urgency (#530 / loop-conformance design).
type Priority string

const (
	PriorityUrgent     Priority = "urgent"
	PriorityJudgment   Priority = "judgment"
	PriorityMechanical Priority = "mechanical"
)

// Origin records who owns the return-to pointer's authority. Authored frames
// are written deliberately by a coordinator/operator; derived frames are a
// backlog fallback recorded by the seam interrupt guard.
type Origin string

const (
	OriginAuthored Origin = "authored"
	OriginDerived  Origin = "derived"
)

// Frame is the durable frontier sidecar (flotilla-<coordinator>-frontier.json).
type Frame struct {
	Coordinator    string    `json:"coordinator"`
	ReturnTo       string    `json:"return_to"`
	Priority       Priority  `json:"priority"`
	ActiveWarrant  string    `json:"active_warrant,omitempty"`
	Source         string    `json:"interrupt_source"`
	SideItem       string    `json:"side_item,omitempty"`
	Origin         Origin    `json:"origin,omitempty"`
	SourcePath     string    `json:"source_path,omitempty"`
	ItemID         string    `json:"item_id,omitempty"`
	SourceRevision string    `json:"source_revision,omitempty"`
	ObservedStatus string    `json:"observed_status,omitempty"`
	At             time.Time `json:"at"`
}

// Result is the turn-final guard verdict for one coordinator finish.
type Result struct {
	Violation bool
	Signal    string
}

// StrikeThreshold fires on the first missed return-to-frontier guard.
const StrikeThreshold = 1

var (
	resumePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\breturn[- ]to[- ]frontier\b`),
		regexp.MustCompile(`(?i)\bresume(?:d|s|ing)?\b.{0,80}\b(?:warrant|frontier|prior|backlog)\b`),
		regexp.MustCompile(`(?i)\b(?:back to|returning to|picking up)\b.{0,80}\b(?:warrant|frontier|goal|backlog)\b`),
		regexp.MustCompile(`(?i)\bfrontier guard\b.{0,80}\b(?:resume|clear|satisfied)\b`),
	}

	reassignPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bflotilla send\b`),
		regexp.MustCompile(`(?i)\b(?:reassign(?:ed)?|delegated?|routed?)\b`),
		regexp.MustCompile(`(?i)\b(?:dispatch(?:ed)?|hand(?:ed)? off)\b.{0,60}\b(?:desk|agent)\b`),
	}

	namedGatePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[(?:blocked|awaiting-auth)\]`),
		regexp.MustCompile(`(?i)\bblocking gate\b`),
		regexp.MustCompile(`(?i)\b(?:blocked on|waiting on)\b.{0,80}\b(?:operator|gate|auth)\b`),
		regexp.MustCompile(`(?i)\[awaiting-auth\]`),
		regexp.MustCompile(`(?i)\bWaiting on you:\b`),
	}

	delegatedPrefix = regexp.MustCompile(`(?i)^(?:[-*+]\s*|\d+\.\s*)?\[(?:in-flight|next|pending)\]\s*delegated(?:\s|[—–:;-]|$)`)
)

// Load reads the frontier sidecar. ok is false when absent.
func Load(path string) (Frame, bool, error) {
	if path == "" {
		return Frame{}, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Frame{}, false, nil
		}
		return Frame{}, false, fmt.Errorf("read frontier %q: %w", path, err)
	}
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return Frame{}, false, fmt.Errorf("parse frontier %q: %w", path, err)
	}
	if strings.TrimSpace(f.ReturnTo) == "" {
		return Frame{}, false, nil
	}
	return f, true, nil
}

// Save persists frame atomically.
func Save(path string, f Frame) error {
	if path == "" || strings.TrimSpace(f.ReturnTo) == "" {
		return nil
	}
	if f.Origin == "" {
		f.Origin = OriginAuthored
	}
	return withSidecarLock(path, func() error { return saveUnlocked(path, f) })
}

func saveUnlocked(path string, f Frame) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %q: %w", path, err)
	}
	return nil
}

func withSidecarLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}
	// Keep one stable lock inode for the sidecar's lifetime. Removing it after
	// unlock would let a waiter hold the unlinked inode while a newcomer creates
	// and locks a different file, defeating cross-process mutual exclusion.
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open frontier lock %q: %w", lockPath, err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock frontier lock %q: %w", lockPath, err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- best-effort unlock on return
	return fn()
}

// Clear removes the frontier sidecar after a satisfied guard or explicit resume.
func Clear(path string) error {
	if path == "" {
		return nil
	}
	return withSidecarLock(path, func() error { return clearUnlocked(path) })
}

// ClearIfUnchanged removes path only when its current frame is the same snapshot
// the caller evaluated. A newly authored/replaced frontier survives an older
// finish guard's clear decision and will be evaluated on the next finish.
func ClearIfUnchanged(path string, expected Frame) (bool, error) {
	if path == "" {
		return false, nil
	}
	cleared := false
	err := withSidecarLock(path, func() error {
		current, ok, err := Load(path)
		if err != nil || !ok {
			return err
		}
		if !sameFrame(current, expected) {
			return nil
		}
		if err := clearUnlocked(path); err != nil {
			return err
		}
		cleared = true
		return nil
	})
	return cleared, err
}

// ReplaceDerivedIfUnchanged atomically replaces the evaluated derived frame. Authored frames and
// concurrently changed derived frames are never overwritten.
func ReplaceDerivedIfUnchanged(path string, expected, replacement Frame) (bool, error) {
	if path == "" || expected.Origin != OriginDerived || replacement.Origin != OriginDerived {
		return false, nil
	}
	replaced := false
	err := withSidecarLock(path, func() error {
		current, ok, err := Load(path)
		if err != nil || !ok {
			return err
		}
		if current.Origin != OriginDerived || !sameFrame(current, expected) {
			return nil
		}
		if err := saveUnlocked(path, replacement); err != nil {
			return err
		}
		replaced = true
		return nil
	})
	return replaced, err
}

func clearUnlocked(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove frontier %q: %w", path, err)
	}
	return nil
}

func sameFrame(a, b Frame) bool {
	return a.Coordinator == b.Coordinator &&
		a.ReturnTo == b.ReturnTo &&
		a.Priority == b.Priority &&
		a.ActiveWarrant == b.ActiveWarrant &&
		a.Source == b.Source &&
		a.SideItem == b.SideItem &&
		a.Origin == b.Origin &&
		a.SourcePath == b.SourcePath &&
		a.ItemID == b.ItemID &&
		a.SourceRevision == b.SourceRevision &&
		a.ObservedStatus == b.ObservedStatus &&
		a.At.Equal(b.At)
}

// RecordPreempt writes a derived frontier fallback when a non-urgent interrupt
// buffers at the seam. An authored frame is authoritative and survives. A derived frame is
// atomically superseded by the newly observed source item so stale derived state is not cached
// forever; returnTo is display text, while ItemID is the durable full identity.
func RecordPreempt(path string, f Frame) error {
	if path == "" || strings.TrimSpace(f.ReturnTo) == "" {
		return nil
	}
	if f.SourcePath == "" || f.ItemID == "" || f.SourceRevision == "" || f.ObservedStatus == "" {
		return errors.New("derived frontier requires source_path, item_id, source_revision, and observed_status")
	}
	if f.At.IsZero() {
		f.At = time.Now().UTC()
	}
	if f.Priority == "" {
		f.Priority = PriorityMechanical
	}
	f.Origin = OriginDerived
	return withSidecarLock(path, func() error {
		if current, ok, err := Load(path); err != nil {
			return err
		} else if ok && current.Origin != OriginDerived {
			return nil
		}
		return saveUnlocked(path, f)
	})
}

// ReturnToFromBacklog returns the first actionable backlog item as display text. Durable derived
// frames should use DeriveFromBacklog, which additionally records full identity and provenance.
func ReturnToFromBacklog(md string) (pointer, label string, ok bool) {
	st := backlog.Parse(md)
	var line string
	for _, candidate := range st.Unblocked {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || delegatedBacklogLine(candidate) {
			continue
		}
		line = candidate
		break
	}
	if line == "" {
		return "", "", false
	}
	pointer = DisplayText(line)
	label = pointer
	return pointer, label, true
}

// DeriveFromBacklog returns a derived frame for the first actionable, non-delegated item.
// The item identity excludes its status marker so next→done can be recognized as the same item.
func DeriveFromBacklog(coordinator, sourcePath string, raw []byte) (Frame, bool, error) {
	if !utf8.Valid(raw) {
		return Frame{}, false, fmt.Errorf("frontier source %q is not valid UTF-8", sourcePath)
	}
	scan := backlog.Scan(string(raw))
	if !scan.Found {
		return Frame{}, false, fmt.Errorf("frontier source %q has no ## Backlog section", sourcePath)
	}
	if err := validateSourceScan(sourcePath, scan); err != nil {
		return Frame{}, false, err
	}
	for _, item := range scan.Items {
		if !actionableItem(item) || delegatedBacklogLine(item.Head) {
			continue
		}
		return Frame{
			Coordinator:    coordinator,
			ReturnTo:       DisplayText(item.Head),
			ActiveWarrant:  DisplayText(item.Head),
			Origin:         OriginDerived,
			SourcePath:     filepath.Clean(sourcePath),
			ItemID:         itemID(item),
			SourceRevision: sourceDigest(raw),
			ObservedStatus: item.Classification,
		}, true, nil
	}
	return Frame{}, false, nil
}

// RefreshDerived verifies a derived frame against a freshly read exact source. active is false
// when the item was removed or moved to a settle-neutral/done/delegated state. An error means the
// source is unverifiable; callers retain the frame as evidence but must suppress its active claim.
func RefreshDerived(f Frame, raw []byte) (refreshed Frame, active bool, err error) {
	if f.Origin != OriginDerived || f.SourcePath == "" || f.ItemID == "" {
		return f, true, nil
	}
	if !utf8.Valid(raw) {
		return f, false, fmt.Errorf("frontier source %q is not valid UTF-8", f.SourcePath)
	}
	scan := backlog.Scan(string(raw))
	if !scan.Found {
		return f, false, fmt.Errorf("frontier source %q has no ## Backlog section", f.SourcePath)
	}
	if err := validateSourceScan(f.SourcePath, scan); err != nil {
		return f, false, err
	}
	for _, item := range scan.Items {
		if itemID(item) != f.ItemID {
			continue
		}
		if !actionableItem(item) || delegatedBacklogLine(item.Head) {
			return f, false, nil
		}
		f.ReturnTo = DisplayText(item.Head)
		f.ActiveWarrant = DisplayText(item.Head)
		f.SourceRevision = sourceDigest(raw)
		f.ObservedStatus = item.Classification
		return f, true, nil
	}
	// The tracked identity disappeared. Advance the derived frontier to the source's next
	// actionable item; the caller CAS-replaces the evaluated frame so a concurrent authored frame
	// still wins. Preserve the interrupt context while replacing only source-derived identity.
	next, ok, err := DeriveFromBacklog(f.Coordinator, f.SourcePath, raw)
	if err != nil {
		return f, false, err
	}
	if ok {
		next.Priority = f.Priority
		next.Source = f.Source
		next.SideItem = f.SideItem
		next.At = f.At
		return next, true, nil
	}
	return f, false, nil
}

func validateSourceScan(path string, scan backlog.ScanResult) error {
	for _, item := range scan.Items {
		if item.Classification == "malformed" {
			return fmt.Errorf("frontier source %q has a malformed item at line %d", path, item.StartLine)
		}
	}
	return nil
}

func actionableItem(item backlog.Item) bool {
	switch item.Classification {
	case "done", "blocked", "needs-attention", "awaiting-auth":
		return false
	default:
		return true
	}
}

var itemIdentityPrefix = regexp.MustCompile(`^\s*(?:\d+\.|[-*+])\s+(?:\[[^]]+\]\s*)?`)

func itemID(item backlog.Item) string {
	identity := itemIdentityPrefix.ReplaceAllString(item.Raw, "")
	sum := sha256.Sum256([]byte(strings.TrimSpace(identity)))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func sourceDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:])
}

// DisplayText returns the rune-safe, display-only projection of full source identity text.
func DisplayText(s string) string {
	return truncateDisplay(strings.TrimSpace(s), 120)
}

func truncateDisplay(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := 0
	for cut < len(s) {
		_, width := utf8.DecodeRuneInString(s[cut:])
		if cut+width > maxBytes {
			break
		}
		cut += width
	}
	return s[:cut]
}

func delegatedBacklogLine(line string) bool {
	return strings.Contains(strings.ToLower(line), "[delegated]") || delegatedPrefix.MatchString(strings.TrimSpace(line))
}

// Check evaluates a coordinator turn-final against an active frontier frame.
func Check(turnFinal string, f Frame) Result {
	if turnFinal == "" || strings.TrimSpace(f.ReturnTo) == "" {
		return Result{}
	}
	if satisfied(turnFinal, f) {
		return Result{}
	}
	return Result{Violation: true, Signal: "return-to-frontier-missed"}
}

func satisfied(text string, f Frame) bool {
	if matchesAny(text, resumePatterns) || matchesAny(text, reassignPatterns) || matchesAny(text, namedGatePatterns) {
		return true
	}
	if pointerReferenced(text, f.ReturnTo) {
		return true
	}
	if f.ActiveWarrant != "" && pointerReferenced(text, f.ActiveWarrant) {
		return true
	}
	return false
}

func pointerReferenced(text, pointer string) bool {
	pointer = strings.TrimSpace(pointer)
	if pointer == "" {
		return false
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, strings.ToLower(pointer)) {
		return true
	}
	// Issue ids (#530) and dispatch nonces are often cited partially.
	if m := regexp.MustCompile(`#\d+`).FindString(pointer); m != "" && strings.Contains(lower, strings.ToLower(m)) {
		return true
	}
	if m := regexp.MustCompile(`flotilla-dispatch-[0-9a-f]{8,16}`).FindString(pointer); m != "" && strings.Contains(lower, m) {
		return true
	}
	// Distinctive substring from a long backlog line.
	needle := distinctiveSnippet(pointer)
	return needle != "" && strings.Contains(lower, strings.ToLower(needle))
}

func distinctiveSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 24 {
		if len(s) > 80 {
			return truncateDisplay(s, 80)
		}
		return s
	}
	return ""
}

func matchesAny(text string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// NudgePrompt is injected when the frontier guard fails on a coordinator turn-final.
func NudgePrompt(coordinator string, f Frame) string {
	var b strings.Builder
	b.WriteString("[flotilla return-to-frontier guard] You finished a side item without ")
	b.WriteString("resuming the prior warrant")
	if coordinator != "" {
		b.WriteString(" (")
		b.WriteString(coordinator)
		b.WriteString(")")
	}
	b.WriteString(".\n\n")
	b.WriteString("Active frontier (durable pointer — not raw history):\n")
	b.WriteString("- return_to: ")
	b.WriteString(f.ReturnTo)
	b.WriteString("\n")
	if f.SideItem != "" {
		b.WriteString("- side_item: ")
		b.WriteString(f.SideItem)
		b.WriteString("\n")
	}
	b.WriteString("\nBefore you go idle you MUST do one of:\n")
	b.WriteString("1. Resume return_to — name the warrant/issue/backlog item and the next authorized step.\n")
	b.WriteString("2. Reassign — `flotilla send <desk> \"…\"` with explicit ownership.\n")
	b.WriteString("3. Name a blocking gate — [blocked] / [awaiting-auth] with a concrete operator ask.\n\n")
	b.WriteString("Execute on this turn; do not settle without addressing the frontier.")
	return b.String()
}
