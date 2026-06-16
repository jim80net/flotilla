package main

import (
	"flag"
	"fmt"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// cmdResult prints the FULL latest completed-turn result for a desk, read from its harness session
// store via the surface driver's optional ResultReader capability — for harnesses (grok) whose pane
// capture shows only a truncated tail. READ-ONLY: it resolves and reads, never writes a pane. A
// surface without a session-store reader reports so (the operator uses the pane capture instead).
func cmdResult(args []string) error {
	fs := flag.NewFlagSet("result", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: flotilla result [--roster <path>] <agent>")
	}
	agentName := rest[0]

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q (known: see internal/surface registry)", agentName, agent.Surface)
	}
	rr, ok := drv.(surface.ResultReader)
	if !ok {
		return fmt.Errorf("surface %q has no session-store result reader — read %q with the pane capture (tmux capture-pane) instead", drv.Name(), agentName)
	}
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	result, err := rr.LatestResult(pane)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}
