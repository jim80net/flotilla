// Command flotilla coordinates a fleet of AI coding agents: it delivers an
// instruction into a target agent's tmux pane (the delivery IS the wake) and
// mirrors it to the Discord audit channel under the sender's identity.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/voice"
	"github.com/jim80net/flotilla/internal/watch"
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
	case "dispatch-status":
		return cmdDispatchStatus(args[1:])
	case "notify":
		return cmdNotify(args[1:])
	case "brief":
		return cmdBrief(args[1:])
	case "parade":
		return cmdParade(args[1:])
	case "speak":
		return cmdSpeak(args[1:])
	case "voice":
		return cmdVoice(args[1:])
	case "watch":
		return cmdWatch(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "dash":
		return cmdDash(args[1:])
	case "channel":
		return cmdChannel(args[1:])
	case "provision-discord":
		return cmdProvisionDiscord(args[1:])
	case "register":
		return cmdRegister(args[1:])
	case "resume":
		return cmdResume(args[1:])
	case "recycle":
		return cmdRecycle(args[1:])
	case "switch":
		return cmdSwitch(args[1:])
	case "workspace":
		return cmdWorkspace(args[1:])
	case "doctrine":
		return cmdDoctrine(args[1:])
	case "push-snippet":
		return cmdPushSnippet(args[1:])
	case "result":
		return cmdResult(args[1:])
	case "inbox":
		return cmdInbox(args[1:])
	case "goals":
		return cmdGoals(args[1:])
	case "accounts":
		return cmdAccounts(args[1:])
	case "mirror-self":
		return cmdMirrorSelf(args[1:])
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
  flotilla send --from <sender> <agent> <message>     inline message
  flotilla send --from <sender> --file <path> <agent> message body from a file ('-' = stdin)
  flotilla dispatch-status [--roster <path>] <nonce>  consumed / queued / delivered / undelivered (#614)
  flotilla notify --from <agent> <message>            post to the operator under <agent>'s webhook (no tmux)
  flotilla notify --from <agent> --file <path>        notify body from a file ('-' = stdin)
  flotilla notify --from <agent> --with-fleet-status  append compressed Status of the fleet (#625)
  flotilla brief [--all] [<desk>] [--audience <who>]  elicit a reader-modeled brief; the shipped mirror publishes it to the desk's channel (secret-free; not notify)
  flotilla parade [--all] [<agent>]                   elicit parade answers (proud of / learned / looking forward to / need / demo); mirror publishes to each channel
  flotilla parade rollup [--all] [<xo>]               wake coordinators to roll up subordinates' parade answers
  flotilla parade fleet                               wake the primary XO for the operator fleet parade report
  flotilla speak <text>                               drop a short spoken reply on the voice outbound spool (non-blocking)
  flotilla speak --file <path>                         speak body from a file ('-' = stdin)
  flotilla voice [--config <voice.env>]               operator↔XO Discord voice (needs a -tags voiceopus build)
  flotilla watch                                      relay + XO heartbeat clock daemon
  flotilla status                                     one line per desk: last-known state + XO ack age (reads the watch snapshot; no daemon)
  flotilla inbox <channel> [--limit N]                read recent messages of a bound channel over REST (role or channel id; recover a dropped operator message; read-only)
  flotilla dash [--bind 127.0.0.1:8787]               optional local web UI: fleet board + federation topology + coordination history (read-only; reads the watch artifacts; loopback only)
  flotilla goals validate [--roster <path>] [--yaml <path>] [--json <path>]
                                                      fail-closed validate fleet-goals.yaml (and json if present)
  flotilla goals compile [--roster <path>] [--yaml <path>] [--json <path>]
                                                      compile fleet-goals.yaml → fleet-goals.json (roster-adjacent)
  flotilla goals link --goal <id> (--issue <ref> | --backlog <match> | --inline <text> | --desk <agent>)
                                                      attach a work item to fleet-goals.yaml (preserves yaml comments) and recompile json
  flotilla accounts init <subscription-id>            scaffold Claude Code config dir + print one-time /login steps
  flotilla accounts list [--json]                     subscription credential health (mtime/expiry only; no secrets)
  flotilla channel create <name> [--type text|category] [--topic <t>] [--category <name|id>]
                                                      create a Discord channel via the bot (idempotent; emits an F#105 binding with --xo)
	flotilla channel list [--json]                      list the guild's channels (id, type, name, parent)
	flotilla channel move <channel-id> --category <ref> reparent an orphan text channel (edit is an alias)
	flotilla channel delete <channel-id> --yes          delete a channel by snowflake id only (the one destructive verb)
	flotilla provision-discord <flotilla-key> [--dry-run] [--apply-roster]
	                                                    provision COS C2 + flotilla product hub + bindings + XO webhook
  flotilla register <agent> [--pane <target>]         tag a pane so it resolves by a stable, drift-immune marker
  flotilla resume <agent> [--launch <path>] [--force]  (re)start a dead desk from its host-local launch recipe
  flotilla recycle <agent> [--launch <path>] [--dry-run]  close a desk's chapter (handoff→graceful close→relaunch→takeover), fail-closed
  flotilla switch <agent> (--to <slot|surface> | --auto | --repair) [--confirm] [--force]  hand a desk across harnesses (FROM handoff→relaunch on TO→TO takeover), fail-closed
  flotilla workspace init <agent> --repo <abs-path>   provision a desk git worktree + ~/.flotilla/<agent>/ host (seeds doctrine into the worktree)
  flotilla workspace path <agent>                     print an agent's workspace directory
  flotilla doctrine install [--refresh] [--all] [<agent>]  install constitutional doctrine (idempotent; --refresh updates drifted fenced blocks)
  flotilla push-snippet <desk-agent>                  print the smart-push convention to append to a non-claude desk's identity file (secret-free; reports to the XO via send)
  flotilla result <agent>                             print a desk's FULL latest result from its harness session store (grok; read-only) — for long results the pane capture truncates
  flotilla mirror-self --from <agent> --file <path|-> session-mirror (+ Discord) without pane Working→Idle — coordinator Stop hooks (#572)
  flotilla version
  flotilla help

flags for 'send':
  --from <name>     sender identity (default $FLOTILLA_SELF)
  --file <path>     read the message body from a file ('-' for stdin) instead of
                    the command line — avoids shell quoting of multi-line bodies
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)
  --attach <path>   attach files to the audit mirror post when mirroring (repeatable)
  --mirror          force-enable the Discord audit mirror for this send
  --no-mirror       force-disable it (--mirror and --no-mirror are mutually exclusive)

Inter-agent send mirroring is DEFAULT-OFF — intra-fleet coordination stays in the
tmux panes and does not clutter Discord; only the operator-facing 'flotilla notify'
posts by default. Set roster "mirror_inter_agent": true to restore the always-on
audit trail (or pass --mirror per call; precedence: flag → roster setting → off).
When it does mirror it is best-effort: an unconfigured/failed mirror still delivers
and the command succeeds (with a warning), so a retry never double-delivers.

flags for 'notify':
  --from <name>     the agent whose webhook the message is posted under
                    (default $FLOTILLA_SELF)
  --file <path>     read the message body from a file ('-' for stdin)
  --attach <path>   attach a file to the Discord message (repeatable; not the
                    message body — use --file for that)
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)
  --roster <path>   roster path (CoS mirror + --with-fleet-status snapshot)
  --with-fleet-status  append compressed **Status of the fleet** from the
                    detector snapshot (same source as status --json); skips
                    --from agent + its adjutant; idempotent if body already
                    has **Status of the fleet** or **Fleet status**; on
                    read failure appends (unavailable) — never silent omit
  --chunk           split an over-limit body into sequential Discord messages
                    (paragraph boundaries, (i/N) prefixes; used by the XO mirror hook)

notify is the operator-facing outbound path: it posts <message> directly to the
operator on Discord, under the <agent>'s own webhook identity, and does NOT
inject into any tmux pane. Use it when an agent (typically the XO) wants to
reach the operator — as opposed to 'send', which wakes another agent's pane and
mirrors the wake to the audit channel. Without --chunk the message must be ≤ 2000
characters (Discord's hard limit); a longer body is rejected (nothing is posted).
With --chunk the full body is delivered across multiple messages. --attach delivers
files as Discord attachments (multipart webhook POST); oversize or unreadable paths
fail closed (nothing is posted).

flags for 'speak':
  --file <path>     read the spoken text from a file ('-' for stdin)

speak is the XO's outbound VOICE path: it drops <text> onto the voice outbound
spool (state/voice/outbound/) and returns IMMEDIATELY. It is decoupled from the
'flotilla voice' process on purpose — speak NEVER blocks on, nor fails because
of, voice being up, so it can never fail the XO's turn (even with voice down it
succeeds by just writing a file). The voice process watches→consumes→deletes
those files. The spool is bounded; on overflow the OLDEST entry is dropped, a
new write is NEVER refused (a refusal would fail the turn). On success speak
prints only the spooled path.

flags for 'watch':
  --roster <path>             roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>            secrets env file: relay bot token + down-alert webhook (default $FLOTILLA_SECRETS)
  --ack-file <path>           XO liveness ack file the XO touches (default $FLOTILLA_ACK_FILE, else <roster-dir>/flotilla-xo-alive)
  --max-missed-acks <n>       missed-ack window K, in intervals, before a down-alert (default 3)

  change-detector (heartbeat v2 — enabled by roster change_detector: true):
  --snapshot-file <path>      detector snapshot (default $FLOTILLA_SNAPSHOT_FILE, else <roster-dir>/flotilla-detector-state.json)
  --awaiting-file <path>      awaiting-operator veto marker (default $FLOTILLA_AWAITING_FILE, else <roster-dir>/flotilla-xo-awaiting)
  --settled-file <path>       XO settle/idle marker (default $FLOTILLA_SETTLED_FILE, else <roster-dir>/flotilla-xo-settled)
  --tracker-file <path>       the XO's {{tracker}} read-source — NOT hashed as a wake signal (default $FLOTILLA_TRACKER_FILE, else <roster-dir>/.flotilla-state.md)
  --signal-file <path>        OPTIONAL external signal file whose content-hash change wakes the XO (a file the XO does NOT write; $FLOTILLA_SIGNAL_FILE; unset ⇒ no external-signal trigger)
  --interval <duration>       change-detector tick interval (overrides roster heartbeat_interval; $FLOTILLA_WATCH_INTERVAL; adaptive ON ⇒ ceiling)
  --event-poll-interval <dur> fast desk turn-end poll (default 5s; $FLOTILLA_EVENT_POLL_INTERVAL; 0 disables)
  --adaptive-interval <bool>  adaptive tick policy (default on; $FLOTILLA_ADAPTIVE_INTERVAL; 0/false disables)
  --interval-floor <dur>      adaptive Active-tier floor (default 2m; $FLOTILLA_INTERVAL_FLOOR)
  --interval-warm <dur>       adaptive Warm tier (default 8m; $FLOTILLA_INTERVAL_WARM)
  --interval-idle-stable <dur> adaptive hysteresis before ceiling (default 10m; $FLOTILLA_INTERVAL_IDLE_STABLE)
  --interval-release-step <dur> adaptive release decay cadence (default 5m; $FLOTILLA_INTERVAL_RELEASE_STEP)
  --max-quiet-intervals <n>   liveness ping cadence N, in intervals (0 ⇒ ping-mode default)
  --max-self-continuations <n> cap on consecutive XO self-continuations with no external change (default 3)

watch runs the XO clock + liveness watchdog (needs neither Discord nor secrets),
and adds the inbound relay when channel_id + operator_user_id + a bot token are
configured. The clock target and interval come from the roster (xo_agent,
heartbeat_interval). By default the clock is the legacy always-wake heartbeat;
set change_detector: true (with liveness_ping_mode none|interval|consecutive) to
wake the XO only on a material change — an idle fleet then costs nothing.

flags for 'status':
  --roster <path>         roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --snapshot-file <path>  the watch change-detector snapshot to read (default $FLOTILLA_SNAPSHOT_FILE, else <roster-dir>/flotilla-detector-state.json)
  --ack-file <path>       XO liveness ack file to age (default $FLOTILLA_ACK_FILE, else <roster-dir>/flotilla-xo-alive)
  --json                  emit machine-readable JSON ({ generated_at, xo, agents[] }) instead of the text table

status prints one line per roster desk — its last-known state (idle / working /
awaiting-input / crashed / unknown) and, for the XO, its last-ack age and whether
it has settled. It is READ-ONLY: it reads the snapshot + ack file that 'flotilla
watch' (with change_detector: true) already writes, starts no daemon, and probes
no panes. The states are the detector's view as of its last tick, so the header
reports the snapshot's age — a stale read is never shown as live. With no readable
snapshot it still lists the roster, every desk as 'unknown'.

flags for 'dash':
  --roster <path>         roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --snapshot-file <path>  the watch change-detector snapshot to read (default $FLOTILLA_SNAPSHOT_FILE, else <roster-dir>/flotilla-detector-state.json)
  --ack-file <path>       XO liveness ack file to age (default $FLOTILLA_ACK_FILE, else <roster-dir>/flotilla-xo-alive)
  --tracker-file <path>   backlog markdown the history view reads (default $FLOTILLA_TRACKER_FILE, else <roster-dir>/.flotilla-state.md)
  --bind <addr>           local listen address (default 127.0.0.1:8787; loopback only in this phase)
  --repo <owner/name>     GitHub repo for the issue tracker (reserved for the tracker phase; unused here)

dash serves an OPTIONAL local web interface over the artifacts 'flotilla watch'
already writes: a fleet board (each desk's last-known state with three-state
snapshot freshness — absent/stale/fresh), the federation topology (channel→XO→
members), and the coordination history (the CoS ledger + the backlog drive-queue).
It is READ-ONLY — it starts no daemon, probes no panes, and writes no fleet state;
'flotilla watch' remains the single writer, so the dash can never diverge from or
double-probe the fleet. Live updates push over Server-Sent Events (with a JSON
poll fallback). The default bind is loopback; a non-loopback bind is refused in
this phase (the token-gated auth surface lands with the control phase — use an SSH
tunnel to the loopback bind for remote access).

flags for 'channel':
  create <name>     create a channel in the roster's guild_id via the bot token
    --type text|category   channel type (default text)
    --topic <t>            channel topic (text channels)
    --category <name|id>   place a text channel under this parent category (by name or snowflake id)
    --xo <agent>           also print the F#105 channel→XO binding for the new channel
    --member <agent>       a member of that binding (repeatable; requires --xo)
    --role <label>         the binding's role label (requires --xo)
  list [--json]     list the guild's channels (id, type, name, parent)
  delete <id> --yes delete a channel by its snowflake id (never by name); --yes is required
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>  secrets env file holding FLOTILLA_BOT_TOKEN (default $FLOTILLA_SECRETS)

channel provisions Discord channels mechanically — the creation complement to the
F#105 routing bindings. create is IDEMPOTENT (re-running skips an existing same-name
channel under the same parent, so a squadron stand-up is safe to re-run) and runs a
Manage-Channels permission preflight first (a clear error if the bot lacks it). The
bot token is read from the secrets file and never logged. With --xo, create also
prints the roster channels[] binding (with the new channel's id) ready to paste, so
routing is live the moment you wire it. These are one-shot REST calls; no gateway is
opened. delete is the one destructive verb: id-only (validated as a snowflake) and
--yes-gated — intended for operator-driven teardown.

flags for 'register':
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --pane <target>   tmux pane to tag (default $TMUX_PANE — the pane this runs in)

register tags a pane with a stable @flotilla_agent marker so flotilla resolves
the agent by that key instead of the tmux pane title. Claude Code retitles its
pane to a task summary every turn, which breaks title-based resolution; the
marker is immune to that drift. Run 'flotilla register <name>' once inside each
desk's pane at launch, or re-tag an already-drifted desk from elsewhere with
'flotilla register <name> --pane <target>' (no need to interrupt the desk).

flags for 'resume':
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --launch <path>   host-local launch recipes (default $FLOTILLA_LAUNCH, else <roster-dir>/flotilla-launch.json)
  --force           resume even if the desk is a LIVE session (kills it first)

resume deterministically (re)starts a desk from its host-local launch recipe
(launch command + working directory, optional tmux target + state pointer). It
resolves the desk by its stable marker first: an existing pane is respawned in
place (refusing a LIVE session unless --force — restart is not resume-and-act);
an ambiguous (mis-tagged) fleet is refused; with no pane it cold-creates the
desk's window (cold-starting the tmux server if the whole server died) and tags
it. The launch file is HOST-LOCAL and gitignored (a sibling of
flotilla-secrets.env), trusted at the secrets level — recipes are shell-run, so
anyone who can write it can already write your secrets. resume (re)starts the
process and ensures it is tagged; it does NOT restore context — drive /takeover
from the printed state pointer yourself.`)
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
	file := fs.String("file", "", "read message body from this file ('-' for stdin)")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	noMirror := fs.Bool("no-mirror", false, "force-skip the Discord audit mirror")
	doMirror := fs.Bool("mirror", false, "force-enable the Discord audit mirror (overrides a default-off roster)")
	var attachPaths attachPathsFlag
	fs.Var(&attachPaths, "attach", "attach a file to the audit mirror Discord post (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *noMirror && *doMirror {
		return fmt.Errorf("--mirror and --no-mirror are mutually exclusive")
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("usage: flotilla send --from <sender> <agent> <message>  (or --file <path> <agent>)")
	}
	if *from == "" {
		return fmt.Errorf("--from is required (or set $FLOTILLA_SELF)")
	}
	agentName := rest[0]
	// Go's flag parser stops at the first positional (the agent), so any flag
	// placed AFTER the agent is silently swallowed. Catch that with a clear
	// message instead of a confusing downstream failure.
	for _, a := range rest[1:] {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unexpected %q after the agent name: put flags before the agent, or use --file for a message that starts with '-'", a)
		}
	}
	// --file - reads stdin; if stdin is an interactive terminal nothing is piped
	// and io.ReadAll would block forever. Fail fast instead of hanging.
	if *file == "-" {
		if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("--file - requires piped stdin, but stdin is a terminal (nothing piped)")
		}
	}
	message, err := resolveMessage(*file, rest[1:], os.Stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("message is empty")
	}
	message, _, err = inbound.AppendDispatchNonce(message)
	if err != nil {
		return fmt.Errorf("append dispatch nonce: %w", err)
	}

	resolvedRoster, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(resolvedRoster)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}
	// Resolve the agent's surface driver (how this surface submits a turn).
	// Unknown surface is a clear error, never a silent mis-drive.
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q (known: see internal/surface registry)", agentName, agent.Surface)
	}

	// Deliver = wake: submit the message into the agent's pane via its driver and CONFIRM a
	// turn started (idle-gate → submit → confirm the Idle→Working edge → Enter-only retry),
	// rather than assuming success from the tmux exit code (the relay silent-drop bug). This is
	// the operation that must succeed; the
	// audit mirror below is best-effort.
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	// Inline retry-with-backoff, then durable per-sender outbox on sustained busy (#475).
	queued, err := deliverOrQueueSend(cfg, resolvedRoster, *from, agentName, drv, pane, message)
	if err != nil {
		return err
	}
	if queued {
		// Queued ≠ delivered — skip audit mirror and ledger (watch delivers later).
		return nil
	}

	// Mirror to the Discord audit channel under the sender's identity. Inter-agent
	// mirroring is DEFAULT-OFF (it cluttered the operator's Discord); precedence is
	// --no-mirror (off) → --mirror (on) → roster mirror_inter_agent (default false).
	// A mirror failure (or absence) is a warning, not a command failure — the message
	// is already delivered, and failing here would tempt a retry into a double-send.
	if !shouldMirror(*noMirror, *doMirror, cfg.MirrorInterAgent) {
		return nil
	}
	runeCap := transportContentCap()
	if n := len([]rune(message)); runeCap > 0 && n > runeCap {
		fmt.Fprintf(os.Stderr, "flotilla: note — message is %d chars; the audit copy is truncated to %d (the full message WAS delivered)\n", n, runeCap)
	}
	if err := mirror(*secretsPath, *from, agentName, message, attachPaths); err != nil {
		fmt.Fprintln(os.Stderr, "flotilla: WARNING — audit mirror skipped (message WAS delivered): "+err.Error())
	}
	return nil
}

// cmdNotify posts a message directly to the operator on Discord, under the
// sender agent's own webhook identity, with NO tmux injection. It is the
// operator-facing outbound path — distinct from 'send', which wakes another
// agent's pane and mirrors that wake to the audit channel. Reuses the exact
// message-resolution (--file / stdin) and webhook-resolution that 'send' uses.
func cmdNotify(args []string) error {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "agent whose webhook to post under")
	file := fs.String("file", "", "read message body from this file ('-' for stdin)")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path (for the CoS context-mirror + --with-fleet-status)")
	withFleet := fs.Bool("with-fleet-status", false, "append compressed fleet posture from status snapshot (#625)")
	chunk := fs.Bool("chunk", false, "split an over-limit body into sequential Discord messages")
	var attachPaths attachPathsFlag
	fs.Var(&attachPaths, "attach", "attach a file to the Discord message (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *chunk && len(attachPaths) > 0 {
		return fmt.Errorf("--chunk and --attach are mutually exclusive")
	}
	if *from == "" {
		return fmt.Errorf("--from is required (or set $FLOTILLA_SELF)")
	}
	rest := fs.Args()
	// Go's flag parser stops at the first positional, so a flag placed AFTER the
	// message words is silently swallowed. Catch it with a clear message — the
	// same guard 'send' uses (there is no agent positional here, so we check the
	// whole tail).
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unexpected flag %q after the message: put flags before the message, or use --file for a body that starts with '-'", a)
		}
	}
	// --file - reads stdin; if stdin is an interactive terminal nothing is piped
	// and io.ReadAll would block forever. Fail fast instead of hanging.
	if *file == "-" {
		if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("--file - requires piped stdin, but stdin is a terminal (nothing piped)")
		}
	}
	var message string
	if *file != "" || len(rest) > 0 {
		var err error
		message, err = resolveMessage(*file, rest, os.Stdin)
		if err != nil {
			return err
		}
	} else if len(attachPaths) == 0 {
		return fmt.Errorf("no message: provide text, --file <path>, or at least one --attach")
	}
	if strings.TrimSpace(message) == "" && len(attachPaths) == 0 {
		return fmt.Errorf("message is empty")
	}
	// Coordinator fleet posture (#625): append before length check so --chunk can
	// split a long body+status, and fail-closed unavailable never silently omits.
	if *withFleet {
		rp, fromName := *rosterPath, *from
		message = withFleetStatus(message, true, func() (string, error) {
			return loadFleetStatusBlock(rp, fromName)
		})
	}
	// Without --chunk, reject an over-length body cleanly (nothing is posted).
	// With --chunk the XO mirror hook (and any caller) delivers the WHOLE body.
	if !*chunk {
		if runeCap := transportContentCap(); runeCap > 0 {
			if n := len([]rune(message)); n > runeCap {
				return fmt.Errorf("message is %d chars; the transport's limit is %d — shorten it, pass --chunk, or split it (nothing was posted)", n, runeCap)
			}
		}
	}

	// flotilla notify posts to the operator's channel — a fleet-internal surface.
	// The partition firewall (Pillar D) does NOT run here (#465); public-repo egress
	// is guarded by check-private-boundary.sh + the pre-push hook instead.

	if *secretsPath == "" {
		return fmt.Errorf("secrets unset (set --secrets or $FLOTILLA_SECRETS)")
	}
	secrets, err := roster.LoadSecrets(*secretsPath)
	if err != nil {
		return err
	}
	hook, err := secrets.Webhook(*from)
	if err != nil {
		return err
	}
	tr, err := outboundTransport(secrets)
	if err != nil {
		return err
	}
	dest := transport.NewWebhookDestination(hook)
	if *chunk {
		chunks := transport.Chunk(message, mirrorChunkLimit)
		n := len(chunks)
		for i, part := range chunks {
			body := part
			if n > 1 {
				body = fmt.Sprintf("(%d/%d)\n%s", i+1, n, part)
			}
			if err := tr.Post(dest, *from, body); err != nil {
				return fmt.Errorf("notify: chunk %d/%d failed: %w", i+1, n, err)
			}
			if i < n-1 {
				time.Sleep(400 * time.Millisecond)
			}
		}
		if n > 1 {
			fmt.Printf("notified operator as %s (%d chunks, %d chars)\n", *from, n, utf8.RuneCountInString(message))
		} else {
			fmt.Printf("notified operator as %s\n", *from)
		}
	} else if err := postOutbound(tr, dest, *from, message, attachPaths); err != nil {
		return err
	} else if len(attachPaths) > 0 {
		fmt.Printf("notified operator as %s (%d attachment(s))\n", *from, len(attachPaths))
	} else {
		fmt.Printf("notified operator as %s\n", *from)
	}
	// CoS context-mirror (#108): record this XO→operator reply in the who-knows-what
	// ledger. Strictly best-effort + observe-only — the operator-facing post already
	// succeeded, so it must never fail notify.
	mirrorNotifyToLedger(*rosterPath, *from, message)
	// #595 / #628: stamp time + body fingerprint so finish-edge / mirror-self skip Discord.
	stampRecentNotifyBody(*rosterPath, *from, message)
	return nil
}

// stampRecentNotifyBody records a successful notify so finish-edge auto-mirror skips Discord
// within the suppression window (#595) and same-body window (#628 dual-egress residual).
// Best-effort — never fails notify.
func stampRecentNotifyBody(rosterPath, agent, body string) {
	if rosterPath == "" || agent == "" {
		return
	}
	path := roster.LayerLastNotifyPath(filepath.Dir(rosterPath), agent)
	if err := watch.RecordRecentNotify(path, time.Now().UTC(), body); err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: WARNING — recent-notify stamp failed (notify succeeded): %v\n", err)
	}
}

// mirrorNotifyToLedger appends a <sender>→operator notify to the CoS who-knows-what ledger
// when cos_agent is configured. #349 E11 source: v1 scoped this to XO senders only (a desk's
// notify was deemed out of the operator↔XO ledger, design §6.3 deferred); that Phase-2 scope
// is now realized so a DESK's notify to the operator also lands in the ledger — and therefore
// in that desk's dash conversation thread. It is BEST-EFFORT: a missing/unreadable roster, an
// inert CoS, or an append error all just skip the ledger — the operator post already
// succeeded, and the mirror is observe-only, so it never fails notify.
func mirrorNotifyToLedger(rosterPath, from, message string) {
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		return // no/unreadable roster ⇒ no ledger (notify already succeeded)
	}
	if cfg.CosLedger == "" {
		return // CoS inert ⇒ no ledger
	}
	channel, ok := cfg.ChannelForXO(from)
	// A federated roster (channels[] declared) whose sender owns no channel binding tags the
	// entry with no channel ("-"). For an XO that is config drift worth surfacing; a desk that
	// legitimately owns no channel is normal, so only warn when the sender is an XO (OCR #109).
	if !ok && len(cfg.Channels) > 0 && cfg.IsXO(from) {
		fmt.Fprintf(os.Stderr, "flotilla notify: XO %q has no channel binding in the federated roster — ledger entry tagged with no channel\n", from)
	}
	if err := cos.Append(cfg.CosLedger, cos.Entry{
		Time:    time.Now(),
		Channel: channel,
		From:    from,
		To:      "operator",
		Gist:    message,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "flotilla notify: cos ledger append failed: %v\n", err)
	}
}

// mirrorSendToLedger records a confirmed `flotilla send` relay (from → to) to the CoS
// who-knows-what ledger, tagged with the RECIPIENT's home channel. This is the CoS↔desk
// (and desk↔desk) relay path that populates a desk's dash conversation thread. #349 E11
// source: `flotilla send` never recorded to the ledger, so these relay lines were invisible
// and a desk's own thread stayed empty regardless of the reader filter. Inert when cos_agent
// is unset; BEST-EFFORT — the message is already delivered, so a ledger error is logged, not
// fatal (failing here would tempt a retry into a double-send).
func mirrorSendToLedger(cfg *roster.Config, from, to, message string) {
	if cfg == nil || cfg.CosLedger == "" {
		return
	}
	// Resolve the recipient's channel by ownership OR membership: a pure desk owns no
	// channel (ChannelForXO would return ""), but is a member of its parent's channel —
	// ChannelForAgent finds that, so a desk-directed relay carries a real channel tag
	// instead of "-" (cubic #362 P2). "" only when the recipient is in no binding.
	channel, _ := cfg.ChannelForAgent(to)
	if err := cos.Append(cfg.CosLedger, cos.Entry{
		Time:    time.Now(),
		Channel: channel,
		From:    from,
		To:      to,
		Gist:    message,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "flotilla send: cos ledger append failed: %v\n", err)
	}
}

// cmdSpeak drops a short spoken reply onto the voice outbound spool and returns
// immediately — the XO's outbound VOICE path. It is deliberately decoupled from the
// `flotilla voice` process: speak NEVER blocks on, nor fails because of, voice being up
// (writing a file waits on no reader), so it can never fail the XO's turn — even with
// voice down it succeeds by just dropping a file. The voice process watches→consumes→
// deletes those files; the spool is bounded with a drop-oldest (never refuse-new) overflow
// policy enforced in voice.WriteSpeak. Reuses the exact --file / stdin message resolution
// that `send`/`notify` use; takes the text as a positional arg otherwise.
func cmdSpeak(args []string) error {
	fs := flag.NewFlagSet("speak", flag.ContinueOnError)
	file := fs.String("file", "", "read the spoken text from this file ('-' for stdin)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	// Go's flag parser stops at the first positional, so a flag placed AFTER the text words
	// is silently swallowed. Catch it with the same guard `notify` uses (no agent positional
	// here, so we check the whole tail).
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unexpected flag %q after the text: put flags before the text, or use --file for a body that starts with '-'", a)
		}
	}
	// --file - reads stdin; if stdin is an interactive terminal nothing is piped and
	// io.ReadAll would block forever. Fail fast instead of hanging.
	if *file == "-" {
		if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("--file - requires piped stdin, but stdin is a terminal (nothing piped)")
		}
	}
	text, err := resolveMessage(*file, rest, os.Stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text is empty")
	}
	// The ONLY thing that can fail here is a local filesystem error creating/writing the
	// spool file — never the voice process's state. On success we print just the spooled
	// path (the contract: speak is quiet, non-blocking, and exits 0 even when it had to
	// create the spool dir).
	path, err := voice.WriteSpeak(text)
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

// resolveMessage determines the message body. With filePath set, it is read
// from that file ("-" reads stdin) and trailing newlines are trimmed; inline
// positional words are then disallowed (mutually exclusive). Without filePath,
// the positional words are joined with spaces.
func resolveMessage(filePath string, inline []string, stdin io.Reader) (string, error) {
	if filePath != "" {
		if len(inline) > 0 {
			return "", fmt.Errorf("--file and an inline message are mutually exclusive")
		}
		raw, err := readSource(filePath, stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(raw, "\r\n"), nil
	}
	if len(inline) == 0 {
		return "", fmt.Errorf("no message: provide an inline message or --file <path>")
	}
	return strings.Join(inline, " "), nil
}

// readSource reads a message body from a file path, or from stdin when the path
// is "-".
func readSource(path string, stdin io.Reader) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read message from stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read message file: %w", err)
	}
	return string(b), nil
}

// shouldMirror resolves whether a `send` mirrors to the Discord audit channel.
// Precedence: --no-mirror forces off, --mirror forces on, else the roster default
// (mirror_inter_agent, itself default false → inter-agent mirroring is off unless
// opted in). The two flags are rejected as mutually exclusive upstream, so at most
// one of noMirror/doMirror is true here.
func shouldMirror(noMirror, doMirror, rosterDefault bool) bool {
	switch {
	case noMirror:
		return false
	case doMirror:
		return true
	default:
		return rosterDefault
	}
}

// mirror posts the delivered instruction to the audit channel under the
// sender's webhook identity. Errors are returned for the caller to warn on.
func mirror(secretsPath, from, agentName, message string, attachPaths []string) error {
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
	tr, err := outboundTransport(secrets)
	if err != nil {
		return err
	}
	body := fmt.Sprintf("→ %s: %s", agentName, message)
	return postOutbound(tr, transport.NewWebhookDestination(hook), from, body, attachPaths)
}

// outboundTransport constructs the OUTBOUND coordination transport for a one-shot CLI
// (send / notify / mirror) — the discord transport built with no bot token (Post,
// MaxContentRunes, and Chunk need only the secrets; the inbound gateway is not used by
// a CLI). It is the wiring-layer construction step that keeps the CLI's posting +
// length-guard paths behind the Transport SPI instead of calling internal/discord
// directly. The roster is not needed for a fixed-webhook Post, so it is omitted.
func outboundTransport(secrets *roster.Secrets) (transport.Transport, error) {
	return transport.Construct("", transport.Config{Secrets: secrets})
}

// transportContentCap returns the default transport's per-message content cap for the
// CLI length guards, without a live session (the discord cap is a pure constant on the
// constructed outbound transport). It replaces the leaked discord.MaxContentRunes const
// at the CLI call sites with the transport's own declared cap.
func transportContentCap() int {
	tr, err := transport.Construct("", transport.Config{})
	if err != nil {
		return 0 // unreachable: the discord factory never errors without a bot token/cursor
	}
	return tr.MaxContentRunes()
}
