package looparbitration

// RouteTarget names the pane(s) that receive an inject (#533).
type RouteTarget string

const (
	RouteLeader   RouteTarget = "leader"
	RouteAdjutant RouteTarget = "adjutant"
	// RouteDual delivers to leader immediately and records on the adjutant for
	// reconciliation (cleanup, dedup, seam summary, return-to-frontier).
	RouteDual RouteTarget = "dual"
)

// BypassClass names an explicit audited bypass. Kind/source labels never imply bypass.
type BypassClass string

const (
	BypassUrgent BypassClass = "urgent" // explicit PriorityUrgent safety valve
)

// ExplicitBypass reports whether req carries an explicit bypass marker (never inferred
// from kind or source alone).
func ExplicitBypass(req InjectRequest) (BypassClass, bool) {
	return explicitBypass(req)
}

func explicitBypass(req InjectRequest) (BypassClass, bool) {
	if req.Priority == PriorityUrgent {
		return BypassUrgent, true
	}
	if req.Bypass != "" {
		return req.Bypass, true
	}
	return "", false
}

// resolveRoute picks the delivery target for a verdict.
//   - no adjutant → leader (fail-safe fallback)
//   - KindAdjutantSeam → leader (adjutant-owned interruption path)
//   - explicit urgent bypass + AllowNow → dual (leader interrupt + adjutant record)
//   - otherwise with adjutant → adjutant (default non-urgent ingress)
func resolveRoute(req InjectRequest, ctx Context, r Result) RouteTarget {
	if ctx.AdjutantFor == "" {
		return RouteLeader
	}
	if req.Kind == KindAdjutantSeam {
		return RouteLeader
	}
	if _, ok := explicitBypass(req); ok && r.Decision == AllowNow {
		return RouteDual
	}
	return RouteAdjutant
}
