package watch

import (
	"strings"
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
	var delivered []string
	var mirrored []Job
	var reEnqueues int

	in := NewInjector(func(agent, _ string) error {
		delivered = append(delivered, agent)
		if agent == "cos" {
			return surface.ErrBusy
		}
		return nil
	}, 20)
	in.SetCoordinatorIngress(NewCoordinatorIngress(adjutantRoster()))
	in.SetMirror(func(j Job) { mirrored = append(mirrored, j) })
	in.reEnqueue = func(j Job, _ time.Duration) {
		reEnqueues++
		if reEnqueues <= 8 {
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
		if reEnqueues >= 8 && countAgent(delivered, "cos-adj") >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout: reEnqueues=%d delivered=%v mirrored=%d", reEnqueues, delivered, len(mirrored))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if got := countAgent(delivered, "cos-adj"); got != 1 {
		t.Fatalf("cos-adj delivered %d times, want 1 (busy defer must not re-fanout): %v", got, delivered)
	}
	if got := countMirroredAgent(mirrored, "cos-adj"); got != 1 {
		t.Fatalf("cos-adj mirrored %d times, want 1: %+v", got, mirrored)
	}
	adj := mirrored[0]
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

	var delivered []string
	in := NewInjector(func(agent, _ string) error {
		delivered = append(delivered, agent)
		return nil
	}, 4)
	in.SetCoordinatorIngress(NewCoordinatorIngress(adjutantRoster()))
	in.Start()
	defer in.Stop()

	if n := ReplayRelayQueue(in, path); n != 1 {
		t.Fatalf("replay count = %d, want 1", n)
	}

	waitUntil := time.Now().Add(time.Second)
	for countAgent(delivered, "cos") < 1 && time.Now().Before(waitUntil) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := countAgent(delivered, "cos-adj"); got != 0 {
		t.Fatalf("replayed leader job re-split to adjutant: delivered=%v", delivered)
	}
}