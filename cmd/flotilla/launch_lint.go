package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
)

func cmdLaunch(args []string) error {
	if len(args) == 0 || args[0] != "lint" {
		return fmt.Errorf("usage: flotilla launch lint [--roster <path>] [--launch <path>]")
	}
	fs := flag.NewFlagSet("launch lint", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	launchPath := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: flotilla launch lint [--roster <path>] [--launch <path>]")
	}
	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	if *launchPath == "" {
		*launchPath = launch.DefaultPath(*rosterPath)
	}
	return reportLaunchChainLint("flotilla launch lint", *launchPath, rosterAgentSet(cfg), func(msg string) {
		fmt.Fprintln(os.Stderr, msg)
	})
}

func rosterAgentSet(cfg *roster.Config) map[string]bool {
	agents := make(map[string]bool, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		agents[agent.Name] = true
	}
	return agents
}

// loadLaunchForChainLint treats an absent launch file as an empty config: every
// roster seat is then visibly unprotected. Malformed files remain errors because
// pretending to inspect a partial config would be a false healthy signal.
func loadLaunchForChainLint(path string, rosterAgents map[string]bool) (*launch.Config, error) {
	cfg, err := launch.Load(path, rosterAgents)
	if errors.Is(err, os.ErrNotExist) {
		return &launch.Config{Agents: map[string]launch.Recipe{}}, nil
	}
	return cfg, err
}

func reportLaunchChainLint(prefix, path string, rosterAgents map[string]bool, emit func(string)) error {
	cfg, err := loadLaunchForChainLint(path, rosterAgents)
	if err != nil {
		return err
	}
	unprotected := cfg.UnprotectedAgents(rosterAgents)
	if len(unprotected) == 0 {
		return nil
	}
	emit(fmt.Sprintf("%s: WARNING: seats without a failover chain: %s — declare fallbacks or acknowledge a deliberate single-harness seat with single_harness=true; schema: docs/harness-subscription-switching.md",
		prefix, strings.Join(unprotected, ", ")))
	return nil
}

func logLaunchChainLint(path string, rosterAgents map[string]bool) {
	if err := reportLaunchChainLint("flotilla watch", path, rosterAgents, func(message string) { log.Print(message) }); err != nil {
		log.Printf("flotilla watch: WARNING: launch chain lint failed for %s: %v", path, err)
	}
}
