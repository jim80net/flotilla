package main

import (
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func layerProtectedWindowDeps(
	cfg *roster.Config,
	rosterDir, relayQueuePath string,
	inj *watch.Injector,
	leader string,
	now time.Time,
) watch.LayerProtectedWindowDeps {
	adj := cfg.AdjutantFor(leader)
	awaitingPath := roster.ResolveLayerClockPath(rosterDir, leader, "", "flotilla-xo-awaiting", "awaiting")
	return watch.LayerProtectedWindowDeps{
		Leader:                 leader,
		Adjutant:               adj,
		AwaitingPath:           awaitingPath,
		RelayQueuePath:         relayQueuePath,
		ActiveConversationPath: roster.LayerLastOperatorRelayPath(rosterDir, leader),
		Injector:               inj,
		Now:                    func() time.Time { return now },
	}
}

func layerOperatorProtected(
	cfg *roster.Config,
	rosterDir, relayQueuePath string,
	inj *watch.Injector,
	leader string,
	now time.Time,
) bool {
	return watch.OperatorProtectedForLayer(layerProtectedWindowDeps(cfg, rosterDir, relayQueuePath, inj, leader, now))
}

func layerOperatorReplyProtected(
	cfg *roster.Config,
	rosterDir, relayQueuePath string,
	inj *watch.Injector,
	leader string,
	now time.Time,
) bool {
	return watch.OperatorReplyProtectedForLayer(layerProtectedWindowDeps(cfg, rosterDir, relayQueuePath, inj, leader, now))
}

func relayLayerLeader(cfg *roster.Config, deliveredAgent string) (leader string, ok bool) {
	if cfg.IsCoordinator(deliveredAgent) {
		return deliveredAgent, true
	}
	if l := cfg.CoordinatorForAdjutant(deliveredAgent); l != "" {
		return l, true
	}
	return "", false
}
