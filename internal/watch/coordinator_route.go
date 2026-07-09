package watch

import (
	"fmt"
	"strings"

	"github.com/jim80net/flotilla/internal/frontier"
	"github.com/jim80net/flotilla/internal/looparbitration"
)

// RouteEvalFunc evaluates one coordinator job for explicit-urgent routing (#533).
type RouteEvalFunc func(job Job) (looparbitration.Result, bool)

// JobToInjectRequest maps a watch delivery job to looparbitration input.
// Kind/source labels are never urgency by themselves.
func JobToInjectRequest(job Job) looparbitration.InjectRequest {
	req := looparbitration.InjectRequest{
		Target:   job.Agent,
		ReturnTo: job.ReturnTo,
		Source:   string(job.Kind),
	}
	switch job.Kind {
	case KindRelay, KindDefault, KindSend:
		req.Kind = looparbitration.KindRelay
		if job.OriginChannel != "" {
			req.Source = "discord-relay"
		} else if job.Kind == KindSend {
			req.Source = "desk-send"
		}
	case KindDetector:
		if strings.Contains(job.Message, "goal-loop") || strings.Contains(job.Message, "goal loop") {
			req.Kind = looparbitration.KindGoalLoop
			req.Source = "goal-loop"
		} else if strings.Contains(job.Message, "change-detector") || strings.Contains(job.Message, "Material change") {
			req.Kind = looparbitration.KindMaterialChange
			req.Source = "detector"
		} else if strings.Contains(job.Message, "adjutant") {
			req.Kind = looparbitration.KindEvaluationTick
			req.Source = "adjutant-eval"
		} else {
			req.Kind = looparbitration.KindDetectorWake
			req.Source = "detector-wake"
		}
	default:
		req.Kind = looparbitration.KindRelay
	}
	if job.Priority != "" {
		req.Priority = looparbitration.Priority(job.Priority)
	} else {
		req.Priority = frontier.PriorityMechanical
	}
	if job.Bypass != "" {
		req.Bypass = looparbitration.BypassClass(job.Bypass)
	}
	return req
}

// JobExplicitBypass reports whether job carries an explicit bypass marker.
func JobExplicitBypass(job Job) bool {
	req := JobToInjectRequest(job)
	_, ok := looparbitration.ExplicitBypass(req)
	return ok
}

// AdjutantUrgentReconciliationBody is the adjutant-side record after RouteDual delivery.
func AdjutantUrgentReconciliationBody(leader, payload, reason string) string {
	return fmt.Sprintf("[flotilla adjutant] Urgent bypass delivered to %s (reason=%s). Reconcile: dedup, seam summary, return-to-frontier.\nPayload: %s",
		leader, reason, truncateRouteReason(payload))
}

func truncateRouteReason(msg string) string {
	msg = strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	if len(msg) <= 120 {
		return msg
	}
	return msg[:120] + "…"
}
