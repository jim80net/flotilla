package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestMCPAddClaudeHTTP(t *testing.T) {
	var got mcpNativeCommand
	err := cmdMCPWithRunner([]string{"add", "--harness", "claude", "--transport", "http", "--scope", "user", "higgsfield", "https://mcp.higgsfield.ai/mcp"}, func(cmd mcpNativeCommand) error {
		got = cmd
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := mcpNativeCommand{bin: "claude", args: []string{"mcp", "add", "--transport", "http", "--scope", "user", "higgsfield", "https://mcp.higgsfield.ai/mcp"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native command = %#v, want %#v", got, want)
	}
}

func TestMCPAddCodexHTTP(t *testing.T) {
	var got mcpNativeCommand
	err := cmdMCPWithRunner([]string{"add", "--harness", "codex", "higgsfield", "https://mcp.higgsfield.ai/mcp"}, func(cmd mcpNativeCommand) error {
		got = cmd
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := mcpNativeCommand{bin: "codex", args: []string{"mcp", "add", "--url", "https://mcp.higgsfield.ai/mcp", "higgsfield"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native command = %#v, want %#v", got, want)
	}
}

func TestMCPLoginUsesHarnessOAuth(t *testing.T) {
	tests := []struct {
		harness string
		want    mcpNativeCommand
	}{
		{harness: "claude-code", want: mcpNativeCommand{bin: "claude", args: []string{"mcp", "login", "higgsfield"}}},
		{harness: "codex-cli", want: mcpNativeCommand{bin: "codex", args: []string{"mcp", "login", "higgsfield"}}},
	}
	for _, tt := range tests {
		t.Run(tt.harness, func(t *testing.T) {
			var got mcpNativeCommand
			if err := cmdMCPWithRunner([]string{"login", "--harness", tt.harness, "higgsfield"}, func(cmd mcpNativeCommand) error {
				got = cmd
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("native command = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMCPAddRejectsUnsafeOrUnsupportedInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "name", args: []string{"add", "bad name", "https://example.com/mcp"}, want: "name must"},
		{name: "transport", args: []string{"add", "--transport", "stdio", "safe", "https://example.com/mcp"}, want: "only http"},
		{name: "credentials", args: []string{"add", "safe", "https://user:secret@example.com/mcp"}, want: "must not contain credentials"},
		{name: "query credentials", args: []string{"add", "safe", "https://example.com/mcp?token=secret"}, want: "query parameters are not accepted"},
		{name: "plain remote", args: []string{"add", "safe", "http://example.com/mcp"}, want: "must use https"},
		{name: "codex scope", args: []string{"add", "--harness", "codex", "--scope", "project", "safe", "https://example.com/mcp"}, want: "only user scope"},
		{name: "harness", args: []string{"add", "--harness", "unknown", "safe", "https://example.com/mcp"}, want: "unsupported harness"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmdMCPWithRunner(tt.args, func(mcpNativeCommand) error {
				t.Fatal("runner called for invalid input")
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestMCPAddAllowsLoopbackHTTP(t *testing.T) {
	if _, err := mcpAddNativeCommand("claude", "http", "user", "local", "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatal(err)
	}
}
