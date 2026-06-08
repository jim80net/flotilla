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
		{"relay", Job{Agent: "v12-dev", Message: "ship it", Kind: "relay"}, "relay", len("ship it")},
		{"heartbeat", Job{Agent: "hydra-ops", Message: "tick-tick", Kind: "heartbeat"}, "heartbeat", len("tick-tick")},
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

func TestInjectorClearFirstRouting(t *testing.T) {
	// A ClearFirst job runs the clearHook first; SkipPrompt suppresses the prompt
	// (never drive a broken XO), the two Proceed verdicts deliver it, and a nil
	// hook delivers it (back-compat). The clearHook runs inside deliver(), so the
	// clear + prompt are one atomic worker iteration.
	cases := []struct {
		name       string
		hook       func(string) ClearDecision
		wantSends  int
		wantHookOK bool
	}{
		{"skip prompt", func(string) ClearDecision { return SkipPrompt }, 0, true},
		{"proceed cleared", func(string) ClearDecision { return ProceedCleared }, 1, true},
		{"proceed no clear", func(string) ClearDecision { return ProceedNoClear }, 1, true},
		{"nil hook delivers (back-compat)", nil, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sends, hookCalls int32
			in := NewInjector(func(string, string) error { atomic.AddInt32(&sends, 1); return nil }, 1)
			if tc.hook != nil {
				in.SetClearHook(func(a string) ClearDecision { atomic.AddInt32(&hookCalls, 1); return tc.hook(a) })
			}
			in.Start()
			in.Enqueue(Job{Agent: "xo", Message: "tick", Kind: "heartbeat", ClearFirst: true})
			in.Stop()
			if got := int(atomic.LoadInt32(&sends)); got != tc.wantSends {
				t.Errorf("sends = %d, want %d", got, tc.wantSends)
			}
			if tc.wantHookOK && atomic.LoadInt32(&hookCalls) != 1 {
				t.Errorf("clearHook calls = %d, want 1", hookCalls)
			}
		})
	}
}

func TestInjectorClearFirstFalseSkipsHook(t *testing.T) {
	// A job WITHOUT ClearFirst must never invoke the clearHook (only idle heartbeat
	// ticks clear; relays and plain ticks deliver straight through).
	var hookCalls, sends int32
	in := NewInjector(func(string, string) error { atomic.AddInt32(&sends, 1); return nil }, 1)
	in.SetClearHook(func(string) ClearDecision { atomic.AddInt32(&hookCalls, 1); return ProceedCleared })
	in.Start()
	in.Enqueue(Job{Agent: "xo", Message: "operator msg", Kind: "relay"}) // ClearFirst false
	in.Stop()
	if atomic.LoadInt32(&hookCalls) != 0 {
		t.Errorf("clearHook called %d times for a non-ClearFirst job, want 0", hookCalls)
	}
	if atomic.LoadInt32(&sends) != 1 {
		t.Error("non-ClearFirst job must still be delivered")
	}
}

func TestInjectorClearFirstAtomicWithPrompt(t *testing.T) {
	// The clear (clearHook) and the tick prompt must be one atomic worker
	// iteration: a relayed message enqueued right after must be delivered AFTER
	// the prompt, never between the clear and the prompt. Record the call order.
	var mu sync.Mutex
	var order []string
	send := func(_, msg string) error {
		mu.Lock()
		order = append(order, "send:"+msg)
		mu.Unlock()
		return nil
	}
	in := NewInjector(send, 4)
	in.SetClearHook(func(string) ClearDecision {
		mu.Lock()
		order = append(order, "clear")
		mu.Unlock()
		return ProceedCleared
	})
	in.Start()
	in.Enqueue(Job{Agent: "xo", Message: "tick", Kind: "heartbeat", ClearFirst: true})
	in.Enqueue(Job{Agent: "xo", Message: "operator", Kind: "relay"})
	in.Stop()

	want := []string{"clear", "send:tick", "send:operator"}
	if len(order) != len(want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("call order = %v, want %v (a relay interleaved between clear and prompt)", order, want)
		}
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
