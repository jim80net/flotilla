package dispatch

import (
	"fmt"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

// Disposition is the lifecycle state of a dispatch nonce (#614).
type Disposition string

const (
	DispositionUnknown     Disposition = "unknown"
	DispositionQueued      Disposition = "queued"      // in sender outbox, not yet pane-confirmed
	DispositionDelivered   Disposition = "delivered"   // inbound ledger pending ack
	DispositionConsumed    Disposition = "consumed"    // durable consumed registry
	DispositionUndelivered Disposition = "undelivered" // queued past age bound
)

// Status is the resolved view for `flotilla dispatch-status`.
type Status struct {
	Nonce       string
	Disposition Disposition
	Sender      string
	Recipient   string
	PayloadHash string
	Reason      string // consume reason when consumed
	ID          string // outbox or inbound id
	Age         time.Duration
	Detail      string
}

// LookupNonce resolves a nonce across consumed → inbound → outbox (first hit
// wins), with one refinement (#707): a send-time coordinator-hop settlement
// ranks BELOW a live inbound row for the same nonce — the same dispatch text
// can sit pending (or stale) on a desk after the coordinator hop settled, and
// the probe must surface the live copy, not mask it. Preference order:
// consumed(real) > inbound > consumed(coordinator-hop) > outbox.
func LookupNonce(rosterDir, nonce string, now time.Time) Status {
	nonce = strings.TrimSpace(nonce)
	st := Status{Nonce: nonce, Disposition: DispositionUnknown}
	if rosterDir == "" || nonce == "" {
		st.Detail = "missing roster or nonce"
		return st
	}
	if e, ok := NewRegistry(rosterDir).LookupNonce(nonce); ok {
		consumed := Status{
			Nonce:       nonce,
			Disposition: DispositionConsumed,
			Sender:      e.Sender,
			Recipient:   e.Recipient,
			PayloadHash: e.PayloadHash,
			Reason:      e.Reason,
			Detail:      fmt.Sprintf("consumed reason=%s at %s", e.Reason, e.ConsumedAt.UTC().Format(time.RFC3339)),
		}
		if !e.ConsumedAt.IsZero() {
			consumed.Age = now.Sub(e.ConsumedAt).Round(time.Second)
		}
		if e.Reason != ReasonCoordinatorRecipient {
			return consumed
		}
		if live := lookupInboundNonce(rosterDir, nonce, now); live != nil {
			return *live
		}
		return consumed
	}
	if live := lookupInboundNonce(rosterDir, nonce, now); live != nil {
		return *live
	}
	for _, e := range outbox.ListAll(rosterDir) {
		if inbound.ParseDispatchNonce(e.Message) != nonce {
			continue
		}
		st.Sender = e.Sender
		st.Recipient = e.Recipient
		st.ID = e.ID
		st.PayloadHash = PayloadHash(e.Message)
		if !e.EnqueuedAt.IsZero() {
			st.Age = now.Sub(e.EnqueuedAt).Round(time.Second)
		}
		if st.Age >= UndeliveredOutboxAge && e.LastStaleEscalation.IsZero() {
			st.Disposition = DispositionUndelivered
			st.Detail = "outbox queued past undelivered age; pane not confirmed"
			return st
		}
		st.Disposition = DispositionQueued
		st.Detail = "queued in sender outbox; waiting for recipient idle"
		return st
	}
	st.Detail = "nonce not found in consumed, inbound, or outbox"
	return st
}

func lookupInboundNonce(rosterDir, nonce string, now time.Time) *Status {
	for _, e := range inbound.ListAll(rosterDir) {
		if e.Nonce != nonce {
			continue
		}
		st := Status{
			Nonce:       nonce,
			Disposition: DispositionDelivered,
			Sender:      e.Sender,
			Recipient:   e.Recipient,
			ID:          e.ID,
			PayloadHash: PayloadHash(e.Message),
			Detail:      "inbound pending durable ack",
		}
		if !e.DeliveredAt.IsZero() {
			st.Age = now.Sub(e.DeliveredAt).Round(time.Second)
			if st.Age >= UndeliveredInboundAge {
				st.Disposition = DispositionUndelivered
				st.Detail = "delivered to pane but unacknowledged past undelivered-ack age"
			}
		}
		return &st
	}
	return nil
}

// FormatStatus is a one-line desk-visible status.
func FormatStatus(s Status) string {
	parts := []string{
		"nonce=" + emptyDash(s.Nonce),
		"disposition=" + string(s.Disposition),
	}
	if s.Sender != "" || s.Recipient != "" {
		parts = append(parts, fmt.Sprintf("%s→%s", emptyDash(s.Sender), emptyDash(s.Recipient)))
	}
	if s.ID != "" {
		parts = append(parts, "id="+s.ID)
	}
	if s.Reason != "" {
		parts = append(parts, "reason="+s.Reason)
	}
	if s.Age > 0 {
		parts = append(parts, "age="+s.Age.String())
	}
	if s.Detail != "" {
		parts = append(parts, s.Detail)
	}
	return strings.Join(parts, " ")
}
