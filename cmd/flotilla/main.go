// Command flotilla coordinates a fleet of AI coding agents: it delivers an
// instruction into a target agent's tmux pane (the delivery IS the wake) and
// mirrors it to the Discord audit channel under the sender's identity.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/voice"
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
	case "notify":
		return cmdNotify(args[1:])
	case "speak":
		return cmdSpeak(args[1:])
	case "voice":
		return cmdVoice(args[1:])
	case "watch":
		return cmdWatch(args[1:])
	case "register":
		return cmdRegister(args[1:])
	case "resume":
		return cmdResume(args[1:])
	case "workspace":
		return cmdWorkspace(args[1:])
	case "push-snippet":
		return cmdPushSnippet(args[1:])
	case "result":
		return cmdResult(args[1:])
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
  flotilla notify --from <agent> <message>            post to the operator under <agent>'s webhook (no tmux)
  flotilla notify --from <agent> --file <path>        notify body from a file ('-' = stdin)
  flotilla speak <text>                               drop a short spoken reply on the voice outbound spool (non-blocking)
  flotilla speak --file <path>                         speak body from a file ('-' = stdin)
  flotilla voice [--config <voice.env>]               operator↔XO Discord voice (needs a -tags voiceopus build)
  flotilla watch                                      relay + XO heartbeat clock daemon
  flotilla register <agent> [--pane <target>]         tag a pane so it resolves by a stable, drift-immune marker
  flotilla resume <agent> [--launch <path>] [--force]  (re)start a dead desk from its host-local launch recipe
  flotilla workspace init <agent>                     scaffold the per-agent ~/.flotilla/<agent>/ home
  flotilla workspace path <agent>                     print an agent's workspace directory
  flotilla push-snippet <desk-agent>                  print the smart-push convention to append to a non-claude desk's identity file (secret-free; reports to the XO via send)
  flotilla result <agent>                             print a desk's FULL latest result from its harness session store (grok; read-only) — for long results the pane capture truncates
  flotilla version
  flotilla help

flags for 'send':
  --from <name>     sender identity (default $FLOTILLA_SELF)
  --file <path>     read the message body from a file ('-' for stdin) instead of
                    the command line — avoids shell quoting of multi-line bodies
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)
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
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)

notify is the operator-facing outbound path: it posts <message> directly to the
operator on Discord, under the <agent>'s own webhook identity, and does NOT
inject into any tmux pane. Use it when an agent (typically the XO) wants to
reach the operator — as opposed to 'send', which wakes another agent's pane and
mirrors the wake to the audit channel. The message must be ≤ 2000 characters
(Discord's hard limit); a longer body is rejected (nothing is posted).

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
  --max-quiet-intervals <n>   liveness ping cadence N, in intervals (0 ⇒ ping-mode default)
  --max-self-continuations <n> cap on consecutive XO self-continuations with no external change (default 3)

watch runs the XO clock + liveness watchdog (needs neither Discord nor secrets),
and adds the inbound relay when channel_id + operator_user_id + a bot token are
configured. The clock target and interval come from the roster (xo_agent,
heartbeat_interval). By default the clock is the legacy always-wake heartbeat;
set change_detector: true (with liveness_ping_mode none|interval|consecutive) to
wake the XO only on a material change — an idle fleet then costs nothing.

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

	cfg, err := roster.Load(*rosterPath)
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
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if err := confirm.Submit(drv, pane, message); err != nil {
		switch {
		case errors.Is(err, surface.ErrBusy):
			return fmt.Errorf("%s is busy (mid-turn) — NOT delivered; retry when it is idle", agentName)
		case errors.Is(err, surface.ErrTransient):
			return fmt.Errorf("%s pane state is uncertain — NOT delivered; retry", agentName)
		case errors.Is(err, surface.ErrCrashed):
			return fmt.Errorf("%s is at a shell (crashed) — NOT delivered", agentName)
		default: // ErrUnconfirmed, or a paste/lock error
			return fmt.Errorf("delivery to %s could not be confirmed: %w", agentName, err)
		}
	}
	fmt.Printf("delivered to %s (pane %s) — turn confirmed\n", agentName, pane)

	// Mirror to the Discord audit channel under the sender's identity. Inter-agent
	// mirroring is DEFAULT-OFF (it cluttered the operator's Discord); precedence is
	// --no-mirror (off) → --mirror (on) → roster mirror_inter_agent (default false).
	// A mirror failure (or absence) is a warning, not a command failure — the message
	// is already delivered, and failing here would tempt a retry into a double-send.
	if !shouldMirror(*noMirror, *doMirror, cfg.MirrorInterAgent) {
		return nil
	}
	if n := len([]rune(message)); n > discord.MaxContentRunes {
		fmt.Fprintf(os.Stderr, "flotilla: note — message is %d chars; the Discord audit copy is truncated to %d (the full message WAS delivered)\n", n, discord.MaxContentRunes)
	}
	if err := mirror(*secretsPath, *from, agentName, message); err != nil {
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
	if err := fs.Parse(args); err != nil {
		return err
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
	message, err := resolveMessage(*file, rest, os.Stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("message is empty")
	}
	// Unlike the best-effort audit mirror (which truncates), this message IS the
	// operator-facing content. Reject an over-length body cleanly rather than
	// silently dropping the tail — the operator must see the whole message.
	if n := len([]rune(message)); n > discord.MaxContentRunes {
		return fmt.Errorf("message is %d chars; Discord's limit is %d — shorten it or split it (nothing was posted)", n, discord.MaxContentRunes)
	}

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
	if err := discord.Post(hook, *from, message); err != nil {
		return err
	}
	fmt.Printf("notified operator as %s\n", *from)
	return nil
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
