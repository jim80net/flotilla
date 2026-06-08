package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
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
	// Validate every agent's surface driver up front — an unknown surface is a
	// clear startup error, never a silent mis-drive at the first tick/delivery.
	for _, a := range cfg.Agents {
		if _, ok := surface.Get(a.Surface); !ok {
			return fmt.Errorf("agent %q: unknown surface %q (known: see internal/surface registry)", a.Name, a.Surface)
		}
	}
	xo := cfg.XOAgent
	if xo == "" {
		xo = cfg.Agents[0].Name
	}
	// The XO's driver (for state assessment in the gate). Surfaces are validated
	// above, so this lookup succeeds.
	xoDrv, _ := surface.Get(agentSurface(cfg, xo))

	interval := cfg.HeartbeatDur() // parsed + validated at load
	if *ackPath == "" {
		*ackPath = filepath.Join(filepath.Dir(*rosterPath), "flotilla-xo-alive")
	}

	// Load secrets once: the bot token (gateway) and the alert/notice webhook.
	// A configured-but-broken secrets file is fatal — don't silently degrade to
	// clock-only (the operator set --secrets expecting the relay).
	var alertHook, botToken string
	if *secretsPath != "" {
		secrets, err := roster.LoadSecrets(*secretsPath)
		if err != nil {
			return err
		}
		botToken = secrets.BotToken()
		if h, err := secrets.Webhook(xo); err == nil {
			alertHook = h
		}
	}
	if alertHook == "" {
		fmt.Fprintln(os.Stderr, "flotilla watch: WARNING — no alert webhook; down-alerts go to stderr (journald) only")
	}
	post := func(username, msg string) {
		if alertHook != "" {
			_ = discord.Post(alertHook, username, msg)
		} else {
			fmt.Fprintln(os.Stderr, "flotilla watch: "+msg)
		}
	}
	alert := func(msg string) { post("flotilla-watch", "⚠️ "+msg) }

	injector := watch.NewInjector(func(agent, message string) error {
		drv, ok := surface.Get(agentSurface(cfg, agent))
		if !ok {
			return fmt.Errorf("unknown surface for agent %q", agent)
		}
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return err
		}
		return drv.Submit(pane, message)
	}, 16)
	// Mirror relayed instructions to the audit channel in full. Heartbeat ticks
	// are NOT mirrored: they fire every interval and a per-tick marker is pure
	// noise in the operator's Discord channel (XO liveness is already covered by
	// the ack file + the missed-ack down alert below). Posted via webhook, which
	// the gateway's feedback filter drops — no loop.
	injector.SetMirror(func(j watch.Job) {
		if j.Kind == "heartbeat" {
			return
		}
		post("flotilla-watch", "→ "+j.Agent+": "+j.Message)
	})
	injector.Start()
	defer injector.Stop()

	wd := watch.NewWatchdog(*maxMissed, alert)
	ack := watch.NewAckWatcher(*ackPath)

	// gate runs every interval: resolve the XO pane ONCE, observe liveness
	// (crash + ack), and skip the tick while the XO is down OR busy. A resolve
	// failure is treated as "down", never fatal to the daemon.
	gate := func() bool {
		pane, err := deliver.ResolvePane(agentTitle(cfg, xo))
		if err != nil {
			wd.Observe(ack.Acked(), true)
			return true
		}
		// The surface driver assesses rendered state (it performs its own pane
		// captures). For claude-code this is byte-identical to the prior inline
		// PaneCommand/IsShell + Busy logic: Shell ⇒ crashed, Working ⇒ busy,
		// capture-error ⇒ Idle (fail-open).
		st := xoDrv.Assess(pane)
		wd.Observe(ack.Acked(), st == surface.StateShell)
		if wd.Down() {
			return true
		}
		return st == surface.StateWorking
	}

	prompt := cfg.HeartbeatMessage
	if prompt == "" {
		prompt = watch.DefaultHeartbeatPrompt
	}
	prompt += "\n(To ack you are alive, run: touch " + *ackPath + ")"

	// busy-gating is handled inside gate (single pane resolve per cycle), so the
	// heartbeat's own busy predicate is nil here.
	hb := watch.NewHeartbeat(interval, xo, prompt, injector.Enqueue, nil)
	hb.SetGate(gate)
	// Activity probe: fingerprint the XO pane (its captured contents). Any change
	// — the XO taking a turn, emitting output — resets the idle clock, so the tick
	// fires only after genuine inactivity, not on a fixed wall-clock. Returning ""
	// (pane unresolved/unreadable) is treated as no-activity and never false-resets.
	hb.SetActivityProbe(func() string {
		pane, err := deliver.ResolvePane(agentTitle(cfg, xo))
		if err != nil {
			return ""
		}
		out, err := deliver.CapturePane(pane)
		if err != nil {
			return ""
		}
		return out
	})
	hb.Start()
	defer hb.Stop()

	fmt.Printf("flotilla watch: clock running — XO=%s interval=%s ack=%s\n", xo, interval, *ackPath)

	// Inbound relay (optional): needs a channel id + bot token (and the bot's
	// privileged Message Content intent) AND an operator_user_id — without the
	// latter, relay.Accept drops every message, so enabling the gateway would
	// claim "relay active" while silently dropping all traffic.
	switch {
	case cfg.ChannelID != "" && botToken != "" && cfg.OperatorUserID != "":
		rel := watch.NewRelay(cfg, xo, injector, hb, func(msg string) { post("flotilla-watch", msg) })
		gw, err := discord.NewGateway(botToken, cfg.ChannelID, rel.Handle)
		if err != nil {
			return err
		}
		if err := gw.Open(); err != nil {
			return err
		}
		defer func() { _ = gw.Close() }()
		fmt.Println("flotilla watch: inbound relay active")
	case cfg.ChannelID != "" && botToken != "" && cfg.OperatorUserID == "":
		return fmt.Errorf("relay requires operator_user_id in the roster (channel_id + bot token are set) — set it, or remove channel_id for clock-only")
	default:
		fmt.Println("flotilla watch: clock-only (relay disabled — set channel_id + bot token + operator_user_id to enable)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	<-ctx.Done()
	fmt.Println("\nflotilla watch: shutting down")
	return nil
}

// agentTitle returns the tmux pane title to resolve for an agent name.
func agentTitle(cfg *roster.Config, name string) string {
	if a, err := cfg.Agent(name); err == nil {
		return a.Title()
	}
	return name
}

// agentSurface returns the surface name configured for an agent (empty ⇒ the
// default driver). An unknown name falls back to "" so surface.Get resolves the
// default rather than erroring on a non-roster name.
func agentSurface(cfg *roster.Config, name string) string {
	if a, err := cfg.Agent(name); err == nil {
		return a.Surface
	}
	return ""
}
