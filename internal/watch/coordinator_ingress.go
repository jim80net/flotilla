package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/roster"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

// CoordinatorIngress aliases coordinator mechanical ingress to the adjutant when
// adjutant_for is configured (#533). The leader (XO/COS) sits behind the adjutant;
// this is topology resolution only — not loop arbitration, posture, or buffering.
type CoordinatorIngress struct {
	Config *roster.Config
}

// NewCoordinatorIngress builds the #533 ingress resolver when adjutant_for exists.
func NewCoordinatorIngress(cfg *roster.Config) *CoordinatorIngress {
	if cfg == nil || !cfg.HasAdjutant() {
		return nil
	}
	return &CoordinatorIngress{Config: cfg}
}

// Apply rewrites one coordinator-targeted job to its adjutant ingress pane, or
// passes through unchanged. Returns at most one job — no fan-out.
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

// IngressTarget resolves the delivery pane for coordinator dash ingress (#533).
// The second return is false when ingress rewriting does not apply.
func (g *CoordinatorIngress) IngressTarget(coordinator string) (string, bool) {
	if g == nil || g.Config == nil || !g.Config.IsCoordinator(coordinator) {
		return coordinator, false
	}
	if adj := g.Config.AdjutantFor(coordinator); adj != "" {
		return adj, true
	}
	return coordinator, true
}

func isAdjutantSeamDrain(job Job) bool {
	return strings.HasPrefix(job.ClaimKey, adjutantSeamClaimPrefix)
}
