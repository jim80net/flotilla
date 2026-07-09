package watch

import (
	"github.com/jim80net/flotilla/internal/looparbitration"
)

// ContextFunc builds looparbitration context for one coordinator evaluation.
type ContextFunc func(coordinator string) looparbitration.Context

// NewRouteEval wires explicit-urgent route evaluation through looparbitration (#533).
func NewRouteEval(arb *looparbitration.Arbitrator, ctxFn ContextFunc) RouteEvalFunc {
	if arb == nil || ctxFn == nil {
		return nil
	}
	return func(job Job) (looparbitration.Result, bool) {
		req := JobToInjectRequest(job)
		ctx := ctxFn(job.Agent)
		return arb.Evaluate(req, ctx), true
	}
}
