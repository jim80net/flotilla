package watch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/utilization"
)

type ownQueueRig struct {
	dir, path  string
	now        time.Time
	state      surface.State
	authorized bool
	protected  bool
	events     []OwnQueueEvent
}

func newOwnQueueRig(t *testing.T, body string) (*ownQueueRig, *OwnQueueClaimer) {
	t.Helper()
	r := &ownQueueRig{dir: t.TempDir(), now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), state: surface.StateIdle, authorized: true}
	r.path = filepath.Join(r.dir, "flotilla-alpha-backlog.md")
	if err := os.WriteFile(r.path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &OwnQueueClaimer{Dir: r.dir, TTL: time.Hour, Now: func() time.Time { return r.now },
		Assess: func(string) surface.State { return r.state },
		Authorize: func(seat, path string) (bool, string) {
			return r.authorized && seat == "alpha" && path == r.path, "fixture authority"
		},
		Protected: func(string) (bool, string) { return r.protected, "operator relay pending" },
		Emit:      func(e OwnQueueEvent) { r.events = append(r.events, e) },
	}
	return r, c
}

func TestOwnQueueClaimNextAndResumeInFlight(t *testing.T) {
	for _, tc := range []struct{ name, body, result string }{
		{"next", "## Backlog\n- [next] build parser\n", "claimed"},
		{"resume", "## Backlog\n- [next] later\n- [in-flight] finish current\n", "resumed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newOwnQueueRig(t, tc.body)
			job, handled := c.Claim("alpha", r.path, 7)
			if !handled || job.Agent != "alpha" || job.Kind != KindDetector || !strings.Contains(job.Message, r.path) {
				t.Fatalf("job=%+v handled=%v", job, handled)
			}
			if got := r.events[len(r.events)-1].Result; got != tc.result {
				t.Fatalf("result=%q", got)
			}
			lease, ok := loadOwnQueueLease(c.leasePath("alpha"))
			if !ok || lease.DetectorTick != 7 || lease.SourceLine < 2 || lease.Outcome != "pending-delivery" {
				t.Fatalf("lease=%+v ok=%v", lease, ok)
			}
		})
	}
}

func TestOwnQueueUsesSharedQueueStateAndScanner797(t *testing.T) {
	const md = "## Backlog\n- [blocked] wait\n  detail\n- [next] nested-safe item\n"
	st := backlog.Parse(md)
	if got := utilization.QueueState(st.Found, len(st.Unblocked)); got != utilization.QueueHasWork {
		t.Fatalf("shared queue state = %q", got)
	}
	r, c := newOwnQueueRig(t, md)
	job, _ := c.Claim("alpha", r.path, 1)
	lease, ok := loadOwnQueueLease(c.leasePath("alpha"))
	if job.Agent == "" || !ok || lease.SourceLine != backlog.Scan(md).Items[1].StartLine {
		t.Fatalf("claim/scanner drift: job=%+v lease=%+v", job, lease)
	}
}

