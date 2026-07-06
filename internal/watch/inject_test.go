package watch

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// captureLog redirects the stdlib logger to a buffer for the duration of a test
// and restores it (and the flags) afterward.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func TestInjectorSerializes(t *testing.T) {
	var inFlight, overlap, count int32
	send := func(agent, message string) error {
		if atomic.AddInt32(&inFlight, 1) != 1 {
			atomic.StoreInt32(&overlap, 1) // a second send entered while one was running
		}
		time.Sleep(time.Millisecond) // widen the window for an overlap to show
		atomic.AddInt32(&inFlight, -1)
		atomic.AddInt32(&count, 1)
		return nil
	}

	in := NewInjector(send, 0)
	in.Start()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); in.Enqueue(Job{Agent: "x", Message: "m"}) }()
	}
	wg.Wait()
	in.Stop()

	if atomic.LoadInt32(&overlap) != 0 {
		t.Error("deliveries overlapped — injector did not serialize")
	}
	if got := atomic.LoadInt32(&count); got != 20 {
		t.Errorf("delivered %d jobs, want 20", got)
	}
}

func TestInjectorSurvivesSendError(t *testing.T) {
	var count int32
	send := func(agent, message string) error {
		atomic.AddInt32(&count, 1)
		return errors.New("boom") // a failing delivery must not kill the worker
	}
	in := NewInjector(send, 4)
	in.Start()
	in.Enqueue(Job{Agent: "a", Message: "1"})
	in.Enqueue(Job{Agent: "b", Message: "2"})
	in.Stop()
	if got := atomic.LoadInt32(&count); got != 2 {
		t.Errorf("processed %d jobs after errors, want 2 (worker must survive)", got)
	}
}

func TestInjectorLogsSuccessfulDelivery(t *testing.T) {
	// Each successful delivery must leave a terse, body-free journal trace so a
	// live delivery is auditable from journalctl independent of the Discord
	// mirror (issue #8). The Kind labels the line; the byte count stands in for
	// the content — the message body must never appear.
	cases := []struct {
		name      string
		job       Job
		wantLabel string
		wantBytes int
	}{
		{"relay", Job{Agent: "backend", Message: "ship it", Kind: "relay"}, "relay", len("ship it")},
		{"heartbeat", Job{Agent: "xo", Message: "tick-tick", Kind: "heartbeat"}, "heartbeat", len("tick-tick")},
		{"bare kind reads as relay", Job{Agent: "xo", Message: "bare", Kind: ""}, "relay", len("bare")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			in := NewInjector(func(string, string) error { return nil }, 1)
			in.Start()
			in.Enqueue(tc.job)
			in.Stop() // drains; the worker has logged by the time Stop returns

			got := buf.String()
			want := fmt.Sprintf("flotilla watch: %s delivered to %q (%d bytes)", tc.wantLabel, tc.job.Agent, tc.wantBytes)
			if !strings.Contains(got, want) {
				t.Errorf("success log = %q, want it to contain %q", got, want)
			}
			if strings.Contains(got, tc.job.Message) {
				t.Errorf("success log leaked the message body %q: %q", tc.job.Message, got)
			}
		})
	}
}

func TestInjectorDoesNotLogSuccessOnFailedDelivery(t *testing.T) {
	buf := captureLog(t)
	in := NewInjector(func(string, string) error { return errors.New("boom") }, 1)
	in.Start()
	in.Enqueue(Job{Agent: "a", Message: "nope", Kind: "relay"})
	in.Stop()

	got := buf.String()
	if !strings.Contains(got, "failed") {
		t.Errorf("expected a failure log, got %q", got)
	}
	if strings.Contains(got, "delivered to") {
		t.Errorf("a failed delivery must not emit a success log: %q", got)
	}
}

func TestInjectorPanelBlockedRelayRaisesActionableAlert(t *testing.T) {
	// #152: a relay that fails ErrPanelBlocked must raise a TERMINAL, ACTIONABLE alert (recipient +
	// payload preview + the keystroke action) — NOT deferred (a panel does not self-heal), NOT silent.
	var alerts []string
	var mu sync.Mutex
	in := NewInjector(func(string, string) error { return surface.ErrPanelBlocked }, 1)
	in.SetEscalate(func(s string) { mu.Lock(); alerts = append(alerts, s); mu.Unlock() })
	in.Start()
	in.Enqueue(Job{Agent: "data", Message: "please run the edge audit", Kind: "relay"})
	in.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want exactly 1 (terminal, not deferred)", len(alerts))
	}
	a := alerts[0]
	for _, want := range []string{"data", "agents panel", "keystroke", "please run the edge audit"} {
		if !strings.Contains(a, want) {
			t.Errorf("alert missing %q: %q", want, a)
		}
	}
}

func TestInjectorPanelBlockedTickDoesNotAlarm(t *testing.T) {
	// A heartbeat/detector tick that hits ErrPanelBlocked must NOT alarm the operator (the next wake
	// re-evaluates) — only relays escalate. The journal still records it.
	var alerts int32
	buf := captureLog(t)
	in := NewInjector(func(string, string) error { return surface.ErrPanelBlocked }, 1)
	in.SetEscalate(func(string) { atomic.AddInt32(&alerts, 1) })
	in.Start()
	in.Enqueue(Job{Agent: "data", Message: "tick", Kind: "heartbeat"})
	in.Stop()

	if got := atomic.LoadInt32(&alerts); got != 0 {
		t.Errorf("a heartbeat panel-block raised %d alerts, want 0 (ticks never alarm)", got)
	}
	if !strings.Contains(buf.String(), "INPUT-BLOCKED") {
		t.Errorf("expected an INPUT-BLOCKED journal line, got %q", buf.String())
	}
}

