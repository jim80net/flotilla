package looparbitration

// RouteTarget names the pane that receives an inject. #533 policy: all non-urgent
// interrupts route through the adjutant when adjutant_for is configured — source/kind
// alone is not the routing key. Urgency, adjutant availability, and posture are.
type RouteTarget string

const (
	RouteLeader   RouteTarget = "leader"
	RouteAdjutant RouteTarget = "adjutant"
)

// resolveRoute picks the delivery pane for a verdict. Exceptions to adjutant routing:
//   - urgent bypass or explicit audited BypassClass → leader
//   - no adjutant configured → leader (fail-safe fallback)
//   - adjutant seam brief drain → leader consolidated brief
//   - dropped-dispatch reinject → leader/recipient target unchanged
func resolveRoute(req InjectRequest, ctx Context, r Result) RouteTarget {
	if isUrgent(req) {
		return RouteLeader
	}
	if ctx.AdjutantFor == "" {
		return RouteLeader
	}
	if req.Kind == KindAdjutantSeam {
		return RouteLeader
	}
	if req.Kind == KindDroppedDispatch {
		return RouteLeader
	}
	return RouteAdjutant
}
