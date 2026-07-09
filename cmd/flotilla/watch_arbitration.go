package main

import (
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func newCoordinatorRouter(
	cfg *roster.Config,
	rosterDir, primaryXO, backlogPath, settledPath, awaitingPath string,
) *watch.CoordinatorRouter {
	return watch.NewCoordinatorRouter(cfg, watch.RouterConfig{
		RosterDir:    rosterDir,
		PrimaryXO:    primaryXO,
		BacklogPath:  backlogPath,
		SettledPath:  settledPath,
		AwaitingPath: awaitingPath,
	})
}
