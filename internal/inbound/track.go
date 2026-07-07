// Package inbound is the recipient-side pending-dispatch ledger (#472). It complements
// sender-side internal/outbox (#475): confirmed delivery proves a turn STARTED, not that the
// desk retained and addressed the dispatch in its turn-final.
package inbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Entry is one confirmed inbound dispatch awaiting acknowledgment in a turn-final.
type Entry struct {
	ID          string    `json:"id"`
	Sender      string    `json:"sender"`
	Recipient   string    `json:"recipient"`
	Message     string    `json:"message"`
	Nonce       string    `json:"nonce"` // echoed in turn-final or explicit clear
	DeliveredAt time.Time `json:"delivered_at"`
	Deferrals   int       `json:"deferrals"` // reinject count; escalate at 2
}

// Action is what the detector should do when a finish edge finds an unacked dispatch.
type Action struct {
	Entry   Entry
	Reinject bool
	Escalate bool // notify dispatching coordinator (sender)
}

// Tracker holds per-recipient pending inbound dispatches in memory (tests and multi-desk
// sketches). Production wiring uses Store per recipient (#472 step 1).
type Tracker struct {
	mu      sync.Mutex
	pending map[string][]Entry // recipient → oldest-first
}

// NewTracker builds an empty inbound tracker.
func NewTracker() *Tracker {
	return &Tracker{pending: make(map[string][]Entry)}
}

// NewID returns a random hex id for a ledger entry.
func NewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("inbound: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// NewNonce returns a short marker agents can echo back in turn-finals.
func NewNonce() (string, error) {
	id, err := NewID()
	if err != nil {
		return "", err
	}
	return "flotilla-dispatch-" + id[:8], nil
}

// Track records a confirmed inbound dispatch for recipient.
func (t *Tracker) Track(e Entry) {
	if e.Recipient == "" || e.Message == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[e.Recipient] = append(t.pending[e.Recipient], e)
}

// Acknowledged reports whether turnFinal acknowledges entry (nonce echo or substantive overlap).
func Acknowledged(turnFinal string, e Entry) bool {
	if turnFinal == "" {
		return false
	}
	if e.Nonce != "" && strings.Contains(turnFinal, e.Nonce) {
		return true
	}
	// Cheap fallback: a distinctive substring from the dispatch body appears in the report.
	needle := distinctiveSnippet(e.Message)
	if needle != "" && strings.Contains(turnFinal, needle) {
		return true
	}
	return false
}

func distinctiveSnippet(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	// Prefer first non-empty line, capped — enough to match without full body echo.
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 24 {
			if len(line) > 80 {
				return line[:80]
			}
			return line
		}
	}
	if len(msg) > 80 {
		return msg[:80]
	}
	return msg
}

// OnFinish evaluates pending entries for recipient against turnFinal. Returns actions for
// reinject (first miss) or escalate-to-sender (second miss). Acknowledged entries are removed.
func (t *Tracker) OnFinish(recipient, turnFinal string) []Action {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.pending[recipient]
	if len(list) == 0 {
		return nil
	}
	actions, remaining := evaluateFinish(list, turnFinal)
	if len(remaining) == 0 {
		delete(t.pending, recipient)
	} else {
		t.pending[recipient] = remaining
	}
	return actions
}

// evaluateFinish is the shared finish-edge policy for Tracker and Store.
func evaluateFinish(list []Entry, turnFinal string) (actions []Action, remaining []Entry) {
	for _, e := range list {
		if Acknowledged(turnFinal, e) {
			continue
		}
		e.Deferrals++
		if e.Deferrals >= 2 {
			actions = append(actions, Action{Entry: e, Escalate: true})
			continue
		}
		actions = append(actions, Action{Entry: e, Reinject: true})
		remaining = append(remaining, e)
	}
	return actions, remaining
}

// Pending returns a copy of pending entries for recipient (tests).
func (t *Tracker) Pending(recipient string) []Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	src := t.pending[recipient]
	out := make([]Entry, len(src))
	copy(out, src)
	return out
}

// ReinjectPreamble prefixes a one-shot resume message.
func ReinjectPreamble(e Entry) string {
	return "[flotilla dropped-dispatch resume] A confirmed dispatch from " + e.Sender +
		" was delivered but not addressed before you went idle" +
		" (an intervening duty turn may have displaced it). Resume now — nonce `" + e.Nonce + "`.\n\n" +
		e.Message
}