package watch

import (
	"strings"
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

// #592: operator→coordinator dual-split must happen once; busy-defer re-enqueues of the
// leader job must not re-spawn adjutant observation copies (each confirmed copy mirrored Discord).
func TestInjectorBusyDeferDoesNotRefanoutAdjutantObs592(t *testing.T) {
	var (
		deliveredMu sync.Mutex
		delivered   []string
		mirroredMu  sync.Mutex
		mirrored    []Job
		reEnqueues  atomic.Int32
	)

	in := NewInjector(func(agent, _ string) error {
		deliveredMu.Lock()
		delivered = append(delivered, agent)
		deliveredMu.Unlock()
		if agent == "cos" {
			return surface.ErrBusy
		}
		return nil
	}, 20)
	in.SetCoordinatorIngress(NewCoordinatorIngress(adjutantRoster()))
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
	for {
		deliveredMu.Lock()
		adjCount := countAgent(delivered, "cos-adj")
		deliveredMu.Unlock()
		if reEnqueues.Load() >= 8 && adjCount >= 1 {
			break
		}
		select {
		case <-deadline:
			deliveredMu.Lock()
			snap := append([]string(nil), delivered...)
			deliveredMu.Unlock()
			mirroredMu.Lock()
			mCount := len(mirrored)
			mirroredMu.Unlock()
			t.Fatalf("timeout: reEnqueues=%d delivered=%v mirrored=%d", reEnqueues.Load(), snap, mCount)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	deliveredMu.Lock()
	adjDeliveries := countAgent(delivered, "cos-adj")
	deliveredSnap := append([]string(nil), delivered...)
	deliveredMu.Unlock()
	if adjDeliveries != 1 {
		t.Fatalf("cos-adj delivered %d times, want 1 (busy defer must not re-fanout): %v", adjDeliveries, deliveredSnap)
	}

	mirroredMu.Lock()
	adjMirrors := countMirroredAgent(mirrored, "cos-adj")
	var adj Job
	for _, j := range mirrored {
		if j.Agent == "cos-adj" {
			adj = j
			break
		}
	}
	mirroredMu.Unlock()
	if adjMirrors != 1 {
		t.Fatalf("cos-adj mirrored %d times, want 1", adjMirrors)
	}
	if adj.MessageID != "m592.adjutant-obs" {
		t.Fatalf("adjutant mirror MessageID = %q, want m592.adjutant-obs", adj.MessageID)
	}
	if !strings.HasPrefix(adj.Message, "[flotilla adjutant front-office]") {
		t.Fatalf("adjutant mirror missing front-office prefix: %q", adj.Message)
	}
}

func TestReplayRelayQueueSkipsIngressApply592(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/relay-queue.json"
	q := newRelayQueueStore(path)
	q.upsert(Job{
		Agent: "cos", Message: "queued while busy", Kind: KindRelay,
		MessageID: "m-replay", OriginChannel: "C1",
	})

	var cosDelivered atomic.Int32
	var adjDelivered atomic.Int32
	in := NewInjector(func(agent, _ string) error {
		switch agent {
		case "cos":
			cosDelivered.Add(1)
		case "cos-adj":
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
	for cosDelivered.Load() < 1 && time.Now().Before(waitUntil) {
		time.Sleep(5 * time.Millisecond)
	}
	if cosDelivered.Load() != 1 {
		t.Fatalf("cos delivered %d times, want 1", cosDelivered.Load())
	}
	if got := adjDelivered.Load(); got != 0 {
		t.Fatalf("replayed leader job re-split to adjutant: cos-adj deliveries=%d", got)
	}
}
