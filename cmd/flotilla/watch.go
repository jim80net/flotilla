package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/cos"
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
	cursorPath := fs.String("relay-cursor-file", os.Getenv("FLOTILLA_RELAY_CURSOR_FILE"), "relay catch-up per-channel cursor file (default <roster-dir>/flotilla-relay-cursor.json); the at-least-once ingestion backstop's durable state")
	awaitingPath := fs.String("awaiting-file", os.Getenv("FLOTILLA_AWAITING_FILE"), "awaiting-operator veto marker (default <roster-dir>/flotilla-xo-awaiting)")
	settledPath := fs.String("settled-file", os.Getenv("FLOTILLA_SETTLED_FILE"), "XO settle (idle) marker (default <roster-dir>/flotilla-xo-settled)")
	trackerPath := fs.String("tracker-file", os.Getenv("FLOTILLA_TRACKER_FILE"), "the XO's state tracker the continuation prompt names as {{tracker}} (default <roster-dir>/.flotilla-state.md); NOT hashed as a wake signal — it is the XO's own output")
	signalPath := fs.String("signal-file", os.Getenv("FLOTILLA_SIGNAL_FILE"), "optional external signal file whose content-hash change wakes the XO (a file the XO does NOT write; unset ⇒ no external-signal trigger)")
	maxQuiet := fs.Int("max-quiet-intervals", 0, "change-detector liveness ping cadence N in intervals (0 ⇒ mode default)")
	maxSelfCont := fs.Int("max-self-continuations", 3, "change-detector cap on consecutive XO self-continuations with no external change")
	backlogPath := fs.String("backlog-file", os.Getenv("FLOTILLA_BACKLOG_FILE"), "the goal-driven loop's fleet backlog (markdown; - [<status>] items). Unset ⇒ the backlog gate is OFF (XO settles as before). Read fresh each tick, NOT content-hashed (it is the XO's own output)")
	backlogStuckCap := fs.Int("backlog-stuck-cap", 5, "goal-driven loop: drives of one unblocked item without progress before it is escalated + deprioritized")
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
	if *cursorPath == "" {
		*cursorPath = filepath.Join(rosterDir, "flotilla-relay-cursor.json")
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

	// Load secrets once: the bot token (gateway), the alert/notice webhook, and — kept for the
	// per-desk visibility mirror — the whole Secrets so each desk's own webhook can be resolved at
	// mirror time (a webhook is channel-bound, so posting under a desk's webhook lands in its
	// channel). A configured-but-broken secrets file is fatal — don't silently degrade to clock-only
	// (the operator set --secrets expecting the relay).
	var alertHook, botToken string
	var secrets *roster.Secrets
	if *secretsPath != "" {
		s, err := roster.LoadSecrets(*secretsPath)
		if err != nil {
			return err
		}
		secrets = s
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

	// confirm turns "the tmux keystrokes ran" into "a turn started": it idle-gates, submits,
	// confirms the Idle→Working edge, retries Enter-only (never re-pasting), and returns a typed
	// error the Injector dispatches on (ErrBusy → defer; failure → loud alert). Closing the
	// relay's silent-drop class.
	// Self-heal (#156) is DEFAULT-OFF: SendCtrlC is wired only when FLOTILLA_SELF_HEAL is enabled (the
	// kill-switch). When unwired, SubmitWithSelfHeal == Submit (inert), so relay and tick behave
	// identically. Ctrl-C is destructive — see surface.selfHeal's safety gates.
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
		log.Printf("flotilla watch: self-heal ENABLED (FLOTILLA_SELF_HEAL) — relay input-blocks attempt bounded Ctrl-C recovery")
	}
	// mkSend resolves the surface + pane + per-pane transaction lock, then submits via `submit`. The
	// TRANSACTION lock spans the WHOLE confirmed-delivery sequence so no other transaction (the
	// detector's /clear rotate, a `flotilla send`, a dash control action) interleaves between the
	// submit and its Enter-only retry / self-heal.
	mkSend := func(submit func(surface.Driver, string, string) error) watch.SendFunc {
		return func(agent, message string) error {
			drv, ok := surface.Get(agentSurface(cfg, agent))
			if !ok {
				return fmt.Errorf("unknown surface for agent %q", agent)
			}
			pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
			if err != nil {
				return err
			}
			txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
			if err != nil {
				return err
			}
			defer txn.Release()
			return submit(drv, pane, message)
		}
	}
	injector := watch.NewInjector(mkSend(confirm.Submit), 16)
	// RELAY-kind jobs route through the self-heal-capable submit; heartbeat/detector ticks keep the
	// plain submit (a tick must never fire an unsolicited Ctrl-C — #156 H2). Inert when self-heal off.
	injector.SetRelaySend(mkSend(confirm.SubmitWithSelfHeal))
	// A failed/undeliverable RELAY (operator message) raises a LOUD alert — the inverse of the
	// silent-success bug. Heartbeat/detector ticks never escalate (a stale tick is dropped).
	injector.SetEscalate(alert)
	// Mirror relayed instructions to the audit channel in full. Heartbeat ticks
	// are NOT mirrored: they fire every interval and a per-tick marker is pure
	// noise in the operator's Discord channel (XO liveness is already covered by
	// the ack file + the missed-ack down alert below). Posted via webhook, which
	// the gateway's feedback filter drops — no loop.
	// #175: the c2-hotline reply-watcher. When an operator message lands on a c2 channel's (federated)
	// XO, watch that XO's session store for the reply and route it back to the channel — the return leg
	// the primary XO already has via its Stop-hook. nil when secrets are absent (no webhooks to resolve).
	replyRtr := newHotlineReplyRouter(context.Background(), cfg, secrets, alert)
	if replyRtr != nil {
		defer replyRtr.Stop() // cancel in-flight hotline watchers on shutdown (runs after <-ctx.Done())
	}
	injector.SetMirror(func(j watch.Job) {
		// Heartbeat ticks and change-detector wakes fire automatically; a per-wake
		// marker is pure noise in the operator's channel (XO liveness is covered by
		// the ack file + the down alert). Only relayed operator traffic is mirrored.
		if j.Kind == "heartbeat" || j.Kind == "detector" {
			return
		}
		post("flotilla-watch", "→ "+j.Agent+": "+j.Message)
		if replyRtr != nil && isHotlineToChannelXO(cfg, j) {
			replyRtr.arm(j.Agent, j.OriginChannel, j.Message) // watch the XO's reply to THIS message, route it back
		}
		// CoS context-mirror (#108): append this confirmed operator→XO relay delivery
		// to the who-knows-what ledger, tagged with the origin channel (the #105
		// Job.OriginChannel seam). Scoped to XO targets (a desk addressed via @name is
		// not operator↔XO traffic in v1 — symmetric with the notify path; broader scope
		// is design §6.3 Phase 2). Inert unless cos_agent is set; observe-only +
		// best-effort (never affects delivery).
		mirrorRelayToLedger(cfg, j)
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

		// The goal-driven loop's backlog gate (opt-in via --backlog-file; unset ⇒ nil ⇒ the
		// detector's inert default ⇒ today's settle behavior). backlogStatusGate reads the file
		// FRESH each tick (NOT content-hashed — the backlog is the XO's OWN output, like the
		// tracker, so hashing it would self-wake on the XO's edits) and raises a LOUD alert once on
		// the edge into a present-but-unparseable state. The gate is called only from the detector's
		// continueXO under its mutex, so the latch inside is single-goroutine.
		var backlogGate func() backlog.Status
		if *backlogPath != "" {
			bp := *backlogPath
			backlogGate = backlogStatusGate(bp, func() ([]byte, error) { return os.ReadFile(bp) }, alert)
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
			case watch.WakeBacklog:
				body = backlogWakeBody(reasons, *backlogPath, ackInstr)
			default: // WakeMaterial
				body = "[flotilla change-detector] Material change(s) detected: " + strings.Join(reasons, "; ") +
					".\nCheck in on the affected desk(s) and advance any authorized coordination. If nothing is " +
					"actionable, reply idle and signal it by running: touch " + *settledPath + "." + ackInstr
			}
			injector.Enqueue(watch.Job{Agent: xo, Message: body, Kind: "detector"})
		}

		// wakeAgent is the PARALLEL agent-targeted wake seam (visibility synthesis, B2). It enqueues a
		// WakeSynthesis to an ARBITRARY synthesizing agent (a project XO / the meta-XO), NOT the
		// hardcoded primary `xo` — the Injector already addresses any agent via Job.Agent. The body
		// names the agent's read set (AgentsBelow) + post target (OwnedChannels) + the per-tier
		// contract; the detailed curation lives in the embedded visibility-synthesis skill. Inert
		// unless the detector is wired with the synthesis seams below (gated on VisibilitySynthesis).
		// The synthesis wake prompt injects `<flotilla-bin> result --roster <path> <name>` so the woken
		// agent can read its subordinates without a workspace skill (the harness-launch-agnostic path).
		// Inject the daemon's OWN ABSOLUTE binary path (os.Executable) and the absolute roster path — NOT
		// bare `flotilla` and not rosterDefault() — because a DIRECTLY-LAUNCHED agent may have neither
		// `flotilla` on its $PATH nor the daemon's cwd (the live fleet invokes ~/go/bin/flotilla by
		// absolute path; bare `flotilla` does not resolve). Both fall back to a sensible default if their
		// resolution errors (os.Executable only fails in exotic cases; an Abs failure means the flag was
		// already absolute).
		synthRosterPath := *rosterPath
		if abs, err := filepath.Abs(*rosterPath); err == nil {
			synthRosterPath = abs
		}
		synthBin := "flotilla" // fallback: bare command (PATH-dependent); the os.Executable path is the robust form
		if exe, err := os.Executable(); err == nil {
			synthBin = exe
		}
		wakeAgent := func(agent string, kind watch.WakeKind, reasons []string) {
			if kind != watch.WakeSynthesis {
				// The only agent-targeted kind today is synthesis; any other is a programming error.
				log.Printf("flotilla watch: ignoring unexpected agent-targeted wake kind %v for %q", kind, agent)
				return
			}
			body := synthesisWakeBody(agent, synthBin, synthRosterPath, synthesisReadSet(cfg, agent), cfg.OwnedChannels(agent), ackInstr)
			injector.Enqueue(watch.Job{Agent: agent, Message: body, Kind: "detector"})
		}

		// Visibility-synthesis (B2) seams — wired ONLY when the roster opts in (default OFF ⇒ all nil
		// ⇒ the detector's inert default; no synthesis wake ever fires, behavior byte-identical). The
		// digest sub-cadence derives from heartbeat_interval (Q-B): a small multiple bounds the wake
		// rate while the skill bounds the content. The materiality read binds to the SHARED
		// ResultReader seam (synthTurnFinal), the SAME path Tier 1 uses (NOT a claudestore bind).
		var synthWakeAgent func(string, watch.WakeKind, []string)
		var synthParents func(string) []string
		var synthRead func(string) (string, bool)
		synthEveryTicks := 0
		synthSidecarPath := filepath.Join(rosterDir, "flotilla-synthesis-state.json")
		if cfg.VisibilitySynthesis {
			synthWakeAgent = wakeAgent
			synthParents = synthParentsResolver(cfg)
			synthRead = synthReadOneFromTurnFinal(synthTurnFinal(cfg))
			synthEveryTicks = synthDigestTicks // a small multiple of the interval (Q-B)
		}

		det := watch.NewDetectorWithSynthSidecar(watch.DetectorConfig{
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
				// Resolve the XO pane FIRST, then take the per-pane TRANSACTION lock keyed by that
				// target (the same key every other transaction writer uses), so the /clear rotate
				// never interleaves between any confirmed delivery's submit and its Enter-only retry
				// — across processes (a dash control action, a `flotilla send`), not just the
				// in-process Injector. The detector invokes Rotate from runTail, OUTSIDE detector.mu,
				// so this bounded acquire cannot stall the tick loop (see Detector.Tick). A txn-lock
				// timeout surfaces as a rotate error (logged, non-fatal — the continuation still
				// proceeds, just in un-rotated context this tick).
				pane, err := deliver.ResolvePane(agentTitle(cfg, xo))
				if err != nil {
					return err
				}
				txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
				if err != nil {
					return err
				}
				defer txn.Release()
				return surface.RotateContext(xoDrv, pane)
			},
			MirrorOnFinish:      deskMirrorOnFinish(cfg, secrets),
			MirrorDispatch:      func(run func()) { go run() }, // mirror I/O off the tick goroutine
			Awaiting:            awaiting.Present,
			SettleConsume:       settled.Consume,
			Alert:               alert,
			MaxMissedAcks:       *maxMissed,
			MaxQuietIntervals:   *maxQuiet,
			LivenessPingMode:    cfg.LivenessPingMode,
			MaxSelfContinuation: *maxSelfCont,
			BacklogGate:         backlogGate,
			BacklogStuckCap:     *backlogStuckCap,
			WakeAgent:           synthWakeAgent,
			SynthParents:        synthParents,
			SynthRead:           synthRead,
			SynthEveryTicks:     synthEveryTicks,
		}, *snapshotPath, synthSidecarPath)
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
		logMirrorCoverage(cfg, secrets, xo)
		logReplyLegCoverage(cfg, secrets) // #175: c2 hotline return-leg webhook coverage
		if cfg.VisibilitySynthesis {
			fmt.Printf("flotilla watch: visibility-synthesis ON — every %d ticks an OWED agent rolls up its tier below; sidecar=%s\n",
				synthDigestTicks, synthSidecarPath)
		}
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

	// Inbound relay (optional): needs at least one channel binding + a bot token
	// (and the bot's privileged Message Content intent) AND an operator_user_id —
	// without the latter, relay.Accept drops every message, so enabling the gateway
	// would claim "relay active" while silently dropping all traffic.
	//
	// The relay listens on the channel SET the roster's bindings declare: the legacy
	// channel_id is the one-binding degenerate case; an explicit channels[] is the
	// federated set (one multi-channel relay routes each message by its origin
	// channel). A daemon whose roster declares NEITHER runs clock-only (no gateway) —
	// which is exactly how a per-XO clock daemon avoids relaying a channel the central
	// multi-channel relay owns (design §7: exactly one relay per channel; the clock is
	// per-XO but the relay is not).
	//
	// The relay open is NON-FATAL: a gateway construct/open failure (the cold-boot
	// DNS blip ~6s post-reboot, a transient network hiccup) must NOT take down the
	// already-running safety-critical clock — that crash-looped flotilla-watch to a
	// permanent `failed` after the 2026-06-10 power-failure reboot, killing the very
	// down-alert relay before it could alert. So we degrade to clock-only and retry
	// the open in the background until it succeeds or shutdown. The warning goes to
	// stderr (journald) ONLY, never the Discord webhook — that needs the same
	// network that just failed.
	bindings := cfg.Bindings()
	channelIDs := make([]string, 0, len(bindings))
	for _, b := range bindings {
		channelIDs = append(channelIDs, b.ChannelID)
	}
	switch {
	case len(channelIDs) > 0 && botToken != "" && cfg.OperatorUserID != "":
		rel := watch.NewRelay(cfg, injector, onAccepted, func(msg string) { post("flotilla-watch", msg) })
		// The REST-based at-least-once ingestion backstop (#161): a gateway gap (a
		// reconnect/resume-failure window, a daemon-restart) drops MESSAGE_CREATE events
		// the live relay never sees. NewCatchup builds the shared dedup gate, wires it into
		// rel (the live path dedups against the poller), and the poller reconciles each
		// channel against a durable cursor over REST — independent of the websocket that
		// just failed. NON-FATAL: a REST construct failure degrades to live-only (the
		// clock and the live relay are unaffected). Recovered notice → channel; bulk/stale
		// + backstop-down → the loud alert. The gateway's OnReconnect kicks an immediate
		// sweep so a reconnect gap is recovered in ~0s, not the poll interval.
		var catchupKick func()
		if rest, err := discord.NewREST(botToken); err != nil {
			fmt.Fprintf(os.Stderr, "flotilla watch: WARNING — relay catch-up backstop failed to start (%v); running LIVE-RELAY-ONLY. A gateway-gap message may be lost without recovery. The clock and live relay are unaffected.\n", err)
		} else {
			cu := watch.NewCatchup(cfg, rel, rest, *cursorPath,
				func(msg string) { post("flotilla-watch", msg) },
				alert)
			catchupKick = cu.Kick
			go cu.Run(ctx)
			fmt.Printf("flotilla watch: relay catch-up backstop active (cursor=%s)\n", *cursorPath)
		}
		factory := func() (gatewayController, error) {
			gw, err := discord.NewGateway(botToken, channelIDs, rel.Handle, catchupKick)
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
	case len(channelIDs) > 0 && botToken != "" && cfg.OperatorUserID == "":
		return fmt.Errorf("relay requires operator_user_id in the roster (channel binding + bot token are set) — set it, or remove the channel binding for clock-only")
	default:
		fmt.Println("flotilla watch: clock-only (relay disabled — set channel_id/channels[] + bot token + operator_user_id to enable)")
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

// backlogStatusGate builds the goal-driven loop's BacklogGate. It reads the backlog (via read),
// parses it (total/fail-safe), and returns the Status the detector's continueXO gates on. It raises
// a LOUD alert ONCE on the EDGE into a present-but-unparseable state — a non-empty file with no
// "## Backlog" section, OR items lacking a [status] marker — so a format slip is never a silent
// no-op; it re-arms after a clean read. Absent/unreadable fails SILENT (zero Status, no gate, no
// alert): the file may not exist yet, and a torn mid-write read self-heals on the next tick. read
// + alert are injected so the alert-once latch is unit-testable; the gate is called only from the
// detector goroutine (under its mutex), so the latch is single-goroutine.
func backlogStatusGate(path string, read func() ([]byte, error), alert func(string)) func() backlog.Status {
	flagged := false
	return func() backlog.Status {
		raw, err := read()
		if err != nil {
			flagged = false // absent/unreadable ⇒ silent no-gate (file may not exist yet; torn read self-heals)
			return backlog.Status{}
		}
		content := string(raw)
		st := backlog.Parse(content)
		// "unparseable" deliberately uses Malformed>0 (NOT the design's broader "Found && Items==0"):
		// a drained backlog (Found, 0 items) is the normal settle-eligible steady state and must NOT
		// false-alarm. A markerless item is the real format slip, and it is flagged here.
		unparseable := (!st.Found && strings.TrimSpace(content) != "") || st.Malformed > 0
		switch {
		case unparseable && !flagged:
			alert(fmt.Sprintf("goal-loop: backlog %s present but unparseable (found=%v, %d markerless item(s)) — fix the [status] format; the loop keeps driving meanwhile", path, st.Found, st.Malformed))
			flagged = true
		case !unparseable:
			flagged = false
		}
		return st
	}
}

// backlogWakeBody composes the goal-driven loop's WakeBacklog prompt: it NAMES the driven item(s)
// and MUST append ackInstr — a continuously-driven XO that is never told to ack would falsely trip
// the AckAge wedge alert (the liveness backstop). Pure so the name + ack invariant is testable.
func backlogWakeBody(items []string, backlogPath, ackInstr string) string {
	return "[flotilla goal-driven loop] Advance the top unblocked backlog item:\n" + strings.Join(items, "\n") +
		"\nDispatch it to the right desk/harness if not started; check in / unblock if in flight; if it is " +
		"genuinely operator-blocked, drive PREP and move to the next. Reply idle ONLY if every remaining " +
		"backlog item is done or operator-blocked — the loop will NOT settle while unblocked work remains. " +
		"Read the backlog file (" + backlogPath + "), not this conversation." + ackInstr
}

// deskMirrorOnFinish builds the detector's MirrorOnFinish side-effect: when a non-XO desk finishes a
// turn, mirror its turn-final output to its home Discord channel. It returns nil — the detector's
// inert default — when no secrets file is configured (no per-desk webhooks to post to), so a
// deployment without --secrets keeps today's behavior byte-identically.
//
// The closure resolves each desk's surface driver and reads the turn-final through the SHARED
// surface.ResultReader seam (the same path `flotilla result` uses), so the CLI and the auto-mirror
// never diverge. A surface without a ResultReader (none today besides claude/grok) is a clean SKIP.
// Everything is OBSERVE-ONLY + BEST-EFFORT inside deskMirror.run — a failure to resolve, read, chunk,
// or post is logged on one line and dropped, NEVER propagated to the tick or delivery.
// logMirrorCoverage emits a ONE-TIME startup line naming which non-XO desks WILL mirror (a webhook
// resolves) and which will NOT (no webhook ⇒ a silent per-desk SKIP at runtime). Without it, a
// newcomer who set --secrets only for the alert webhook sees empty desk channels with no clue why —
// the visibility door looks broken when it is merely unprovisioned. With secrets nil the mirror is
// inert and this prints nothing.
func logMirrorCoverage(cfg *roster.Config, secrets *roster.Secrets, xo string) {
	if secrets == nil {
		return
	}
	var withMirror, without []string
	for _, a := range cfg.Agents {
		if a.Name == xo {
			continue // the XO has its own mirror path, not the per-desk one
		}
		if url, err := secrets.Webhook(a.Name); err == nil && url != "" {
			withMirror = append(withMirror, a.Name)
		} else {
			without = append(without, a.Name)
		}
	}
	fmt.Printf("flotilla watch: desk mirror — %d will mirror %v; %d have no webhook (will not mirror) %v\n",
		len(withMirror), withMirror, len(without), without)
}

func deskMirrorOnFinish(cfg *roster.Config, secrets *roster.Secrets) func(agent string) {
	if secrets == nil {
		return nil
	}
	return func(agent string) {
		m := deskMirror{
			webhook: func(a string) (string, bool) {
				url, err := secrets.Webhook(a)
				if err != nil || url == "" {
					return "", false
				}
				return url, true
			},
			turnFinal: func(a string) (string, bool, error) {
				drv, ok := surface.Get(agentSurface(cfg, a))
				if !ok {
					return "", false, fmt.Errorf("unknown surface for agent %q", a)
				}
				rr, ok := drv.(surface.ResultReader)
				if !ok {
					// No session-store reader for this surface — nothing substantive to mirror (clean skip).
					return "", false, nil
				}
				pane, err := deliver.ResolvePane(agentTitle(cfg, a))
				if err != nil {
					return "", false, err
				}
				// LatestResult collapses "no substantive completed turn yet" and a genuine read failure
				// into one error (its CLI contract). For the mirror BOTH are non-fatal — a SKIP, never a
				// MIRROR-FAIL — so we return ok=false. The error is carried through ONLY so the decision
				// log names the reason; deskMirror.run logs it as a SKIP, not a failure, and drops it.
				text, err := rr.LatestResult(pane)
				if err != nil {
					return "", false, err
				}
				return text, true, nil
			},
			post: discord.Post,
			logf: log.Printf,
		}
		m.run(agent)
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

// mirrorRelayToLedger appends a confirmed operator→XO relay delivery to the CoS
// who-knows-what ledger (#108), tagged with the Job's origin channel (#105 seam), so
// the chief of staff can see which side-conversation (and which XO) was told what.
// Scoped to XO targets via cfg.IsXO — an operator message addressed to a DESK (@name)
// is not operator↔XO traffic in v1, symmetric with the notify path's IsXO gate; the
// broader scope (XO↔desk, operator↔desk) is design §6.3 Phase 2. cfg.CosLedger == ""
// (cos_agent unset) ⇒ inert. BEST-EFFORT + observe-only: the confirmed delivery already
// happened, so a ledger failure NEVER affects it — it is reported to stderr and ignored.
func mirrorRelayToLedger(cfg *roster.Config, j watch.Job) {
	if cfg.CosLedger == "" || !cfg.IsXO(j.Agent) {
		return
	}
	if err := cos.Append(cfg.CosLedger, cos.Entry{
		Time:    time.Now(),
		Channel: j.OriginChannel,
		From:    "operator",
		To:      j.Agent,
		Gist:    j.Message,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "flotilla watch: cos ledger append failed: %v\n", err)
	}
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
