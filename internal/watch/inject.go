// Package watch assembles the flotilla watch daemon: a single serialized
// injector through which all deliveries (relayed operator messages and
// heartbeat ticks) flow, plus the gateway reader, heartbeat, and watchdog loops
// that feed it. Serialization is the core invariant — two deliveries must never
// interleave into a pane's composer.
package watch

import "log"

// Job is one delivery: a message destined for an agent's pane.
type Job struct {
	Agent   string
	Message string
}

// SendFunc delivers a message to an agent's pane. Production wires
// deliver.ResolvePane + deliver.Send; tests inject a stub.
type SendFunc func(agent, message string) error

// Injector serializes all deliveries through one worker goroutine, so a relayed
// message and a heartbeat tick that are ready at the same instant are delivered
// one fully after the other — never interleaved.
type Injector struct {
	jobs chan Job
	send SendFunc
	done chan struct{}
}

// NewInjector builds an injector with the given send function and queue buffer.
func NewInjector(send SendFunc, buffer int) *Injector {
	return &Injector{
		jobs: make(chan Job, buffer),
		send: send,
		done: make(chan struct{}),
	}
}

// Start launches the single worker. It runs until Stop closes the queue.
func (in *Injector) Start() {
	go func() {
		defer close(in.done)
		for j := range in.jobs {
			if err := in.send(j.Agent, j.Message); err != nil {
				// A failed delivery must not kill the worker — log and continue,
				// so one bad pane can't take down the whole relay.
				log.Printf("flotilla watch: deliver to %q failed: %v", j.Agent, err)
			}
		}
	}()
}

// Enqueue submits a delivery. It blocks if the queue buffer is full (back
// pressure), guaranteeing every enqueued job is eventually delivered in order.
func (in *Injector) Enqueue(j Job) {
	in.jobs <- j
}

// Stop closes the queue and waits for the worker to drain remaining jobs.
func (in *Injector) Stop() {
	close(in.jobs)
	<-in.done
}
