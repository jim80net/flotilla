package dispatch

import (
	"fmt"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

// UndeliveredOutboxAge is how long a busy-queued send may sit before a loud
// journal/escalation line is emitted (#614). Aligns with outbox.StaleMaxAge so
// the first coordinator-surface alert and the dispatch undelivered marker fire
// on the same bound.
const UndeliveredOutboxAge = outbox.StaleMaxAge

// UndeliveredInboundAge is how long a confirmed-delivered dispatch may remain
// unacknowledged in the recipient inbound ledger before a loud undelivered-ack
// surface (#614). Distinct from the #472 reinject path (first miss reinjects;
// second escalates) — this is the age-based LOUD observability arm.
const UndeliveredInboundAge = 15 * time.Minute

// UndeliveredReport is one loud undelivered observation for journal / escalate.
type UndeliveredReport struct {
	Kind      string // "outbox" | "inbound-ack"
	ID        string
	Nonce     string
	Sender    string
	Recipient string
	Age       time.Duration
	Message   string // human-readable escalate line
}

// ScanUndeliveredOutbox returns outbox entries older than age that have not yet
// received a stale-escalation stamp (exactly-once loud surface).
func ScanUndeliveredOutbox(rosterDir string, now time.Time, age time.Duration) []UndeliveredReport {
	if rosterDir == "" {
		return nil
	}
	if age <= 0 {
		age = UndeliveredOutboxAge
	}
	var out []UndeliveredReport
	for _, e := range outbox.ListAll(rosterDir) {
		if e.EnqueuedAt.IsZero() {
			continue
		}
		if !e.LastStaleEscalation.IsZero() {
			// Already loud-surfaced via #477; still report for status, but mark as known.
			continue
		}
		got := now.Sub(e.EnqueuedAt)
		if got < age {
			continue
		}
		nonce := inbound.ParseDispatchNonce(e.Message)
		out = append(out, UndeliveredReport{
			Kind:      "outbox",
			ID:        e.ID,
			Nonce:     nonce,
			Sender:    e.Sender,
			Recipient: e.Recipient,
			Age:       got.Round(time.Second),
			Message: fmt.Sprintf(
				"dispatch undelivered: outbox send id=%s nonce=%s %s→%s still queued after %s — pane has not confirmed delivery",
				e.ID, emptyDash(nonce), e.Sender, e.Recipient, got.Round(time.Second),
			),
		})
	}
	return out
}

// ScanUndeliveredInbound returns inbound pending entries older than age that
// were never acknowledged (still on the ledger).
func ScanUndeliveredInbound(rosterDir string, now time.Time, age time.Duration) []UndeliveredReport {
	if rosterDir == "" {
		return nil
	}
	if age <= 0 {
		age = UndeliveredInboundAge
	}
	var out []UndeliveredReport
	for _, e := range inbound.ListAll(rosterDir) {
		if e.DeliveredAt.IsZero() {
			continue
		}
		got := now.Sub(e.DeliveredAt)
		if got < age {
			continue
		}
		// Skip if already consumed (finish path lag / race).
		if IsConsumed(rosterDir, e.Nonce, PayloadHash(e.Message)) {
			continue
		}
		out = append(out, UndeliveredReport{
			Kind:      "inbound-ack",
			ID:        e.ID,
			Nonce:     e.Nonce,
			Sender:    e.Sender,
			Recipient: e.Recipient,
			Age:       got.Round(time.Second),
			Message: fmt.Sprintf(
				"dispatch undelivered-ack: inbound id=%s nonce=%s %s→%s unacknowledged after %s (delivered to pane, no turn-final ack)",
				e.ID, emptyDash(e.Nonce), e.Sender, e.Recipient, got.Round(time.Second),
			),
		})
	}
	return out
}

// ScanUndelivered returns outbox + inbound undelivered reports.
func ScanUndelivered(rosterDir string, now time.Time) []UndeliveredReport {
	out := ScanUndeliveredOutbox(rosterDir, now, UndeliveredOutboxAge)
	out = append(out, ScanUndeliveredInbound(rosterDir, now, UndeliveredInboundAge)...)
	return out
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
