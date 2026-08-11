package backlog

import (
	"errors"
	"strings"
	"testing"
)

func TestLintThresholdBoundariesAndContinuationMetrics(t *testing.T) {
	options := LintOptions{
		WarnCharacters: 80, FailCharacters: 300, CheckpointWarn: 2,
		ReferenceWarn: 2, DetailCharacters: 80,
	}
	cleanAtBoundary := "- [next] " + strings.Repeat("x", 71) // 80 runes including list prefix.
	md := "## Backlog\n" + cleanAtBoundary + "\n" +
		"- [in-flight] compact identity\n" +
		"  2026-07-01 checkpoint #1\n" +
		"  2026-07-02 checkpoint #2\n" +
		"  2026-07-03 checkpoint #3 #4\n" +
		"  [detail: notes/compact.md]\n"
	report := Lint("backlog.md", md, options)
	if report.ItemCount != 2 || len(report.Items) != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Items[0].Characters != options.WarnCharacters {
		t.Fatalf("boundary characters = %d, want %d", report.Items[0].Characters, options.WarnCharacters)
	}
	if got := report.Items[1]; got.Checkpoints != 3 || got.References != 4 || !got.HasDetail || got.Line != 3 {
		t.Fatalf("continuation metrics = %+v", got)
	}
	codes := findingCodes(report.Findings)
	for _, want := range []string{"item_mass_warning", "checkpoint_sprawl", "reference_sprawl"} {
		if !codes[want] {
			t.Errorf("findings = %+v, missing %s", report.Findings, want)
		}
	}
	if codes["missing_detail_pointer"] {
		t.Errorf("detail pointer was present: %+v", report.Findings)
	}
}

func TestLintFailuresAndExitAggregation(t *testing.T) {
	options := LintOptions{
		WarnCharacters: 20, FailCharacters: 40, CheckpointWarn: 5,
		ReferenceWarn: 20, DetailCharacters: 20,
	}
	nested := Lint("nested.md", "## Backlog\n- [next] parent\n  - nested child\n", options)
	if nested.ItemCount != 2 || nested.Items[1].Line != 3 || nested.Items[1].Classification != "malformed" {
		t.Fatalf("nested item boundary = %+v", nested)
	}
	if !findingCodes(nested.Findings)["malformed_status"] {
		t.Fatalf("nested finding = %+v", nested.Findings)
	}

	oversized := Lint("large.md", "## Backlog\n- [next] "+strings.Repeat("x", 50)+"\n", options)
	codes := findingCodes(oversized.Findings)
	if !codes["item_mass_failure"] || !codes["missing_detail_pointer"] {
		t.Fatalf("oversized findings = %+v", oversized.Findings)
	}
	missing := Lint("missing.md", "# Notes\n- [next] outside\n", options)
	if !missing.MissingSection || !findingCodes(missing.Findings)["missing_backlog_section"] {
		t.Fatalf("missing section = %+v", missing)
	}
	unreadable := UnreadableFileReport("gone.md", errors.New("not found"))
	report := BuildLintReport([]FileReport{nested, oversized, missing, unreadable})
	if report.ExitCode() != 2 || report.Summary.Files != 4 || report.Summary.MissingSections != 1 ||
		report.Summary.Unreadable != 1 || report.Summary.Failures < 4 || report.Result != "failures" {
		t.Fatalf("aggregate = %+v", report)
	}
}

func TestLintWarningAndCleanExitCodes(t *testing.T) {
	options := DefaultLintOptions()
	clean := Lint("clean.md", "## Backlog\n- [next] concise item\n", options)
	if got := BuildLintReport([]FileReport{clean}); got.ExitCode() != 0 || got.Result != "clean" {
		t.Fatalf("clean report = %+v", got)
	}
	warningOptions := options
	warningOptions.CheckpointWarn = 0
	warning := Lint("warn.md", "## Backlog\n- [next] checkpoint\n", warningOptions)
	if got := BuildLintReport([]FileReport{warning}); got.ExitCode() != 1 || got.Result != "warnings" {
		t.Fatalf("warning report = %+v", got)
	}
}

func TestLintOptionsValidation(t *testing.T) {
	options := DefaultLintOptions()
	options.FailCharacters = options.WarnCharacters - 1
	if err := options.Validate(); err == nil {
		t.Fatal("fail threshold below warning threshold must be invalid")
	}
	options = DefaultLintOptions()
	options.ReferenceWarn = -1
	if err := options.Validate(); err == nil {
		t.Fatal("negative threshold must be invalid")
	}
}

func TestLintThresholdsAreStrictlyAbove(t *testing.T) {
	md := "## Backlog\n- [next] checkpoint 2026-07-01 #1 #2\n"
	baseline := Lint("boundary.md", md, DefaultLintOptions())
	if len(baseline.Items) != 1 {
		t.Fatalf("baseline = %+v", baseline)
	}
	item := baseline.Items[0]
	atBoundary := LintOptions{
		WarnCharacters: item.Characters, FailCharacters: item.Characters,
		CheckpointWarn: item.Checkpoints, ReferenceWarn: item.References,
		DetailCharacters: item.Characters,
	}
	if report := Lint("boundary.md", md, atBoundary); len(report.Findings) != 0 {
		t.Fatalf("metrics equal to thresholds must be clean: %+v", report.Findings)
	}

	tests := []struct {
		name string
		edit func(*LintOptions)
		code string
	}{
		{"failure mass", func(o *LintOptions) { o.FailCharacters-- }, "item_mass_failure"},
		{"warning mass", func(o *LintOptions) { o.WarnCharacters--; o.FailCharacters++ }, "item_mass_warning"},
		{"checkpoint", func(o *LintOptions) { o.CheckpointWarn-- }, "checkpoint_sprawl"},
		{"references", func(o *LintOptions) { o.ReferenceWarn-- }, "reference_sprawl"},
		{"detail", func(o *LintOptions) { o.DetailCharacters-- }, "missing_detail_pointer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			options := atBoundary
			tc.edit(&options)
			if !findingCodes(Lint("boundary.md", md, options).Findings)[tc.code] {
				t.Fatalf("expected %s above threshold", tc.code)
			}
		})
	}
}

func findingCodes(findings []Finding) map[string]bool {
	result := map[string]bool{}
	for _, finding := range findings {
		result[finding.Code] = true
	}
	return result
}
