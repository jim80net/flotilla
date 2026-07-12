package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChannelCreateArgs(t *testing.T) {
	t.Run("name before flags", func(t *testing.T) {
		o, err := parseChannelCreateArgs([]string{"fleet-command", "--topic", "ops"})
		if err != nil {
			t.Fatal(err)
		}
		if o.name != "fleet-command" || o.topic != "ops" || o.ctype != "text" {
			t.Fatalf("got %+v", o)
		}
	})
	t.Run("name after flags", func(t *testing.T) {
		o, err := parseChannelCreateArgs([]string{"--type", "category", "Alpha Group"})
		if err != nil {
			t.Fatal(err)
		}
		if o.name != "Alpha Group" || o.ctype != "category" {
			t.Fatalf("got %+v", o)
		}
	})
	t.Run("repeatable member with xo", func(t *testing.T) {
		o, err := parseChannelCreateArgs([]string{"fleet-alpha", "--xo", "alpha-xo", "--member", "d1", "--member", "d2", "--role", "project"})
		if err != nil {
			t.Fatal(err)
		}
		if o.xo != "alpha-xo" || len(o.members) != 2 || o.members[1] != "d2" || o.role != "project" {
			t.Fatalf("got %+v", o)
		}
	})
	t.Run("invalid type rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"x", "--type", "voice"})
		if err == nil || !strings.Contains(err.Error(), "text or category") {
			t.Fatalf("want type error, got %v", err)
		}
	})
	t.Run("empty/whitespace name rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"   "})
		if err == nil || !strings.Contains(err.Error(), "name is empty") {
			t.Fatalf("want empty-name error, got %v", err)
		}
	})
	t.Run("topic on a category rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"Fam", "--type", "category", "--topic", "x"})
		if err == nil || !strings.Contains(err.Error(), "only valid for text") {
			t.Fatalf("want topic-on-category error, got %v", err)
		}
	})
	t.Run("empty member rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"x", "--xo", "a", "--member", ""})
		if err == nil || !strings.Contains(err.Error(), "--member is empty") {
			t.Fatalf("want empty-member error, got %v", err)
		}
	})
	t.Run("member without xo rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"x", "--member", "d1"})
		if err == nil || !strings.Contains(err.Error(), "require --xo") {
			t.Fatalf("want require-xo error, got %v", err)
		}
	})
	t.Run("missing name rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"--topic", "x"})
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("want usage error, got %v", err)
		}
	})
	t.Run("stray positional after name rejected", func(t *testing.T) {
		_, err := parseChannelCreateArgs([]string{"a", "b"})
		if err == nil {
			t.Fatalf("want error for extra positional")
		}
	})
}

func TestParseChannelDeleteArgs(t *testing.T) {
	t.Run("id with --yes", func(t *testing.T) {
		id, _, err := parseChannelDeleteArgs([]string{"123456789012345678", "--yes"})
		if err != nil || id != "123456789012345678" {
			t.Fatalf("got (%q,%v)", id, err)
		}
	})
	t.Run("requires --yes", func(t *testing.T) {
		_, _, err := parseChannelDeleteArgs([]string{"123"})
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("want --yes error, got %v", err)
		}
	})
	t.Run("rejects non-snowflake (a name)", func(t *testing.T) {
		_, _, err := parseChannelDeleteArgs([]string{"fleet-command", "--yes"})
		if err == nil || !strings.Contains(err.Error(), "not a channel id") {
			t.Fatalf("want non-snowflake error, got %v", err)
		}
	})
	t.Run("missing id rejected", func(t *testing.T) {
		_, _, err := parseChannelDeleteArgs([]string{"--yes"})
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("want usage error, got %v", err)
		}
	})
}

// writeTemp writes content to a uniquely-named temp file and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCmdChannelCreatePrecedence exercises the command orchestration's check ORDER
// (roster/agents → bot token → guild_id) — all of which abort BEFORE any network
// call, so the risky sequencing is covered without a live Discord. The create/preflight
// network path beyond guild_id is the deliberately-live seam.
func TestCmdChannelCreatePrecedence(t *testing.T) {
	rosterGood := writeTemp(t, "flotilla.json", `{"guild_id":"100","agents":[{"name":"alpha-xo"}]}`)
	rosterNoGuild := writeTemp(t, "flotilla-ng.json", `{"agents":[{"name":"alpha-xo"}]}`)
	secretsGood := writeTemp(t, "secrets.env", "FLOTILLA_BOT_TOKEN=tok\n")
	secretsNoTok := writeTemp(t, "secrets-empty.env", "# no token here\n")

	t.Run("unknown --xo fails before the token/network", func(t *testing.T) {
		err := cmdChannelCreate([]string{"chan", "--xo", "ghost", "--roster", rosterGood, "--secrets", secretsGood})
		if err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Fatalf("want unknown-agent error, got %v", err)
		}
	})
	t.Run("missing bot token fails", func(t *testing.T) {
		err := cmdChannelCreate([]string{"chan", "--roster", rosterGood, "--secrets", secretsNoTok})
		if err == nil || !strings.Contains(err.Error(), "bot token") {
			t.Fatalf("want bot-token error, got %v", err)
		}
	})
	t.Run("missing guild_id fails (after token)", func(t *testing.T) {
		err := cmdChannelCreate([]string{"chan", "--roster", rosterNoGuild, "--secrets", secretsGood})
		if err == nil || !strings.Contains(err.Error(), "guild_id") {
			t.Fatalf("want guild_id error, got %v", err)
		}
	})
}

func TestCmdChannelUnknownSub(t *testing.T) {
	if err := cmdChannel([]string{"frobnicate"}); err == nil || !strings.Contains(err.Error(), "unknown channel subcommand") {
		t.Fatalf("want unknown-subcommand error, got %v", err)
	}
	if err := cmdChannel(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("want usage error for no args, got %v", err)
	}
}
