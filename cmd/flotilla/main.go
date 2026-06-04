// Command flotilla coordinates a fleet of AI coding agents: it delivers an
// instruction into a target agent's tmux pane (the delivery IS the wake) and
// mirrors it to the Discord audit channel under the sender's identity.
package main

import (
	"flag"
	"fmt"
	"io"
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
	case "notify":
		return cmdNotify(args[1:])
	case "watch":
		return cmdWatch(args[1:])
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
  flotilla watch                                      relay + XO heartbeat clock daemon
  flotilla version
  flotilla help

flags for 'send':
  --from <name>     sender identity (default $FLOTILLA_SELF)
  --file <path>     read the message body from a file ('-' for stdin) instead of
                    the command line — avoids shell quoting of multi-line bodies
  --roster <path>   roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>  secrets env file (default $FLOTILLA_SECRETS)
  --no-mirror       deliver via tmux only; skip the Discord audit post

The Discord audit mirror is best-effort: if it is unconfigured or fails, the
message is still delivered and the command succeeds (with a warning), so a
retry never double-delivers.

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

flags for 'watch':
  --roster <path>         roster config (default ./flotilla.json or $FLOTILLA_ROSTER)
  --secrets <path>        secrets env file: relay bot token + down-alert webhook (default $FLOTILLA_SECRETS)
  --ack-file <path>       XO liveness ack file the XO touches (default $FLOTILLA_ACK_FILE, else <roster-dir>/flotilla-xo-alive)
  --max-missed-acks <n>   consecutive missed acks before a down-alert (default 3)

watch runs the XO heartbeat clock + liveness watchdog (needs neither Discord nor
secrets), and adds the inbound relay when channel_id + operator_user_id + a bot
token are configured. The heartbeat target and interval come from the roster
(xo_agent, heartbeat_interval).`)
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
	noMirror := fs.Bool("no-mirror", false, "skip the Discord audit mirror")
	if err := fs.Parse(args); err != nil {
		return err
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
