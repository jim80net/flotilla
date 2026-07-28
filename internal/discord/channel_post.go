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
	inspect     func(channelID string) (*discordgo.Channel, error)
	permissions func(channel *discordgo.Channel) (int64, error)
	send        func(channelID, content string) (*discordgo.Message, error)
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
	bot, err := sess.User("@me")
	if err != nil {
		return nil, fmt.Errorf("discord channel poster: inspect bot identity: %w", err)
	}
	if bot == nil || strings.TrimSpace(bot.ID) == "" {
		return nil, fmt.Errorf("discord channel poster: bot identity has no id")
	}
	return &ChannelPoster{
		inspect: func(channelID string) (*discordgo.Channel, error) {
			return sess.Channel(channelID)
		},
		permissions: func(channel *discordgo.Channel) (int64, error) {
			channelID := channel.ID
			if channel.IsThread() {
				if strings.TrimSpace(channel.ParentID) == "" {
					return 0, fmt.Errorf("thread %s has no parent channel", channel.ID)
				}
				channelID = channel.ParentID
			}
			return sess.UserChannelPermissions(bot.ID, channelID)
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

// Verify confirms, without posting, that the relay bot can resolve the exact
// target and currently holds the effective permission needed to send there.
// Permission changes or transport failures can still occur after preflight, so
// callers must surface send failures rather than automatically retrying.
func (p *ChannelPoster) Verify(channelID string) error {
	if p == nil || p.inspect == nil || p.permissions == nil {
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
	required, err := requiredChannelSendPermissions(channel)
	if err != nil {
		return fmt.Errorf("discord channel poster: channel %s is not message-capable: %w", channelID, err)
	}
	effective, err := p.permissions(channel)
	if err != nil {
		return fmt.Errorf("discord channel poster: resolve effective permissions for %s: %w", channelID, err)
	}
	if effective&required != required {
		return fmt.Errorf("discord channel poster: channel %s lacks effective message-send permission", channelID)
	}
	return nil
}

func requiredChannelSendPermissions(channel *discordgo.Channel) (int64, error) {
	if channel == nil {
		return 0, fmt.Errorf("channel is nil")
	}
	switch channel.Type {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
		return discordgo.PermissionViewChannel | discordgo.PermissionSendMessages, nil
	case discordgo.ChannelTypeGuildNewsThread,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread:
		if channel.ThreadMetadata == nil {
			return 0, fmt.Errorf("thread metadata is unavailable")
		}
		if channel.ThreadMetadata.Archived || channel.ThreadMetadata.Locked {
			return 0, fmt.Errorf("thread is archived or locked")
		}
		return discordgo.PermissionViewChannel | discordgo.PermissionSendMessagesInThreads, nil
	default:
		return 0, fmt.Errorf("unsupported Discord channel type %d", channel.Type)
	}
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
