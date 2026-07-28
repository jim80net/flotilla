package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

type synthesisChannelPoster interface {
	Verify(channelID string) error
	Post(channelID, content string) error
}

type synthesisPublishDeps struct {
	webhookChannel func(webhookURL string) (string, error)
	webhookPost    func(webhookURL, username, content string) error
	newBot         func(token string) (synthesisChannelPoster, error)
	stdout         io.Writer
}

func defaultSynthesisPublishDeps() synthesisPublishDeps {
	return synthesisPublishDeps{
		webhookChannel: discord.WebhookChannel,
		webhookPost:    discord.Post,
		newBot: func(token string) (synthesisChannelPoster, error) {
			return discord.NewChannelPoster(token)
		},
		stdout: os.Stdout,
	}
}

func cmdSynthesis(args []string) error {
	if len(args) == 0 || args[0] != "publish" {
		return fmt.Errorf("usage: flotilla synthesis publish --from <agent> [--file <path>|<message>]")
	}
	return cmdSynthesisPublish(args[1:], defaultSynthesisPublishDeps())
}

// cmdSynthesisPublish delivers a curated synthesis once to every unique channel
// owned by the publishing XO. The command inspects which owned channel the XO's
// webhook is actually bound to; every other owned channel uses the authenticated
// relay bot because a webhook is bound to exactly one Discord channel. Ordinary
// operator notify is intentionally not reused: it remains a single-destination
// operator path.
func cmdSynthesisPublish(args []string, deps synthesisPublishDeps) error {
	fs := flag.NewFlagSet("synthesis publish", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "publishing XO")
	file := fs.String("file", "", "read synthesis from this file ('-' for stdin)")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	chunk := fs.Bool("chunk", false, "split an over-limit synthesis into sequential messages")
	dryRun := fs.Bool("dry-run", false, "validate and print the delivery routes without posting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*from) == "" {
		return fmt.Errorf("--from is required (or set $FLOTILLA_SELF)")
	}
	if *file == "-" {
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("--file - requires piped stdin, but stdin is a terminal")
		}
	}
	message, err := resolveMessage(*file, fs.Args(), os.Stdin)
	if err != nil {
		return err
	}
	message = strings.TrimSpace(inbound.StripDispatchFooter(message))
	if message == "" {
		return fmt.Errorf("synthesis is empty")
	}

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	if _, err := cfg.Agent(*from); err != nil {
		return err
	}
	owned := uniqueNonEmpty(cfg.OwnedChannels(*from))
	if len(owned) == 0 {
		return fmt.Errorf("synthesis publish: %q owns no channel in roster %s", *from, *rosterPath)
	}
	if strings.TrimSpace(*secretsPath) == "" {
		return fmt.Errorf("secrets unset (set --secrets or $FLOTILLA_SECRETS)")
	}
	secrets, err := roster.LoadSecrets(*secretsPath)
	if err != nil {
		return err
	}
	seatWebhook, err := secrets.Webhook(*from)
	if err != nil {
		return fmt.Errorf("synthesis publish: no seat webhook for %q: %w", *from, err)
	}
	webhookChannel, err := deps.webhookChannel(seatWebhook)
	if err != nil {
		return fmt.Errorf("synthesis publish: inspect seat webhook binding: %w (nothing was posted)", err)
	}
	if !containsString(owned, webhookChannel) {
		return fmt.Errorf("synthesis publish: seat webhook resolves to channel %s, which %q does not own; nothing was posted", webhookChannel, *from)
	}
	botToken := secrets.BotToken()
	if len(owned) > 1 && strings.TrimSpace(botToken) == "" {
		return fmt.Errorf("synthesis publish: %q owns %d channels but no relay bot token is configured; nothing was posted", *from, len(owned))
	}

	prefix := fmt.Sprintf("**Visibility synthesis · %s**\n\n", *from)
	parts, err := synthesisParts(message, prefix, *chunk, transportContentCap())
	if err != nil {
		return err
	}
	// Build every required client before the first external write. Secondary
	// destinations go first so a relay failure cannot leave only the operator/home
	// channel updated and tempt a retry that duplicates that visible post.
	var bot synthesisChannelPoster
	if len(owned) > 1 {
		bot, err = deps.newBot(botToken)
		if err != nil {
			return fmt.Errorf("synthesis publish: relay preflight: %w (nothing was posted)", err)
		}
		for _, channelID := range owned {
			if channelID == webhookChannel {
				continue
			}
			if err := bot.Verify(channelID); err != nil {
				return fmt.Errorf("synthesis publish: relay cannot access owned channel %s: %w (nothing was posted)", channelID, err)
			}
		}
	}
	if *dryRun {
		fmt.Fprintf(deps.stdout, "synthesis routes for %s: %d unique channel(s)\n", *from, len(owned))
		for _, channelID := range owned {
			if channelID == webhookChannel {
				fmt.Fprintf(deps.stdout, "  %s via seat webhook (verified binding)\n", channelID)
			} else {
				fmt.Fprintf(deps.stdout, "  %s via relay bot (verified access)\n", channelID)
			}
		}
		return nil
	}
	for _, channelID := range owned {
		if channelID == webhookChannel {
			continue
		}
		for i, part := range parts {
			body := prefix + numberedPart(part, i, len(parts))
			if err := bot.Post(channelID, body); err != nil {
				return fmt.Errorf("synthesis publish: secondary channel %s part %d/%d failed; home channel was not posted: %w", channelID, i+1, len(parts), err)
			}
			if i < len(parts)-1 {
				time.Sleep(400 * time.Millisecond)
			}
		}
	}
	for i, part := range parts {
		if err := deps.webhookPost(seatWebhook, *from, numberedPart(part, i, len(parts))); err != nil {
			return fmt.Errorf("synthesis publish: webhook channel %s part %d/%d failed after relay delivery: %w", webhookChannel, i+1, len(parts), err)
		}
		if i < len(parts)-1 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	fmt.Fprintf(deps.stdout, "published synthesis as %s to %d unique channel(s) (%d part(s), %d chars)\n", *from, len(owned), len(parts), utf8.RuneCountInString(message))
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func synthesisParts(message, botPrefix string, chunk bool, cap int) ([]string, error) {
	if cap <= 0 {
		return []string{message}, nil
	}
	botBudget := cap - utf8.RuneCountInString(botPrefix)
	if botBudget < 1 {
		return nil, fmt.Errorf("synthesis identity prefix exceeds transport limit")
	}
	if !chunk {
		if n := utf8.RuneCountInString(message); n > botBudget {
			return nil, fmt.Errorf("synthesis is %d chars; multi-channel limit is %d — shorten it or pass --chunk (nothing was posted)", n, botBudget)
		}
		return []string{message}, nil
	}
	// Reserve stable headroom for the largest practical (i/N) prefix.
	budget := botBudget - 24
	if budget < 1 {
		return nil, fmt.Errorf("transport limit is too small for chunk metadata")
	}
	return transport.Chunk(message, budget), nil
}

func numberedPart(part string, i, total int) string {
	if total <= 1 {
		return part
	}
	return fmt.Sprintf("(%d/%d)\n%s", i+1, total, part)
}
