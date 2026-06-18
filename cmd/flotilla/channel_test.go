package main

import (
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
		o, err := parseChannelCreateArgs([]string{"--type", "category", "Family Office"})
		if err != nil {
			t.Fatal(err)
		}
		if o.name != "Family Office" || o.ctype != "category" {
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

func TestCmdChannelUnknownSub(t *testing.T) {
	if err := cmdChannel([]string{"frobnicate"}); err == nil || !strings.Contains(err.Error(), "unknown channel subcommand") {
		t.Fatalf("want unknown-subcommand error, got %v", err)
	}
	if err := cmdChannel(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("want usage error for no args, got %v", err)
	}
}
