package watch

import "time"

// LayerProtectedWindowDeps carries production seams for one coordinator layer (#523).
type LayerProtectedWindowDeps struct {
	Leader                 string
	Adjutant               string
	AwaitingPath           string
	RelayQueuePath         string
	ActiveConversationPath string
	Injector               *Injector
	Now                    func() time.Time
}

// OperatorProtectedForLayer evaluates the mechanical protected-window predicate for leader.
func OperatorProtectedForLayer(d LayerProtectedWindowDeps) bool {
	return operatorProtectedForLayer(d, true)
}

// OperatorReplyProtectedForLayer evaluates the protected-window predicate for a
// buffered operator reply headed to the leader. The awaiting marker is excluded:
// the reply may contain the authority decision that clears it. Every concrete
// conversation/compose signal remains protective.
func OperatorReplyProtectedForLayer(d LayerProtectedWindowDeps) bool {
	return operatorProtectedForLayer(d, false)
}

func operatorProtectedForLayer(d LayerProtectedWindowDeps, includeAwaiting bool) bool {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	var awaiting func(string) bool
	if includeAwaiting {
		awaiting = func(string) bool {
			return NewAwaitingMarker(d.AwaitingPath).Present()
		}
	}
	return OperatorProtectedWindow(ProtectedWindowInput{
		Leader:   d.Leader,
		Awaiting: awaiting,
		RelayQueuePending: func(string) bool {
			return RelayQueuePendingLayer(d.RelayQueuePath, d.Leader, d.Adjutant)
		},
		InjectorRelayPending: func(string) bool {
			return InjectorRelayPendingLayer(d.Injector, d.Leader, d.Adjutant)
		},
		ActiveConversation: func(string) bool {
			return ActiveConversationTail(d.ActiveConversationPath, DefaultActiveConversationTTL, now())
		},
	})
}
