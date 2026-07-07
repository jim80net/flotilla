package inbound

// CoordinatorPredicate reports whether an agent is a coordinator seat. Inbound tracking
// skips coordinators so finish evaluation does not grow unbounded (#472, #494).
type CoordinatorPredicate func(agent string) bool

// TrackConfirmedSend records a confirmed inter-agent delivery into the recipient's durable
// inbound ledger under flock (#494 CLI path, #472 daemon path). Coordinator recipients are
// skipped when isCoordinator reports true.
func TrackConfirmedSend(rosterDir, sender, recipient, message, entryID string, isCoordinator CoordinatorPredicate) error {
	if rosterDir == "" || sender == "" || recipient == "" || message == "" {
		return nil
	}
	if isCoordinator != nil && isCoordinator(recipient) {
		return nil
	}
	return Record(rosterDir, Entry{
		ID: entryID, Sender: sender, Recipient: recipient, Message: message,
		Nonce: ParseDispatchNonce(message),
	})
}
