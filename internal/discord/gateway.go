package discord

import (
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// MessageHandler receives the fields the relay needs from each gateway message,
// including the message's ORIGIN channel id so a multi-channel relay can route by
// it. Narrow by design, so the gateway is decoupled from the relay/watch packages.
type MessageHandler func(channelID, webhookID, authorID, content string)

// Gateway streams a SET of Discord channels and dispatches their messages to a
// handler. It is the inbound half of the bus (the relay); the send half stays
// webhook-only. One bot session admits every channel in the set (federation: the
// bot must be present with the Message Content intent in each).
type Gateway struct {
	session    *discordgo.Session
	channelIDs []string
}

// NewGateway opens a session configured with the Guild Messages + Message
// Content intents (the latter is privileged — it must be enabled on the bot in
// the Developer Portal) and registers a MESSAGE_CREATE handler scoped to the SET
// of channelIDs. A message in any bound channel is dispatched with its origin
// channel id (so the relay can route by it); messages in any other channel are
// ignored.
func NewGateway(botToken string, channelIDs []string, handler MessageHandler) (*Gateway, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent
	admit := make(map[string]struct{}, len(channelIDs))
	for _, id := range channelIDs {
		admit[id] = struct{}{}
	}
	s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) {
		if _, ok := admit[m.ChannelID]; !ok {
			return
		}
		authorID := ""
		if m.Author != nil {
			authorID = m.Author.ID
		}
		handler(m.ChannelID, m.WebhookID, authorID, m.Content)
	})
	// Log gateway flaps so a message lost during a reconnect window is
	// explainable in the journal ("gateway disconnected 14:03 / resumed 14:03").
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		log.Printf("flotilla watch: gateway disconnected")
	})
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Resumed) {
		log.Printf("flotilla watch: gateway resumed")
	})
	return &Gateway{session: s, channelIDs: channelIDs}, nil
}

// Open connects the gateway. discordgo auto-reconnects thereafter; messages sent
// during a disconnect window are not replayed (the operator can resend).
func (g *Gateway) Open() error {
	if err := g.session.Open(); err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}
	log.Printf("flotilla watch: gateway connected (channels %s)", strings.Join(g.channelIDs, ", "))
	return nil
}

// Close shuts the gateway down gracefully (call on SIGTERM).
func (g *Gateway) Close() error { return g.session.Close() }
