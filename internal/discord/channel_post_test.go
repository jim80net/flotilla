package discord

import (
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestChannelPosterPostsExactChannelAndContent(t *testing.T) {
	var gotChannel, gotContent string
	p := &ChannelPoster{send: func(channelID, content string) (*discordgo.Message, error) {
		gotChannel, gotContent = channelID, content
		return &discordgo.Message{ID: "1"}, nil
	}}
	if err := p.Post("C_SECONDARY", "rollup"); err != nil {
		t.Fatal(err)
	}
	if gotChannel != "C_SECONDARY" || gotContent != "rollup" {
		t.Fatalf("post = (%q, %q), want (C_SECONDARY, rollup)", gotChannel, gotContent)
	}
}

func TestChannelPosterVerifiesExactChannelWithoutPosting(t *testing.T) {
	var inspected string
	p := &ChannelPoster{inspect: func(channelID string) (*discordgo.Channel, error) {
		inspected = channelID
		return &discordgo.Channel{ID: channelID}, nil
	}}
	if err := p.Verify("C_SECONDARY"); err != nil {
		t.Fatal(err)
	}
	if inspected != "C_SECONDARY" {
		t.Fatalf("inspected = %q", inspected)
	}
}

func TestChannelPosterRejectsEmptyAndSurfacesFailure(t *testing.T) {
	p := &ChannelPoster{send: func(string, string) (*discordgo.Message, error) {
		return nil, errors.New("denied")
	}}
	if err := p.Post("", "rollup"); err == nil {
		t.Fatal("empty channel = nil error")
	}
	if err := p.Post("C", ""); err == nil {
		t.Fatal("empty content = nil error")
	}
	if err := p.Post("C", "rollup"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("API failure = %v, want surfaced denial", err)
	}
}
