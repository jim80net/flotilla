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
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	return OperatorProtectedWindow(ProtectedWindowInput{
		Leader: d.Leader,
		Awaiting: func(string) bool {
			return NewAwaitingMarker(d.AwaitingPath).Present()
		},
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
