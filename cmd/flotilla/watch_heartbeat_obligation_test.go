package main

import "testing"

func TestHeartbeatOwedWorkGateRequiresPositiveUnblockedWork(t *testing.T) {
	tests := []struct {
		name          string
		unblocked     []string
		pendingOutbox bool
		held          bool
		want          bool
	}{
		{name: "completed-idle-after-clean-recycle"},
		{name: "unblocked-backlog", unblocked: []string{"- [in-flight] finish rollout"}, want: true},
		{name: "pending-recipient-outbox", pendingOutbox: true, want: true},
		{name: "explicit-hold-suppresses-backlog", unblocked: []string{"- [next] resume after hold"}, held: true},
		{name: "explicit-hold-suppresses-outbox", pendingOutbox: true, held: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gate := heartbeatOwedWorkGate(
				func() bool { return len(tc.unblocked) > 0 },
				func() bool { return tc.pendingOutbox },
				func() bool { return tc.held },
			)
			if got := gate(); got != tc.want {
				t.Fatalf("heartbeat owed work = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHeartbeatOwedWorkGateWithoutConfiguredSourcesIsNotOwed(t *testing.T) {
	if heartbeatOwedWorkGate(nil, nil, nil)() {
		t.Fatal("missing evidence must not manufacture a heartbeat obligation")
	}
}
