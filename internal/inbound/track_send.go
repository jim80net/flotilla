package inbound

import "log"

// CoordinatorPredicate reports whether an agent is a coordinator seat. Inbound tracking
// skips coordinators so finish evaluation does not grow unbounded (#472, #494).
type CoordinatorPredicate func(agent string) bool

// TrackDecision is the journal outcome of TrackConfirmedSend (#498).
type TrackDecision string

const (
	// TrackRecorded: entry written (or idempotent duplicate id).
	TrackRecorded TrackDecision = "recorded"
	// TrackSkipped: recipient is a coordinator — ledger deliberately not grown.
	TrackSkipped TrackDecision = "skipped"
	// TrackNoop: missing required args; no journal line (not a policy decision).
	TrackNoop TrackDecision = "noop"
)

// TrackConfirmedSend records a confirmed inter-agent delivery into the recipient's durable
// inbound ledger under flock (#494 CLI path, #472 daemon path). Coordinator recipients are
// skipped when isCoordinator reports true.
//
// Journal (stderr via log package) — one line per policy decision (#498):
//
//	inbound track <recipient> skipped reason=coordinator
//	inbound track <recipient> recorded reason=ok
func TrackConfirmedSend(rosterDir, sender, recipient, message, entryID string, isCoordinator CoordinatorPredicate) (TrackDecision, error) {
	if rosterDir == "" || sender == "" || recipient == "" || message == "" {
		return TrackNoop, nil
	}
	if isCoordinator != nil && isCoordinator(recipient) {
		log.Printf("inbound track %s skipped reason=coordinator", recipient)
		return TrackSkipped, nil
	}
	if err := Record(rosterDir, Entry{
		ID: entryID, Sender: sender, Recipient: recipient, Message: message,
		Nonce: ParseDispatchNonce(message),
	}); err != nil {
		log.Printf("inbound track %s failed: %v", recipient, err)
		return TrackNoop, err
	}
	log.Printf("inbound track %s recorded reason=ok", recipient)
	return TrackRecorded, nil
}