func TestPreviewBody(t *testing.T) {
	// Bounded, single-line, rune-safe preview for the alert.
	if got := previewBody("hello\n  world\t!"); got != "hello world !" {
		t.Errorf("previewBody collapse = %q, want %q", got, "hello world !")
	}
	long := strings.Repeat("é", 200) // 200 multibyte runes
	got := previewBody(long)
	if r := []rune(got); len(r) != 161 || r[160] != '…' { // 160 runes + the ellipsis
		t.Errorf("previewBody bound = %d runes (last %q), want 161 ending in …", len(r), string(r[len(r)-1]))
	}
}

func TestInjectorRelaySendForRelayOnly(t *testing.T) {
	// #156 H2: a RELAY job routes through the self-heal-capable relaySend; a heartbeat/detector tick
	// uses the plain send (a tick must never trigger an unsolicited Ctrl-C). When relaySend is unset,
	// relays use the plain send (inert default).
	var plain, relay []string
	in := NewInjector(func(agent, _ string) error { plain = append(plain, agent); return nil }, 1)
	in.SetRelaySend(func(agent, _ string) error { relay = append(relay, agent); return nil })
	in.Start()
	in.Enqueue(Job{Agent: "relay-desk", Message: "m", Kind: "relay"})
	in.Enqueue(Job{Agent: "bare-desk", Message: "m", Kind: ""}) // bare kind == relay
	in.Enqueue(Job{Agent: "hb-desk", Message: "m", Kind: "heartbeat"})
	in.Enqueue(Job{Agent: "det-desk", Message: "m", Kind: "detector"})
	in.Stop()

	if len(relay) != 2 || relay[0] != "relay-desk" || relay[1] != "bare-desk" {
		t.Errorf("relaySend got %v, want [relay-desk bare-desk] (relay + bare-kind)", relay)
	}
	if len(plain) != 2 || plain[0] != "hb-desk" || plain[1] != "det-desk" {
		t.Errorf("plain send got %v, want [hb-desk det-desk] (ticks never self-heal)", plain)
	}
}

func TestInjectorEnqueueAfterStopDoesNotPanic(t *testing.T) {
	// Regression: an in-flight relay handler may Enqueue after Stop. With the old
	// close-from-sender design this was send-on-closed-channel → panic.
	in := NewInjector(func(string, string) error { return nil }, 1)
	in.Start()
	in.Stop()
	in.Enqueue(Job{Agent: "x", Message: "late"}) // must drop safely, not panic
	in.Stop()                                    // idempotent
}

// TestJobKindWireValuesAndPolicy locks the audit-mirror wire values of the JobKind
// constants and the relay-vs-tick policy split (isRelay / deliveryKind). The wire
// strings are persisted in logs and consulted by the cmd-side mirror suppression,
// so a rename here is a behavior change, not a refactor — this test fails it. It is
// the behavior-preservation proof for the string→JobKind typed-enum change.
func TestJobKindWireValuesAndPolicy(t *testing.T) {
	// Wire values (the audit labels) are load-bearing and must not drift.
	cases := []struct {
		kind JobKind
		wire string
	}{
		{KindDefault, ""},
		{KindRelay, "relay"},
		{KindHeartbeat, "heartbeat"},
		{KindDetector, "detector"},
		{KindSend, "send"},
	}
	for _, c := range cases {
		if string(c.kind) != c.wire {
			t.Errorf("JobKind %v wire value = %q, want %q", c.kind, string(c.kind), c.wire)
		}
	}

	// isRelay: an empty Kind (a bare Job{}) and an explicit relay are relays; ticks are not.
	for kind, want := range map[JobKind]bool{
		KindDefault:   true,
		KindRelay:     true,
		KindSend:      false,
		KindHeartbeat: false,
		KindDetector:  false,
	} {
		if got := isRelay(kind); got != want {
			t.Errorf("isRelay(%q) = %v, want %v", string(kind), got, want)
		}
	}

	for kind, want := range map[JobKind]bool{
		KindDefault:   true,
		KindRelay:     true,
		KindSend:      true,
		KindHeartbeat: false,
		KindDetector:  false,
	} {
		if got := isDeferredDelivery(kind); got != want {
			t.Errorf("isDeferredDelivery(%q) = %v, want %v", string(kind), got, want)
		}
	}

	// deliveryKind: the empty Kind reads as "relay" in the audit log; others read verbatim.
	for kind, want := range map[JobKind]string{
		KindDefault:   "relay",
		KindRelay:     "relay",
		KindSend:      "send",
		KindHeartbeat: "heartbeat",
		KindDetector:  "detector",
	} {
		if got := deliveryKind(kind); got != want {
			t.Errorf("deliveryKind(%q) = %q, want %q", string(kind), got, want)
		}
	}
}
