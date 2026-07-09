package looparbitration

// RouteTarget names the pane that receives an inject. #533: when adjutant_for is set,
// all coordinator notifications go to the adjutant — source/kind/priority/bypass labels
// do not route around the adjutant. Leader delivery: no-adjutant fallback and
// KindAdjutantSeam drain only.
type RouteTarget string

const (
	RouteLeader   RouteTarget = "leader"
	RouteAdjutant RouteTarget = "adjutant"
)

// resolveRoute picks the delivery pane for a verdict.
//   - no adjutant → leader (fail-safe fallback)
//   - KindAdjutantSeam → leader (adjutant-owned interruption path)
//   - otherwise with adjutant → adjutant (all notification ingress)
func resolveRoute(req InjectRequest, ctx Context, _ Result) RouteTarget {
	if ctx.AdjutantFor == "" {
		return RouteLeader
	}
	if req.Kind == KindAdjutantSeam {
		return RouteLeader
	}
	return RouteAdjutant
}
