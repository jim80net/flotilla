package dispatch

import (
	"fmt"
	"log"
	"strings"
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
// second escalates) — this is the age-based LOUD observability arm (layer 1:
// adjutant triage per #628).
const UndeliveredInboundAge = 15 * time.Minute

// OperatorLayerMultiplier is how many × the layer-1 age bound must pass before
// the operator Discord webhook is a second-layer surface (#628). Layer 1 is
// always journal + adjutant (when configured); operator is not dual-fired on
// the first crossing.
const OperatorLayerMultiplier = 3

// OperatorLayerOutboxAge is the second-layer age for busy-outbox undelivered.
func OperatorLayerOutboxAge() time.Duration {
	return OperatorLayerMultiplier * UndeliveredOutboxAge
}

// OperatorLayerInboundAge is the second-layer age for unacked inbound.
func OperatorLayerInboundAge() time.Duration {
	return OperatorLayerMultiplier * UndeliveredInboundAge
}

// OperatorLayerAge returns the second-layer age bound for a report kind.
func OperatorLayerAge(kind string) time.Duration {
	if kind == "inbound-ack" {
		return OperatorLayerInboundAge()
	}
	return OperatorLayerOutboxAge()
}

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
// were never acknowledged (still on the ledger). Entries already in the consumed
// registry are skipped (and should have been removed by ReconcileInboundAcks).
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

// TurnFinalReader loads a desk's latest turn-final for reconcile (#628).
// ok=false means unreadable (no reader / no session) — not an error for suppress.
type TurnFinalReader func(agent string) (text string, ok bool, err error)

// ReconcileInboundAcks heals recipient inbound ledgers before undelivered scan (#628):
//  1. Remove entries whose nonce is already in the durable consumed registry
//  2. If readTurnFinal is set, remove entries the latest turn-final acknowledges
//     (same matcher as #472 OnFinish) and durable-consume them
//
// Returns how many entries were cleared. Prefer root-cause finish-edge consume;
// this is the sweep-time belt so false-positive undelivered-ack never reaches Discord.
func ReconcileInboundAcks(rosterDir string, readTurnFinal TurnFinalReader) int {
	if rosterDir == "" {
		return 0
	}
	reg := NewRegistry(rosterDir)
	n := 0
	for _, path := range inbound.ListInboundPaths(rosterDir) {
		recipient := inbound.RecipientFromInboundPath(path)
		if recipient == "" {
			continue
		}
		st := inbound.NewStore(path)
		clearedCons := st.ClearConsumed(func(nonce, message string) bool {
			return reg.IsConsumed(nonce, PayloadHash(message))
		})
		n += len(clearedCons)
		if readTurnFinal == nil {
			continue
		}
		text, ok, err := readTurnFinal(recipient)
		if err != nil || !ok || text == "" {
			continue
		}
		for _, e := range st.ClearAcknowledged(text) {
			n++
			if _, cerr := reg.Consume(ConsumeFromInbound(e.Nonce, e.Message, ReasonTurnFinalAck, e.Sender, e.Recipient)); cerr != nil {
				log.Printf("flotilla dispatch: reconcile consume-ack failed nonce=%s: %v", e.Nonce, cerr)
			}
		}
	}
	return n
}

// ScanUndelivered returns outbox + inbound undelivered reports.
func ScanUndelivered(rosterDir string, now time.Time) []UndeliveredReport {
	out := ScanUndeliveredOutbox(rosterDir, now, UndeliveredOutboxAge)
	out = append(out, ScanUndeliveredInbound(rosterDir, now, UndeliveredInboundAge)...)
	return out
}

// FormatAdjutantTriage builds the detector-wake body for layer-1 adjutant routing (#628).
// Actionable: nonce, path, age, recommended triage steps. No deployment-specific names.
func FormatAdjutantTriage(r UndeliveredReport) string {
	nonce := emptyDash(r.Nonce)
	var b strings.Builder
	b.WriteString("[flotilla undelivered-dispatch triage]\n")
	b.WriteString("kind: ")
	b.WriteString(r.Kind)
	b.WriteString("\n")
	b.WriteString("nonce: ")
	b.WriteString(nonce)
	b.WriteString("\n")
	b.WriteString("id: ")
	b.WriteString(emptyDash(r.ID))
	b.WriteString("\n")
	b.WriteString("from→to: ")
	b.WriteString(emptyDash(r.Sender))
	b.WriteString("→")
	b.WriteString(emptyDash(r.Recipient))
	b.WriteString("\n")
	b.WriteString("age: ")
	b.WriteString(r.Age.String())
	b.WriteString("\n\n")
	b.WriteString("Journal: ")
	b.WriteString(r.Message)
	b.WriteString("\n\n")
	b.WriteString("Recommended triage:\n")
	switch r.Kind {
	case "inbound-ack":
		b.WriteString("1. Check recipient pane (idle mid-turn without turn-final ack? crashed? busy?)\n")
		b.WriteString("2. If work is done: ensure turn-final echoes the nonce, or durable-consume it\n")
		b.WriteString("3. If still owed: reinject / re-send when idle (`flotilla send`)\n")
		b.WriteString("4. Escalate to operator only if stuck after triage (second-layer age)\n")
	default:
		b.WriteString("1. Check recipient pane (busy mid-turn is normal; crashed/blocked is not)\n")
		b.WriteString("2. Confirm outbox still holds the send; watch will deliver when idle\n")
		b.WriteString("3. If stuck/wedged: clear wedge or re-queue; do not silent-drop\n")
		b.WriteString("4. Escalate to operator only if stuck after triage (second-layer age)\n")
	}
	return b.String()
}

// FormatOperatorL2 appends second-layer context so the operator Discord line is
// distinct from a raw first-fire and names that adjutant triage already ran.
func FormatOperatorL2(r UndeliveredReport) string {
	return r.Message + " — second-layer: still undelivered after adjutant triage window; operator action may be needed"
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
