package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/roster"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

// VerbatimBodyMarker opens the operator-authored body section in an adjutant ingress
// envelope. Tests assert the source text appears after this marker unmodified.
const VerbatimBodyMarker = "--- operator message (verbatim) ---\n"

// CoordinatorIngress is the ingress/topology slice of the adjutant front office (#533, #593).
//
// When adjutant_for:<leader> exists, the adjutant is the locus of fleet interaction
// intelligence — the brainstem / CNS to the leader brain (#593 operator framing): faithful
// reproduction of reflexes and signals; iterative tuning of CoS↔XO↔desk interaction paths.
// Operator-authored prose enters as single ingress; the adjutant coalesces conversation arcs,
// disaggregates multi-intent traffic, and forwards leader-judgment material verbatim at safe
// seams. Fidelity at delivery, not dual mechanical fanout at ingress.
//
// This type wires mechanical ingress aliasing (watch inject + dash route). Judgment layers
// (arc assembly, intent segmentation, charter tuning) live in the adjutant seat + buffer substrate.
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

// Apply resolves coordinator-targeted jobs through the adjutant front office (#533, #593).
//
//	Operator-authored relay (KindRelay / KindDefault): single-alias to adjutant — the
//	  intelligent conversation buffer holds / triages / forwards verbatim at seam.
//	System detector wakes: single-alias to adjutant (existing #533).
//	Durable inter-agent sends (KindSend): pass through to the named recipient.
//	Adjutant seam drains: pass through to the leader unchanged.
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
	adj := g.Config.AdjutantFor(job.Agent)
	if adj == "" {
		return []Job{job}
	}
	if isOperatorAuthoredRelay(job) {
		leader := job.Agent
		redirected := job
		redirected.Agent = adj
		redirected.Message = AdjutantOperatorIngressBody(leader, job.Message)
		return []Job{redirected}
	}
	if job.Kind != KindDetector {
		return []Job{job}
	}
	// System wake: adjutant front office only.
	redirected := job
	redirected.Agent = adj
	return []Job{redirected}
}

// IngressTarget resolves the dash delivery pane via the adjutant front office (#533, #593).
// Operator-authored dash prose enters the adjutant buffer path, not the leader mid-turn.
func (g *CoordinatorIngress) IngressTarget(coordinator string) (string, bool) {
	if g == nil || g.Config == nil || !g.Config.IsCoordinator(coordinator) {
		return coordinator, false
	}
	if adj := g.Config.AdjutantFor(coordinator); adj != "" {
		return adj, true
	}
	return coordinator, true
}

// AdjutantOperatorIngressBody frames operator prose for the adjutant front office (#593).
// The original body appears after VerbatimBodyMarker unmodified — never rewritten at ingress.
func AdjutantOperatorIngressBody(leader, body string) string {
	var b strings.Builder
	b.WriteString("[flotilla adjutant front-office] Operator message for ")
	b.WriteString(leader)
	b.WriteString(" — you are the conversation buffer. Hold / batch / forward at a safe seam; ")
	b.WriteString("interrupt the leader when the operator needs them now. ")
	b.WriteString("When you forward, the leader receives the operator's words verbatim (byte-for-byte).\n\n")
	b.WriteString(VerbatimBodyMarker)
	b.WriteString(body)
	return b.String()
}

// ExtractVerbatimBody returns the operator body from an ingress envelope, or the
// input unchanged when the marker is absent (already-verbatim leader delivery).
func ExtractVerbatimBody(message string) string {
	if i := strings.Index(message, VerbatimBodyMarker); i >= 0 {
		return message[i+len(VerbatimBodyMarker):]
	}
	return message
}

// isOperatorAuthoredRelay is true for human Discord / CLI relay traffic (not detector ticks).
func isOperatorAuthoredRelay(job Job) bool {
	return isRelay(job.Kind)
}

// isAdjutantSeamDrain is true when the front office is recalling the leader at a safe seam.
func isAdjutantSeamDrain(job Job) bool {
	return strings.HasPrefix(job.ClaimKey, adjutantSeamClaimPrefix)
}
