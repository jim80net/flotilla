package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

// cmdInbox reads recent messages from a bound channel over the Discord REST API and
// prints them — the manual re-fetch / recovery path (#161 gap 2). When a message is
// dropped (a gateway gap the catch-up backstop also covers automatically) or the
// operator simply wants to see what was said, this reads the channel directly
// instead of hand-rolling a Discord API call with the bot token. It is READ-ONLY:
// it starts no daemon, opens no gateway websocket, and never re-injects (re-relaying
// from the CLI would bypass the gateway's operator-only Accept guard — out of scope).
func cmdInbox(args []string) error {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path (for the bot token)")
	limit := fs.Int("limit", 20, "number of recent messages to fetch (1-100)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: flotilla inbox <channel> [--limit N]\n  <channel> = a binding role or a raw channel id")
	}
	if *limit < 1 || *limit > 100 {
		return fmt.Errorf("--limit must be 1-100 (Discord's max page is 100)")
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	ch, err := resolveInboxChannel(cfg, rest[0])
	if err != nil {
		return err
	}
	token, err := botToken(*secretsPath)
	if err != nil {
		return err
	}
	client, err := discord.NewREST(token)
	if err != nil {
		return err
	}
	msgs, err := client.Recent(ch.ChannelID, *limit)
	if err != nil {
		return fmt.Errorf("fetch channel %s: %w", inboxLabel(ch), err)
	}
	writeInbox(os.Stdout, msgs, cfg.OperatorUserID, ch)
	return nil
}

// resolveInboxChannel maps a CLI <channel> arg to a bound channel: an exact
// channel-id match (always unambiguous) wins; otherwise a case-insensitive role
// match — exactly one binding with that role resolves, several is an ambiguity
// error (use a channel id), none lists the valid options.
func resolveInboxChannel(cfg *roster.Config, arg string) (roster.Channel, error) {
	bindings := cfg.Bindings()
	for _, b := range bindings {
		if b.ChannelID == arg {
			return b, nil
		}
	}
	var roleMatches []roster.Channel
	for _, b := range bindings {
		if b.Role != "" && strings.EqualFold(b.Role, arg) {
			roleMatches = append(roleMatches, b)
		}
	}
	switch len(roleMatches) {
	case 1:
		return roleMatches[0], nil
	case 0:
		return roster.Channel{}, fmt.Errorf("no bound channel %q; valid channels: %s", arg, strings.Join(inboxOptions(bindings), ", "))
	default:
		ids := make([]string, len(roleMatches))
		for i, b := range roleMatches {
			ids[i] = b.ChannelID
		}
		return roster.Channel{}, fmt.Errorf("role %q is ambiguous (%d channels: %s) — pass a channel id instead", arg, len(roleMatches), strings.Join(ids, ", "))
	}
}

// inboxOptions lists the resolvable channel labels for an error message.
func inboxOptions(bindings []roster.Channel) []string {
	opts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if b.Role != "" {
			opts = append(opts, fmt.Sprintf("%s (%s)", b.Role, b.ChannelID))
		} else {
			opts = append(opts, b.ChannelID)
		}
	}
	return opts
}

func inboxLabel(b roster.Channel) string {
	if b.Role != "" {
		return fmt.Sprintf("%s (%s)", b.Role, b.ChannelID)
	}
	return b.ChannelID
}

// writeInbox prints the messages in chronological (ascending) order. Each message
// gets a header (timestamp · authorship flag · id) and its content indented below,
// so a multi-line operator directive is fully readable and recoverable. The flag is
// [OP] for the operator, [wh] for a webhook (the audit mirror), [..] otherwise.
func writeInbox(out io.Writer, msgs []discord.Message, operatorID string, ch roster.Channel) {
	fmt.Fprintf(out, "flotilla inbox — %s (%d message(s), oldest first)\n\n", inboxLabel(ch), len(msgs))
	for _, m := range msgs {
		flag := "[..]"
		switch {
		case m.WebhookID != "":
			flag = "[wh]"
		case operatorID != "" && m.AuthorID == operatorID:
			flag = "[OP]"
		}
		fmt.Fprintf(out, "%s  %s  %s\n", m.Timestamp.Local().Format("2006-01-02 15:04:05"), flag, m.ID)
		for _, line := range strings.Split(m.Content, "\n") {
			fmt.Fprintf(out, "    %s\n", line)
		}
		fmt.Fprintln(out)
	}
}
