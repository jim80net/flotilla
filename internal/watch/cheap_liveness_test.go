package watch

import (
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
	wd := NewWatchdog(3, nil)
	h := NewHeartbeat(time.Minute, "cos", "expensive Grok prompt", c.enqueue, nil)
	h.SetGate(func() bool { return LegacyHeartbeatGate(wd, ack, false, false, needsBrain) })
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
			h := NewHeartbeat(time.Minute, "cos", "targeted poke", c.enqueue, nil)
			h.SetGate(func() bool {
				return LegacyHeartbeatGate(NewWatchdog(3, nil), NewAckWatcher(filepath.Join(t.TempDir(), "alive")), false, false, needsBrain)
			})
			h.tick()
			if c.count() != 1 || c.jobs[0].Kind != KindHeartbeat {
				t.Fatalf("%s cycle jobs=%+v, want one KindHeartbeat poke", tc.name, c.jobs)
			}
		})
	}
}

func TestCheapLivenessMissedAcksAlertAfterKMisses(t *testing.T) {
	var alerts []string
	wd := NewWatchdog(3, func(message string) { alerts = append(alerts, message) })
	ack := NewAckWatcher(filepath.Join(t.TempDir(), "missing", "alive"))
	for i := 0; i < 2; i++ {
		LegacyHeartbeatGate(wd, ack, false, false, false)
	}
	if len(alerts) != 0 {
		t.Fatalf("cheap ack alerted before K misses: %v", alerts)
	}
	LegacyHeartbeatGate(wd, ack, false, false, false)
	if len(alerts) != 1 || !wd.Down() {
		t.Fatalf("after K cheap-ack misses alerts=%v down=%t, want one/down", alerts, wd.Down())
	}
	if got := alerts[0]; !strings.Contains(got, "cheap liveness") {
		t.Fatalf("cheap-ack alert=%q, want cheap-liveness diagnosis", got)
	}
}
