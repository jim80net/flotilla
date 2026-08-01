package backlog

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const LintSchema = "flotilla.backlog_lint/v1"

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityFailure Severity = "failure"
)

type LintOptions struct {
	WarnCharacters   int
	FailCharacters   int
	CheckpointWarn   int
	ReferenceWarn    int
	DetailCharacters int
}

func DefaultLintOptions() LintOptions {
	return LintOptions{
		WarnCharacters: 2000, FailCharacters: 10000, CheckpointWarn: 5,
		ReferenceWarn: 20, DetailCharacters: 2000,
	}
}

func (o LintOptions) Validate() error {
	if o.WarnCharacters < 0 || o.FailCharacters < 0 || o.CheckpointWarn < 0 ||
		o.ReferenceWarn < 0 || o.DetailCharacters < 0 {
		return fmt.Errorf("lint thresholds must be non-negative")
	}
	if o.FailCharacters < o.WarnCharacters {
		return fmt.Errorf("--fail-chars must be greater than or equal to --warn-chars")
	}
	return nil
}

type ItemMetrics struct {
	Line           int    `json:"line"`
	Identity       string `json:"identity"`
	Classification string `json:"classification"`
	Characters     int    `json:"characters"`
	Checkpoints    int    `json:"checkpoints"`
	References     int    `json:"references"`
	HasDetail      bool   `json:"has_detail"`
}

type Finding struct {
	Line        int      `json:"line"`
	Severity    Severity `json:"severity"`
	Code        string   `json:"code"`
	Identity    string   `json:"identity,omitempty"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation"`
}

type FileReport struct {
	Path           string        `json:"path"`
	ItemCount      int           `json:"item_count"`
	MissingSection bool          `json:"missing_section"`
	Unreadable     string        `json:"unreadable,omitempty"`
	Items          []ItemMetrics `json:"items"`
	Findings       []Finding     `json:"findings"`
}

type LintSummary struct {
	Files           int `json:"files"`
	Items           int `json:"items"`
	MissingSections int `json:"missing_sections"`
	Unreadable      int `json:"unreadable"`
	Warnings        int `json:"warnings"`
	Failures        int `json:"failures"`
}

type LintReport struct {
	Schema  string       `json:"schema"`
	Files   []FileReport `json:"files"`
	Summary LintSummary  `json:"summary"`
	Result  string       `json:"result"`
}

var (
	checkpointPattern = regexp.MustCompile(`(?i)\bcheckpoint\b|\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b`)
	referencePattern  = regexp.MustCompile(`(?i)(?:#[0-9]+|(?:issues|pull)/[0-9]+)`)
	detailPattern     = regexp.MustCompile(`(?i)\[detail:\s*[^]\s][^]]*\]`)
)

// Lint consumes Scan's boundaries and classifications; it defines hygiene
// policy only and deliberately owns no Markdown item grammar or marker list.
func Lint(path, md string, options LintOptions) FileReport {
	scan := Scan(md)
	report := FileReport{
		Path: path, ItemCount: len(scan.Items), MissingSection: !scan.Found,
		Items: []ItemMetrics{}, Findings: []Finding{},
	}
	if !scan.Found {
		report.Findings = append(report.Findings, Finding{
			Line: 1, Severity: SeverityFailure, Code: "missing_backlog_section",
			Message:     "required ## Backlog section is missing",
			Remediation: "add a ## Backlog heading above the backlog item list",
		})
		return report
	}
	for _, item := range scan.Items {
		metrics := ItemMetrics{
			Line: item.StartLine, Identity: lintIdentity(item.Head), Classification: item.Classification,
			Characters:  utf8.RuneCountInString(item.Raw),
			Checkpoints: countCheckpointLines(item.Raw),
			References:  len(referencePattern.FindAllStringIndex(item.Raw, -1)),
			HasDetail:   detailPattern.MatchString(item.Raw),
		}
		report.Items = append(report.Items, metrics)
		if item.Classification == "malformed" {
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityFailure, "malformed_status",
				"item has a missing or unknown leading status marker",
				"add one recognized leading status marker such as [in-flight], [next], [blocked], or [done]"))
		}
		switch {
		case metrics.Characters > options.FailCharacters:
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityFailure, "item_mass_failure",
				fmt.Sprintf("item has %d characters; failure threshold is %d", metrics.Characters, options.FailCharacters),
				"move history to a detail file and keep a concise backlog pointer"))
		case metrics.Characters > options.WarnCharacters:
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityWarning, "item_mass_warning",
				fmt.Sprintf("item has %d characters; warning threshold is %d", metrics.Characters, options.WarnCharacters),
				"trim inline history or move it to a detail file"))
		}
		if metrics.Characters > options.DetailCharacters && !metrics.HasDetail {
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityFailure, "missing_detail_pointer",
				fmt.Sprintf("over-budget item has no [detail: <path>] pointer (threshold %d)", options.DetailCharacters),
				"add a [detail: path/to/context.md] pointer and move durable history there"))
		}
		if metrics.Checkpoints > options.CheckpointWarn {
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityWarning, "checkpoint_sprawl",
				fmt.Sprintf("item has %d checkpoint/date markers; warning threshold is %d", metrics.Checkpoints, options.CheckpointWarn),
				"collapse checkpoint history into the linked detail file"))
		}
		if metrics.References > options.ReferenceWarn {
			report.Findings = append(report.Findings, itemFinding(metrics, SeverityWarning, "reference_sprawl",
				fmt.Sprintf("item has %d issue/PR references; warning threshold is %d", metrics.References, options.ReferenceWarn),
				"retain only the current controlling references and move history to detail"))
		}
	}
	return report
}

func UnreadableFileReport(path string, err error) FileReport {
	message := "input could not be read"
	if err != nil {
		message += ": " + err.Error()
	}
	return FileReport{
		Path: path, Unreadable: message, Items: []ItemMetrics{},
		Findings: []Finding{{
			Line: 0, Severity: SeverityFailure, Code: "unreadable_input", Message: message,
			Remediation: "verify the path and read permissions, then run lint again",
		}},
	}
}

func BuildLintReport(files []FileReport) LintReport {
	report := LintReport{Schema: LintSchema, Files: files, Result: "clean"}
	report.Summary.Files = len(files)
	for _, file := range files {
		report.Summary.Items += file.ItemCount
		if file.MissingSection {
			report.Summary.MissingSections++
		}
		if file.Unreadable != "" {
			report.Summary.Unreadable++
		}
		for _, finding := range file.Findings {
			switch finding.Severity {
			case SeverityFailure:
				report.Summary.Failures++
			case SeverityWarning:
				report.Summary.Warnings++
			}
		}
	}
	if report.Summary.Failures > 0 {
		report.Result = "failures"
	} else if report.Summary.Warnings > 0 {
		report.Result = "warnings"
	}
	return report
}

func (r LintReport) ExitCode() int {
	if r.Summary.Failures > 0 {
		return 2
	}
	if r.Summary.Warnings > 0 {
		return 1
	}
	return 0
}

func itemFinding(item ItemMetrics, severity Severity, code, message, remediation string) Finding {
	return Finding{
		Line: item.Line, Severity: severity, Code: code, Identity: item.Identity,
		Message: message, Remediation: remediation,
	}
}

func countCheckpointLines(raw string) int {
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		if checkpointPattern.MatchString(line) {
			count++
		}
	}
	return count
}

func lintIdentity(head string) string {
	const maxRunes = 120
	trimmed := strings.TrimSpace(head)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes-1]) + "…"
}
