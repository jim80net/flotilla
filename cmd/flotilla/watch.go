package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/workspace"
)

// detectorContinuationBuiltin is the default change-detector continuation prompt (the
// self-continuation tick). workspace.ResolvePrompt substitutes {{tracker}}/{{settle}}
// with the resolved tracker + settle paths and lets a per-agent HEARTBEAT.md override
// the wording. Kept as a package const so the no-workspace byte-identity (the prompt the
// XO receives when no workspace exists) is regression-locked by a test.
const detectorContinuationBuiltin = "[flotilla change-detector] You just finished a turn. Advance the next clear, " +
	"ALREADY-AUTHORIZED step if one remains — reading durable state, not memory: (1) the goal+task tracker " +
	"{{tracker}}; (2) the active openspec change's unchecked tasks; (3) the roadmap/README. A task " +
	"blocked only from landing (a push gate, a pending review) is NOT idle — advance it locally, then " +
	"surface the blocker in one line. If nothing AUTHORIZED remains, reply 'idle', do NOT manufacture " +
	"work, and signal idle by running: touch {{settle}}. (Your context is rotated between steps " +
	"— rely on durable state, not this conversation.)"

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
	maxMissed := fs.Int("max-missed-acks", 3, "consecutive missed acks (K) before a down alert")
	// change-detector (heartbeat v2) paths + tuning; consulted only when the
	// roster sets change_detector: true.
	snapshotPath := fs.String("snapshot-file", os.Getenv("FLOTILLA_SNAPSHOT_FILE"), "change-detector snapshot file (default <roster-dir>/flotilla-detector-state.json)")
	awaitingPath := fs.String("awaiting-file", os.Getenv("FLOTILLA_AWAITING_FILE"), "awaiting-operator veto marker (default <roster-dir>/flotilla-xo-awaiting)")
	settledPath := fs.String("settled-file", os.Getenv("FLOTILLA_SETTLED_FILE"), "XO settle (idle) marker (default <roster-dir>/flotilla-xo-settled)")
	trackerPath := fs.String("tracker-file", os.Getenv("FLOTILLA_TRACKER_FILE"), "the XO's state tracker the continuation prompt names as {{tracker}} (default <roster-dir>/.flotilla-state.md); NOT hashed as a wake signal — it is the XO's own output")
	signalPath := fs.String("signal-file", os.Getenv("FLOTILLA_SIGNAL_FILE"), "optional external signal file whose content-hash change wakes the XO (a file the XO does NOT write; unset ⇒ no external-signal trigger)")
	maxQuiet := fs.Int("max-quiet-intervals", 0, "change-detector liveness ping cadence N in intervals (0 ⇒ mode default)")
	maxSelfCont := fs.Int("max-self-continuations", 3, "change-detector cap on consecutive XO self-continuations with no external change")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	// Validate every agent's surface driver up front — an unknown surface is a
	// clear startup error, never a silent mis-drive at the first tick/delivery.
	if err := validateAgentSurfaces(cfg); err != nil {
		return err
	}
	xo := cfg.XOAgent
	if xo == "" {
		xo = cfg.Agents[0].Name
	}
	// The XO's driver (for state assessment in the gate). Surfaces are validated
	// above, so this lookup succeeds.
	xoDrv, _ := surface.Get(agentSurface(cfg, xo))

	interval := cfg.HeartbeatDur() // parsed + validated at load
	rosterDir := filepath.Dir(*rosterPath)
	if *ackPath == "" {
		*ackPath = filepath.Join(rosterDir, "flotilla-xo-alive")
	}
	if *snapshotPath == "" {
		*snapshotPath = filepath.Join(rosterDir, "flotilla-detector-state.json")
	}
	if *awaitingPath == "" {
		*awaitingPath = filepath.Join(rosterDir, "flotilla-xo-awaiting")
	}
	if *settledPath == "" {
		*settledPath = filepath.Join(rosterDir, "flotilla-xo-settled")
	}
	if *trackerPath == "" {
		*trackerPath = filepath.Join(rosterDir, ".flotilla-state.md")
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

	// paneMus serializes the daemon's two in-process pane writers — a confirmed delivery
	// (below) and the change-detector's /clear rotate (Rotate closure) — so the rotate cannot
	// interleave between a confirmed delivery's submit and its Enter-only retry. Shared by both.
	paneMus := watch.NewPaneMutexes()
	// confirm turns "the tmux keystrokes ran" into "a turn started": it idle-gates, submits,
	// confirms the Idle→Working edge, retries Enter-only (never re-pasting), and returns a typed
	// error the Injector dispatches on (ErrBusy → defer; failure → loud alert). Closing the
	// relay's silent-drop class (docs/findings-inbound-relay-lastmile.md).
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	injector := watch.NewInjector(func(agent, message string) error {
		drv, ok := surface.Get(agentSurface(cfg, agent))
		if !ok {
			return fmt.Errorf("unknown surface for agent %q", agent)
		}
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return err
		}
		// Hold the per-agent pane mutex across the WHOLE confirmed-delivery sequence so the
		// detector's /clear rotate cannot interleave between the submit and the retry.
		unlock := paneMus.Lock(agent)
		defer unlock()
		return confirm.Submit(drv, pane, message)
	}, 16)
	// A failed/undeliverable RELAY (operator message) raises a LOUD alert — the inverse of the
	// silent-success bug. Heartbeat/detector ticks never escalate (a stale tick is dropped).
	injector.SetEscalate(alert)
	// Mirror relayed instructions to the audit channel in full. Heartbeat ticks
	// are NOT mirrored: they fire every interval and a per-tick marker is pure
	// noise in the operator's Discord channel (XO liveness is already covered by
	// the ack file + the missed-ack down alert below). Posted via webhook, which
	// the gateway's feedback filter drops — no loop.
	injector.SetMirror(func(j watch.Job) {
		// Heartbeat ticks and change-detector wakes fire automatically; a per-wake
		// marker is pure noise in the operator's channel (XO liveness is covered by
		// the ack file + the down alert). Only relayed operator traffic is mirrored.
		if j.Kind == "heartbeat" || j.Kind == "detector" {
			return
		}
		post("flotilla-watch", "→ "+j.Agent+": "+j.Message)
	})
	injector.Start()
	defer injector.Stop()

	ack := watch.NewAckWatcher(*ackPath)
	ackInstr := "\n(To ack you are alive, run: touch " + *ackPath + ")"

	// onAccepted is the relay's clock hook: legacy resets the heartbeat timer; v2
	// clears the detector's settled flag when the message targets the XO.
	var onAccepted func(string)

	if cfg.ChangeDetector {
		// ---- heartbeat v2: the change-detector (wake only on a material change) ----
		desks := make([]string, 0, len(cfg.Agents))
		for _, a := range cfg.Agents {
			desks = append(desks, a.Name)
		}
		awaiting := watch.NewAwaitingMarker(*awaitingPath)
		settled := watch.NewSettledMarker(*settledPath)

		// The tracker path is resolved ONCE (workspace state.md → --tracker-file/default)
		// and used ONLY as the {{tracker}} the continuation prompt names — the XO's own
		// read+write source. The detector deliberately does NOT hash it as a wake signal:
		// the heartbeat instructs the XO to keep the tracker current, so hashing it would
		// self-wake the XO on its own writes (a loop until it settles). External wake
		// deltas flow through the separate, optional --signal-file (a file the XO does not
		// write); see signalHash below.
		resolvedTracker, err := workspace.ResolveTracker(xo, *trackerPath)
		if err != nil {
			return err
		}

		// The external-signal wake source: hash the --signal-file when configured, else
		// leave nil so the detector defaults it to inert (no external-signal trigger).
		var signalHash func() (string, bool)
		if *signalPath != "" {
			signalHash = contentHasher(*signalPath)
		}

		// The continuation prompt carries the narrow-answer discipline (advance the next
		// AUTHORIZED step or reply idle — never manufacture work) and tells the XO how to
		// signal idle (touch the settle marker). Context is rotated between steps, so it
		// instructs reading durable state rather than this conversation. The per-agent
		// HEARTBEAT.md may override the wording; with no workspace the built-in below is
		// used and {{tracker}}/{{settle}} substitute byte-identically to direct interpolation.
		continuationPrompt, err := workspace.ResolvePrompt(xo, detectorContinuationBuiltin, resolvedTracker, *settledPath)
		if err != nil {
			return err
		}
		continuationPrompt += ackInstr

		wake := func(kind watch.WakeKind, reasons []string) {
			var body string
			switch kind {
			case watch.WakeContinuation:
				body = continuationPrompt
			case watch.WakePing:
				body = "[flotilla change-detector] Liveness check — reply with a one-line ack only; take no other action." + ackInstr
			default: // WakeMaterial
				body = "[flotilla change-detector] Material change(s) detected: " + strings.Join(reasons, "; ") +
					".\nCheck in on the affected desk(s) and advance any authorized coordination. If nothing is " +
					"actionable, reply idle and signal it by running: touch " + *settledPath + "." + ackInstr
			}
			injector.Enqueue(watch.Job{Agent: xo, Message: body, Kind: "detector"})
		}

		det := watch.NewDetector(watch.DetectorConfig{
			XOAgent:  xo,
			Desks:    desks,
			Interval: interval,
			Assess: func(agent string) surface.State {
				drv, ok := surface.Get(agentSurface(cfg, agent))
				if !ok {
					return surface.StateUnknown
				}
				pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
				if err != nil {
					// The pane titled for this agent is gone (the session died and
					// the pane closed, or its title no longer matches) — that is a
					// crash, equivalent to a pane that dropped back to a shell. Map
					// it to Shell so the detector's two-consecutive debounce absorbs a
					// transient resolve blip but a persistent vanish still crash-alerts
					// the XO immediately (preserving — and bettering — the legacy gate,
					// which alerted on the very first resolve failure).
					return surface.StateShell
				}
				return drv.Assess(pane)
			},
			SignalHash: signalHash,
			AckAge:     ack.Age,
			Wake:       wake,
			Rotate: func() error {
				// Serialize the /clear rotate against an in-flight confirmed delivery to the XO
				// pane (the same paneMus the Injector holds), so the rotate never interleaves
				// between a delivery's submit and its Enter-only retry.
				unlock := paneMus.Lock(xo)
				defer unlock()
				pane, err := deliver.ResolvePane(agentTitle(cfg, xo))
				if err != nil {
					return err
				}
				return surface.RotateContext(xoDrv, pane)
			},
			Awaiting:            awaiting.Present,
			SettleConsume:       settled.Consume,
			Alert:               alert,
			MaxMissedAcks:       *maxMissed,
			MaxQuietIntervals:   *maxQuiet,
			LivenessPingMode:    cfg.LivenessPingMode,
			MaxSelfContinuation: *maxSelfCont,
		}, *snapshotPath)
		det.Start()
		defer det.Stop()
		onAccepted = func(target string) {
			if target == xo {
				det.OperatorWake() // an operator message re-engages a settled XO
			}
		}
		mode := cfg.LivenessPingMode
		if mode == "" {
			mode = "none"
		}
		fmt.Printf("flotilla watch: change-detector running — XO=%s interval=%s ping-mode=%s ack=%s snapshot=%s\n",
			xo, interval, mode, *ackPath, *snapshotPath)
	} else {
		// ---- legacy always-wake heartbeat ----
		wd := watch.NewWatchdog(*maxMissed, alert)

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
			// captures). For claude-code: Shell ⇒ crashed, Working ⇒ busy. (capture-error
			// ⇒ Unknown since #55, converging all drivers; here in the legacy gate Idle
			// and Unknown are equivalent — both are not-Shell and not-Working, so the
			// tick fires either way.)
			st := xoDrv.Assess(pane)
			wd.Observe(ack.Acked(), st == surface.StateShell)
			if wd.Down() {
				return true
			}
			return st == surface.StateWorking
		}

		// Legacy heartbeat prompt resolution: a non-empty workspace HEARTBEAT.md →
		// roster heartbeat_message → DefaultHeartbeatPrompt. Legacy mode has no detector
		// tracker/settle path, so {{tracker}}/{{settle}} are substituted with empty strings
		// here: NEITHER the roster heartbeat_message NOR a HEARTBEAT.md override should use
		// those placeholders in legacy mode (the built-in legacy prompts don't — no-op).
		legacyBuiltin := cfg.HeartbeatMessage
		if legacyBuiltin == "" {
			legacyBuiltin = watch.DefaultHeartbeatPrompt
		}
		prompt, err := workspace.ResolvePrompt(xo, legacyBuiltin, "", "")
		if err != nil {
			return err
		}
		prompt += ackInstr

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
		onAccepted = func(string) { hb.Reset() }

		fmt.Printf("flotilla watch: clock running — XO=%s interval=%s ack=%s\n", xo, interval, *ackPath)
	}

	// The daemon's shutdown context — established BEFORE the relay so the relay's
	// background open-retry goroutine can be tied to it (and so cancellation on
	// SIGTERM/SIGINT unwinds the retry cleanly, no leak).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Inbound relay (optional): needs a channel id + bot token (and the bot's
	// privileged Message Content intent) AND an operator_user_id — without the
	// latter, relay.Accept drops every message, so enabling the gateway would
	// claim "relay active" while silently dropping all traffic.
	//
	// The relay open is NON-FATAL: a gateway construct/open failure (the cold-boot
	// DNS blip ~6s post-reboot, a transient network hiccup) must NOT take down the
	// already-running safety-critical clock — that crash-looped flotilla-watch to a
	// permanent `failed` after the 2026-06-10 power-failure reboot, killing the very
	// down-alert relay before it could alert. So we degrade to clock-only and retry
	// the open in the background until it succeeds or shutdown. The warning goes to
	// stderr (journald) ONLY, never the Discord webhook — that needs the same
	// network that just failed.
	switch {
	case cfg.ChannelID != "" && botToken != "" && cfg.OperatorUserID != "":
		rel := watch.NewRelay(cfg, xo, injector, onAccepted, func(msg string) { post("flotilla-watch", msg) })
		factory := func() (gatewayController, error) {
			gw, err := discord.NewGateway(botToken, cfg.ChannelID, rel.Handle)
			if err != nil {
				return nil, err
			}
			if err := gw.Open(); err != nil {
				return nil, err
			}
			return gw, nil
		}
		// warn → stderr (journald) for routine per-attempt noise; note → stdout for
		// active/recovered; escalate → the down-alert webhook (alert) so a SUSTAINED
		// relay-down state (bad token / real outage) surfaces loudly to the operator
		// exactly once. alert already falls back to stderr when no webhook is set.
		rc := newRelayController(factory, defaultRelayBackoff, stderrWarn, func(msg string) { fmt.Println(msg) }, alert)
		rc.Start(ctx)
		defer rc.Shutdown()
	case cfg.ChannelID != "" && botToken != "" && cfg.OperatorUserID == "":
		return fmt.Errorf("relay requires operator_user_id in the roster (channel_id + bot token are set) — set it, or remove channel_id for clock-only")
	default:
		fmt.Println("flotilla watch: clock-only (relay disabled — set channel_id + bot token + operator_user_id to enable)")
	}

	<-ctx.Done()
	fmt.Println("\nflotilla watch: shutting down")
	return nil
}

// contentHasher returns a content-hash function for a file the change-detector
// watches as an external wake signal. A change in the hash is a material signal.
// Absent OR unreadable both report ok=false (no update) so the detector carries the
// prior hash forward and treats it as unchanged (absent → no-signal, read-error →
// treat-unchanged — never a wake-storm).
func contentHasher(path string) func() (string, bool) {
	return func() (string, bool) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:]), true
	}
}

// agentTitle returns the tmux pane title to resolve for an agent name.
func agentTitle(cfg *roster.Config, name string) string {
	if a, err := cfg.Agent(name); err == nil {
		return a.Title()
	}
	return name
}

// validateAgentSurfaces checks that every agent's configured surface resolves to
// a registered driver, so a misconfigured roster refuses to start rather than
// mis-driving a pane at the first tick/delivery. An empty surface resolves to the
// default (claude-code); "aider" resolves to the aider driver; an unregistered
// name (e.g. "nope") is a clear startup error.
func validateAgentSurfaces(cfg *roster.Config) error {
	for _, a := range cfg.Agents {
		if _, ok := surface.Get(a.Surface); !ok {
			return fmt.Errorf("agent %q: unknown surface %q (known: see internal/surface registry)", a.Name, a.Surface)
		}
	}
	return nil
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
