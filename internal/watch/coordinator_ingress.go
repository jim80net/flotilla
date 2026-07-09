package watch

import (
	"strings"

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
}

// NewCoordinatorIngress builds the front-office ingress resolver when adjutant_for exists.
func NewCoordinatorIngress(cfg *roster.Config) *CoordinatorIngress {
	if cfg == nil || !cfg.HasAdjutant() {
		return nil
	}
	return &CoordinatorIngress{Config: cfg}
}

// Apply aliases one coordinator-targeted job to the adjutant front-office ingress pane.
// Adjutant seam drains (leader recall at safe seam) pass through to the leader.
// Returns at most one job — no fan-out.
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
	if adj := g.Config.AdjutantFor(job.Agent); adj != "" {
		redirected := job
		redirected.Agent = adj
		return []Job{redirected}
	}
	return []Job{job}
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
