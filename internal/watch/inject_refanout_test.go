package watch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func countAgent(xs []string, agent string) int {
	n := 0
	for _, a := range xs {
		if a == agent {
			n++
		}
	}
	return n
}

func countMirroredAgent(xs []Job, agent string) int {
	n := 0
	for _, j := range xs {
		if j.Agent == agent {
			n++
		}
	}
	return n
}

// #592/#593: single adjutant ingress; busy-defer re-enqueues must not re-Apply, re-buffer,
// or spawn a second adjutant job — only retry the same resolved job.
func TestInjectorBusyDeferDoesNotRefanoutAdjutantObs592(t *testing.T) {
	var (
		deliveredMu sync.Mutex
		delivered   []string
		mirroredMu  sync.Mutex
		mirrored    []Job
		reEnqueues  atomic.Int32
		bufferCalls atomic.Int32
	)

	in := NewInjector(func(agent, _ string) error {
		deliveredMu.Lock()
		delivered = append(delivered, agent)
		deliveredMu.Unlock()
		if agent == "cos-adj" {
			return surface.ErrBusy
		}
		return nil
	}, 20)
	in.SetCoordinatorIngress(NewCoordinatorIngress(adjutantRoster()))
	in.SetOperatorRelayBuffer(func(leader, messageID, body, channelID, operatorID string) error {
		bufferCalls.Add(1)
		if leader != "cos" || messageID != "m592" || body != "operator task" {
			t.Errorf("buffer hook leader=%q id=%q body=%q", leader, messageID, body)
		}
		return nil
	})
	in.SetMirror(func(j Job) {
		mirroredMu.Lock()
		mirrored = append(mirrored, j)
		mirroredMu.Unlock()
	})
	in.reEnqueue = func(j Job, _ time.Duration) {
		if reEnqueues.Add(1) <= 8 {
			in.Enqueue(j)
		}
	}
	in.Start()
	defer in.Stop()

	in.Enqueue(Job{
		Agent: "cos", Message: "operator task", Kind: KindRelay,
		MessageID: "m592", OriginChannel: "C1",
	})

	deadline := time.After(2 * time.Second)
	for reEnqueues.Load() < 8 {
		select {
		case <-deadline:
			t.Fatalf("timeout: reEnqueues=%d bufferCalls=%d", reEnqueues.Load(), bufferCalls.Load())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	deliveredMu.Lock()
	snap := append([]string(nil), delivered...)
	deliveredMu.Unlock()
	if got := countAgent(snap, "cos"); got != 0 {
		t.Fatalf("leader must not receive ingress fanout, cos deliveries=%d: %v", got, snap)
	}
	if bufferCalls.Load() != 1 {
		t.Fatalf("buffer append called %d times, want 1 (re-enqueue must not re-buffer)", bufferCalls.Load())
	}
	mirroredMu.Lock()
	defer mirroredMu.Unlock()
	if len(mirrored) != 0 {
		t.Fatalf("mirrored %d while adjutant still busy, want 0", len(mirrored))
	}
}

func TestReplayRelayQueueSkipsIngressApply592(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/relay-queue.json"
	q := newRelayQueueStore(path)
	q.upsert(Job{
		Agent: "cos-adj", Message: "queued while busy", Kind: KindRelay,
		MessageID: "m-replay", OriginChannel: "C1", ingressResolved: true,
	})

	var adjDelivered atomic.Int32
	in := NewInjector(func(agent, _ string) error {
		if agent == "cos-adj" {
			adjDelivered.Add(1)
		}
		return nil
	}, 4)
	in.SetCoordinatorIngress(NewCoordinatorIngress(adjutantRoster()))
	in.Start()
	defer in.Stop()

	if n := ReplayRelayQueue(in, path); n != 1 {
		t.Fatalf("replay count = %d, want 1", n)
	}

	waitUntil := time.Now().Add(time.Second)
	for adjDelivered.Load() < 1 && time.Now().Before(waitUntil) {
		time.Sleep(5 * time.Millisecond)
	}
	if adjDelivered.Load() != 1 {
		t.Fatalf("cos-adj delivered %d times, want 1", adjDelivered.Load())
	}
}
