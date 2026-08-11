package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jim80net/flotilla/internal/backlog"
)

type commandExitError int

func (e commandExitError) Error() string { return "" }
func (e commandExitError) ExitCode() int { return int(e) }

func cmdBacklog(args []string) error {
	if len(args) == 0 || args[0] != "lint" {
		return fmt.Errorf("usage: flotilla backlog lint [flags] FILE [FILE...]")
	}
	if code := runBacklogLint(args[1:], os.Stdout, os.Stderr); code != 0 {
		return commandExitError(code)
	}
	return nil
}

func runBacklogLint(args []string, stdout, stderr io.Writer) int {
	defaults := backlog.DefaultLintOptions()
	fs := flag.NewFlagSet("backlog lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "emit stable JSON report")
	warnChars := fs.Int("warn-chars", defaults.WarnCharacters, "warn when an item exceeds this character count")
	failChars := fs.Int("fail-chars", defaults.FailCharacters, "fail when an item exceeds this character count")
	checkpointWarn := fs.Int("checkpoint-warn", defaults.CheckpointWarn, "warn when checkpoint/date markers exceed this count")
	referenceWarn := fs.Int("reference-warn", defaults.ReferenceWarn, "warn when issue/PR references exceed this count")
	detailChars := fs.Int("detail-chars", defaults.DetailCharacters, "require [detail: <path>] above this character count")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: flotilla backlog lint [flags] FILE [FILE...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "backlog lint: at least one FILE is required")
		return 2
	}
	options := backlog.LintOptions{
		WarnCharacters: *warnChars, FailCharacters: *failChars,
		CheckpointWarn: *checkpointWarn, ReferenceWarn: *referenceWarn,
		DetailCharacters: *detailChars,
	}
	if err := options.Validate(); err != nil {
		fmt.Fprintln(stderr, "backlog lint: "+err.Error())
		return 2
	}
	files := make([]backlog.FileReport, 0, fs.NArg())
	for _, path := range fs.Args() {
		raw, err := os.ReadFile(path)
		if err != nil {
			files = append(files, backlog.UnreadableFileReport(path, err))
			continue
		}
		files = append(files, backlog.Lint(path, string(raw), options))
	}
	report := backlog.BuildLintReport(files)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "backlog lint: encode JSON: "+err.Error())
			return 2
		}
	} else {
		writeBacklogLintHuman(stdout, report)
	}
	return report.ExitCode()
}

func writeBacklogLintHuman(w io.Writer, report backlog.LintReport) {
	for _, file := range report.Files {
		if len(file.Findings) == 0 {
			fmt.Fprintf(w, "%s: clean (%d items)\n", file.Path, file.ItemCount)
			continue
		}
		metrics := make(map[int]backlog.ItemMetrics, len(file.Items))
		for _, item := range file.Items {
			metrics[item.Line] = item
		}
		for _, finding := range file.Findings {
			line := finding.Line
			if line <= 0 {
				line = 1
			}
			fmt.Fprintf(w, "%s:%d: %s %s: %s", file.Path, line, finding.Severity, finding.Code, finding.Message)
			if item, ok := metrics[finding.Line]; ok {
				fmt.Fprintf(w, "; item=%q; metrics characters=%d checkpoints=%d references=%d detail=%t",
					item.Identity, item.Characters, item.Checkpoints, item.References, item.HasDetail)
			}
			fmt.Fprintf(w, "; fix: %s\n", finding.Remediation)
		}
	}
	fmt.Fprintf(w, "summary: files=%d items=%d missing_sections=%d unreadable=%d warnings=%d failures=%d result=%s\n",
		report.Summary.Files, report.Summary.Items, report.Summary.MissingSections, report.Summary.Unreadable,
		report.Summary.Warnings, report.Summary.Failures, report.Result)
}
