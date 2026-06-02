// Command flotilla coordinates a fleet of AI coding agents: it delivers an
// instruction into a target agent's tmux pane (the delivery IS the wake) and
// mirrors it to the Discord audit channel under the sender's identity.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

const version = "0.0.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "flotilla: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "send":
		return cmdSend(args[1:])
	case "version", "-v", "--version":
		fmt.Println("flotilla " + version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: flotilla help)", args[0])
	}
}

func usage() {
	fmt.Println(`flotilla — coordinate a fleet of AI coding agents

usage:
  flotilla send --from <sender> <agent> <message>   deliver to an agent's pane + mirror to Discord
  flotilla version
  flotilla help

flags for 'send':
  --from <name>     sender identity (default $FLOTILLA_SELF)
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)
  --no-mirror       deliver via tmux only; skip the Discord audit post

The Discord audit mirror is best-effort: if it is unconfigured or fails, the
message is still delivered and the command succeeds (with a warning), so a
retry never double-delivers.`)
}

func rosterDefault() string {
	if p := os.Getenv("FLOTILLA_ROSTER"); p != "" {
		return p
	}
	return "flotilla.json"
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "sender identity")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	noMirror := fs.Bool("no-mirror", false, "skip the Discord audit mirror")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: flotilla send --from <sender> <agent> <message>")
	}
	agentName := rest[0]
	message := strings.Join(rest[1:], " ")
	if *from == "" {
		return fmt.Errorf("--from is required (or set $FLOTILLA_SELF)")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("message is empty")
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}

	// Deliver = wake: paste the message into the agent's pane and submit. This
	// is the operation that must succeed; the audit mirror below is best-effort.
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	if err := deliver.Send(pane, message); err != nil {
		return err
	}
	fmt.Printf("delivered to %s (pane %s)\n", agentName, pane)

	// Mirror to the Discord audit channel under the sender's identity. A mirror
	// failure (or absence) is a warning, not a command failure — the message is
	// already delivered, and failing here would tempt a retry into a double-send.
	if *noMirror {
		return nil
	}
	if err := mirror(*secretsPath, *from, agentName, message); err != nil {
		fmt.Fprintln(os.Stderr, "flotilla: WARNING — audit mirror skipped (message WAS delivered): "+err.Error())
	}
	return nil
}

// mirror posts the delivered instruction to the audit channel under the
// sender's webhook identity. Errors are returned for the caller to warn on.
func mirror(secretsPath, from, agentName, message string) error {
	if secretsPath == "" {
		return fmt.Errorf("secrets unset (set --secrets/$FLOTILLA_SECRETS, or pass --no-mirror)")
	}
	secrets, err := roster.LoadSecrets(secretsPath)
	if err != nil {
		return err
	}
	hook, err := secrets.Webhook(from)
	if err != nil {
		return err
	}
	return discord.Post(hook, from, fmt.Sprintf("→ %s: %s", agentName, message))
}
