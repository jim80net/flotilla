package main

import (
	"log"
	"sync/atomic"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/interstitial"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// watchInterstitialOnTick supplies fleet-state gravity independently of send:
// every watch tick considers every configured desk. One batch may be in flight;
// overlapping ticks skip rather than interleave pane transactions.
func watchInterstitialOnTick(cfg *roster.Config, desks []string) func() {
	var running atomic.Bool
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
		for _, agent := range desks {
			reconcileDeskInterstitial(cfg, manager, agent)
		}
	}
}

func reconcileDeskInterstitial(cfg *roster.Config, manager *interstitial.Manager, agent string) {
	driver, ok := surface.Get(agentSurface(cfg, agent))
	if !ok {
		return
	}
	probe, ok := driver.(surface.ComposerStateProbe)
	if !ok {
		// No verified composer means no permission to type. The classifier cannot
		// establish consistent idle on this surface, so leave it untouched.
		return
	}
	pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
	if err != nil {
		return
	}
	txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
	if err != nil {
		log.Printf("flotilla watch: interstitial transaction unavailable for %q: %v", agent, err)
		return
	}
	defer txn.Release()

	result := manager.Reconcile(agent, pane, func() (interstitial.Observation, error) {
		state := surface.AssessForFleet(driver, pane)
		composer := probe.ComposerState(pane)
		frame, err := deliver.CapturePane(pane)
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
