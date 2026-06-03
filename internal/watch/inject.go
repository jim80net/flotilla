// Package watch assembles the flotilla watch daemon: a single serialized
// injector through which all deliveries (relayed operator messages and
// heartbeat ticks) flow, plus the gateway reader, heartbeat, and watchdog loops
// that feed it. Serialization is the core invariant — two deliveries must never
// interleave into a pane's composer.
package watch

import (
	"log"
	"sync"
)

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
//
// The jobs channel is NEVER closed (closing-from-the-sender is the bug): the
// relay handler and the heartbeat are both senders, and a handler goroutine
// in-flight at shutdown could otherwise send on a closed channel and panic.
// Stop signals the worker to drain-and-exit and makes Enqueue drop instead.
type Injector struct {
	jobs    chan Job
	send    SendFunc
	stop    chan struct{} // worker: drain then exit
	stopped chan struct{} // Enqueue: stop accepting (closed once)
	done    chan struct{}
	once    sync.Once
}

// NewInjector builds an injector with the given send function and queue buffer.
func NewInjector(send SendFunc, buffer int) *Injector {
	return &Injector{
		jobs:    make(chan Job, buffer),
		send:    send,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start launches the single worker. It runs until Stop.
func (in *Injector) Start() {
	go func() {
		defer close(in.done)
		for {
			select {
			case j := <-in.jobs:
				in.deliver(j)
			case <-in.stop:
				for { // drain remaining buffered jobs, then exit
					select {
					case j := <-in.jobs:
						in.deliver(j)
					default:
						return
					}
				}
			}
		}
	}()
}

func (in *Injector) deliver(j Job) {
	if err := in.send(j.Agent, j.Message); err != nil {
		// A failed delivery must not kill the worker — log and continue, so one
		// bad pane can't take down the whole relay.
		log.Printf("flotilla watch: deliver to %q failed: %v", j.Agent, err)
	}
}

// Enqueue submits a delivery. It blocks under back pressure (full buffer) so
// jobs are delivered in order; after Stop it drops the job (shutting down)
// rather than blocking or panicking — the jobs channel is never closed, so a
// late Enqueue from an in-flight relay handler is always safe.
func (in *Injector) Enqueue(j Job) {
	select {
	case in.jobs <- j:
	case <-in.stopped:
	}
}

// Stop signals the worker to drain and exit, and stops Enqueue from accepting.
// Idempotent; waits for the worker to finish.
func (in *Injector) Stop() {
	in.once.Do(func() {
		close(in.stopped)
		close(in.stop)
	})
	<-in.done
}
