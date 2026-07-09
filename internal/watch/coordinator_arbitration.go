package watch

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/frontier"
	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

const adjutantSeamClaimPrefix = "adjutant-seam:"

// RouterConfig carries production paths for NewCoordinatorRouter.
type RouterConfig struct {
	RosterDir, PrimaryXO string
	BacklogPath          string
	SettledPath          string
	AwaitingPath         string
}

// CoordinatorRouter applies looparbitration (#533) before coordinator pane delivery.
// Source labels are not routing keys — adjutant_for controls routing.
type CoordinatorRouter struct {
	Config    *roster.Config
	RosterDir string
	Arb       *looparbitration.Arbitrator

	Posture          func(coordinator string) (looparbitration.Posture, bool)
	GoalActive       func(coordinator string) (bool, bool)
	SafeSeam         func(coordinator string) bool
	ProtectedWindow  func(coordinator string) bool
	FrontierReturnTo func(coordinator string) string
}

// NewCoordinatorRouter builds the #533 router when adjutant_for is configured.
func NewCoordinatorRouter(cfg *roster.Config, rc RouterConfig) *CoordinatorRouter {
	if cfg == nil || !cfg.HasAdjutant() {
		return nil
	}
	backlogPath := rc.BacklogPath
	return &CoordinatorRouter{
		Config:    cfg,
		RosterDir: rc.RosterDir,
		Arb:       &looparbitration.Arbitrator{},
		Posture: func(coordinator string) (looparbitration.Posture, bool) {
			if active, ok := goalLoopActive(backlogPath); ok && active {
				return looparbitration.PostureGoalActive, true
			}
			return assessCoordinatorPosture(cfg, coordinator)
		},
		GoalActive: func(coordinator string) (bool, bool) {
			return goalLoopActive(backlogPath)
		},
		SafeSeam: func(coordinator string) bool {
			path := roster.ResolveLayerClockPath(rc.RosterDir, coordinator, rc.SettledPath, "flotilla-xo-settled", "settled")
			_, err := os.Stat(path)
			return err == nil
		},
		ProtectedWindow: func(coordinator string) bool {
			path := roster.ResolveLayerClockPath(rc.RosterDir, coordinator, rc.AwaitingPath, "flotilla-xo-awaiting", "awaiting")
			return NewAwaitingMarker(path).Present()
		},
		FrontierReturnTo: func(coordinator string) string {
			if backlogPath == "" {
				return ""
			}
			raw, err := os.ReadFile(backlogPath)
			if err != nil {
				return ""
			}
			rt, _, ok := frontier.ReturnToFromBacklog(string(raw))
			if ok {
				return rt
			}
			return ""
		},
	}
}

// Apply evaluates one job targeting a coordinator and returns the job(s) to enqueue.
func (r *CoordinatorRouter) Apply(job Job) []Job {
	if r == nil || r.Config == nil || r.Arb == nil {
		return []Job{job}
	}
	if job.Kind == KindHeartbeat || !r.Config.IsCoordinator(job.Agent) {
		return []Job{job}
	}
	if isAdjutantSeamDrain(job) {
		return []Job{job}
	}
	coordinator := job.Agent
	req := jobToInjectRequest(job)
	ctx := r.buildContext(coordinator, job)
	result := r.Arb.Evaluate(req, ctx)

	switch result.Decision {
	case looparbitration.Defer:
		log.Printf("flotilla watch: arbitration defer %s %s (reason=%s)", job.Kind, coordinator, result.Reason)
		return nil
	case looparbitration.Buffer:
		return r.bufferDelivery(job, coordinator, req, result)
	}

	if result.Route == looparbitration.RouteLeader {
		return []Job{job}
	}
	if adj := r.Config.AdjutantFor(coordinator); adj != "" {
		redirected := job
		redirected.Agent = adj
		return []Job{redirected}
	}
	return []Job{job}
}

// DeliveryTarget resolves the pane agent for dash/control confirmed delivery (#533).
func (r *CoordinatorRouter) DeliveryTarget(coordinator, message string) (string, looparbitration.Result, bool) {
	if r == nil || r.Config == nil || r.Arb == nil || !r.Config.IsCoordinator(coordinator) {
		return coordinator, looparbitration.Result{}, false
	}
	req := looparbitration.InjectRequest{
		Target: coordinator, Kind: looparbitration.KindRelay, Source: "dash",
	}
	ctx := r.buildContext(coordinator, Job{Agent: coordinator, Message: message, Kind: KindRelay})
	result := r.Arb.Evaluate(req, ctx)

	switch result.Decision {
	case looparbitration.Defer:
		return coordinator, result, true
	case looparbitration.Buffer:
		r.bufferReason(coordinator, fmt.Sprintf("dash: %s", truncateReason(message)), result.ReturnTo)
		if adj := r.Config.AdjutantFor(coordinator); adj != "" {
			return adj, result, true
		}
		return coordinator, result, true
	}
	if result.Route == looparbitration.RouteLeader {
		return coordinator, result, true
	}
	if adj := r.Config.AdjutantFor(coordinator); adj != "" {
		return adj, result, true
	}
	return coordinator, result, true
}

