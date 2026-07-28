package harnessquality

import (
	"sort"
	"time"
)

const SummarySchema = "flotilla.harness_quality_summary/v1"

type Summary struct {
	Schema                 string  `json:"schema"`
	GeneratedAt            string  `json:"generated_at"`
	State                  string  `json:"state"`
	Diagnostic             string  `json:"diagnostic,omitempty"`
	TotalEvents            int     `json:"total_events"`
	CompletionEvents       int     `json:"completion_events"`
	GateEvents             int     `json:"gate_events"`
	BounceEvents           int     `json:"bounce_events"`
	TotalBounces           int     `json:"total_bounces"`
	ReworkedCompletions    int     `json:"reworked_completions"`
	ClassifiedEvents       int     `json:"classified_events"`
	TaggingCoveragePercent float64 `json:"tagging_coverage_percent"`
	BounceRatePercent      float64 `json:"bounce_rate_percent"`
	ReworkRatePercent      float64 `json:"rework_rate_percent"`
	Groups                 []Group `json:"groups"`
}

type Group struct {
	Surface             string    `json:"surface"`
	Model               string    `json:"model"`
	WorkClass           WorkClass `json:"work_class"`
	Events              int       `json:"events"`
	CompletionEvents    int       `json:"completion_events"`
	GateEvents          int       `json:"gate_events"`
	BounceEvents        int       `json:"bounce_events"`
	TotalBounces        int       `json:"total_bounces"`
	ReworkedCompletions int       `json:"reworked_completions"`
	BounceRatePercent   float64   `json:"bounce_rate_percent"`
	ReworkRatePercent   float64   `json:"rework_rate_percent"`
	HarnessVersions     []string  `json:"harness_versions"`
	FlotillaVersions    []string  `json:"flotilla_versions"`
}

type groupKey struct {
	surface, model string
	workClass      WorkClass
}

func BuildSummary(events []Event, now time.Time) Summary {
	if now.IsZero() {
		now = time.Now()
	}
	summary := Summary{Schema: SummarySchema, GeneratedAt: now.UTC().Format(time.RFC3339), State: "available", Groups: []Group{}}
	groups := make(map[groupKey]*Group)
	versions := make(map[groupKey]map[string]map[string]bool)
	for _, event := range events {
		summary.TotalEvents++
		if event.WorkClass != WorkUnclassified {
			summary.ClassifiedEvents++
		}
		key := groupKey{event.Surface, event.Model, event.WorkClass}
		group := groups[key]
		if group == nil {
			group = &Group{Surface: key.surface, Model: key.model, WorkClass: key.workClass}
			groups[key] = group
			versions[key] = map[string]map[string]bool{"harness": {}, "flotilla": {}}
		}
		group.Events++
		if event.HarnessVersion != "" {
			versions[key]["harness"][event.HarnessVersion] = true
		}
		versions[key]["flotilla"][event.FlotillaVersion] = true
		switch event.Kind {
		case KindCompletion:
			summary.CompletionEvents++
			group.CompletionEvents++
			if event.ReworkCount > 0 {
				summary.ReworkedCompletions++
				group.ReworkedCompletions++
			}
		case KindGate:
			summary.GateEvents++
			group.GateEvents++
			if event.Outcome == OutcomeBounced {
				summary.BounceEvents++
				group.BounceEvents++
			}
			summary.TotalBounces += event.BounceCount
			group.TotalBounces += event.BounceCount
		}
	}
	for key, group := range groups {
		group.BounceRatePercent = percent(group.BounceEvents, group.GateEvents)
		group.ReworkRatePercent = percent(group.ReworkedCompletions, group.CompletionEvents)
		group.HarnessVersions = sortedKeys(versions[key]["harness"])
		group.FlotillaVersions = sortedKeys(versions[key]["flotilla"])
		summary.Groups = append(summary.Groups, *group)
	}
	sort.Slice(summary.Groups, func(i, j int) bool {
		a, b := summary.Groups[i], summary.Groups[j]
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.WorkClass < b.WorkClass
	})
	summary.TaggingCoveragePercent = percent(summary.ClassifiedEvents, summary.TotalEvents)
	summary.BounceRatePercent = percent(summary.BounceEvents, summary.GateEvents)
	summary.ReworkRatePercent = percent(summary.ReworkedCompletions, summary.CompletionEvents)
	return summary
}

func LoadSummary(rosterDir string, now time.Time) Summary {
	events, err := Load(rosterDir)
	if err != nil {
		return Summary{Schema: SummarySchema, GeneratedAt: now.UTC().Format(time.RFC3339), State: "unavailable", Diagnostic: "quality_ledger_invalid"}
	}
	return BuildSummary(events, now)
}

func percent(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) * 100 / float64(d)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
