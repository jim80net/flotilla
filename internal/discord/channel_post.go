package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// ChannelPoster is the narrow authenticated REST path for posting to a channel
// that has no dedicated webhook. It deliberately does not open the gateway.
// Visibility synthesis uses it only for secondary owned channels; the owner's
// home channel keeps the seat's webhook identity.
type ChannelPoster struct {
	inspect func(channelID string) (*discordgo.Channel, error)
	send    func(channelID, content string) (*discordgo.Message, error)
}

// NewChannelPoster constructs a bot-authenticated, REST-only channel poster.
func NewChannelPoster(botToken string) (*ChannelPoster, error) {
	if strings.TrimSpace(botToken) == "" {
		return nil, fmt.Errorf("discord channel poster: bot token is empty")
	}
	sess, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord channel poster: session: %w", err)
	}
	sess.ShouldRetryOnRateLimit = false
	return &ChannelPoster{
		inspect: func(channelID string) (*discordgo.Channel, error) {
			return sess.Channel(channelID)
		},
		send: func(channelID, content string) (*discordgo.Message, error) {
			return sess.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content: content,
				AllowedMentions: &discordgo.MessageAllowedMentions{
					Parse: []discordgo.AllowedMentionType{},
				},
			})
		},
	}, nil
}

// Verify confirms, without posting, that the relay bot can resolve the target
// channel. Discord still performs the final send-time permission check, but this
// catches a missing/inaccessible channel before any synthesis destination is
// mutated.
func (p *ChannelPoster) Verify(channelID string) error {
	if p == nil || p.inspect == nil {
		return fmt.Errorf("discord channel poster is not initialized")
	}
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("discord channel poster: channel id is empty")
	}
	channel, err := p.inspect(channelID)
	if err != nil {
		return fmt.Errorf("discord channel poster: verify channel %s: %w", channelID, err)
	}
	if channel == nil || channel.ID != channelID {
		return fmt.Errorf("discord channel poster: verification returned the wrong channel for %s", channelID)
	}
	return nil
}

// Post sends one message to an explicit channel id. The caller must enforce
// Discord's content limit before calling; this method validates the destination
// and reports API failures without retrying into a possible duplicate.
func (p *ChannelPoster) Post(channelID, content string) error {
	if p == nil || p.send == nil {
		return fmt.Errorf("discord channel poster is not initialized")
	}
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("discord channel poster: channel id is empty")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("discord channel poster: content is empty")
	}
	if _, err := p.send(channelID, content); err != nil {
		return fmt.Errorf("discord channel poster: channel %s: %w", channelID, err)
	}
	return nil
}
