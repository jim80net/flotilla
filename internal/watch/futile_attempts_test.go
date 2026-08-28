package watch

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestFutileAttemptStormEscalatesOnceAndRecoveryResets(t *testing.T) {
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	now := base
	busy := true
	in := NewInjector(func(string, string) error {
		if busy {
			return surface.ErrBusy
		}
		return nil
	}, 1)
	in.internalWedgeDispatch = func(run func()) { run() }
	in.now = func() time.Time { return now }
	var alarms []string
	var operatorAlerts []string
	in.SetInternalWedgeAlert(func(_ string, msg string) { alarms = append(alarms, msg) })
	in.SetEscalate(func(msg string) { operatorAlerts = append(operatorAlerts, msg) })

	for i := 0; i < futileAttemptThreshold+20; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindDetector, Message: "time-relative tick"})
		now = now.Add(5 * time.Second)
	}
	if len(alarms) != 1 {
		t.Fatalf("drop storm alarms = %d, want exactly 1: %v", len(alarms), alarms)
	}
	for _, want := range []string{"desk", fmt.Sprint(futileAttemptThreshold), futileAttemptWindow.String(), base.Format(time.RFC3339)} {
		if !strings.Contains(alarms[0], want) {
			t.Fatalf("alarm %q missing %q", alarms[0], want)
		}
	}

	busy = false
	in.deliver(Job{Agent: "desk", Kind: KindDetector, Message: "recovery"})
	if got := len(alarms); got != 2 || !strings.Contains(alarms[1], "cleared") {
		t.Fatalf("confirmed recovery notices = %v, want one alarm + one clear", alarms)
	}

	busy = true
	for i := 0; i < futileAttemptThreshold; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindHeartbeat, Message: "new storm"})
		now = now.Add(5 * time.Second)
	}
	if len(alarms) != 2 {
		t.Fatalf("cooldown allowed second-wave alarm: %v", alarms)
	}
	if len(operatorAlerts) != 0 {
		t.Fatalf("wedge lifecycle leaked to operator alerts: %v", operatorAlerts)
	}

	now = in.futileAttempts["desk"].cooldownUntil.Add(time.Second)
	for i := 0; i < futileAttemptThreshold; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindHeartbeat, Message: "bounded rearm"})
		now = now.Add(5 * time.Second)
	}
	if len(alarms) != 3 || !strings.Contains(alarms[2], "delivery wedge") {
		t.Fatalf("cooldown did not re-arm after its bound: %v", alarms)
	}
}

func TestWorkingRecipientWithDetectorProgressNeverAccruesFutileAttempts(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
	in.internalWedgeDispatch = func(run func()) { run() }
	in.now = func() time.Time { return now }
	in.SetRecipientProgress(func(_ string, since time.Time) (outbox.RecipientClass, bool) {
		return outbox.RecipientWorking, !now.Before(since)
	})
	var internal, operator []string
	in.SetInternalWedgeAlert(func(_ string, msg string) { internal = append(internal, msg) })
	in.SetEscalate(func(msg string) { operator = append(operator, msg) })

	// More than two threshold-sized storms across more than five minutes. Every
	// refusal has independent detector progress in its active window.
	for i := 0; i < futileAttemptThreshold*2+5; i++ {
		in.deliver(Job{Agent: "working-seat", Kind: KindDetector})
		now = now.Add(5 * time.Second)
	}
	if len(internal) != 0 || len(operator) != 0 {
		t.Fatalf("Working+progress alarmed: internal=%v operator=%v", internal, operator)
	}
	if _, ok := in.futileAttempts["working-seat"]; ok {
		t.Fatal("Working+progress must not increment or retain the futile counter")
	}
}

func TestStaticWorkingDetectorProductionSeamAlarmsAsUnprogressing(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	d := NewDetector(DetectorConfig{
		XOAgent: "coordinator",
		Desks:   []string{"stalled-seat"},
		Assess:  func(string) surface.State { return surface.StateWorking },
		Now:     func() time.Time { return now },
		AckAge:  func() time.Duration { return 0 },
		Persist: func(Snapshot) error { return nil },
	}, filepath.Join(t.TempDir(), "missing-snapshot.json"))

	in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
	in.now = func() time.Time { return now }
	in.internalWedgeDispatch = func(run func()) { run() }
	in.SetRecipientProgress(d.RecipientDeliveryEvidence)
	var internal, operator []string
	in.SetInternalWedgeAlert(func(_ string, msg string) { internal = append(internal, msg) })
	in.SetEscalate(func(msg string) { operator = append(operator, msg) })

	// Cold-seed the unchanged Working baseline, then run 65 real detector ingests
	// and injector refusals at the production five-second retry cadence. No state
	// transition, turn-end, or output delta occurs.
	d.Tick()
	for i := 0; i < futileAttemptThreshold+5; i++ {
		d.Tick()
		in.deliver(Job{Agent: "stalled-seat", Kind: KindDetector})
		now = now.Add(5 * time.Second)
	}
	if len(internal) != 1 || !strings.Contains(internal[0], "delivery wedge") {
		t.Fatalf("static Working production seam alerts = %v, want one internal wedge", internal)
	}
	if len(operator) != 0 {
		t.Fatalf("static Working wedge leaked to operator destination: %v", operator)
	}
	if class, progressed := d.RecipientDeliveryEvidence("stalled-seat", now.Add(-futileAttemptWindow)); class != outbox.RecipientWorking || progressed {
		t.Fatalf("static Working final evidence = (%q, %v), want Working without progress", class, progressed)
	}
}

