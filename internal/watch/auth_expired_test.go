package watch

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestAuthExpiredAlarmEdgesAndRecovery(t *testing.T) {
	r := newRig(nil)
	frame := "prior conversation\nLogin expired - Please run /login\nfooter"
	driver := surface.NewClaudeObservationDriver(
		func(string) (string, error) { return "node", nil },
		func(string) bool { return false },
		func(string) (string, error) { return frame, nil },
		deliver.ParseBusyAt,
		func(string) (int, bool, error) { return 1, false, nil },
	)
	assessor := NewAuthStateAssessor(r.in)
	assessor.Assess("desk", driver, "pane")
	assessor.Assess("desk", driver, "pane")
	if len(r.alerts) != 1 {
		t.Fatalf("first auth-expired episode alerts = %d, want 1", len(r.alerts))
	}
	for _, want := range []string{"desk", "auth-expired", "human login", "durably held"} {
		if !strings.Contains(r.alerts[0], want) {
			t.Fatalf("alert %q missing %q", r.alerts[0], want)
		}
	}

	frame = "prior conversation\n❯ \nfooter"
	assessor.Assess("desk", driver, "pane")
	frame = "prior conversation\nLogin expired - Please run /login\nfooter"
	assessor.Assess("desk", driver, "pane")
	if len(r.alerts) != 2 {
		t.Fatalf("post-login auth-expired episode alerts = %d, want rearmed second alert", len(r.alerts))
	}
}

func TestAuthExpiredEpisodePersistsAcrossDegradedObservation(t *testing.T) {
	r := newRig(nil)
	if active := r.in.ObserveAuthState("desk", surface.AuthExpired); !active {
		t.Fatal("initial expiry did not activate the episode")
	}
	if active := r.in.ObserveAuthState("desk", surface.AuthUndetermined); !active {
		t.Fatal("degraded observation prematurely cleared the expiry episode")
	}
	if active := r.in.ObserveAuthState("desk", surface.AuthExpired); !active {
		t.Fatal("restored cursor provenance lost the unchanged expiry episode")
	}
	if len(r.alerts) != 1 {
		t.Fatalf("expired→degraded→expired alerts = %d, want exactly 1", len(r.alerts))
	}
	if active := r.in.ObserveAuthState("desk", surface.AuthRecovered); active {
		t.Fatal("positive healthy-composer evidence did not clear the episode")
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
