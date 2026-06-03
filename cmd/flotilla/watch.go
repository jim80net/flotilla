package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// cmdWatch runs the long-lived watch daemon. This is the CLOCK half: it
// heartbeats the XO so a turn-based agent keeps advancing clear, authorized work
// without operator input, and watches liveness (tick→ack) so a dead or
// context-exhausted XO is surfaced. The inbound Discord relay is added on top
// (it needs the gateway + Message Content intent); the clock needs neither.
func cmdWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path (for the down-alert webhook)")
	ackPath := fs.String("ack-file", os.Getenv("FLOTILLA_ACK_FILE"), "XO liveness ack file (the XO touches it)")
	maxMissed := fs.Int("max-missed-acks", 3, "consecutive missed acks before a down alert")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	xo := cfg.XOAgent
	if xo == "" {
		xo = cfg.Agents[0].Name
	}

	interval := time.Duration(0)
	if cfg.HeartbeatInterval != "" && cfg.HeartbeatInterval != "0" {
		interval, _ = time.ParseDuration(cfg.HeartbeatInterval) // validated at load
	}
	if *ackPath == "" {
		*ackPath = filepath.Join(filepath.Dir(*rosterPath), "flotilla-xo-alive")
	}

	alert := buildAlert(*secretsPath, xo)

	injector := watch.NewInjector(func(agent, message string) error {
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return err
		}
		return deliver.Send(pane, message)
	}, 16)
	injector.Start()
	defer injector.Stop()

	wd := watch.NewWatchdog(*maxMissed, alert)
	ack := watch.NewAckWatcher(*ackPath)

	// gate runs every interval: observe XO liveness (crash + ack) and skip the
	// tick while the XO is down (don't wind a dead clock). ResolvePane failures
	// are treated as "down", never fatal to the daemon.
	gate := func() bool {
		crashed := false
		if pane, err := deliver.ResolvePane(agentTitle(cfg, xo)); err != nil {
			crashed = true
		} else if cmdName, err := deliver.PaneCommand(pane); err != nil || deliver.IsShell(cmdName) {
			crashed = true
		}
		wd.Observe(ack.Acked(), crashed)
		return wd.Down()
	}

	busy := func(agent string) bool {
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return false
		}
		b, _ := deliver.Busy(pane)
		return b
	}

	prompt := cfg.HeartbeatMessage
	if prompt == "" {
		prompt = watch.DefaultHeartbeatPrompt
	}
	prompt += "\n(To ack you are alive, run: touch " + *ackPath + ")"

	hb := watch.NewHeartbeat(interval, xo, prompt, injector.Enqueue, busy)
	hb.SetGate(gate)
	hb.Start()
	defer hb.Stop()

	fmt.Printf("flotilla watch: clock running — XO=%s interval=%s ack=%s\n", xo, interval, *ackPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	<-ctx.Done()
	fmt.Println("\nflotilla watch: shutting down")
	return nil
}

// buildAlert returns the down-alert sink: a Discord post under a flotilla-watch
// identity when a webhook is available, else stderr.
func buildAlert(secretsPath, xo string) func(string) {
	if secretsPath != "" {
		if secrets, err := roster.LoadSecrets(secretsPath); err == nil {
			if hook, err := secrets.Webhook(xo); err == nil {
				return func(msg string) { _ = discord.Post(hook, "flotilla-watch", "⚠️ "+msg) }
			}
		}
	}
	return func(msg string) { fmt.Fprintln(os.Stderr, "flotilla watch ALERT: "+msg) }
}

// agentTitle returns the tmux pane title to resolve for an agent name.
func agentTitle(cfg *roster.Config, name string) string {
	if a, err := cfg.Agent(name); err == nil {
		return a.Title()
	}
	return name
}
