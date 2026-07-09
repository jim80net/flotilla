package looparbitration

// RouteTarget names the pane that receives an inject. #533 policy: when adjutant_for
// is set, non-urgent coordinator notifications go to the adjutant — source/kind are
// not routing keys. Leader delivery: no-adjutant fallback, audited operator/manual
// bypass (BypassClass), and KindAdjutantSeam drain.
type RouteTarget string

const (
	RouteLeader   RouteTarget = "leader"
	RouteAdjutant RouteTarget = "adjutant"
)

// resolveRoute picks the delivery pane for a verdict.
//   - no adjutant → leader (fail-safe fallback)
//   - KindAdjutantSeam → leader (adjutant-owned interruption path)
//   - explicit Bypass → leader (audited operator/manual bypass)
//   - otherwise with adjutant → adjutant (all non-urgent notification ingress)
func resolveRoute(req InjectRequest, ctx Context, _ Result) RouteTarget {
	if ctx.AdjutantFor == "" {
		return RouteLeader
	}
	if req.Kind == KindAdjutantSeam {
		return RouteLeader
	}
	if req.Bypass != "" {
		return RouteLeader
	}
	return RouteAdjutant
}
