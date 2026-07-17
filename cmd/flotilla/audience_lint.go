package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/audience"
)

var errAudienceLint = errors.New("audience lint failed")

func cmdParadeLint(args []string) error {
	return runAudienceLint("parade", args, os.Stdout, os.Stderr)
}

func cmdPR(args []string) error {
	if len(args) < 2 || args[0] != "body" || args[1] != "lint" {
		return fmt.Errorf("usage: flotilla pr body lint --audience operator [--json] [--jargon comma,list] <body.md|->")
	}
	return runAudienceLint("operator-pr", args[2:], os.Stdout, os.Stderr)
}

func runAudienceLint(kind string, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("audience-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit machine-readable findings")
	jargonRaw := fs.String("jargon", "", "additional comma-separated terms requiring a first-use gloss")
	audienceName := fs.String("audience", "", "reader contract (operator for PR bodies)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		if kind == "parade" {
			return fmt.Errorf("usage: flotilla parade lint [--json] [--jargon comma,list] <slides.md|->")
		}
		return fmt.Errorf("usage: flotilla pr body lint --audience operator [--json] [--jargon comma,list] <body.md|->")
	}
	if kind == "operator-pr" && *audienceName != "operator" {
		return fmt.Errorf("flotilla pr body lint requires --audience operator")
	}
	if kind == "parade" && *audienceName != "" {
		return fmt.Errorf("flotilla parade lint does not accept --audience")
	}
	path := fs.Args()[0]
	raw, err := readLintInput(path)
	if err != nil {
		return err
	}
	jargon := append(audience.DefaultJargon(), splitJargon(*jargonRaw)...)
	var findings []audience.Finding
	switch kind {
	case "parade":
		findings = audience.LintParade(string(raw), jargon)
	case "operator-pr":
		findings = audience.LintOperatorPR(string(raw), jargon)
	default:
		return fmt.Errorf("unknown audience lint kind %q", kind)
	}
	if *jsonOut {
		if findings == nil {
			findings = []audience.Finding{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(findings); err != nil {
			return err
		}
	} else if len(findings) == 0 {
		fmt.Fprintf(stdout, "audience lint: PASS %s\n", path)
	} else {
		for _, finding := range findings {
			fmt.Fprintf(stderr, "%s:%d: %s: %s\n", path, finding.Line, finding.Code, finding.Message)
		}
	}
	if !*jsonOut {
		fmt.Fprintln(stdout, "human gate: read the operator-facing text as a stranger in 20 seconds; confirm the product meaning is clear")
	}
	if len(findings) > 0 {
		return fmt.Errorf("%w (%d finding(s))", errAudienceLint, len(findings))
	}
	return nil
}

func readLintInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("audience lint: read %s: %w", path, err)
	}
	return raw, nil
}

func splitJargon(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
