package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// paradeArgs is the parsed `flotilla parade` invocation.
type paradeArgs struct {
	mode        string // "", "rollup", or "fleet"
	target      string // agent or xo name when not --all
	all         bool
	from        string
	rosterPath  string
	secretsPath string
	binPath     string
}

// parseParadeArgs parses `flotilla parade [--all] [<agent>]`, `flotilla parade rollup
// [--all] [<xo>]`, or `flotilla parade fleet`.
func parseParadeArgs(args []string) (paradeArgs, error) {
	if len(args) == 0 {
		return paradeArgs{}, fmt.Errorf("usage: flotilla parade [--all] [<agent>] | flotilla parade rollup [--all] [<xo>] | flotilla parade fleet")
	}
	mode := ""
	rest := args
	switch args[0] {
	case "rollup":
		mode = "rollup"
		rest = args[1:]
	case "fleet":
		mode = "fleet"
		rest = args[1:]
	}
	target, flagArgs, err := parseParadeInterleavedArgs(rest)
	if err != nil {
		return paradeArgs{}, err
	}
	fs := flag.NewFlagSet("parade", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "orchestrator identity issuing the parade request")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path (for the dark-desk pre-check)")
	all := fs.Bool("all", false, "target every agent (answer), every coordinator with subordinates (rollup), or the primary XO (fleet)")
	if err := fs.Parse(flagArgs); err != nil {
		return paradeArgs{}, err
	}
	if len(fs.Args()) != 0 {
		return paradeArgs{}, fmt.Errorf("usage: flotilla parade [--all] [<agent>] | flotilla parade rollup [--all] [<xo>] | flotilla parade fleet")
	}
	if *all && target != "" {
		return paradeArgs{}, fmt.Errorf("usage: flotilla parade … (not both --all and <name>)")
	}
	switch mode {
	case "":
		if !*all && target == "" {
			return paradeArgs{}, fmt.Errorf("usage: flotilla parade [--all] [<agent>]")
		}
	case "rollup":
		if !*all && target == "" {
			return paradeArgs{}, fmt.Errorf("usage: flotilla parade rollup [--all] [<xo>]")
		}
	case "fleet":
		if *all || target != "" {
			return paradeArgs{}, fmt.Errorf("usage: flotilla parade fleet (no --all or <name>)")
		}
	default:
		return paradeArgs{}, fmt.Errorf("unknown parade mode %q", mode)
	}
	binPath, err := os.Executable()
	if err != nil {
		binPath = "flotilla"
	}
	return paradeArgs{
		mode:        mode,
		target:      target,
		all:         *all,
		from:        *from,
		rosterPath:  *rosterPath,
		secretsPath: *secretsPath,
		binPath:     binPath,
	}, nil
}

// parseParadeInterleavedArgs splits an optional target name from flag tokens.
func parseParadeInterleavedArgs(args []string) (target string, flagArgs []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--roster" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case a == "--secrets" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case a == "--from" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-"):
			flagArgs = append(flagArgs, a)
		case target == "":
			target = a
		default:
			return "", nil, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return target, flagArgs, nil
}

// buildParadeRequest is the individual four-domain parade prompt injected into a seat's pane.
func buildParadeRequest() string {
	return `flotilla parade request — answer the four parade domains AS YOUR TURN-FINAL.

(1) ACCOMPLISHMENTS (required): what you are proud of — concrete wins, not activity theater.
(2) WORKING ON NEXT (optional): omit the section entirely if nothing notable.
(3) LEARNINGS (required): what should outlive this chat. Include a fenced "## Learnings" section.
    Tag each learning fleet-wide (doctrine/skill) or local (desk-scoped). Fleet-wide learnings
    name a generic propagation target (skill, identity rule, memory) — never a deployment path.
(4) NEEDS HELP (optional): omit if clear; one line per genuine ask.

Use this shape:

[parade answer]

ACCOMPLISHMENTS:
  • …

WORKING ON NEXT:          ← omit if nothing notable
  • …

## Learnings
  • …

NEEDS HELP:               ← omit if none
  • …

Do NOT run "flotilla notify" and do NOT touch secrets — answer in-pane; the fleet mirror
publishes your turn-final to your channel automatically.`
}

