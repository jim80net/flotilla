package watch

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
