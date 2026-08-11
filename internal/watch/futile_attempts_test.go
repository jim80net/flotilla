package watch

import (
	"fmt"
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
	in.now = func() time.Time { return now }
	var alarms []string
	in.SetEscalate(func(msg string) { alarms = append(alarms, msg) })

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
	if _, ok := in.futileAttempts["desk"]; ok {
		t.Fatal("confirmed delivery did not reset recipient futile-attempt state")
	}

	busy = true
	for i := 0; i < futileAttemptThreshold; i++ {
		in.deliver(Job{Agent: "desk", Kind: KindHeartbeat, Message: "new storm"})
		now = now.Add(5 * time.Second)
	}
	if len(alarms) != 2 {
		t.Fatalf("post-recovery storm alarms = %d, want a re-armed second alarm", len(alarms))
	}
}

func TestFutileAttemptWindowRequiresConsecutiveDensity(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	in := NewInjector(func(string, string) error { return surface.ErrBusy }, 1)
	in.now = func() time.Time { return now }
	var alarms int
	in.SetEscalate(func(string) { alarms++ })
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
	in.now = func() time.Time { return now }
	in.reEnqueue = func(Job, time.Duration) {}
	var alarms int
	in.SetEscalate(func(string) { alarms++ })
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

func TestQueuedHeadAgeWithZeroAttemptsEscalatesOnceAndRearmsOnDelivery(t *testing.T) {
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	now := base.Add(outbox.StaleMaxAge + time.Minute)
	in := NewInjector(func(string, string) error { return nil }, 1)
	in.now = func() time.Time { return now }
	var alarms []string
	in.SetEscalate(func(msg string) { alarms = append(alarms, msg) })
	entry := outbox.Entry{ID: "head-1", Recipient: "alpha-desk", EnqueuedAt: base}

	in.ObserveQueuedHead(entry)
	in.ObserveQueuedHead(entry)
	if len(alarms) != 1 {
		t.Fatalf("aged zero-attempt head alarms = %d, want exactly 1: %v", len(alarms), alarms)
	}
	for _, want := range []string{"alpha-desk", "head-1", "zero attempts", base.Format(time.RFC3339)} {
		if !strings.Contains(alarms[0], want) {
			t.Fatalf("alarm %q missing %q", alarms[0], want)
		}
	}

	in.deliver(Job{Agent: "alpha-desk", Kind: KindSend, Message: "confirmed"})
	entry.ID = "head-2"
	in.ObserveQueuedHead(entry)
	if len(alarms) != 2 {
		t.Fatalf("confirmed delivery did not rearm age alarm: %v", alarms)
	}
}

func TestQueuedHeadAgeIgnoresYoungOrAttemptedEntries(t *testing.T) {
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	in := NewInjector(func(string, string) error { return nil }, 1)
	in.now = func() time.Time { return base.Add(outbox.StaleMaxAge + time.Minute) }
	var alarms int
	in.SetEscalate(func(string) { alarms++ })
	in.ObserveQueuedHead(outbox.Entry{ID: "attempted", Recipient: "alpha-desk", EnqueuedAt: base, Deferrals: 1})
	in.ObserveQueuedHead(outbox.Entry{ID: "young", Recipient: "beta-desk", EnqueuedAt: base.Add(2 * time.Minute)})
	if alarms != 0 {
		t.Fatalf("non-wedge heads raised %d alarms", alarms)
	}
}
