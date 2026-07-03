package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/goals"
)

type goalsPaths struct {
	yaml string
	json string
}

func resolveGoalsPaths(rosterPath, yamlPath, jsonPath string) (goalsPaths, error) {
	rosterDir := filepath.Dir(rosterPath)
	if yamlPath == "" {
		yamlPath = filepath.Join(rosterDir, "fleet-goals.yaml")
	}
	if jsonPath == "" {
		if p := os.Getenv("FLOTILLA_GOALS_FILE"); p != "" {
			jsonPath = p
		} else {
			jsonPath = filepath.Join(rosterDir, "fleet-goals.json")
		}
	}
	return goalsPaths{yaml: yamlPath, json: jsonPath}, nil
}

func cmdGoals(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla goals validate|compile [--roster <path>] [--yaml <path>] [--json <path>]")
	}
	switch args[0] {
	case "validate":
		return cmdGoalsValidate(args[1:])
	case "compile":
		return cmdGoalsCompile(args[1:])
	default:
		return fmt.Errorf("unknown goals subcommand %q (try: validate, compile)", args[0])
	}
}

func cmdGoalsValidate(args []string) error {
	fs := flag.NewFlagSet("goals validate", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	yamlPath := fs.String("yaml", os.Getenv("FLOTILLA_GOALS_YAML"), "goals source yaml (default <roster-dir>/fleet-goals.yaml)")
	jsonPath := fs.String("json", "", "compiled goals json to cross-check (default <roster-dir>/fleet-goals.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolveGoalsPaths(*rosterPath, *yamlPath, *jsonPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(paths.yaml)
	if err != nil {
		return fmt.Errorf("goals validate: read %q: %w", paths.yaml, err)
	}
	f, err := goals.ParseYAML(raw)
	if err != nil {
		return fmt.Errorf("goals validate: %w", err)
	}
	// Cross-check the compiled json if it exists. A missing json is fine (compile
	// may not have run yet) — but any OTHER read error (permissions, a directory,
	// I/O) is a real failure and must not be silently skipped (fail-closed, matching
	// the yaml read above).
	jb, err := os.ReadFile(paths.json)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("goals validate: read json %q: %w", paths.json, err)
		}
	} else if _, err := dash.ParseGoalsFile(jb); err != nil {
		return fmt.Errorf("goals validate: json %q: %w", paths.json, err)
	}
	fmt.Printf("goals: ok (%d nodes) — %s\n", len(f.Goals), paths.yaml)
	return nil
}

func cmdGoalsCompile(args []string) error {
	fs := flag.NewFlagSet("goals compile", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	yamlPath := fs.String("yaml", os.Getenv("FLOTILLA_GOALS_YAML"), "goals source yaml (default <roster-dir>/fleet-goals.yaml)")
	jsonPath := fs.String("json", "", "compiled output json (default <roster-dir>/fleet-goals.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolveGoalsPaths(*rosterPath, *yamlPath, *jsonPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(paths.yaml)
	if err != nil {
		return fmt.Errorf("goals compile: read %q: %w", paths.yaml, err)
	}
	f, err := goals.ParseYAML(raw)
	if err != nil {
		return fmt.Errorf("goals compile: %w", err)
	}
	if err := goals.WriteJSON(paths.json, f); err != nil {
		return err
	}
	fmt.Printf("goals: compiled %d nodes — %s → %s\n", len(f.Goals), paths.yaml, paths.json)
	return nil
}
