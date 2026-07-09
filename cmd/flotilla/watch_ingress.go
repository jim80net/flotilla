package main

import (
	"os"

	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

type coordinatorIngressConfig struct {
	cfg          *roster.Config
	rosterDir    string
	primaryXO    string
	settledPath  string
	awaitingPath string
}

// newCoordinatorIngress wires adjutant front-office ingress (#533). Explicit urgent bypass
// dual-routes via looparbitration; default non-urgent traffic aliases to the adjutant.
func newCoordinatorIngress(ic coordinatorIngressConfig) *watch.CoordinatorIngress {
	ingress := watch.NewCoordinatorIngress(ic.cfg)
	if ingress == nil {
		return nil
	}
	arb := &looparbitration.Arbitrator{
		Audit: looparbitration.NewAuditLog(roster.LayerArbitrationAuditPath(ic.rosterDir, ic.primaryXO)),
	}
	ingress.Arb = arb
	ingress.RouteEval = watch.NewRouteEval(arb, func(coordinator string) looparbitration.Context {
		ctx := looparbitration.Context{
			Coordinator:   coordinator,
			AdjutantFor:   ic.cfg.AdjutantFor(coordinator),
			TimedFallback: true,
		}
		settled := roster.ResolveLayerClockPath(ic.rosterDir, coordinator, ic.settledPath, "flotilla-xo-settled", "settled")
		if _, err := os.Stat(settled); err == nil {
			ctx.SafeSeam = true
		}
		awaiting := roster.ResolveLayerClockPath(ic.rosterDir, coordinator, ic.awaitingPath, "flotilla-xo-awaiting", "awaiting")
		ctx.ProtectedWindow = watch.NewAwaitingMarker(awaiting).Present()
		return ctx
	})
	return ingress
}
