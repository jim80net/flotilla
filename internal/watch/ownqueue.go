package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
)

const (
	OwnQueueLeaseSchema = 1
	OwnQueueClaimPrefix = "own-queue:"
)

// OwnQueueLease is the restart-durable proof that one backlog item has already
// been selected for a seat. It contains provenance only, never pane content.
type OwnQueueLease struct {
	Schema       int       `json:"schema"`
	Seat         string    `json:"seat"`
	ItemDigest   string    `json:"item_digest"`
	SourcePath   string    `json:"source_path"`
	SourceLine   int       `json:"source_line"`
	ClaimedAt    time.Time `json:"claimed_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	DetectorTick uint64    `json:"detector_tick"`
	Outcome      string    `json:"outcome"`
}

// OwnQueueEvent is the stable structured diagnostic emitted for every claim
// decision. Detail is operator-readable and contains no pane transcript.
type OwnQueueEvent struct {
	Schema     int    `json:"schema"`
	Seat       string `json:"seat"`
	Result     string `json:"result"`
	Reason     string `json:"reason,omitempty"`
	ItemDigest string `json:"item_digest,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	SourceLine int    `json:"source_line,omitempty"`
}

type OwnQueueClaimer struct {
	Dir       string
	TTL       time.Duration
	Now       func() time.Time
	Assess    func(string) surface.State
	Authorize func(seat, backlogPath string) (bool, string)
	Protected func(string) (bool, string)
	Emit      func(OwnQueueEvent)
}

// Claim atomically revalidates and selects work for seat. handled is true when
// the queue is known to contain actionable work, including a durable duplicate;
// callers must not replace such a decision with a generic heartbeat.
func (c *OwnQueueClaimer) Claim(seat, backlogPath string, tick uint64) (job Job, handled bool) {
	// This callback runs only after the detector found a heartbeat warrant. Once
	// entered, every outcome belongs to the claim path; a refusal must not fall
	// through to a generic, less-specific heartbeat.
	handled = true
	event := OwnQueueEvent{Schema: OwnQueueLeaseSchema, Seat: seat, SourcePath: backlogPath}
	emit := func(result, reason string) (Job, bool) {
		event.Result, event.Reason = result, reason
		if c.Emit != nil {
			c.Emit(event)
		}
		return Job{}, handled
	}
	if seat == "" || backlogPath == "" {
		return emit("refused", "missing seat or own-backlog path")
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return emit("refused", "state directory unavailable: "+err.Error())
	}
	lock, err := acquireOwnQueueLock(filepath.Join(c.Dir, "flotilla-own-queue-"+seat+".lock"))
	if err != nil {
		return emit("refused", "claim lock unavailable: "+err.Error())
	}
	defer lock.Close()

	if c.Assess == nil || c.Assess(seat) != surface.StateIdle {
		return emit("refused", "fresh pane proof is not idle")
	}
	if c.Authorize == nil {
		return emit("refused", "authority resolver unavailable")
	}
	if ok, reason := c.Authorize(seat, backlogPath); !ok {
		return emit("refused", "authority mismatch: "+reason)
	}
	if c.Protected == nil {
		return emit("refused", "protected-window resolver unavailable")
	}
	if protected, reason := c.Protected(seat); protected {
		return emit("refused", "operator protected window: "+reason)
	}
	raw, err := os.ReadFile(backlogPath)
	if err != nil {
		return emit("refused", "queue unreadable: "+err.Error())
	}
	scan := backlog.Scan(string(raw))
	if !scan.Found {
		return emit("refused", "queue unknown: missing ## Backlog section")
	}
	leasePath := c.leasePath(seat)
	previous, exists := loadOwnQueueLease(leasePath)
	var selected *backlog.Item
	for i := range scan.Items {
		if scan.Items[i].Classification == "malformed" {
			return emit("refused", fmt.Sprintf("queue malformed at line %d", scan.Items[i].StartLine))
		}
		if selected == nil && scan.Items[i].Classification == "in-flight" {
			selected = &scan.Items[i]
		}
	}
	if selected == nil {
		for i := range scan.Items {
			if scan.Items[i].Classification == "next" {
				selected = &scan.Items[i]
				break
			}
		}
	}
	if selected == nil {
		if exists {
			_ = os.Remove(leasePath)
		}
		return emit("no-eligible-item", "queue has no in-flight or next item")
	}
	digest := ownQueueDigest(seat, selected.Raw)
	event.ItemDigest, event.SourceLine = digest, selected.StartLine
	now := c.now()
	if exists && previous.ItemDigest == digest && now.Before(previous.ExpiresAt) {
		return emit("already-claimed", "durable lease is active")
	}
	result := "claimed"
	if selected.Classification == "in-flight" {
		result = "resumed"
	}
	if exists && previous.ItemDigest == digest && !now.Before(previous.ExpiresAt) {
		result = "expiry-recovery"
	}
	lease := OwnQueueLease{Schema: OwnQueueLeaseSchema, Seat: seat, ItemDigest: digest,
		SourcePath: backlogPath, SourceLine: selected.StartLine, ClaimedAt: now,
		ExpiresAt: now.Add(c.ttl()), DetectorTick: tick, Outcome: "pending-delivery"}
	if err := saveOwnQueueLease(leasePath, lease); err != nil {
		return emit("refused", "lease persistence failed: "+err.Error())
	}
	event.Result = result
	if c.Emit != nil {
		c.Emit(event)
	}
	message := fmt.Sprintf("[flotilla own-queue] Resume the claimed backlog item at %s:%d:\n%s\nThe durable claim digest is %s. Update the backlog status as work advances.", backlogPath, selected.StartLine, selected.Head, digest)
	return Job{Agent: seat, Message: message, Kind: KindDetector, ClaimKey: OwnQueueClaimPrefix + seat + ":" + digest}, true
}

