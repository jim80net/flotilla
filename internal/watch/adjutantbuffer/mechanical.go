package adjutantbuffer

import "strings"

// IsMechanicalFinishEdge reports bare detector finish-edge keys that must not
// become leader "Needs you" briefs (Wave 0.2 content-first / #628 residual).
// These are informational Working→Idle edges, not judgment work.
func IsMechanicalFinishEdge(reason string) bool {
	r := strings.TrimSpace(reason)
	if r == "" || IsOperatorReason(r) {
		return false
	}
	lower := strings.ToLower(r)
	// Canonical material reason shapes from the change-detector:
	//   "backend: finished a turn (working→idle)"
	//   "backend Working→Idle"
	if strings.Contains(lower, "working→idle") || strings.Contains(lower, "working->idle") {
		return true
	}
	if strings.Contains(lower, "finished a turn") {
		return true
	}
	return false
}

// PartitionJudgment splits undelivered system items into judgment (Needs you)
// vs mechanical finish-edges (auto-consume, no leader inject).
func PartitionJudgment(items []Item) (judgment, mechanical []Item) {
	for _, it := range items {
		if IsMechanicalFinishEdge(it.Reason) {
			mechanical = append(mechanical, it)
		} else {
			judgment = append(judgment, it)
		}
	}
	return judgment, mechanical
}
