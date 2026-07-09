package watch

import "testing"

func TestOperatorProtectedWindow_pendingRelay(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		RelayQueuePending: func(agent string) bool {
			return agent == "xo"
		},
	}
	if !OperatorProtectedWindow(in) {
		t.Fatal("pending relay queue entry should protect leader")
	}
}

func TestOperatorProtectedWindow_awaiting(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		Awaiting: func(agent string) bool {
			return agent == "xo"
		},
	}
	if !OperatorProtectedWindow(in) {
		t.Fatal("awaiting marker should protect leader")
	}
}

func TestOperatorProtectedWindow_injectorRelay(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		InjectorRelayPending: func(agent string) bool {
			return agent == "xo"
		},
	}
	if !OperatorProtectedWindow(in) {
		t.Fatal("in-flight injector relay should protect leader")
	}
}

func TestOperatorProtectedWindow_activeConversationTail(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		ActiveConversation: func(agent string) bool {
			return agent == "xo"
		},
	}
	if !OperatorProtectedWindow(in) {
		t.Fatal("active conversation tail should protect leader")
	}
}

func TestOperatorProtectedWindow_failSafeUnreadable(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		ActiveConversation: func(string) bool {
			return true // corrupt/unreadable sidecar wired fail-safe
		},
	}
	if !OperatorProtectedWindow(in) {
		t.Fatal("unreadable active-conversation sidecar should fail-safe to protected")
	}
}

func TestOperatorProtectedWindow_allClear(t *testing.T) {
	in := ProtectedWindowInput{
		Leader: "xo",
		Awaiting: func(string) bool {
			return false
		},
		RelayQueuePending: func(string) bool {
			return false
		},
		InjectorRelayPending: func(string) bool {
			return false
		},
		ActiveConversation: func(string) bool {
			return false
		},
	}
	if OperatorProtectedWindow(in) {
		t.Fatal("all-clear signals should not protect leader")
	}
}

func TestOperatorProtectedWindow_emptyLeader(t *testing.T) {
	if OperatorProtectedWindow(ProtectedWindowInput{Awaiting: func(string) bool { return true }}) {
		t.Fatal("empty leader should not protect")
	}
}
