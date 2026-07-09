package watch

// ProtectedWindowInput carries mechanical signals for operator protected-window detection (#523).
// Kind/source labels are not used here — only durable operator-conversation signals.
type ProtectedWindowInput struct {
	Leader string
	// Awaiting reports the awaiting-operator marker for leader.
	Awaiting func(string) bool
	// RelayQueuePending reports a durable pending relay queue entry for leader.
	RelayQueuePending func(string) bool
	// InjectorRelayPending reports an in-flight relay in the injector worker queue.
	InjectorRelayPending func(string) bool
	// ActiveConversation reports post-confirmed relay tail within TTL for leader.
	ActiveConversation func(string) bool
	// BridgeComposeActive is optional dash bridge compose state; nil ⇒ inert.
	BridgeComposeActive func(string) bool
}

// OperatorProtectedWindow reports whether leader must not receive a routine adjutant seam
// inject. Fail-safe: any positive signal suppresses inject; ambiguity in wired closures
// (awaiting unreadable, active-conversation sidecar corrupt) must return true from those
// closures — this function OR-combines sources only.
func OperatorProtectedWindow(in ProtectedWindowInput) bool {
	if in.Leader == "" {
		return false
	}
	if in.Awaiting != nil && in.Awaiting(in.Leader) {
		return true
	}
	if in.RelayQueuePending != nil && in.RelayQueuePending(in.Leader) {
		return true
	}
	if in.InjectorRelayPending != nil && in.InjectorRelayPending(in.Leader) {
		return true
	}
	if in.ActiveConversation != nil && in.ActiveConversation(in.Leader) {
		return true
	}
	if in.BridgeComposeActive != nil && in.BridgeComposeActive(in.Leader) {
		return true
	}
	return false
}