func TestWorkingWithoutProgressAndIdleEvidenceAccrueInternally(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class outbox.RecipientClass
	}{
		{name: "working-stalled", class: outbox.RecipientWorking},
		{name: "idle-refused", class: outbox.RecipientUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
			in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
			in.internalWedgeDispatch = func(run func()) { run() }
			in.now = func() time.Time { return now }
			in.SetRecipientProgress(func(string, time.Time) (outbox.RecipientClass, bool) { return tc.class, false })
			var internal, operator []string
			in.SetInternalWedgeAlert(func(_ string, msg string) { internal = append(internal, msg) })
			in.SetEscalate(func(msg string) { operator = append(operator, msg) })
			for i := 0; i < futileAttemptThreshold; i++ {
				in.deliver(Job{Agent: "generic-seat", Kind: KindDetector})
				now = now.Add(5 * time.Second)
			}
			if len(internal) != 1 || len(operator) != 0 {
				t.Fatalf("alarm destinations: internal=%v operator=%v", internal, operator)
			}
		})
	}
}

func TestKnownWedgeAndRecoveryUseInternalDestinationExactlyOnce(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	result := error(surface.ErrWedge)
	in := NewInjector(func(string, string) error { return result }, 1)
	in.internalWedgeDispatch = func(run func()) { run() }
	in.now = func() time.Time { return now }
	var internal, operator []string
	in.SetInternalWedgeAlert(func(_ string, msg string) { internal = append(internal, msg) })
	in.SetEscalate(func(msg string) { operator = append(operator, msg) })

	in.deliver(Job{Agent: "generic-seat", Kind: KindDetector})
	in.deliver(Job{Agent: "generic-seat", Kind: KindDetector})
	if len(internal) != 1 || !strings.Contains(internal[0], "temporal classifier") {
		t.Fatalf("known-wedge edge notices = %v, want one internal alarm", internal)
	}
	result = nil
	in.deliver(Job{Agent: "generic-seat", Kind: KindDetector})
	in.deliver(Job{Agent: "generic-seat", Kind: KindDetector})
	if len(internal) != 2 || !strings.Contains(internal[1], "cleared") {
		t.Fatalf("known-wedge lifecycle notices = %v, want alarm + exactly one clear", internal)
	}
	if len(operator) != 0 {
		t.Fatalf("known-wedge lifecycle leaked to operator destination: %v", operator)
	}
}

func TestFutileAttemptWindowRequiresConsecutiveDensity(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
	in.internalWedgeDispatch = func(run func()) { run() }
	in.now = func() time.Time { return now }
	var alarms int
	in.SetInternalWedgeAlert(func(string, string) { alarms++ })
	for i := 0; i < futileAttemptThreshold; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindDetector})
		now = now.Add(futileAttemptWindow + time.Second)
	}
	if alarms != 0 {
		t.Fatalf("sparse futile attempts escalated %d times", alarms)
	}
}

func TestFutileAttemptPrimitiveSharedAcrossTickAndSend(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
	in.internalWedgeDispatch = func(run func()) { run() }
	in.now = func() time.Time { return now }
	in.reEnqueue = func(Job, time.Duration) {}
	var alarms int
	in.SetInternalWedgeAlert(func(string, string) { alarms++ })
	for i := 0; i < futileAttemptThreshold/2; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindDetector})
		now = now.Add(5 * time.Second)
	}
	for i := futileAttemptThreshold / 2; i < futileAttemptThreshold; i++ {
		in.handleBusy(Job{Agent: "desk", Kind: KindSend}, surface.ErrBusy)
		now = now.Add(5 * time.Second)
	}
	if alarms != 1 {
		t.Fatalf("combined tick/send futile attempts alarms = %d, want 1", alarms)
	}
}
