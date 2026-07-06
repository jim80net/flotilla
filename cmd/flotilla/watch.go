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
	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/decisionbrief"
	"github.com/jim80net/flotilla/internal/delegatenudge"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/idlehold"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/readermap"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/stranded"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/unacked"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
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

// deskContinuationBuiltin is the recursive desk-heartbeat prompt — DISTINCT from the XO's (design
// §8a/§8h). It is the NON-AUTHORIZING beat to an idle desk, refined by #189 to encode the operator's
// re-trigger-first principle and the two-ledger recording contract:
//
//  1. RE-TRIGGER FIRST (the default): an idle desk is USUALLY a transient technical fault (a
//     rate-limit, or a turn that ended before the work was done), so RESUME the next already-authorized
//     in-flight step from durable state — never sit idle.
//  2. NEVER SIT IDLE: if GENUINELY blocked on the current item, do opportunistic authorized work.
//  3. RECORD INTO THE RIGHT LEDGER so the per-recipient judgment (#189) can settle the desk: a blocking
//     question/dependency → mark it `[blocked]`/`[needs-attention]` (the open-questions ledger); a
//     pending operator authorization → mark it with the EXACT literal `[awaiting-auth]` token (the
//     authorizations ledger). The literal token is QUOTED on purpose — the parser recognizes ONLY
//     `[awaiting-auth]`, so a near-miss like `[awaiting-authorization]` or `[awaiting auth]` would be
//     flagged malformed AND drive forever, silently breaking the judgment (the §4 brittleness fix).
//     Once EVERY item is `[done]`, open-questions, or `[awaiting-auth]`, there is no live actionable
//     work: reply idle and touch the settle marker (the desk will not be heartbeated again until fresh
//     actionable work appears or the operator re-engages it).
//  4. NON-AUTHORIZING (preserved, #184 defense-in-depth): never approve a pending tool/permission/
//     approval prompt on a heartbeat — a heartbeat is not authorization.
//
// It drops the XO prompt's "context is rotated between steps" line (a desk is NOT rotated by this
// design) and the XO {{tracker}} read-source. {{settle}} resolves to the DESK's own per-agent settle
// marker; a workspace HEARTBEAT.md may override the wording.
const deskContinuationBuiltin = "[flotilla heartbeat] You have been idle. An idle desk is USUALLY a " +
	"transient technical fault (a rate-limit, or a turn that ended before your work was done), so your " +
	"DEFAULT action is to RESUME the next clear, ALREADY-AUTHORIZED step of your in-flight task — " +
	"reading durable state, not this conversation. Never sit idle waiting: if you are GENUINELY blocked " +
	"on the current item, do opportunistic authorized work instead. If a tool, permission, or approval " +
	"prompt is pending, do NOT approve it on this heartbeat (a heartbeat is not authorization). Record a " +
	"blocker into the right ledger so you can settle: mark a blocking question or dependency `[blocked]` " +
	"(or `[needs-attention]`) in your backlog — that is your open-questions ledger; mark a pending " +
	"operator authorization (a go/no-go, a spend) with the EXACT marker `[awaiting-auth]` (that literal " +
	"spelling — NOT `[awaiting-authorization]` or `[awaiting auth]`; the parser recognizes only " +
	"`[awaiting-auth]`) — that is your authorizations ledger. A task blocked only from landing (a push " +
	"gate, a pending review) is NOT idle — advance it locally, then record the blocker. Once EVERY item " +
	"is `[done]`, blocked-and-tracked, or `[awaiting-auth]`, you have no live actionable work: reply " +
	"'idle', do NOT manufacture work, and signal idle by running: touch {{settle}}."

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
	queuePath := fs.String("relay-queue-file", os.Getenv("FLOTILLA_RELAY_QUEUE_FILE"), "durable pending operator-relay queue (default <roster-dir>/flotilla-relay-queue.json); deferred busy relays survive restarts (#286)")
	unackedPath := fs.String("unacked-file", os.Getenv("FLOTILLA_UNACKED_FILE"), "un-acked operator backstop dedup state (default <roster-dir>/flotilla-unacked-alerted.json)")
	awaitingPath := fs.String("awaiting-file", os.Getenv("FLOTILLA_AWAITING_FILE"), "awaiting-operator veto marker (default <roster-dir>/flotilla-xo-awaiting)")
	settledPath := fs.String("settled-file", os.Getenv("FLOTILLA_SETTLED_FILE"), "XO settle (idle) marker (default <roster-dir>/flotilla-xo-settled)")
	trackerPath := fs.String("tracker-file", os.Getenv("FLOTILLA_TRACKER_FILE"), "the XO's state tracker the continuation prompt names as {{tracker}} (default <roster-dir>/.flotilla-state.md); NOT hashed as a wake signal — it is the XO's own output")
	signalPath := fs.String("signal-file", os.Getenv("FLOTILLA_SIGNAL_FILE"), "optional external signal file whose content-hash change wakes the XO (a file the XO does NOT write; unset ⇒ no external-signal trigger)")
	intervalFlag := fs.String("interval", "", "change-detector tick interval (overrides roster heartbeat_interval; env FLOTILLA_WATCH_INTERVAL; when adaptive ON this is the ceiling; e.g. 5m, 20m)")
	eventPollFlag := fs.String("event-poll-interval", "", "fast desk-state poll for turn-end pokes (env FLOTILLA_EVENT_POLL_INTERVAL; default 5s; 0 disables)")
	adaptiveFlag := fs.String("adaptive-interval", "", "adaptive detector tick policy (env FLOTILLA_ADAPTIVE_INTERVAL; default on; 0/false disables)")
	intervalFloorFlag := fs.String("interval-floor", "", "adaptive Active-tier floor (env FLOTILLA_INTERVAL_FLOOR; default 2m)")
	intervalWarmFlag := fs.String("interval-warm", "", "adaptive Warm tier (env FLOTILLA_INTERVAL_WARM; default 8m)")
	intervalIdleStableFlag := fs.String("interval-idle-stable", "", "adaptive hysteresis before ceiling (env FLOTILLA_INTERVAL_IDLE_STABLE; default 10m)")
	intervalReleaseStepFlag := fs.String("interval-release-step", "", "adaptive release decay cadence (env FLOTILLA_INTERVAL_RELEASE_STEP; default 5m)")
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
	intervalStr := strings.TrimSpace(*intervalFlag)
	if intervalStr == "" {
		intervalStr = strings.TrimSpace(os.Getenv("FLOTILLA_WATCH_INTERVAL"))
	}
	if intervalStr != "" {
		d, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("watch --interval %q: %w", intervalStr, err)
		}
		if d <= 0 {
			return fmt.Errorf("watch --interval %q: must be positive", intervalStr)
		}
		interval = d
	}
	eventPollInterval := watch.DefaultEventPollInterval
	eventPollStr := strings.TrimSpace(*eventPollFlag)
	if eventPollStr == "" {
		eventPollStr = strings.TrimSpace(os.Getenv("FLOTILLA_EVENT_POLL_INTERVAL"))
	}
	if eventPollStr != "" {
		d, err := time.ParseDuration(eventPollStr)
		if err != nil {
			return fmt.Errorf("watch --event-poll-interval %q: %w", eventPollStr, err)
		}
		eventPollInterval = d
	}
	rosterDir := filepath.Dir(*rosterPath)
	// Per-coordinator clock sidecars (#439 phase 1b): canonical flotilla-<xo>-{alive,settled,awaiting}
	// with legacy flotilla-xo-* fallback when that file already exists on disk.
	*ackPath = roster.ResolveLayerClockPath(rosterDir, xo, *ackPath, "flotilla-xo-alive", "alive")
	*awaitingPath = roster.ResolveLayerClockPath(rosterDir, xo, *awaitingPath, "flotilla-xo-awaiting", "awaiting")
	*settledPath = roster.ResolveLayerClockPath(rosterDir, xo, *settledPath, "flotilla-xo-settled", "settled")
	defaultPath(snapshotPath, rosterDir, "flotilla-detector-state.json")
	defaultPath(cursorPath, rosterDir, "flotilla-relay-cursor.json")
	defaultPath(queuePath, rosterDir, "flotilla-relay-queue.json")
	defaultPath(unackedPath, rosterDir, "flotilla-unacked-alerted.json")
	defaultPath(trackerPath, rosterDir, ".flotilla-state.md")

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

	// Construct the coordination transport (discord) once for this daemon run — the
	// stateful-transport CONSTRUCTION step (the SPI separates this from init-time
	// REGISTRATION because the bot token / roster / cursor path are not available at
	// init). The OUTBOUND half (Post, used by the down-alert + desk-mirror paths) needs
	// only the roster + secrets, so it is built whenever secrets are present, even with
	// no bot token (clock-only-with-alert-webhook). The INBOUND gateway + REST catch-up
	// are wired later, in the relay block, only when a bot token is set. A construction
	// failure (e.g. the catch-up REST session) is NON-FATAL: the daemon degrades to a
	// transport-less post path (stderr) and the safety-critical clock keeps running.
	var tr transport.Transport
	if secrets != nil {
		t, err := transport.Construct("", transport.Config{
			BotToken:   botToken,
			CursorPath: *cursorPath,
			Roster:     cfg,
			Secrets:    secrets,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "flotilla watch: WARNING — coordination transport construct failed (%v); down-alerts go to stderr only, relay disabled. The safety-critical clock is unaffected.\n", err)
		} else {
			tr = t
			defer func() { _ = tr.Close() }()
		}
	}
	// alertDest is the fixed down-alert webhook target (the XO's webhook), wrapped as a
	// transport Destination so the post path is medium-agnostic. nil when there is no
	// alert webhook OR no transport — post then degrades to stderr.
	var alertDest transport.Destination
	if tr != nil && alertHook != "" {
		alertDest = transport.NewWebhookDestination(alertHook)
	}
	post := func(username, msg string) {
		if tr != nil && alertDest != nil {
			_ = tr.Post(alertDest, username, msg)
		} else {
			fmt.Fprintln(os.Stderr, "flotilla watch: "+msg)
		}
	}
	alert := func(msg string) { post("flotilla-watch", "⚠️ "+msg) }

	// Load the partition firewall (Pillar D) ONCE — the runtime backstop that keeps a
	// deployment specific from leaking into a desk's published turn-final. A broken
	// term list (an uncompilable denylist regex) is FATAL, not silently skipped: a
	// silent partition hole is the exact failure this guard exists to prevent (the same
	// fail-fast posture as a configured-but-broken secrets file above).
	firewall, err := LoadFirewall()
	if err != nil {
		return err
	}
	// Make the firewall's configuration VISIBLE at boot — a silently-unconfigured
	// deployment denylist (e.g. the daemon's cwd has no .flotilla list and no env is
	// set) would otherwise look like the runtime guard is protecting when only the
	// built-in generic + canonical patterns are on.
	if dCfg, wCfg := firewall.Configured(); dCfg || wCfg {
		fmt.Printf("flotilla watch: partition firewall — deployment denylist=%v, warnlist=%v (generic + canonical patterns always on)\n", dCfg, wCfg)
	} else {
		fmt.Println("flotilla watch: partition firewall — NO deployment denylist/warnlist configured (only built-in generic + canonical patterns; set .flotilla/private-denylist or $FLOTILLA_PRIVATE_DENYLIST)")
	}

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
	injector.SetRelayQueue(*queuePath)
	// Mirror relayed instructions to the audit channel in full. Heartbeat ticks
	// are NOT mirrored: they fire every interval and a per-tick marker is pure
	// noise in the operator's Discord channel (XO liveness is already covered by
	// the ack file + the missed-ack down alert below). Posted via webhook, which
	// the gateway's feedback filter drops — no loop.
	// #175/#177: the c2-hotline reply-watcher. When an operator message lands on a channel's XO (ANY
	// channel's XO including the primary — #177 unified them), watch that XO's session store for the
	// reply and route it back to the channel — the flotilla-native return leg (the primary XO's old
	// host-local Stop-hook is retired). nil when secrets are absent (no webhooks to resolve).
	replyRtr := newHotlineReplyRouter(context.Background(), cfg, secrets, tr, firewall, alert)
	if replyRtr != nil {
		defer replyRtr.Stop() // cancel in-flight hotline watchers on shutdown (runs after <-ctx.Done())
	}
	// Log return-leg webhook coverage HERE (not in the change-detector branch): the router arms in
	// BOTH the change-detector and the legacy clock modes, so a legacy-mode operator must also see a
	// mis-provisioned federated XO at startup. No-op when secrets are absent.
	logReplyLegCoverage(cfg, secrets)
	injector.SetMirror(func(j watch.Job) {
		// Heartbeat ticks and change-detector wakes fire automatically; a per-wake
		// marker is pure noise in the operator's channel (XO liveness is covered by
		// the ack file + the down alert). Only relayed operator traffic is mirrored.
		if j.Kind == watch.KindHeartbeat || j.Kind == watch.KindDetector {
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
	watch.ReplayRelayQueue(injector, *queuePath)
	defer injector.Stop()

	// Daemon-native wall-clock scheduler (#413): durable last-fired sidecar beside the
	// roster; poll loop + optional detector hook share one Scheduler (mutex-safe).
	var sched *watch.Scheduler
	scheduleSidecarPath := filepath.Join(rosterDir, "flotilla-schedule-state.json")
	if len(cfg.Schedules) > 0 {
		sched = watch.NewScheduler(cfg.Schedules, scheduleSidecarPath, rosterDir, injector.Enqueue)
		fmt.Printf("flotilla watch: wall-clock scheduler active (%d schedule(s), sidecar=%s)\n",
			len(cfg.Schedules), scheduleSidecarPath)
	}

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

		primaryAdjutant := cfg.AdjutantFor(xo)
		leaderAckPath := *ackPath // resolved per-coordinator path (legacy fallback when present)
		layerBufferPath := roster.LayerBufferPath(rosterDir, xo)

		drainAdjutantSeam := func() {
			if primaryAdjutant == "" {
				return
			}
			brief, ok, clearAfter := adjutantSeamBrief(layerBufferPath, xo, rosterDir)
			if !ok {
				return
			}
			injector.Enqueue(watch.Job{Agent: xo, Message: brief, Kind: watch.KindDetector})
			if clearAfter {
				if err := adjutantbuffer.Clear(layerBufferPath); err != nil {
					log.Printf("flotilla watch: adjutant buffer clear after enqueue failed: %v", err)
				}
			}
		}

		wake := func(kind watch.WakeKind, reasons []string) {
			var body string
			target := xo
			switch kind {
			case watch.WakeContinuation, watch.WakeBacklog:
				// Judgment/continuation stays on the leader — adjutant observes, does not replace.
				switch kind {
				case watch.WakeContinuation:
					body = continuationPrompt
				default:
					body = backlogWakeBody(reasons, *backlogPath, ackInstr)
				}
			case watch.WakePing:
				if primaryAdjutant != "" {
					target = primaryAdjutant
					charterPath := roster.LayerCharterPath(rosterDir, xo)
					if layerCharterMissing(charterPath) {
						// Evaluation ticks require an established charter (#439 spec).
						body = adjutantCharterPairingBody(xo, primaryAdjutant, charterPath, leaderAckPath)
					} else {
						body = adjutantEvaluationTickBody(xo, leaderAckPath, layerBufferPath)
					}
				} else {
					body = leaderPingBody(leaderAckPath)
				}
			default: // WakeMaterial
				if primaryAdjutant != "" && !cfg.UrgentMaterial(reasons) {
					if err := adjutantbuffer.Append(layerBufferPath, xo, reasons); err != nil {
						log.Printf("flotilla watch: adjutant buffer append failed, falling back to leader wake: %v", err)
						body = leaderMaterialBody(reasons, *settledPath, ackInstr)
					} else {
						target = primaryAdjutant
						body = adjutantBufferedNoteBody(xo, len(reasons))
					}
				} else {
					body = leaderMaterialBody(reasons, *settledPath, ackInstr)
				}
			}
			injector.Enqueue(watch.Job{Agent: target, Message: body, Kind: watch.KindDetector})
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
			ack := ""
			if agent == xo {
				ack = ackInstr // liveness ack follows the TARGET, not the wake kind (#190)
			}
			body := synthesisWakeBody(agent, synthBin, synthRosterPath, synthesisReadSet(cfg, agent), cfg.OwnedChannels(agent), ack)
			injector.Enqueue(watch.Job{Agent: agent, Message: body, Kind: watch.KindDetector})
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

		// Recursive desk-heartbeat (#183) seams — DEFAULT-ON, roster opt-OUT (§5.2). HeartbeatEnabled
		// is ALWAYS wired (the directive is universal); roster.Config.HeartbeatEnabled resolves the
		// opt-OUT: the primary XO (its own clock), approval-sensitive desks (default-off, #184), and
		// any agent explicitly `heartbeat: false` (incl. a federated sub-XO that runs its OWN daemon
		// — the §8i double-drive opt-out is the roster flag, since this daemon cannot introspect
		// another). The per-agent settle markers live alongside the XO marker in the roster dir
		// (<dir>/flotilla-<agent>-settled). The cadence is the heartbeat interval — the detector's
		// tick IS the interval, so DeskHeartbeatEveryTicks=1 (an idle desk is re-engaged within one
		// interval). WakeDeskHeartbeat enqueues the non-authorizing desk-continuation beat (audit-
		// suppressed); DeskEscalate raises the loud cap-alert to the desk's owning XO.
		deskSettled := watch.NewSettledMarkerSet(rosterDir)
		deskHeartbeatEnabled := func(agent string) bool { return cfg.HeartbeatEnabled(agent) }
		wakeDeskHeartbeat := newDeskHeartbeatDispatch(injector.Enqueue, deskSettled.Path)
		deskEscalate := newDeskEscalate(cfg, xo, alert)
		// #189 per-recipient heartbeat JUDGMENT — the warrant seam, ALWAYS wired (the judgment is
		// universal, like the #183 default-ON). The backlog read is performed HERE, OFF the detector
		// lock: deskWarrantedGate reads each agent's OWN backlog (<rosterDir>/flotilla-<agent>-backlog.md)
		// fresh each call and returns cfg.HeartbeatWarranted(agent, st). A desk with NO per-recipient
		// ledger self-defaults to WARRANTED (the missing-ledger fallback — NOT the shared backlog), so a
		// deployment that keeps no per-recipient backlogs is #183-equivalent. The shared --backlog-file
		// is deliberately NOT consulted here (it is the XO's drive queue, not a desk's work).
		deskHeartbeatWarranted := deskWarrantedGate(cfg,
			func(agent string) ([]byte, bool, error) {
				p := filepath.Join(rosterDir, "flotilla-"+agent+"-backlog.md")
				raw, err := os.ReadFile(p)
				if err != nil {
					if os.IsNotExist(err) {
						return nil, false, nil // absent ⇒ missing-ledger fallback (warranted)
					}
					return nil, true, err // present-but-unreadable/torn ⇒ fail-safe (warranted)
				}
				return raw, true, nil
			},
			alert)

		// #216 idle-hold antipattern: per-agent consecutive-strike tracker; the break
		// prompt fires after StrikeThreshold idle-hold turn-finals.
		idleHoldTracker := idlehold.NewTracker()

		// #216 stranded-handoff extension: gate work settled without gate-holder report.
		strandedTracker := stranded.NewTracker()

		// #232 coordinator delegation: every XO and CoS — not only the primary clock XO.
		delegationTracker := delegatenudge.NewTracker()

		// #349 item D: auto decision-brief trigger when operator-gated goals lack a brief.
		decisionBriefTracker := decisionbrief.NewTracker()
		goalsJSONPath := filepath.Join(rosterDir, "fleet-goals.json")
		if gp := strings.TrimSpace(os.Getenv("FLOTILLA_GOALS_FILE")); gp != "" {
			goalsJSONPath = gp
		}
		var deskStateLabels func() map[string]string

		// #205 auto-switch: load flat launch recipes for storm-cooldown + slot metadata.
		launchPath := os.Getenv("FLOTILLA_LAUNCH")
		if launchPath == "" {
			launchPath = launch.DefaultPath(*rosterPath)
		}
		var flatLaunch *launch.Config
		if _, statErr := os.Stat(launchPath); statErr == nil {
			rosterAgents := make(map[string]bool, len(cfg.Agents))
			for _, a := range cfg.Agents {
				rosterAgents[a.Name] = true
			}
			if loaded, lerr := launch.Load(launchPath, rosterAgents); lerr == nil {
				flatLaunch = loaded
			}
		}
		var endAutoSwitch func(string)
		autoSwitchOn := surface.AutoSwitchEnabled()
		if autoSwitchOn {
			log.Printf("flotilla watch: auto-switch ON (default; disable with FLOTILLA_AUTOSWITCH=0) — non-sensitive workers may auto-relocate on sustained throttle, bounded by the safety guardrails")
		} else {
			log.Printf("flotilla watch: auto-switch DISABLED (FLOTILLA_AUTOSWITCH=0)")
		}

		referenceInterval := cfg.HeartbeatDur()
		if referenceInterval <= 0 {
			referenceInterval = interval
		}
		ceiling := interval
		adaptiveOn := adaptiveIntervalEnabled(*adaptiveFlag)
		var adaptivePolicy watch.AdaptiveInterval
		if adaptiveOn {
			acfg := watch.DefaultAdaptiveConfig(ceiling)
			if d, ok := optionalDuration(*intervalFloorFlag, "FLOTILLA_INTERVAL_FLOOR"); ok {
				acfg.Floor = d
			}
			if d, ok := optionalDuration(*intervalWarmFlag, "FLOTILLA_INTERVAL_WARM"); ok {
				acfg.Warm = d
			}
			if d, ok := optionalDuration(*intervalIdleStableFlag, "FLOTILLA_INTERVAL_IDLE_STABLE"); ok {
				acfg.IdleStableFor = d
			}
			if d, ok := optionalDuration(*intervalReleaseStepFlag, "FLOTILLA_INTERVAL_RELEASE_STEP"); ok {
				acfg.ReleaseStepEvery = d
			}
			adaptivePolicy = watch.NewAdaptiveInterval(acfg)
			interval = adaptivePolicy.Current()
			log.Printf("flotilla watch: adaptive-interval ON — cold-start at ceiling=%s floor=%s warm=%s (activity fail-safe Active until first tick ingests fleet state; restart has no durable tier)", acfg.Ceiling, acfg.Floor, acfg.Warm)
		} else {
			log.Printf("flotilla watch: adaptive-interval OFF — fixed tick %s", interval)
		}
		activity := watch.NewActivityTracker(watch.DefaultActivityConfig())
		detCfg := watch.DetectorConfig{
			XOAgent:           xo,
			Desks:             desks,
			Interval:          interval,
			ReferenceInterval: referenceInterval,
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
			RateLimitMaterial: rateLimitMaterial(cfg),
			RateLimitReset:    rateLimitReset(cfg),
			RateLimitDispatch: func(run func()) { go run() },
			RateLimitAutoSwitchEligible: func(agent string) bool {
				if !cfg.AutoSwitchEligible(agent) {
					return false
				}
				// Claude-storm only: desks already on grok (or another FROM) are not candidates.
				return agentSurface(cfg, agent) == surface.DefaultSurface
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
			MirrorOnFinish:            deskMirrorOnFinish(cfg, secrets, tr, firewall, alert, rosterDir),
			CoordinatorMirrorOnFinish: coordinatorMirrorOnFinish(cfg, firewall, alert, rosterDir),
			AdjutantSeamOnFinish:      drainAdjutantSeam,
			IdleHoldOnFinish:          idleHoldOnFinish(cfg, idleHoldTracker, injector.Enqueue),
			StrandedHandoffOnFinish:   strandedHandoffOnFinish(cfg, strandedTracker, injector.Enqueue),
			IsCoordinator:             cfg.IsCoordinator,
			DelegationNudgeOnFinish:   delegationNudgeOnFinish(cfg, delegationTracker, injector.Enqueue),
			DecisionBriefOnTick: decisionBriefOnTick(
				goalsJSONPath, *backlogPath, decisionBriefTracker, injector.Enqueue, cfg,
				func() map[string]string {
					if deskStateLabels == nil {
						return nil
					}
					return deskStateLabels()
				}),
			MirrorDispatch:      func(run func()) { go run() }, // mirror I/O off the tick goroutine
			Awaiting:            awaiting.Present,
			SettleConsume:       settled.Consume,
			DeskSettleConsume:   deskSettled.Consume,
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
			// Recursive desk-heartbeat (#183): default-ON, roster opt-OUT. Cadence = the heartbeat
			// interval (the tick IS the interval ⇒ 1 tick); cap = 3 (NewDetector defaults 0 to 3).
			HeartbeatEnabled:        deskHeartbeatEnabled,
			HeartbeatWarranted:      deskHeartbeatWarranted,
			WakeDeskHeartbeat:       wakeDeskHeartbeat,
			DeskEscalate:            deskEscalate,
			DeskHeartbeatEveryTicks: 1,
			Activity:                activity,
			AdaptiveInterval:        adaptivePolicy,
		}
		if autoSwitchOn {
			probeMaterial := rateLimitMaterial(cfg)
			detCfg.RateLimitAutoSwitchDispatch = func(run func()) { go run() }
			detCfg.RateLimitAutoSwitch = newRateLimitAutoSwitchDispatch(cfg, *rosterPath, launchPath, flatLaunch, probeMaterial, func(agent string) {
				if endAutoSwitch != nil {
					endAutoSwitch(agent)
				}
			})
		}
		if sched != nil {
			detCfg.ScheduleOnTick = sched.Tick
		}
		det := watch.NewDetectorWithSynthSidecar(detCfg, *snapshotPath, synthSidecarPath)
		deskStateLabels = det.DeskStateLabels
		endAutoSwitch = det.EndAutoSwitchFlight
		turnPoller := watch.NewTurnEndPoller(xo, desks, detCfg.Assess, func() {
			activity.OnTurnEnd("", time.Now())
			det.Poke()
		}, eventPollInterval)
		det.Start()
		enqueueAdjutantCharterPairing(primaryAdjutant, xo, rosterDir, leaderAckPath, injector.Enqueue)
		turnPoller.Start()
		defer func() {
			turnPoller.Stop()
			det.Stop()
		}()
		onAccepted = func(target string) {
			if target == xo {
				det.OperatorWake() // an operator message re-engages a settled XO
				return
			}
			// #183 G3.2 re-arm: an operator/XO message to a DESK re-engages its recursive heartbeat
			// (clears settled+stopped, resets the cadence/cap counters), the per-agent analogue of
			// OperatorWake. Without this a settled/wedged desk stays silent forever (design §8b). The
			// relay routes @desk messages and calls onAccepted(deskName), so every non-XO target
			// re-arms its own desk only.
			det.AgentWake(target)
		}
		mode := cfg.LivenessPingMode
		if mode == "" {
			mode = "none"
		}
		intervalLabel := interval.String()
		if adaptiveOn {
			intervalLabel = "adaptive ceiling=" + ceiling.String() + " current=" + interval.String()
		}
		fmt.Printf("flotilla watch: change-detector running — XO=%s interval=%s event-poll=%s ping-mode=%s ack=%s snapshot=%s\n",
			xo, intervalLabel, eventPollInterval, mode, *ackPath, *snapshotPath)
		logMirrorCoverage(cfg, secrets, xo)
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
	case len(channelIDs) > 0 && botToken != "" && cfg.OperatorUserID != "" && tr != nil:
		rel := watch.NewRelay(cfg, injector, onAccepted, func(msg string) { post("flotilla-watch", msg) })
		// The destinations the bus subscribes to + reconciles: one per bound channel,
		// resolved from the transport (the channel-id set stays inside the transport's
		// Destination rather than leaking as bare strings to the gateway open).
		dests := transportDestinations(tr, channelIDs)
		// The REST-based at-least-once ingestion backstop (#161): a gateway gap (a
		// reconnect/resume-failure window, a daemon-restart) drops MESSAGE_CREATE events
		// the live relay never sees. NewCatchup builds the shared dedup gate, wires it into
		// rel (the live path dedups against the poller), and the poller reconciles each
		// channel against a durable cursor over REST — independent of the websocket that
		// just failed. NON-FATAL: an absent catch-up capability degrades to live-only (the
		// clock and the live relay are unaffected). Recovered notice → channel; bulk/stale
		// + backstop-down → the loud alert. The gateway's OnReconnect kicks an immediate
		// sweep so a reconnect gap is recovered in ~0s, not the poll interval.
		//
		// Catch-up is an OPTIONAL transport capability (type-asserted, mirroring
		// surface.ResultReader): a transport whose live delivery cannot gap (a future
		// loopback web transport) does not implement it, and the backstop is skipped
		// cleanly. The discord transport implements it (the gateway gaps), adapted to the
		// watch poller's string-channel-keyed MessageReader seam.
		var catchupKick func()
		if cap, ok := tr.(transport.CatchUp); ok {
			reader := &transportCatchUpReader{cap: cap, dest: destByChannel(dests, channelIDs)}
			cu := watch.NewCatchup(cfg, rel, reader, *cursorPath,
				func(msg string) { post("flotilla-watch", msg) },
				alert)
			catchupKick = cu.Kick
			go cu.Run(ctx)
			fmt.Printf("flotilla watch: relay catch-up backstop active (cursor=%s)\n", *cursorPath)
		} else {
			fmt.Fprintln(os.Stderr, "flotilla watch: WARNING — the coordination transport has no catch-up capability; running LIVE-RELAY-ONLY. A gateway-gap message may be lost without recovery. The clock and live relay are unaffected.")
		}
		// The relay open is the transport's Subscribe, wrapped as the gatewayController
		// the non-fatal-with-retry relayController consumes (Open = Subscribe, Close =
		// transport teardown). Folding construct+subscribe into one factory attempt lets
		// the single retry loop recover from either a construct or a subscribe failure —
		// neither fatal to the safety-critical clock (the 2026-06-10 crash-loop guard).
		factory := func() (gatewayController, error) {
			ctrl := &transportGateway{tr: tr, ctx: ctx, dests: dests, handler: rel.Handle, onReconnect: catchupKick}
			if err := ctrl.Open(); err != nil {
				return nil, err
			}
			return ctrl, nil
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

	if sched != nil {
		go sched.Run(ctx)
	}

	// Standing un-acked operator backstop (#234): REST history scan independent of
	// gateway health. Alert-once per message id; coordinator wake retries on busy.
	if len(channelIDs) > 0 && botToken != "" && cfg.OperatorUserID != "" && tr != nil {
		if hist, ok := tr.(transport.RecentHistory); ok {
			dests := transportDestinations(tr, channelIDs)
			reader := &transportRecentReader{cap: hist, dest: destByChannel(dests, channelIDs)}
			backstop := watch.NewUnackedBackstop(cfg, reader, *unackedPath, alert, mkSend(confirm.Submit), nil)
			go backstop.Run(ctx)
			fmt.Printf("flotilla watch: un-acked backstop active (state=%s scan=%s min-age=%s)\n",
				*unackedPath, unacked.DefaultScanInterval, unacked.DefaultMinAge)
		} else {
			fmt.Fprintln(os.Stderr, "flotilla watch: WARNING — coordination transport has no recent-history capability; un-acked backstop disabled")
		}
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

// deskWarrantedGate builds the #189 per-recipient HeartbeatWarranted seam (a func(agent) bool) that
// the detector invokes in its PHASE-1 warrant snapshot (deskWarrantSnapshot), OFF the detector lock,
// BEFORE the under-lock decision runs. The backlog read is FILE I/O and lives HERE — off d.mu — so the
// under-lock phase-2 decision consults only the resulting pure boolean (the detector's load-bearing
// off-mutex invariant, the same one synthesis + the mirror honor). For each agent it reads that agent's
// OWN backlog (read is keyed by agent and resolves <rosterDir>/flotilla-<agent>-backlog.md), parses it
// fresh each call (it is the desk's own output — NOT content-hashed), and returns
// cfg.HeartbeatWarranted(agent, st).
//
// The fail-safe direction is toward WARRANTED (keep the desk moving — never the silent-stall #183
// fixed):
//   - ABSENT per-recipient file ⇒ WARRANTED via the missing-ledger fallback (the desk has not opted
//     into the judgment ⇒ driven exactly as #183). It does NOT fall back to the shared fleet backlog
//     (that is the XO's drive queue, not THIS desk's; consulting it would warrant every ledger-less
//     desk on a busy fleet — re-creating the indiscriminate poking this change exists to end).
//   - UNREADABLE/torn present file ⇒ WARRANTED (fail-safe; a torn mid-write read self-heals next tick).
//   - PRESENT-but-sectionless (Found=false) ⇒ WARRANTED via cfg.HeartbeatWarranted's !Found arm, AND a
//     LOUD alert ONCE on the edge into that state (mirroring backlogStatusGate's alert-once latch), so
//     a format slip is loud, never a silent always-beat. The latch re-arms after a clean read.
//
// read + alert are injected so the latch + fallback are unit-testable. The seam is called only from
// the detector's single Tick goroutine (in deskWarrantSnapshot, off d.mu), so the per-agent latch map
// is single-goroutine — no concurrent Tick, and the other detector-state writers (OperatorWake/
// AgentWake) never invoke this seam.
func deskWarrantedGate(cfg *roster.Config, read func(agent string) ([]byte, bool, error), alert func(string)) func(agent string) bool {
	flagged := map[string]bool{}
	return func(agent string) bool {
		raw, exists, err := read(agent)
		if !exists || err != nil {
			// Absent (missing-ledger fallback) OR unreadable/torn (fail-safe): WARRANTED. Neither is a
			// present-but-sectionless format slip, so re-arm the latch and do NOT alert.
			flagged[agent] = false
			return true
		}
		content := string(raw)
		st := backlog.Parse(content)
		// A present, readable file with NO "## Backlog" section (and non-empty content) is the format
		// slip the !Found warrant arm keeps WARRANTED — alert ONCE on the edge so it is loud, not silent.
		sectionless := !st.Found && strings.TrimSpace(content) != ""
		switch {
		case sectionless && !flagged[agent]:
			alert(fmt.Sprintf("desk-heartbeat: %s backlog present but has no '## Backlog' section — fix the format; the judgment keeps the desk warranted meanwhile", agent))
			flagged[agent] = true
		case !sectionless:
			flagged[agent] = false
		}
		return cfg.HeartbeatWarranted(agent, st)
	}
}

// leaderPingBody is the primary-coordinator liveness ping (no adjutant configured).
func leaderPingBody(ackPath string) string {
	return "[flotilla change-detector] Liveness check — reply with a one-line ack only; take no other action." +
		"\n(To ack you are alive, run: touch " + ackPath + ")"
}

// adjutantEvaluationTickBody routes a stale-leader timeout to the adjutant as an
// evaluation tick (#439 operator amendment): ack → evaluate → act-by-tier — not a
// dead-man's ack to the leader.
func adjutantEvaluationTickBody(leader, leaderAckPath, bufferPath string) string {
	return "[flotilla adjutant] Evaluation tick for " + leader +
		" — leader alive file is stale (timeout signal; not a dead-man ack to the leader).\n\n" +
		"Three-step duty (required-minimum charter):\n" +
		"1. ACK — touch the leader alive file (mechanical liveness; mandatory):\n" +
		"   touch " + leaderAckPath + "\n" +
		"2. EVALUATE — sweep " + leader + "'s layer: unhandled edges, PRs at gates, stale lanes, " +
		"unanswered operator items. Distinguish all-quiet (nothing to do) from work-found (quiet but stuck).\n" +
		"3. ACT BY TIER — all-quiet → ack only, no leader interrupt; work-found → buffer judgment items " +
		"in " + bufferPath + " and inject a digest at " + leader + "'s next seam (immediately if urgent-class).\n\n" +
		"This tick catches idle-holding: leader idle but queue not empty is work-found, not all-quiet." +
		adjutantDualObservationContract(leader)
}

// adjutantCharterPairingBody is the first-presentation charter turn for a new adjutant pair (#439 2.5).
func adjutantCharterPairingBody(leader, adjutant, charterPath, leaderAckPath string) string {
	return "[flotilla adjutant] First-presentation charter pairing for " + leader +
		" — negotiate solo-authority bounds with the leader and write the durable charter.\n\n" +
		"Your duty:\n" +
		"1. Propose defaults for what you may do without " + leader + " (mechanical handling, liveness ack, recovery attempts).\n" +
		"2. Coordinate a one-time charter turn with " + leader + " — leader affirms or edits your proposal.\n" +
		"3. Write the agreed charter to:\n   " + charterPath + "\n\n" +
		"Required minimum (non-negotiable): on evaluation ticks you MUST ack liveness by touching:\n   " +
		leaderAckPath + "\n\n" +
		"Evaluation ticks are gated until this charter exists. Solo authority beyond the minimum is negotiated, not invented." +
		adjutantDualObservationContract(leader)
}

// leaderCharterPairingBody asks the coordinator to affirm the adjutant charter (#439 2.5).
func leaderCharterPairingBody(leader, adjutant, charterPath, leaderAckPath string) string {
	return "[flotilla change-detector] First-presentation charter pairing with adjutant " + adjutant +
		" — establish what " + adjutant + " may do without you.\n\n" +
		"Your duty:\n" +
		"1. Review " + adjutant + "'s proposed solo-authority bounds.\n" +
		"2. Affirm or edit — especially mechanical handling, recovery attempts, and escalation thresholds.\n" +
		"3. Ensure the written charter at " + charterPath + " includes liveness ack on evaluation ticks " +
		"(touch " + leaderAckPath + "; mandatory minimum).\n\n" +
		"This is a one-time pairing turn; buffered interrupts resume laminar flow after the charter lands."
}

// adjutantDualObservationContract is the prompt-contract for dual desk+leader observation (#439 2.3).
func adjutantDualObservationContract(leader string) string {
	return "\n\nDual observation (standing duty):\n" +
		"1. Desk stream — subtree desks under " + leader + ": pane Assess state, finish-edges, crash/shell.\n" +
		"2. Leader stream — " + leader + ": Working/Idle, settle/awaiting markers, turn-final tail.\n" +
		"Buffer when leader is Working without await marker; inject consolidated briefs at Idle/settled seams."
}

// leaderMaterialBody is the legacy material-change wake to the coordinator pane.
func leaderMaterialBody(reasons []string, settledPath, ackInstr string) string {
	return "[flotilla change-detector] Material change(s) detected: " + strings.Join(reasons, "; ") +
		".\nCheck in on the affected desk(s) and advance any authorized coordination. If nothing is " +
		"actionable, reply idle and signal it by running: touch " + settledPath + "." + ackInstr
}

// adjutantBufferedNoteBody notifies the adjutant that items were buffered (#439 phase 1b).
func adjutantBufferedNoteBody(leader string, n int) string {
	return "[flotilla adjutant] Buffered " + fmt.Sprintf("%d", n) + " interrupt(s) for " + leader +
		"'s layer. Triage mechanical items locally. Judgment items stay buffered until " +
		leader + "'s next seam — the leader receives a consolidated brief then, not mid-thought. " +
		"On evaluation ticks: ack → evaluate → act-by-tier." +
		adjutantDualObservationContract(leader)
}

// layerCharterMissing reports whether the first-presentation charter sidecar is absent.
// Fail toward pairing: any stat error (not just NotExist) triggers re-negotiation rather
// than silently treating an unreadable path as charter-present.
func layerCharterMissing(charterPath string) bool {
	_, err := os.Stat(charterPath)
	return err != nil
}

// enqueueAdjutantCharterPairing wakes adjutant+leader once when charter is missing (#439 2.5).
func enqueueAdjutantCharterPairing(adjutant, leader, rosterDir, leaderAckPath string, enqueue func(watch.Job)) {
	if adjutant == "" {
		return
	}
	charterPath := roster.LayerCharterPath(rosterDir, leader)
	if !layerCharterMissing(charterPath) {
		return
	}
	log.Printf("flotilla watch: adjutant charter pairing — %s/%s (missing %s)", leader, adjutant, charterPath)
	enqueue(watch.Job{
		Agent:   adjutant,
		Message: adjutantCharterPairingBody(leader, adjutant, charterPath, leaderAckPath),
		Kind:    watch.KindDetector,
	})
	enqueue(watch.Job{
		Agent:   leader,
		Message: leaderCharterPairingBody(leader, adjutant, charterPath, leaderAckPath),
		Kind:    watch.KindDetector,
	})
}

// adjutantSeamBrief peeks the layer buffer and formats the leader inject at a seam. The caller
// must Clear the sidecar only AFTER enqueue (enqueue-then-delete — same at-least-once window as
// detector persist).
func adjutantSeamBrief(bufferPath, leader, rosterDir string) (brief string, ok bool, clearAfter bool) {
	f, hasItems, quarantined, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		log.Printf("flotilla watch: adjutant buffer peek failed: %v", err)
		return "", false, false
	}
	if !hasItems && !quarantined {
		return "", false, false
	}
	_, charterErr := os.Stat(roster.LayerCharterPath(rosterDir, leader))
	return adjutantbuffer.FormatBrief(leader, f, os.IsNotExist(charterErr), quarantined), true, hasItems
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

// deskHeartbeatBody resolves the recursive desk-heartbeat (#183) continuation prompt for ONE desk: it
// runs the desk-continuation builtin through workspace.ResolvePrompt so a per-agent HEARTBEAT.md may
// override the wording, substituting {{settle}} with the DESK's OWN per-agent settle path (resolved
// via settleFor) — NOT the XO's. The XO-only {{tracker}} placeholder is absent from the desk builtin,
// so an empty tracker is passed. A desk beat carries NO liveness-ack instruction: the AckAge wedge
// watches the SINGLE XO ack file (a desk has no per-agent AckAge), so telling a beaten desk to touch
// that file would let an idle desk mask a genuinely-dead XO from its own watchdog (G4 review P1). The
// synthesis wake carries a liveness-ack instruction ONLY when its target IS the clock XO (#190). A HEARTBEAT.md read error
// fails open to the builtin (ResolvePrompt's posture). Pure relative to the panes (a file read).
func deskHeartbeatBody(agent string, settleFor func(string) string) (string, error) {
	return workspace.ResolvePrompt(agent, deskContinuationBuiltin, "", settleFor(agent))
}

// newDeskHeartbeatDispatch builds the detector's WakeDeskHeartbeat seam (a func(agent)): it resolves
// the desk-continuation body (per-agent HEARTBEAT.md + that desk's settle path) and enqueues it as an
// AUDIT-SUPPRESSED Kind:"detector" job to the desk. Kind:"detector" is load-bearing twice over (design
// §8d): SetMirror suppresses it (no operator-channel spam) AND it is isRelay-false, so a busy/
// input-blocked pane drops it (fire-and-forget) rather than queueing into a focused modal. enqueue is
// injected (the Injector's Enqueue) so the dispatch is unit-testable without tmux. A body-resolution
// error is logged and the beat dropped — a broken optional HEARTBEAT.md must never crash the daemon.
func newDeskHeartbeatDispatch(enqueue func(watch.Job), settleFor func(string) string) func(string) {
	return func(agent string) {
		body, err := deskHeartbeatBody(agent, settleFor)
		if err != nil {
			log.Printf("flotilla watch: desk-heartbeat prompt resolve failed for %q: %v (beat dropped this tick)", agent, err)
			return
		}
		enqueue(watch.Job{Agent: agent, Message: body, Kind: watch.KindDetector})
	}
}

// newDeskEscalate builds the detector's DeskEscalate seam (#183 §8e): when a desk is capped (idle +
// un-progressing across N beats), it raises ONE LOUD operator-visible alert to the desk's OWNING XO —
// the channel the desk is a member of / its parent (roster.OwningXO), falling back to the primary XO.
// It uses the LOUD alert path (operator-visible), NOT a quiet WakeAgent to a possibly-idle parent: a
// wedged desk must surface loudly, not be poked further. The detector fires this ONCE on the ==capN
// edge then stops beating the desk; a re-arm (AgentWake) resumes the cadence.
func newDeskEscalate(cfg *roster.Config, primaryXO string, alert func(string)) func(string) {
	return func(agent string) {
		owner := cfg.OwningXO(agent, primaryXO)
		alert(fmt.Sprintf("desk-heartbeat: %q has been idle and un-progressing across the heartbeat cap — it appears WEDGED. Owning XO %q: check in on it (or re-engage it to clear the wedge). It will not be heartbeated again until re-engaged.", agent, owner))
	}
}

// deskMirrorOnFinish builds the detector's MirrorOnFinish side-effect: when a non-XO desk finishes a
// turn, mirror its turn-final output to its home Discord channel. It returns nil — the detector's
// inert default — when no secrets file is configured (no per-desk webhooks to post to), so a
// deployment without --secrets keeps today's behavior byte-identically.
//
// The closure resolves each desk's surface driver and reads the turn-final through the SHARED
// surface.ResultReader seam (the same path `flotilla result` uses), so the CLI and the auto-mirror
// never diverge. A surface without a ResultReader is a clean SKIP.
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

// rateLimitMaterial returns a DetectorConfig callback that probes a desk for a
// material provider throttle (#204). ok=false when the surface lacks RateLimitProbe.
// Invoked OFF d.mu from runRateLimitProbes — never under tickLocked.
func rateLimitMaterial(cfg *roster.Config) func(agent string) (bool, surface.RateLimitScope, string, bool) {
	return func(agent string) (bool, surface.RateLimitScope, string, bool) {
		drv, ok := surface.Get(agentSurface(cfg, agent))
		if !ok {
			return false, 0, "", false
		}
		probe, ok := surface.RateLimitSupport(drv)
		if !ok {
			return false, 0, "", false
		}
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return false, 0, "", false
		}
		limited, scope, detail := probe.RateLimited(pane)
		return limited, scope, detail, true
	}
}

// rateLimitReset clears a desk's consecutive-read streak when it leaves the probe
// candidate states (Idle/Errored).
func rateLimitReset(cfg *roster.Config) func(agent string) {
	return func(agent string) {
		drv, ok := surface.Get(agentSurface(cfg, agent))
		if !ok {
			return
		}
		if _, ok := surface.RateLimitSupport(drv); !ok {
			return
		}
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return
		}
		surface.ClearRateLimitStreak(pane)
	}
}

// readDeskTurnFinal returns a desk's substantive turn-final via the shared
// surface.ResultReader seam (the same path `flotilla result` and the auto-mirror use).
func readDeskTurnFinal(cfg *roster.Config, agent string) (text string, ok bool, err error) {
	drv, ok := surface.Get(agentSurface(cfg, agent))
	if !ok {
		return "", false, fmt.Errorf("unknown surface for agent %q", agent)
	}
	rr, ok := drv.(surface.ResultReader)
	if !ok {
		return "", false, nil
	}
	pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
	if err != nil {
		return "", false, err
	}
	text, err = rr.LatestResult(pane)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

// coordinatorMirrorOnFinish builds the detector's CoordinatorMirrorOnFinish side-effect for the
// primary clock XO: ledger-only session-mirror append with readerModelInternal derivation. Discord
// posting is deliberately omitted — the XO Stop hook (deploy/flotilla-xo-discord-mirror.sh) already
// posts the turn-final via flotilla notify, and a second deskMirror post would double-publish.
func coordinatorMirrorOnFinish(cfg *roster.Config, firewall *readermap.TermSet, alert func(string), rosterDir string) func(agent string) {
	if rosterDir == "" {
		return nil
	}
	return func(agent string) {
		m := deskMirror{
			ledgerOnly: true,
			firewall:   firewall,
			alert:      alert,
			rosterDir:  rosterDir,
			turnFinal: func(a string) (string, bool, error) {
				return readDeskTurnFinal(cfg, a)
			},
			logf: log.Printf,
		}
		m.run(agent)
	}
}

func deskMirrorOnFinish(cfg *roster.Config, secrets *roster.Secrets, tr transport.Transport, firewall *readermap.TermSet, alert func(string), rosterDir string) func(agent string) {
	if secrets == nil || tr == nil {
		return nil
	}
	return func(agent string) {
		m := deskMirror{
			firewall:  firewall,
			alert:     alert,
			rosterDir: rosterDir,
			webhook: func(a string) (string, bool) {
				url, err := secrets.Webhook(a)
				if err != nil || url == "" {
					return "", false
				}
				return url, true
			},
			turnFinal: func(a string) (string, bool, error) {
				return readDeskTurnFinal(cfg, a)
			},
			post: func(url, username, content string) error {
				return tr.Post(transport.NewWebhookDestination(url), username, content)
			},
			logf: log.Printf,
		}
		m.run(agent)
	}
}

// delegationNudgeOnFinish builds the #232 coordinator delegation nudge: on each
// coordinator finish (any XO or CoS, including the primary clock XO) it classifies
// the turn-final for inline build/ship work without delegation and injects a
// dispatch nudge when consecutive strikes meet the threshold.
func delegationNudgeOnFinish(cfg *roster.Config, tracker *delegatenudge.Tracker, enqueue func(watch.Job)) func(agent string) {
	if tracker == nil {
		return nil
	}
	return func(agent string) {
		if !cfg.IsCoordinator(agent) {
			return
		}
		text, ok, err := readDeskTurnFinal(cfg, agent)
		if err != nil {
			log.Printf("flotilla watch: delegation-nudge SKIP %s: read turn-final: %v", agent, err)
			return
		}
		if !ok {
			return
		}
		r := delegatenudge.Check(text, agentSurface(cfg, agent))
		if !tracker.Record(agent, r) {
			return
		}
		log.Printf("flotilla watch: delegation-nudge %s: inline-build signal", agent)
		enqueue(watch.Job{Agent: agent, Message: delegatenudge.NudgePrompt(agent), Kind: watch.KindDetector})
	}
}

// strandedHandoffOnFinish builds the #216 stranded-handoff break seam: on each desk
// finish it classifies the turn-final for dropped gate reports and injects a break
// prompt on detection. nil tracker ⇒ inert.
func strandedHandoffOnFinish(cfg *roster.Config, tracker *stranded.Tracker, enqueue func(watch.Job)) func(agent string) {
	if tracker == nil {
		return nil
	}
	return func(agent string) {
		text, ok, err := readDeskTurnFinal(cfg, agent)
		if err != nil {
			log.Printf("flotilla watch: stranded-handoff SKIP %s: read turn-final: %v", agent, err)
			return
		}
		if !ok {
			return
		}
		r := stranded.Check(text)
		if !tracker.Record(agent, r) {
			return
		}
		log.Printf("flotilla watch: stranded-handoff break %s: signal=%s", agent, r.Signal)
		enqueue(watch.Job{Agent: agent, Message: stranded.NudgePrompt(agent), Kind: watch.KindDetector})
	}
}

// decisionBriefOnTick scans fleet-goals.json each detector tick for operator-gated
// items missing a brief and dispatches the owning desk once per gap (#349 item D).
func decisionBriefOnTick(
	goalsPath, backlogPath string,
	tracker *decisionbrief.Tracker,
	enqueue func(watch.Job),
	cfg *roster.Config,
	deskStates func() map[string]string,
) func() {
	if tracker == nil {
		return nil
	}
	return func() {
		raw, err := os.ReadFile(goalsPath)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("flotilla watch: decision-brief SKIP: read goals %q: %v", goalsPath, err)
			}
			return
		}
		gf, err := dash.ParseGoalsFile(raw)
		if err != nil {
			log.Printf("flotilla watch: decision-brief SKIP: parse goals %q: %v", goalsPath, err)
			return
		}
		var backlogMarkdown string
		if backlogPath != "" {
			bb, rerr := os.ReadFile(backlogPath)
			if rerr != nil {
				if !os.IsNotExist(rerr) {
					log.Printf("flotilla watch: decision-brief: read backlog %q: %v (continuing without backlog binding)", backlogPath, rerr)
				}
			} else {
				backlogMarkdown = string(bb)
			}
		}
		gaps := decisionbrief.FindGaps(decisionbrief.Inputs{
			File: gf, FileOK: true,
			Backlog: backlogMarkdown, DeskStates: deskStates(),
		})
		active := make(map[string]bool, len(gaps))
		for _, g := range gaps {
			owner := strings.TrimSpace(g.Owner)
			if owner == "" {
				log.Printf("flotilla watch: decision-brief SKIP goal %q: no owning desk", g.GoalID)
				continue
			}
			if _, err := cfg.Agent(owner); err != nil {
				log.Printf("flotilla watch: decision-brief SKIP goal %q: owner %q not in roster", g.GoalID, owner)
				continue
			}
			key := decisionbrief.GapKey(g)
			active[key] = true
			if !tracker.TryClaim(key) {
				continue
			}
			log.Printf("flotilla watch: decision-brief dispatch %s → %s (class=%s)", g.GoalID, owner, g.Class)
			enqueue(watch.Job{Agent: owner, Message: decisionbrief.DispatchPrompt(g), Kind: "detector"})
		}
		tracker.Reconcile(active)
	}
}

// idleHoldOnFinish builds the #216 idle-hold break seam: on each desk finish it
// classifies the turn-final, accrues consecutive strikes, and injects the break
// prompt when the threshold is met. nil tracker ⇒ inert.
func idleHoldOnFinish(cfg *roster.Config, tracker *idlehold.Tracker, enqueue func(watch.Job)) func(agent string) {
	if tracker == nil {
		return nil
	}
	return func(agent string) {
		text, ok, err := readDeskTurnFinal(cfg, agent)
		if err != nil {
			log.Printf("flotilla watch: idle-hold SKIP %s: read turn-final: %v", agent, err)
			return
		}
		if !ok {
			return
		}
		r := idlehold.Check(text)
		if !tracker.Record(agent, r) {
			return
		}
		log.Printf("flotilla watch: idle-hold break %s: signal=%s strikes=%d", agent, r.Signal, tracker.Strikes(agent))
		enqueue(watch.Job{Agent: agent, Message: idlehold.BreakPrompt(r.Recommendation), Kind: watch.KindDetector})
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

// mirrorRelayToLedger appends a confirmed operator→<agent> relay delivery to the CoS
// who-knows-what ledger (#108), tagged with the Job's origin channel (#105 seam), so the
// chief of staff can see which side-conversation (and which desk/XO) was told what. #349
// E11 source: v1 scoped this to XO targets only (an operator message to a DESK was deemed
// out of the operator↔XO ledger, design §6.3 deferred); that Phase-2 scope is now realized
// so an operator→desk relay also lands in the ledger — and therefore in that desk's dash
// conversation thread. cfg.CosLedger == "" (cos_agent unset) ⇒ inert. BEST-EFFORT +
// observe-only: the confirmed delivery already happened, so a ledger failure NEVER affects
// it — it is reported to stderr and ignored.
func mirrorRelayToLedger(cfg *roster.Config, j watch.Job) {
	if cfg.CosLedger == "" {
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

// agentSurface returns the surface name a desk is CURRENTLY driven through, with
// overlay-first precedence: the active-harness overlay's surface when set, else the
// roster Agent.surface, else "" (the default driver). This is the seam that makes
// watch/send/assess route to the LIVE harness after a `flotilla switch` with NO commit
// to the portable roster — the overlay is the runtime source of truth for which harness
// a desk runs.
//
// Reading the overlay is fail-SAFE: a missing overlay (the common, un-switched case) and
// a torn/unreadable overlay BOTH fall through to the roster surface — a bad overlay must
// never make a live desk unroutable. An unknown name falls back to "" so surface.Get
// resolves the default rather than erroring on a non-roster name.
func agentSurface(cfg *roster.Config, name string) string {
	if ov, ok, err := workspace.ReadActiveOverlay(name); err == nil && ok && ov.Surface != "" {
		return ov.Surface
	}
	if a, err := cfg.Agent(name); err == nil {
		return a.Surface
	}
	return ""
}

// adaptiveIntervalEnabled resolves the adaptive master switch: explicit --adaptive-interval
// wins, else FLOTILLA_ADAPTIVE_INTERVAL (default on).
func adaptiveIntervalEnabled(flagVal string) bool {
	if s := strings.TrimSpace(flagVal); s != "" {
		switch strings.ToLower(s) {
		case "0", "false", "no", "off":
			return false
		case "1", "true", "yes", "on":
			return true
		default:
			// Unrecognized explicit value — fail closed to fixed tick (safer than guessing).
			return false
		}
	}
	return watch.AdaptiveIntervalEnabled()
}

// defaultPath sets *p to filepath.Join(parts...) only when *p is empty, so an
// operator-supplied flag/env value always wins and an unset one falls back to the
// roster-relative default. It factors out the ~identical unset-path defaulting
// repeated for every watch state file (ack, snapshot, cursor, queue, …).
func defaultPath(p *string, parts ...string) {
	if *p == "" {
		*p = filepath.Join(parts...)
	}
}

// optionalDuration reads a positive duration from a CLI flag, else from envKey.
func optionalDuration(flagVal, envKey string) (time.Duration, bool) {
	if s := strings.TrimSpace(flagVal); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return 0, false
		}
		return d, true
	}
	if s := strings.TrimSpace(os.Getenv(envKey)); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return 0, false
		}
		return d, true
	}
	return 0, false
}
