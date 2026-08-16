package main

import (
	"log"
	"sync/atomic"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/interstitial"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

type interstitialWatchOps struct {
	resolvePane func(string) (string, error)
	paneCommand func(string) (string, error)
	getDriver   func(string) (surface.Driver, bool)
	acquireTxn  func(string) (release func(), err error)
	capturePane func(string) (string, error)
}

func productionInterstitialWatchOps() interstitialWatchOps {
	return interstitialWatchOps{
		resolvePane: deliver.ResolvePane,
		paneCommand: deliver.PaneCommand,
		getDriver:   surface.Get,
		acquireTxn: func(pane string) (func(), error) {
			txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
			if err != nil {
				return nil, err
			}
			return txn.Release, nil
		},
		capturePane: deliver.CapturePane,
	}
}

// watchInterstitialOnTick supplies fleet-state gravity independently of send:
// every watch tick considers every configured desk against the current roster
// snapshot and live pane command. One batch may be in flight; overlapping ticks
// skip rather than interleave pane transactions.
func watchInterstitialOnTick(currentRoster func() *roster.Config, desks []string) func() {
	var running atomic.Bool
	ops := productionInterstitialWatchOps()
	manager := interstitial.NewManager(interstitial.Options{
		SendEscape: deliver.SendEscape,
		NamedGap: func(agent, gap string) {
			log.Printf("flotilla watch: PRODUCT_GAP %s agent=%q (automatic clearing withheld; officer capture route remains available)", gap, agent)
		},
	})
	return func() {
		if !running.CompareAndSwap(false, true) {
			return
		}
		defer running.Store(false)
		cfg := currentRoster()
		if cfg == nil {
			return
		}
		for _, agent := range desks {
			reconcileDeskInterstitialWithOps(manager, agent, agentSurface(cfg, agent), agentTitle(cfg, agent), ops)
		}
	}
}

func reconcileDeskInterstitialWithOps(manager *interstitial.Manager, agent, rosterSurface, title string, ops interstitialWatchOps) {
	// Resolve the concrete pane first. A roster/overlay surface is intent; the
	// live foreground command is authority for a path that may emit a key.
	pane, err := ops.resolvePane(title)
	if err != nil {
		return
	}
	command, err := ops.paneCommand(pane)
	if err != nil {
		return // unreadable live identity: fail closed, never roster-fallback
	}
	liveSurface, ok := surface.SurfaceFromPaneCommand(command)
	if !ok {
		return // shell or unknown future harness: leave the pane untouched
	}
	driver, ok := ops.getDriver(liveSurface)
	if !ok {
		return
	}
	probe, ok := driver.(surface.ComposerStateProbe)
	if !ok {
		return // no verified composer capability means no permission to type
	}
	if wanted := effectiveSurface(rosterSurface); wanted != liveSurface {
		log.Printf("flotilla watch: interstitial manager using live surface %q for %q (roster/overlay says %q)", liveSurface, agent, wanted)
	}
	release, err := ops.acquireTxn(pane)
	if err != nil {
		log.Printf("flotilla watch: interstitial transaction unavailable for %q: %v", agent, err)
		return
	}
	defer release()

	result := manager.Reconcile(agent, pane, func() (interstitial.Observation, error) {
		state := surface.AssessForFleet(driver, pane)
		composer := probe.ComposerState(pane)
		frame, err := ops.capturePane(pane)
		if err != nil {
			return interstitial.Observation{}, err
		}
		return interstitial.Observation{Frame: frame, State: state, Composer: composer}, nil
	})
	if result.Err != nil {
		log.Printf("flotilla watch: interstitial manager failed for %q: %v", agent, result.Err)
		return
	}
	if result.Cleared {
		log.Printf("flotilla watch: cleared interstitial for %q after %d Escape key(s); consistent idle confirmed", agent, result.Attempts)
	}
}
