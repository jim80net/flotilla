package watch

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestAuthExpiredAlarmEdgesAndRecovery(t *testing.T) {
	r := newRig(surface.ErrAuthExpired)
	r.in.ObserveAuthExpired("desk", true)
	r.in.ObserveAuthExpired("desk", true)
	if len(r.alerts) != 1 {
		t.Fatalf("first auth-expired episode alerts = %d, want 1", len(r.alerts))
	}
	for _, want := range []string{"desk", "auth-expired", "human login", "durably held"} {
		if !strings.Contains(r.alerts[0], want) {
			t.Fatalf("alert %q missing %q", r.alerts[0], want)
		}
	}

	r.in.ObserveAuthExpired("desk", false)
	r.in.ObserveAuthExpired("desk", true)
	if len(r.alerts) != 2 {
		t.Fatalf("post-login auth-expired episode alerts = %d, want rearmed second alert", len(r.alerts))
	}
}

func TestAuthExpiredDefersDeliveriesWithoutFutileStormNoise(t *testing.T) {
	r := newRig(surface.ErrAuthExpired)
	r.in.deliver(Job{Agent: "desk", Kind: KindSend, Message: "queued work"})
	if len(r.deferred) != 1 {
		t.Fatalf("auth-expired send deferred = %d, want 1", len(r.deferred))
	}
	if len(r.alerts) != 1 {
		t.Fatalf("auth-expired delivery alerts = %d, want immediate edge alarm", len(r.alerts))
	}
	if _, ok := r.in.futileAttempts["desk"]; ok {
		t.Fatal("auth-expired delivery counted as futile-storm noise")
	}
}

func TestAuthExpiredDeliveryResumesAfterLogin(t *testing.T) {
	expired := true
	r := newRig(nil)
	r.in.send = func(string, string) error {
		if expired {
			return surface.ErrAuthExpired
		}
		return nil
	}
	r.in.deliver(Job{Agent: "desk", Kind: KindSend, Message: "queued work"})
	if len(r.deferred) != 1 {
		t.Fatalf("expired delivery deferred = %d, want 1", len(r.deferred))
	}
	expired = false
	r.in.deliver(r.deferred[0])
	if len(r.mirrored) != 0 {
		t.Fatalf("ordinary send mirrored = %d, want 0", len(r.mirrored))
	}
	r.in.authExpiredMu.Lock()
	alarmed := r.in.authExpiredAlarmed["desk"]
	r.in.authExpiredMu.Unlock()
	if alarmed {
		t.Fatal("successful post-login delivery did not rearm auth-expired alarm")
	}
}
