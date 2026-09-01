package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestCheapLivenessAllIdleSkipsKindHeartbeat(t *testing.T) {
	states := map[string]surface.State{"cos": surface.StateIdle, "backend": surface.StateIdle}
	ledgers := map[string]backlog.Status{
		"backend": backlog.Parse("## Backlog\n- [done] shipped\n"),
	}
	needsBrain, reasons := FleetNeedsBrain(states, ledgers)
	if needsBrain || len(reasons) != 0 {
		t.Fatalf("all-idle fleet needsBrain=%t reasons=%v", needsBrain, reasons)
	}

	c := &collector{}
	ack := NewAckWatcher(filepath.Join(t.TempDir(), "alive"))
	guard := NewLegacyHeartbeatGuard(NewWatchdog(3, nil), NewWatchdog(3, nil), ack)
	h := NewHeartbeat(time.Minute, "cos", "expensive Grok prompt", c.enqueue, nil)
	h.SetGate(func() bool { return guard.Gate(surface.StateIdle, true, needsBrain) })
	h.tick()
	if c.count() != 0 {
		t.Fatalf("all-idle cheap cycle enqueued %d heartbeat(s), want zero", c.count())
	}
	if age := ack.Age(); age > time.Second {
		t.Fatalf("cheap ack age=%v, want fresh", age)
	}
}

func TestCheapLivenessBlockedOrErroredDeskStillPokes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   surface.State
		ledgers map[string]backlog.Status
	}{
		{name: "blocked", state: surface.StateAwaitingInput},
		{name: "errored", state: surface.StateErrored},
		{
			name: "blocked-ledger", state: surface.StateIdle,
			ledgers: map[string]backlog.Status{
				"backend": backlog.Parse("## Backlog\n- [blocked] needs operator judgment\n"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			needsBrain, reasons := FleetNeedsBrain(
				map[string]surface.State{"cos": surface.StateIdle, "backend": tc.state}, tc.ledgers,
			)
			if !needsBrain || len(reasons) == 0 {
				t.Fatalf("state=%s needsBrain=%t reasons=%v", tc.state, needsBrain, reasons)
			}
			c := &collector{}
			guard := NewLegacyHeartbeatGuard(NewWatchdog(3, nil), NewWatchdog(3, nil), NewAckWatcher(filepath.Join(t.TempDir(), "alive")))
			h := NewHeartbeat(time.Minute, "cos", "targeted poke", c.enqueue, nil)
			h.SetGate(func() bool { return guard.Gate(surface.StateIdle, true, needsBrain) })
			h.tick()
			if c.count() != 1 || c.jobs[0].Kind != KindHeartbeat {
				t.Fatalf("%s cycle jobs=%+v, want one KindHeartbeat poke", tc.name, c.jobs)
			}
		})
	}
}

func TestCheapLivenessMissedAcksAlertAfterKMisses(t *testing.T) {
	var alerts []string
	cheapWatchdog := NewWatchdog(3, func(message string) { alerts = append(alerts, message) })
	ack := NewAckWatcher(filepath.Join(t.TempDir(), "missing", "alive"))
	guard := NewLegacyHeartbeatGuard(NewWatchdog(3, nil), cheapWatchdog, ack)
	for i := 0; i < 2; i++ {
		guard.Gate(surface.StateIdle, true, false)
	}
	if len(alerts) != 0 {
		t.Fatalf("cheap ack alerted before K misses: %v", alerts)
	}
	guard.Gate(surface.StateIdle, true, false)
	if len(alerts) != 1 || !cheapWatchdog.Down() {
		t.Fatalf("after K cheap-ack misses alerts=%v down=%t, want one/down", alerts, cheapWatchdog.Down())
	}
	if got := alerts[0]; !strings.Contains(got, "cheap liveness") {
		t.Fatalf("cheap-ack alert=%q, want cheap-liveness diagnosis", got)
	}
}

func TestCheapLivenessStillAlertsWhenAdmittedXOMissesKAcks(t *testing.T) {
	var alerts []string
	xoWatchdog := NewWatchdog(3, func(message string) { alerts = append(alerts, message) })
	ack := NewAckWatcher(filepath.Join(t.TempDir(), "alive"))
	guard := NewLegacyHeartbeatGuard(xoWatchdog, NewWatchdog(3, nil), ack)

	if gated := guard.Gate(surface.StateIdle, true, true); gated {
		t.Fatal("first warranted heartbeat was gated")
	}
	for i := 0; i < 2; i++ {
		guard.Gate(surface.StateIdle, true, false)
	}
	if len(alerts) != 0 {
		t.Fatalf("XO watchdog alerted before K misses: %v", alerts)
	}
	guard.Gate(surface.StateIdle, true, false)
	if len(alerts) != 1 || !xoWatchdog.Down() {
		t.Fatalf("after K missed XO acks alerts=%v down=%t, want one/down", alerts, xoWatchdog.Down())
	}
}

func TestCheapLivenessAgentAckRemainsIndependentOfWatchSignal(t *testing.T) {
	ackPath := filepath.Join(t.TempDir(), "alive")
	ack := NewAckWatcher(ackPath)
	guard := NewLegacyHeartbeatGuard(NewWatchdog(3, nil), NewWatchdog(3, nil), ack)
	if gated := guard.Gate(surface.StateIdle, true, true); gated {
		t.Fatal("first warranted heartbeat was gated")
	}
	if err := os.Chtimes(ackPath, time.Now(), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("touch agent ack: %v", err)
	}
	guard.Gate(surface.StateIdle, true, false)
	if guard.awaitingXOAck {
		t.Fatal("agent-owned touch was masked by watch's cheap signal")
	}
}

func TestCheapLivenessUnresolvedXOPaneAlertsImmediately(t *testing.T) {
	var alerts []string
	xoWatchdog := NewWatchdog(3, func(message string) { alerts = append(alerts, message) })
	guard := NewLegacyHeartbeatGuard(xoWatchdog, NewWatchdog(3, nil), NewAckWatcher(filepath.Join(t.TempDir(), "alive")))
	guard.Gate(surface.StateUnknown, false, false)
	if len(alerts) != 1 || !xoWatchdog.Down() {
		t.Fatalf("unresolved XO alerts=%v down=%t, want immediate crash alert", alerts, xoWatchdog.Down())
	}
}