// paradeRollupWakeBody composes the roll-up wake for a coordinating seat — self-sufficient
// like synthesisWakeBody, referencing the parade-formation skill contract.
func paradeRollupWakeBody(agent, binPath, rosterPath string, readSet, postChannels []string, fleet bool) string {
	var b strings.Builder
	if fleet {
		b.WriteString("[flotilla parade-formation] Fleet parade — curate the project-XO rollups into an operator parade report. ")
	} else {
		b.WriteString("[flotilla parade-formation] Domain parade roll-up — curate your subordinates' parade answers. ")
	}
	b.WriteString("Run your `parade-formation` skill (or, if you have none, follow the contract below).\n")

	if len(readSet) > 0 {
		b.WriteString("READ — for EACH agent below you, run `")
		b.WriteString(binPath)
		b.WriteString(" result --roster ")
		b.WriteString(rosterPath)
		b.WriteString(" <name>` to get its LATEST parade answer or roll-up. Your subordinates: ")
		b.WriteString(strings.Join(readSet, ", "))
		b.WriteString(".\n")
	} else {
		b.WriteString("READ: (no subordinates resolve right now — surface this in your roll-up)\n")
	}

	if len(postChannels) > 0 {
		b.WriteString("POST your parade roll-up into the channel you own: ")
		b.WriteString(strings.Join(postChannels, ", "))
		b.WriteString(" (via its webhook).\n")
	} else {
		b.WriteString("POST: (no owned channel resolved — surface this, do not drop the roll-up)\n")
	}

	if fleet {
		b.WriteString("CONTRACT (Tier 3 / fleet): fleet HEADLINE celebrating the whole fleet + wins GROUPED BY XO + " +
			"consolidated ## Learnings (deduplicated, attributed) + optional NEEDS HELP flags + detail footer " +
			"(agent names / channel IDs only in the footer).\n")
	} else {
		b.WriteString("CONTRACT (Tier 2 / domain): curated roll-up GROUPED BY subordinate — accomplishments, optional next, " +
			"extracted ## Learnings per desk, optional needs-help — plus a consolidated ## Learnings section at the top " +
			"or end aggregating fleet-wide items.\n")
	}
	b.WriteString("DISCIPLINE: celebratory and honest — no manufactured wins. Learnings are REQUIRED in every roll-up. " +
		"SKIP an unreadable subordinate (treat as UNKNOWN, never as 'went silent'). " +
		"After the fleet parade posts, coordinators persist fleet-wide learnings per the skill's propagation section " +
		"(append to roster-adjacent fleet-learnings.md, then run reflect/compound-learnings on each fleet-wide item).\n")
	return b.String()
}

// paradeAnswerTargets returns every roster agent for individual parade answers.
func paradeAnswerTargets(cfg *roster.Config) []string {
	out := make([]string, 0, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		out = append(out, agent.Name)
	}
	return out
}

// paradeRollupTargets returns every agent with at least one subordinate (synthesis read set).
func paradeRollupTargets(cfg *roster.Config) []string {
	out := make([]string, 0, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		if len(cfg.AgentsBelow(agent.Name)) > 0 {
			out = append(out, agent.Name)
		}
	}
	return out
}

