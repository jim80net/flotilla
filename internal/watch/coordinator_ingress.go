package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/roster"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

// VerbatimBodyMarker opens the operator-authored body section in an adjutant observation
// envelope (#549). Tests assert the source text appears after this marker unmodified.
const VerbatimBodyMarker = "--- operator message (verbatim) ---\n"

// CoordinatorIngress is the ingress/topology slice of the adjutant front office (#533).
//
// When adjutant_for:<leader> exists, the adjutant is the leader's lifecycle surface:
// ingress, liveness observation, buffering, seam timing, return-to-frontier protection,
// and deciding when to bring the leader back in. The leader (XO/COS) works behind
// that front office for system wakes — but operator-authored prose (#549) is dual-
// delivered so the leader receives the operator's words byte-for-byte (never only an
// AI paraphrase of a front-office rewrite).
//
// This type wires mechanical ingress aliasing (watch inject + dash route). Full
// lifecycle management (buffer, seam briefs, frontier guard) lives elsewhere.
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

// Apply resolves coordinator-targeted jobs through the adjutant front office (#533, #549).
//
//	Operator-authored relay (KindRelay / KindDefault): dual-enqueue —
//	  1. leader receives job.Message EXACTLY (byte-for-byte, no wrap)
//	  2. adjutant receives an additive observation envelope wrapping the same body
//	System wakes (detector / heartbeat): single-alias to adjutant (existing #533).
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
	leader := job.Agent
	if isOperatorAuthoredRelay(job) {
		toLeader := job // Message, OriginChannel, Kind unchanged — verbatim
		toAdj := job
		toAdj.Agent = adj
		toAdj.Message = AdjutantObservationEnvelope(leader, job.Message)
		// Distinct MessageID so durable relay-queue / inbound track for the operator
		// message stays on the leader path only (observation copy is not a second delivery).
		if job.MessageID != "" {
			toAdj.MessageID = job.MessageID + ".adjutant-obs"
		}
		return []Job{toLeader, toAdj}
	}
	// System wake: adjutant front office only.
	redirected := job
	redirected.Agent = adj
	return []Job{redirected}
}

// IngressTarget resolves the dash delivery pane via the adjutant front office (#533).
// The second return is false when front-office ingress rewriting does not apply.
//
// #549: dash control is operator-authored prose — deliver to the LEADER so the
// coordinator receives the operator's words verbatim. The adjutant is not a
// paraphrase hop for human→coordinator dash messages. (System detector wakes still
// use Apply's single-alias path.)
func (g *CoordinatorIngress) IngressTarget(coordinator string) (string, bool) {
	if g == nil || g.Config == nil || !g.Config.IsCoordinator(coordinator) {
		return coordinator, false
	}
	if adj := g.Config.AdjutantFor(coordinator); adj != "" {
		// Prefer leader for operator-authored dash route (verbatim #549).
		// Callers that need the adjutant for lifecycle inject use Apply on detector jobs.
		return coordinator, true
	}
	return coordinator, true
}

// AdjutantObservationEnvelope wraps operator-authored prose for the adjutant front office
// with additive metadata only (#549). The original body appears after VerbatimBodyMarker
// unmodified — never rewritten or paraphrased by the daemon.
func AdjutantObservationEnvelope(leader, body string) string {
	var b strings.Builder
	b.WriteString("[flotilla adjutant front-office] Operator message for ")
	b.WriteString(leader)
	b.WriteString(" was also delivered VERBATIM to the leader pane (byte-for-byte).\n")
	b.WriteString("Do NOT rephrase, summarize, or re-send the operator's words — dual observation / buffer / seam only.\n\n")
	b.WriteString(VerbatimBodyMarker)
	b.WriteString(body)
	return b.String()
}

// ExtractVerbatimBody returns the operator body from an observation envelope, or the
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
