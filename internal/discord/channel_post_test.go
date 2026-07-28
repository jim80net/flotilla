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
	p := &ChannelPoster{
		inspect: func(channelID string) (*discordgo.Channel, error) {
			inspected = channelID
			return &discordgo.Channel{ID: channelID, Type: discordgo.ChannelTypeGuildText}, nil
		},
		permissions: func(*discordgo.Channel) (int64, error) {
			return discordgo.PermissionViewChannel | discordgo.PermissionSendMessages, nil
		},
	}
	if err := p.Verify("C_SECONDARY"); err != nil {
		t.Fatal(err)
	}
	if inspected != "C_SECONDARY" {
		t.Fatalf("inspected = %q", inspected)
	}
}

func TestChannelPosterRejectsViewOnlyChannelBeforePosting(t *testing.T) {
	p := &ChannelPoster{
		inspect: func(channelID string) (*discordgo.Channel, error) {
			return &discordgo.Channel{ID: channelID, Type: discordgo.ChannelTypeGuildText}, nil
		},
		permissions: func(*discordgo.Channel) (int64, error) {
			return discordgo.PermissionViewChannel, nil
		},
		send: func(string, string) (*discordgo.Message, error) {
			t.Fatal("view-only channel must not be posted")
			return nil, nil
		},
	}
	if err := p.Verify("C_VIEW_ONLY"); err == nil || !strings.Contains(err.Error(), "message-send permission") {
		t.Fatalf("view-only Verify = %v, want effective send-permission rejection", err)
	}
}

func TestChannelPosterRequiresThreadSendPermissionAndOpenThread(t *testing.T) {
	channel := &discordgo.Channel{
		ID: "T_SECONDARY", ParentID: "C_PARENT",
		Type:           discordgo.ChannelTypeGuildPublicThread,
		ThreadMetadata: &discordgo.ThreadMetadata{},
	}
	p := &ChannelPoster{
		inspect: func(string) (*discordgo.Channel, error) { return channel, nil },
		permissions: func(got *discordgo.Channel) (int64, error) {
			if got != channel {
				t.Fatal("permission preflight received the wrong channel")
			}
			return discordgo.PermissionViewChannel | discordgo.PermissionSendMessagesInThreads, nil
		},
	}
	if err := p.Verify(channel.ID); err != nil {
		t.Fatalf("open sendable thread Verify: %v", err)
	}
	channel.ThreadMetadata.Archived = true
	if err := p.Verify(channel.ID); err == nil || !strings.Contains(err.Error(), "archived or locked") {
		t.Fatalf("archived thread Verify = %v, want rejection", err)
	}
}

func TestChannelPosterRejectsNonMessageChannelType(t *testing.T) {
	p := &ChannelPoster{
		inspect: func(channelID string) (*discordgo.Channel, error) {
			return &discordgo.Channel{ID: channelID, Type: discordgo.ChannelTypeGuildCategory}, nil
		},
		permissions: func(*discordgo.Channel) (int64, error) {
			t.Fatal("unsupported channel type must fail before permission lookup")
			return 0, nil
		},
	}
	if err := p.Verify("C_CATEGORY"); err == nil || !strings.Contains(err.Error(), "not message-capable") {
		t.Fatalf("category Verify = %v, want message-capable rejection", err)
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