func (r *CoordinatorRouter) buildContext(coordinator string, job Job) looparbitration.Context {
	ctx := looparbitration.Context{
		Coordinator:      coordinator,
		AdjutantFor:      r.Config.AdjutantFor(coordinator),
		FrontierReturnTo: r.frontierReturnTo(coordinator),
	}
	if r.Posture != nil {
		if p, ok := r.Posture(coordinator); ok {
			ctx.Posture, ctx.PostureOK = p, true
		}
	}
	if r.GoalActive != nil {
		if g, ok := r.GoalActive(coordinator); ok {
			ctx.GoalActive, ctx.GoalActiveOK = g, true
		}
	}
	if r.SafeSeam != nil {
		ctx.SafeSeam = r.SafeSeam(coordinator)
	}
	if r.ProtectedWindow != nil {
		ctx.ProtectedWindow = r.ProtectedWindow(coordinator)
	}
	return ctx
}

func (r *CoordinatorRouter) bufferDelivery(job Job, coordinator string, req looparbitration.InjectRequest, result looparbitration.Result) []Job {
	reason := bufferReasonFromJob(job)
	r.bufferReason(coordinator, reason, result.ReturnTo)
	adjutant := r.Config.AdjutantFor(coordinator)
	if adjutant == "" {
		return []Job{job}
	}
	charterPath := roster.LayerCharterPath(r.RosterDir, coordinator)
	body := fmt.Sprintf("[flotilla adjutant] Buffered coordinator interrupt for %s (source=%s kind=%s).\n%s",
		coordinator, req.Source, req.Kind, truncateReason(job.Message))
	body += adjutantCharterGovernanceLine(charterPath)
	return []Job{{Agent: adjutant, Message: body, Kind: KindDetector}}
}

func (r *CoordinatorRouter) bufferReason(coordinator, reason, returnTo string) {
	bufferPath := roster.LayerBufferPath(r.RosterDir, coordinator)
	if err := adjutantbuffer.Append(bufferPath, coordinator, []string{reason}); err != nil {
		log.Printf("flotilla watch: adjutant buffer append failed for %q: %v", coordinator, err)
		return
	}
	if returnTo != "" {
		recordBufferedFrontier(r.RosterDir, coordinator, returnTo, reason)
	}
}

func (r *CoordinatorRouter) frontierReturnTo(coordinator string) string {
	if r.FrontierReturnTo != nil {
		return r.FrontierReturnTo(coordinator)
	}
	return ""
}

func assessCoordinatorPosture(cfg *roster.Config, coordinator string) (looparbitration.Posture, bool) {
	agent, err := cfg.Agent(coordinator)
	if err != nil {
		return "", false
	}
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return "", false
	}
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return "", false
	}
	switch drv.Assess(pane) {
	case surface.StateWorking:
		return looparbitration.PostureComposing, true
	case surface.StateIdle:
		return looparbitration.PostureAvailable, true
	default:
		return looparbitration.PostureBlocked, true
	}
}

func goalLoopActive(backlogPath string) (bool, bool) {
	if backlogPath == "" {
		return false, false
	}
	raw, err := os.ReadFile(backlogPath)
	if err != nil {
		return false, false
	}
	st := backlog.Parse(string(raw))
	return st.Found && len(st.Unblocked) > 0, true
}

func recordBufferedFrontier(rosterDir, coordinator, returnTo, reason string) {
	f := frontier.Frame{
		Coordinator: coordinator,
		ReturnTo:    returnTo,
		Priority:    frontier.PriorityMechanical,
		Source:      "adjutant-buffer",
		SideItem:    truncateReason(reason),
	}
	if err := frontier.RecordPreempt(roster.LayerFrontierPath(rosterDir, coordinator), f); err != nil {
		log.Printf("flotilla watch: frontier record failed for %q: %v", coordinator, err)
	}
}

func jobToInjectRequest(job Job) looparbitration.InjectRequest {
	req := looparbitration.InjectRequest{
		Target: job.Agent,
		Source: string(job.Kind),
	}
	switch job.Kind {
	case KindRelay, KindDefault, KindSend:
		req.Kind = looparbitration.KindRelay
		if job.OriginChannel != "" {
			req.Source = "discord-relay"
		} else if job.Kind == KindSend {
			req.Source = "desk-send"
		} else if strings.Contains(job.Message, "gate") {
			req.Source = "gate-report"
		}
	case KindDetector:
		switch {
		case strings.HasPrefix(job.ClaimKey, "inbound-reinject:"):
			req.Kind = looparbitration.KindDroppedDispatch
			req.Source = "dropped-dispatch"
		case strings.Contains(job.Message, "goal-loop") || strings.Contains(job.Message, "goal loop"):
			req.Kind = looparbitration.KindGoalLoop
			req.Source = "goal-loop"
		case strings.Contains(job.Message, "synthesis") || strings.Contains(job.Message, "visibility"):
			req.Kind = looparbitration.KindMaterialChange
			req.Source = "synthesis"
		case strings.Contains(job.Message, "change-detector") || strings.Contains(job.Message, "Material change"):
			req.Kind = looparbitration.KindMaterialChange
			req.Source = "detector"
		default:
			req.Kind = looparbitration.KindDetectorWake
			req.Source = "detector-wake"
		}
	default:
		req.Kind = looparbitration.KindRelay
	}
	return req
}

func isAdjutantSeamDrain(job Job) bool {
	return strings.HasPrefix(job.ClaimKey, adjutantSeamClaimPrefix)
}

func bufferReasonFromJob(job Job) string {
	prefix := string(job.Kind)
	if job.OriginChannel != "" {
		prefix = "discord-relay"
	}
	return fmt.Sprintf("%s: %s", prefix, truncateReason(job.Message))
}

func truncateReason(msg string) string {
	msg = strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	if len(msg) <= 120 {
		return msg
	}
	return msg[:120] + "…"
}

func adjutantCharterGovernanceLine(charterPath string) string {
	return "\nYour charter at " + charterPath + " governs classification — consult it before composing any brief."
}
