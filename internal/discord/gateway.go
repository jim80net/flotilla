package discord

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// MessageHandler receives the fields the relay needs from each gateway message.
// Narrow by design, so the gateway is decoupled from the relay/watch packages.
type MessageHandler func(webhookID, authorID, content string)

// Gateway streams one Discord channel and dispatches its messages to a handler.
// It is the inbound half of the bus (the relay); the send half stays webhook-only.
type Gateway struct {
	session   *discordgo.Session
	channelID string
}

// NewGateway opens a session configured with the Guild Messages + Message
// Content intents (the latter is privileged — it must be enabled on the bot in
// the Developer Portal) and registers a MESSAGE_CREATE handler scoped to
// channelID.
func NewGateway(botToken, channelID string, handler MessageHandler) (*Gateway, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent
	s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) {
		if m.ChannelID != channelID {
			return
		}
		authorID := ""
		if m.Author != nil {
			authorID = m.Author.ID
		}
		handler(m.WebhookID, authorID, m.Content)
	})
	// Log gateway flaps so a message lost during a reconnect window is
	// explainable in the journal ("gateway disconnected 14:03 / resumed 14:03").
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		log.Printf("flotilla watch: gateway disconnected")
	})
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Resumed) {
		log.Printf("flotilla watch: gateway resumed")
	})
	return &Gateway{session: s, channelID: channelID}, nil
}

// Open connects the gateway. discordgo auto-reconnects thereafter; messages sent
// during a disconnect window are not replayed (the operator can resend).
func (g *Gateway) Open() error {
	if err := g.session.Open(); err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}
	log.Printf("flotilla watch: gateway connected (channel %s)", g.channelID)
	return nil
}

// Close shuts the gateway down gracefully (call on SIGTERM).
func (g *Gateway) Close() error { return g.session.Close() }