// cmdParade elicits parade answers or coordinator roll-ups. Operator-triggered v1 — no daemon cadence.
func cmdParade(args []string) error {
	a, err := parseParadeArgs(args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(a.rosterPath)
	if err != nil {
		return err
	}
	var secrets *roster.Secrets
	if a.secretsPath != "" {
		secrets, err = roster.LoadSecrets(a.secretsPath)
		if err != nil {
			return err
		}
	} else if a.mode == "" {
		fmt.Fprintln(os.Stderr, "flotilla parade: note — no --secrets, skipping the dark-desk webhook pre-check (the parade request is still injected)")
	}
	switch a.mode {
	case "":
		if a.all {
			return cmdParadeAnswerAll(cfg, secrets, a)
		}
		return deliverParadeOne(cfg, secrets, a, a.target, buildParadeRequest())
	case "rollup":
		if a.all {
			return cmdParadeRollupAll(cfg, a)
		}
		return deliverParadeRollup(cfg, a, a.target, false)
	case "fleet":
		xo := primaryXOAgent(cfg)
		if xo == "" {
			return fmt.Errorf("flotilla parade fleet: no primary XO resolved from roster")
		}
		return deliverParadeRollup(cfg, a, xo, true)
	default:
		return fmt.Errorf("unknown parade mode %q", a.mode)
	}
}

func cmdParadeAnswerAll(cfg *roster.Config, secrets *roster.Secrets, a paradeArgs) error {
	req := buildParadeRequest()
	var failures int
	for _, agent := range paradeAnswerTargets(cfg) {
		if err := deliverParadeOne(cfg, secrets, a, agent, req); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %s: %v\n", agent, err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("flotilla parade --all: %d agent(s) failed (roster %s)", failures, a.rosterPath)
	}
	return nil
}

func cmdParadeRollupAll(cfg *roster.Config, a paradeArgs) error {
	var failures int
	for _, xo := range paradeRollupTargets(cfg) {
		if err := deliverParadeRollup(cfg, a, xo, false); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %s: %v\n", xo, err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("flotilla parade rollup --all: %d coordinator(s) failed (roster %s)", failures, a.rosterPath)
	}
	return nil
}

func deliverParadeRollup(cfg *roster.Config, a paradeArgs, xo string, fleet bool) error {
	readSet := synthesisReadSet(cfg, xo)
	postChannels := cfg.OwnedChannels(xo)
	msg := paradeRollupWakeBody(xo, a.binPath, a.rosterPath, readSet, postChannels, fleet)
	return deliverParadeOne(cfg, nil, a, xo, msg)
}

// deliverParadeOne injects a parade prompt into one agent via confirmed delivery.
func deliverParadeOne(cfg *roster.Config, secrets *roster.Secrets, a paradeArgs, agentName, message string) error {
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}
	if secrets != nil {
		url, werr := secrets.Webhook(agentName)
		if deskIsDark(url, werr) {
			return fmt.Errorf("agent %q is DARK: its channel webhook does not resolve — its parade answer cannot be published (configure the webhook in secrets, then retry)", agentName)
		}
	}
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q", agentName, agent.Surface)
	}
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
	if err != nil {
		return fmt.Errorf("%s pane is busy (another delivery/rotate in progress) — parade NOT delivered; retry: %w", agentName, err)
	}
	defer txn.Release()
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	if err := confirm.SubmitWithSelfHeal(drv, pane, message); err != nil {
		switch {
		case errors.Is(err, surface.ErrBusy):
			return fmt.Errorf("%s is busy (mid-turn) — parade NOT delivered; retry when it is idle", agentName)
		case errors.Is(err, surface.ErrCrashed):
			return fmt.Errorf("%s is at a shell (crashed) — parade NOT delivered", agentName)
		case errors.Is(err, surface.ErrPanelBlocked):
			return fmt.Errorf("%s is input-blocked behind the agents panel — parade NOT delivered; it needs a human keystroke or click into the composer at its pane, then retry", agentName)
		default:
			return fmt.Errorf("parade request to %s could not be confirmed: %w", agentName, err)
		}
	}
	if a.from != "" {
		fmt.Printf("parade request from %s delivered to %s (pane %s) — turn confirmed\n", a.from, agentName, pane)
	} else {
		fmt.Printf("parade request delivered to %s (pane %s) — turn confirmed\n", agentName, pane)
	}
	return nil
}