func TestOwnQueueConcurrentAndRestartDedupe(t *testing.T) {
	r, first := newOwnQueueRig(t, "## Backlog\n- [next] one only\n")
	job, _ := first.Claim("alpha", r.path, 1)
	second := *first // models a restarted daemon sharing only the durable sidecar
	job2, handled := second.Claim("alpha", r.path, 2)
	if !handled || job2.Agent != "" || r.events[len(r.events)-1].Result != "already-claimed" {
		t.Fatalf("job=%+v events=%+v", job2, r.events)
	}
	if job.ClaimKey == "" {
		t.Fatal("first claim missing key")
	}

	lock, err := acquireOwnQueueLock(filepath.Join(r.dir, "flotilla-own-queue-alpha.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	job3, _ := first.Claim("alpha", r.path, 3)
	if job3.Agent != "" || !strings.Contains(r.events[len(r.events)-1].Reason, "already in progress") {
		t.Fatalf("concurrent outcome=%+v", r.events[len(r.events)-1])
	}
}

func TestOwnQueueFailClosedRevalidation(t *testing.T) {
	cases := []struct {
		name, body string
		mutate     func(*ownQueueRig)
	}{
		{"stale-idle", "## Backlog\n- [next] work\n", func(r *ownQueueRig) { r.state = surface.StateWorking }},
		{"blocked", "## Backlog\n- [blocked] operator\n", func(*ownQueueRig) {}},
		{"awaiting", "## Backlog\n- [awaiting-auth] permission\n", func(*ownQueueRig) {}},
		{"unknown", "# Notes\n- [next] outside\n", func(*ownQueueRig) {}},
		{"malformed", "## Backlog\n- do something\n", func(*ownQueueRig) {}},
		{"protected", "## Backlog\n- [next] work\n", func(r *ownQueueRig) { r.protected = true }},
		{"authority", "## Backlog\n- [next] work\n", func(r *ownQueueRig) { r.authorized = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newOwnQueueRig(t, tc.body)
			tc.mutate(r)
			job, _ := c.Claim("alpha", r.path, 1)
			if job.Agent != "" {
				t.Fatalf("unexpected job %+v", job)
			}
		})
	}
}

func TestOwnQueueDeliveryConfirmFailureReleaseAndExpiry(t *testing.T) {
	r, c := newOwnQueueRig(t, "## Backlog\n- [next] durable work\n")
	job, _ := c.Claim("alpha", r.path, 1)
	c.Confirm(job.ClaimKey)
	lease, ok := loadOwnQueueLease(c.leasePath("alpha"))
	if !ok || lease.Outcome != "delivered" {
		t.Fatalf("lease=%+v", lease)
	}
	c.Abort(job.ClaimKey)
	if _, ok := loadOwnQueueLease(c.leasePath("alpha")); ok {
		t.Fatal("failed delivery retained lease")
	}
	job, _ = c.Claim("alpha", r.path, 2)
	if job.Agent == "" {
		t.Fatal("release did not permit retry")
	}
	r.now = r.now.Add(2 * time.Hour)
	job, _ = c.Claim("alpha", r.path, 3)
	if job.Agent == "" || r.events[len(r.events)-1].Result != "expiry-recovery" {
		t.Fatalf("expiry=%+v", r.events[len(r.events)-1])
	}
	r.state = surface.StateWorking
	r.now = r.now.Add(2 * time.Hour)
	if job, _ := c.Claim("alpha", r.path, 4); job.Agent != "" {
		t.Fatal("expiry recovered without fresh idle proof")
	}
}

func TestOwnQueueCompletionClearsLease(t *testing.T) {
	r, c := newOwnQueueRig(t, "## Backlog\n- [next] finish me\n")
	job, _ := c.Claim("alpha", r.path, 1)
	c.Confirm(job.ClaimKey)
	if err := os.WriteFile(r.path, []byte("## Backlog\n- [done] finish me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Reconcile("alpha", r.path)
	if job, handled := c.Claim("alpha", r.path, 2); job.Agent != "" || !handled {
		t.Fatalf("job=%+v handled=%v", job, handled)
	}
	if _, ok := loadOwnQueueLease(c.leasePath("alpha")); ok {
		t.Fatal("completed item retained lease")
	}
}

func TestOwnQueueInjectorOutcomeHooks(t *testing.T) {
	r, c := newOwnQueueRig(t, "## Backlog\n- [next] injected\n")
	job, _ := c.Claim("alpha", r.path, 1)
	in := NewInjector(func(string, string) error { return errors.New("delivery broke") }, 1)
	in.SetDetectorClaimHooks(c.Confirm, c.Abort)
	in.Start()
	in.Enqueue(job)
	in.Stop()
	if _, ok := loadOwnQueueLease(c.leasePath("alpha")); ok {
		t.Fatal("injector failure did not release")
	}

	job, _ = c.Claim("alpha", r.path, 2)
	in = NewInjector(func(string, string) error { return nil }, 1)
	in.SetDetectorClaimHooks(c.Confirm, c.Abort)
	in.Start()
	in.Enqueue(job)
	in.Stop()
	lease, ok := loadOwnQueueLease(c.leasePath("alpha"))
	if !ok || lease.Outcome != "delivered" {
		t.Fatalf("lease=%+v", lease)
	}
}

func TestDetectorOwnQueuePrecedesGenericHeartbeat(t *testing.T) {
	var mu sync.Mutex
	calls := []string{}
	d := &Detector{cfg: DetectorConfig{
		PullDeskWork:      func(string) bool { mu.Lock(); calls = append(calls, "pull"); mu.Unlock(); return true },
		WakeDeskHeartbeat: func(string) { mu.Lock(); calls = append(calls, "heartbeat"); mu.Unlock() },
	}}
	d.runDeskHeartbeats([]string{"alpha"}, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "pull" {
		t.Fatalf("calls=%v", calls)
	}
}
