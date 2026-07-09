package looparbitration

// RouteTarget names the pane that receives an inject. #533 policy: when adjutant_for
// is set, coordinator notifications go to the adjutant — source, kind, priority, and
// bypass labels do not route around the adjutant. The adjutant may interrupt the
// leader via KindAdjutantSeam drain (RouteLeader).
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