// Reconcile clears a delivered lease after its exact item is completed or
// removed. It is safe to call during the detector's off-lock queue snapshot.
func (c *OwnQueueClaimer) Reconcile(seat, backlogPath string) {
	lock, err := acquireOwnQueueLock(filepath.Join(c.Dir, "flotilla-own-queue-"+seat+".lock"))
	if err != nil {
		return
	}
	defer lock.Close()
	path := c.leasePath(seat)
	lease, exists := loadOwnQueueLease(path)
	if !exists {
		return
	}
	raw, err := os.ReadFile(backlogPath)
	if err != nil {
		return // unreadable evidence never clears a durable claim
	}
	for _, item := range backlog.Scan(string(raw)).Items {
		if ownQueueDigest(seat, item.Raw) == lease.ItemDigest && (item.Classification == "in-flight" || item.Classification == "next") {
			return
		}
	}
	_ = os.Remove(path)
	if c.Emit != nil {
		c.Emit(OwnQueueEvent{Schema: OwnQueueLeaseSchema, Seat: seat, Result: "completed", ItemDigest: lease.ItemDigest, SourcePath: lease.SourcePath, SourceLine: lease.SourceLine})
	}
}

func (c *OwnQueueClaimer) Confirm(key string) { c.finish(key, "delivered", false) }
func (c *OwnQueueClaimer) Abort(key string)   { c.finish(key, "delivery-failed", true) }

func (c *OwnQueueClaimer) finish(key, outcome string, release bool) {
	seat, digest, ok := parseOwnQueueClaimKey(key)
	if !ok {
		return
	}
	lock, err := acquireOwnQueueLock(filepath.Join(c.Dir, "flotilla-own-queue-"+seat+".lock"))
	if err != nil {
		return
	}
	defer lock.Close()
	path := c.leasePath(seat)
	lease, exists := loadOwnQueueLease(path)
	if !exists || lease.ItemDigest != digest {
		return
	}
	if release {
		_ = os.Remove(path)
	} else {
		lease.Outcome = outcome
		_ = saveOwnQueueLease(path, lease)
	}
	if c.Emit != nil {
		c.Emit(OwnQueueEvent{Schema: OwnQueueLeaseSchema, Seat: seat, Result: outcome, ItemDigest: digest, SourcePath: lease.SourcePath, SourceLine: lease.SourceLine})
	}
}

func (c *OwnQueueClaimer) leasePath(seat string) string {
	return filepath.Join(c.Dir, "flotilla-own-queue-"+seat+".json")
}
func (c *OwnQueueClaimer) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}
func (c *OwnQueueClaimer) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 2 * time.Hour
}

func ownQueueDigest(seat, raw string) string {
	s := sha256.Sum256([]byte(seat + "\x00" + strings.TrimSpace(raw)))
	return hex.EncodeToString(s[:])
}

func parseOwnQueueClaimKey(key string) (string, string, bool) {
	if !strings.HasPrefix(key, OwnQueueClaimPrefix) {
		return "", "", false
	}
	p := strings.SplitN(strings.TrimPrefix(key, OwnQueueClaimPrefix), ":", 2)
	if len(p) != 2 || p[0] == "" || p[1] == "" {
		return "", "", false
	}
	return p[0], p[1], true
}

func loadOwnQueueLease(path string) (OwnQueueLease, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return OwnQueueLease{}, false
	}
	var lease OwnQueueLease
	if json.Unmarshal(raw, &lease) != nil || lease.Schema != OwnQueueLeaseSchema {
		return OwnQueueLease{}, false
	}
	return lease, true
}

func saveOwnQueueLease(path string, lease OwnQueueLease) error {
	raw, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".own-queue-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(raw, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

type ownQueueLock struct{ f *os.File }

func acquireOwnQueueLock(path string) (*ownQueueLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("claim already in progress")
		}
		return nil, err
	}
	return &ownQueueLock{f: f}, nil
}
func (l *ownQueueLock) Close() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
