package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
)

// cmdRegister tags a tmux pane with the stable @flotilla_agent marker, so the
// agent resolves by key regardless of how Claude Code (or any TUI) later
// retitles the pane. Run it once inside the agent's pane at launch
// (`flotilla register <name>`), or from anywhere with an explicit target
// (`flotilla register <name> --pane <target>`) to (re-)tag an already-running,
// already-drifted desk WITHOUT interrupting it.
func cmdRegister(args []string) error {
	agentName, pane, rosterPath, err := parseRegisterArgs(args, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	if pane == "" {
		return fmt.Errorf("no pane to tag: run inside the agent's tmux pane, or pass --pane <target> (e.g. %%4 or session:win.pane)")
	}

	cfg, err := roster.Load(rosterPath)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}
	// The marker records the agent's resolution key (its tmux_title override, else
	// its name) — the exact value ResolvePane matches against.
	key := agent.Title()
	if err := deliver.TagPane(pane, key); err != nil {
		return err
	}
	fmt.Printf("registered %s → pane %s (marker @flotilla_agent=%s); title drift no longer breaks resolution\n", agentName, pane, key)
	return nil
}

// parseRegisterArgs resolves the agent, pane target, and roster path from the
// register args, accepting the agent positional EITHER before or after the flags.
// Go's flag package stops at the first positional, so the natural migration form
// `register <name> --pane <target>` would otherwise drop the flags; we pull a
// leading positional out first, then parse the remainder as flags (and accept a
// trailing positional too). Pure (no tmux/roster I/O) so the ordering is unit
// tested. paneDefault is the fallback pane (production passes $TMUX_PANE).
func parseRegisterArgs(args []string, paneDefault string) (agent, pane, rosterPath string, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	pn := fs.String("pane", paneDefault, "tmux pane target to tag (default $TMUX_PANE — the pane this runs in)")
	if err = fs.Parse(args); err != nil {
		return "", "", "", err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 { // agent supplied after the flags
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", "", fmt.Errorf("usage: flotilla register <agent> [--pane <target>]")
	}
	return agent, *pn, *rp, nil
}
