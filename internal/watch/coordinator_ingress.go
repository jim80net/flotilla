package watch

import (
	"log"
	"strings"

	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/roster"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

// CoordinatorIngress is the ingress/topology slice of the adjutant front office (#533).
//
// When adjutant_for:<leader> exists, the adjutant is the leader's lifecycle surface:
// ingress, liveness observation, buffering, seam timing, return-to-frontier protection,
// and deciding when to bring the leader back in. The leader (XO/COS) works behind
// that front office — notification paths do not route around it.
//
// This type wires only mechanical ingress aliasing (watch inject + dash route).
// Full lifecycle management (buffer, seam briefs, frontier guard) is follow-on work.
type CoordinatorIngress struct {
	Config *roster.Config
	// RouteEval resolves explicit-urgent jobs via looparbitration (#533). When nil or the
	// job has no explicit bypass marker, Apply uses default adjutant ingress aliasing.
	RouteEval RouteEvalFunc
	// Arb is optional; when set with RouteEval, dual-route bypasses are audited.
	Arb *looparbitration.Arbitrator
}

// NewCoordinatorIngress builds the front-office ingress resolver when adjutant_for exists.
func NewCoordinatorIngress(cfg *roster.Config) *CoordinatorIngress {
	if cfg == nil || !cfg.HasAdjutant() {
		return nil
	}
	return &CoordinatorIngress{Config: cfg}
}

// Apply routes coordinator-targeted jobs through the adjutant front office (#533).
// Default non-urgent ingress aliases to the adjutant. Explicit PriorityUrgent or
// BypassClass may dual-route (leader interrupt + adjutant reconciliation record)
// when RouteEval returns RouteDual at AllowNow. Adjutant seam drains reach the leader.
func (g *CoordinatorIngress) Apply(job Job) []Job {
	if g == nil || g.Config == nil {
		return []Job{job}
	}
	if job.Kind == KindHeartbeat || !g.Config.IsCoordinator(job.Agent) {
		return []Job{job}
	}
	if isAdjutantSeamDrain(job) {
		return []Job{job}
	}
	coordinator := job.Agent
	adj := g.Config.AdjutantFor(coordinator)
	if g.RouteEval != nil && JobExplicitBypass(job) {
		result, ok := g.RouteEval(job)
		if ok {
			switch {
			case result.Decision == looparbitration.Defer:
				log.Printf("flotilla watch: ingress defer %s → %s (reason=%s)", coordinator, result.Route, result.Reason)
				if adj != "" {
					redirected := job
					redirected.Agent = adj
					return []Job{redirected}
				}
				return nil
			case result.Route == looparbitration.RouteDual && result.Decision == looparbitration.AllowNow && adj != "":
				recon := job
				recon.Agent = adj
				recon.Message = AdjutantUrgentReconciliationBody(coordinator, job.Message, result.Reason)
				return []Job{job, recon}
			case result.Route == looparbitration.RouteAdjutant && adj != "":
				redirected := job
				redirected.Agent = adj
				return []Job{redirected}
			case result.Route == looparbitration.RouteLeader:
				return []Job{job}
			}
		}
	}
	if adj != "" {
		redirected := job
		redirected.Agent = adj
		return []Job{redirected}
	}
	return []Job{job}
}

// IngressRoute resolves dash delivery for a coordinator message (#533).
// For RouteDual at AllowNow, primary is the leader and adjFollowUp carries the
// adjutant reconciliation record. ok is false when ingress policy does not apply.
func (g *CoordinatorIngress) IngressRoute(coordinator, message, priority string) (primary, adjAgent, adjFollowUp string, result looparbitration.Result, ok bool) {
	if g == nil || g.Config == nil || !g.Config.IsCoordinator(coordinator) {
		return coordinator, "", "", looparbitration.Result{}, false
	}
	job := Job{Agent: coordinator, Message: message, Kind: KindRelay, Priority: priority}
	if g.RouteEval != nil && JobExplicitBypass(job) {
		r, evalOK := g.RouteEval(job)
		if evalOK {
			adj := g.Config.AdjutantFor(coordinator)
			switch {
			case r.Route == looparbitration.RouteDual && r.Decision == looparbitration.AllowNow && adj != "":
				return coordinator, adj, AdjutantUrgentReconciliationBody(coordinator, message, r.Reason), r, true
			case r.Route == looparbitration.RouteAdjutant && adj != "":
				return adj, "", "", r, true
			}
			return coordinator, "", "", r, true
		}
	}
	if adj := g.Config.AdjutantFor(coordinator); adj != "" {
		return adj, "", "", looparbitration.Result{}, true
	}
	return coordinator, "", "", looparbitration.Result{}, true
}

// IngressTarget resolves the dash delivery pane via the adjutant front office (#533).
// The second return is false when front-office ingress rewriting does not apply.
func (g *CoordinatorIngress) IngressTarget(coordinator string) (string, bool) {
	if g == nil || g.Config == nil || !g.Config.IsCoordinator(coordinator) {
		return coordinator, false
	}
	if adj := g.Config.AdjutantFor(coordinator); adj != "" {
		return adj, true
	}
	return coordinator, true
}

// isAdjutantSeamDrain is true when the front office is recalling the leader at a safe seam.
func isAdjutantSeamDrain(job Job) bool {
	return strings.HasPrefix(job.ClaimKey, adjutantSeamClaimPrefix)
}
