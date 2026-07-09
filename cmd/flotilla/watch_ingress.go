package main

import (
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// newCoordinatorIngress wires the adjutant front-office ingress slice (#533).
// Lifecycle surfaces beyond ingress (buffer, seam timing, frontier guard) are follow-on.
func newCoordinatorIngress(cfg *roster.Config) *watch.CoordinatorIngress {
	return watch.NewCoordinatorIngress(cfg)
}
