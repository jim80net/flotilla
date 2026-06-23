package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

func inboxCfg() *roster.Config {
	return &roster.Config{
		OperatorUserID: "op",
		Agents: []roster.Agent{
			{Name: "meta-xo"}, {Name: "alpha-xo"}, {Name: "alpha-be"}, {Name: "beta-xo"}, {Name: "beta-be"},
		},
		Channels: []roster.Channel{
			{Role: "fleet-command", ChannelID: "C_CMD", XOAgent: "meta-xo", Members: []string{"alpha-xo", "beta-xo"}},
			{Role: "project", ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"alpha-be"}},
			{Role: "project", ChannelID: "C_BETA", XOAgent: "beta-xo", Members: []string{"beta-be"}},
		},
	}
}

func TestResolveInboxChannel_ByID(t *testing.T) {
	ch, err := resolveInboxChannel(inboxCfg(), "C_ALPHA")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ch.ChannelID != "C_ALPHA" {
		t.Fatalf("resolved %q, want C_ALPHA", ch.ChannelID)
	}
}

func TestResolveInboxChannel_ByUniqueRole(t *testing.T) {
	ch, err := resolveInboxChannel(inboxCfg(), "Fleet-Command") // case-insensitive
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ch.ChannelID != "C_CMD" {
		t.Fatalf("resolved %q, want C_CMD", ch.ChannelID)
	}
}

func TestResolveInboxChannel_AmbiguousRole(t *testing.T) {
	_, err := resolveInboxChannel(inboxCfg(), "project") // two channels share role "project"
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v, want an ambiguity error naming both channels", err)
	}
	if !strings.Contains(err.Error(), "C_ALPHA") || !strings.Contains(err.Error(), "C_BETA") {
		t.Fatalf("ambiguity error should list both ids, got: %v", err)
	}
}

func TestResolveInboxChannel_UnknownListsOptions(t *testing.T) {
	_, err := resolveInboxChannel(inboxCfg(), "nope")
	if err == nil {
		t.Fatal("want error for unknown channel")
	}
	for _, want := range []string{"fleet-command", "C_CMD", "C_ALPHA"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should list valid options incl %q, got: %v", want, err)
		}
	}
}

// cubic #162 P2: --limit must work AFTER the positional <channel> (the documented
// order), not only before it.
func TestParseInboxArgs_FlagAfterPositional(t *testing.T) {
	opts, err := parseInboxArgs([]string{"fleet-command", "--limit", "30"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if opts.channel != "fleet-command" || opts.limit != 30 {
		t.Fatalf("got channel=%q limit=%d, want fleet-command/30", opts.channel, opts.limit)
	}
}

func TestParseInboxArgs_FlagBeforePositional(t *testing.T) {
	opts, err := parseInboxArgs([]string{"--limit", "30", "fleet-command"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if opts.channel != "fleet-command" || opts.limit != 30 {
		t.Fatalf("got channel=%q limit=%d, want fleet-command/30", opts.channel, opts.limit)
	}
}

func TestParseInboxArgs_ChannelOnlyDefaultLimit(t *testing.T) {
	opts, err := parseInboxArgs([]string{"C_ALPHA"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if opts.channel != "C_ALPHA" || opts.limit != 20 {
		t.Fatalf("got channel=%q limit=%d, want C_ALPHA/20 (default)", opts.channel, opts.limit)
	}
}

func TestParseInboxArgs_Errors(t *testing.T) {
	cases := map[string][]string{
		"no channel":         {"--limit", "5"},
		"two positionals":    {"a", "b"},
		"limit out of range": {"x", "--limit", "0"},
		"limit too high":     {"x", "--limit", "9999"},
	}
	for name, args := range cases {
		if _, err := parseInboxArgs(args); err == nil {
			t.Errorf("%s: expected an error for %v", name, args)
		}
	}
}

func TestWriteInbox_FlagsAndOrder(t *testing.T) {
	ts := time.Date(2026, 6, 22, 20, 54, 40, 0, time.UTC)
	msgs := []discord.Message{
		{ID: "10", AuthorID: "op", Content: "first directive", Timestamp: ts},
		{ID: "20", AuthorID: "someone", Content: "chatter", Timestamp: ts.Add(time.Minute)},
		{ID: "30", AuthorID: "", WebhookID: "wh1", Content: "→ mirror echo", Timestamp: ts.Add(2 * time.Minute)},
		{ID: "40", AuthorID: "op", Content: "line one\nline two", Timestamp: ts.Add(3 * time.Minute)},
	}
	var buf bytes.Buffer
	ch := roster.Channel{Role: "fleet-command", ChannelID: "C_CMD"}
	writeInbox(&buf, msgs, "op", ch)
	out := buf.String()

	if !strings.Contains(out, "fleet-command (C_CMD)") {
		t.Errorf("missing channel header: %s", out)
	}
	// flags
	if !strings.Contains(out, "[OP]  10") {
		t.Errorf("operator message should be flagged [OP]: %s", out)
	}
	if !strings.Contains(out, "[..]  20") {
		t.Errorf("non-operator message should be flagged [..]: %s", out)
	}
	if !strings.Contains(out, "[wh]  30") {
		t.Errorf("webhook message should be flagged [wh]: %s", out)
	}
	// multi-line content preserved + indented
	if !strings.Contains(out, "    line one\n    line two") {
		t.Errorf("multi-line content should be indented per line: %s", out)
	}
	// chronological order: 10 before 40
	if strings.Index(out, "[OP]  10") > strings.Index(out, "[OP]  40") {
		t.Errorf("messages not in chronological order: %s", out)
	}
}
