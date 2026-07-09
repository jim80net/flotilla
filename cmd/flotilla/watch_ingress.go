package main

import (
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func newCoordinatorIngress(cfg *roster.Config) *watch.CoordinatorIngress {
	return watch.NewCoordinatorIngress(cfg)
}
