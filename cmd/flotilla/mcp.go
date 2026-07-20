package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type mcpNativeCommand struct {
	bin  string
	args []string
}

type mcpCommandRunner func(mcpNativeCommand) error

func cmdMCP(args []string) error {
	return cmdMCPWithRunner(args, runMCPNative)
}

func cmdMCPWithRunner(args []string, runner mcpCommandRunner) error {
	if len(args) == 0 {
		return errors.New("usage: flotilla mcp add|login")
	}
	switch args[0] {
	case "add":
		return cmdMCPAdd(args[1:], runner)
	case "login":
		return cmdMCPLogin(args[1:], runner)
	default:
		return fmt.Errorf("unknown mcp subcommand %q (try: add, login)", args[0])
	}
}

func cmdMCPAdd(args []string, runner mcpCommandRunner) error {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	harness := fs.String("harness", "claude", "target harness (claude or codex)")
	transport := fs.String("transport", "http", "MCP transport (http)")
	scope := fs.String("scope", "user", "harness configuration scope (Claude: user, local, project; Codex: user)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: flotilla mcp add [--harness claude|codex] [--transport http] [--scope user] <name> <url>")
	}
	name, endpoint := fs.Arg(0), fs.Arg(1)
	cmd, err := mcpAddNativeCommand(*harness, *transport, *scope, name, endpoint)
	if err != nil {
		return err
	}
	if err := runner(cmd); err != nil {
		return err
	}
	fmt.Printf("MCP %q registered for %s (%s).\n", name, normalizeMCPHarness(*harness), endpoint)
	fmt.Println("OAuth is not complete. The human operator must now run:")
	fmt.Printf("  flotilla mcp login --harness %s %s\n", normalizeMCPHarness(*harness), name)
	return nil
}

func cmdMCPLogin(args []string, runner mcpCommandRunner) error {
	fs := flag.NewFlagSet("mcp login", flag.ContinueOnError)
	harness := fs.String("harness", "claude", "target harness (claude or codex)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: flotilla mcp login [--harness claude|codex] <name>")
	}
	name := fs.Arg(0)
	if err := validateMCPName(name); err != nil {
		return err
	}
	h := normalizeMCPHarness(*harness)
	var cmd mcpNativeCommand
	switch h {
	case "claude":
		cmd = mcpNativeCommand{bin: "claude", args: []string{"mcp", "login", name}}
	case "codex":
		cmd = mcpNativeCommand{bin: "codex", args: []string{"mcp", "login", name}}
	default:
		return fmt.Errorf("mcp: unsupported harness %q (supported: claude, codex)", *harness)
	}
	if err := runner(cmd); err != nil {
		return err
	}
	fmt.Printf("MCP %q OAuth login completed for %s.\n", name, h)
	return nil
}

func mcpAddNativeCommand(harness, transport, scope, name, endpoint string) (mcpNativeCommand, error) {
	if err := validateMCPName(name); err != nil {
		return mcpNativeCommand{}, err
	}
	if strings.ToLower(strings.TrimSpace(transport)) != "http" {
		return mcpNativeCommand{}, fmt.Errorf("mcp: unsupported transport %q (only http is supported)", transport)
	}
	if err := validateMCPHTTPURL(endpoint); err != nil {
		return mcpNativeCommand{}, err
	}
	h := normalizeMCPHarness(harness)
	s := strings.ToLower(strings.TrimSpace(scope))
	switch h {
	case "claude":
		if s != "user" && s != "local" && s != "project" {
			return mcpNativeCommand{}, fmt.Errorf("mcp: unsupported Claude scope %q (supported: user, local, project)", scope)
		}
		return mcpNativeCommand{bin: "claude", args: []string{"mcp", "add", "--transport", "http", "--scope", s, name, endpoint}}, nil
	case "codex":
		if s != "user" {
			return mcpNativeCommand{}, fmt.Errorf("mcp: Codex HTTP MCP registration supports only user scope, got %q", scope)
		}
		return mcpNativeCommand{bin: "codex", args: []string{"mcp", "add", "--url", endpoint, name}}, nil
	default:
		return mcpNativeCommand{}, fmt.Errorf("mcp: unsupported harness %q (supported: claude, codex)", harness)
	}
}

func normalizeMCPHarness(harness string) string {
	switch strings.ToLower(strings.TrimSpace(harness)) {
	case "claude", "claude-code":
		return "claude"
	case "codex", "codex-cli":
		return "codex"
	default:
		return strings.ToLower(strings.TrimSpace(harness))
	}
}

func validateMCPName(name string) error {
	if !mcpNamePattern.MatchString(name) {
		return errors.New("mcp: name must start with an alphanumeric character and contain only letters, numbers, '.', '_', or '-'")
	}
	return nil
}

func validateMCPHTTPURL(raw string) error {
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("mcp: URL must be an absolute http:// or https:// endpoint")
	}
	if u.User != nil {
		return errors.New("mcp: URL must not contain credentials; use harness-owned OAuth login")
	}
	if u.RawQuery != "" {
		return errors.New("mcp: URL query parameters are not accepted; use harness-owned OAuth login instead of URL credentials")
	}
	if u.Fragment != "" {
		return errors.New("mcp: URL must not contain a fragment")
	}
	if u.Scheme == "http" && !isLoopbackMCPHost(u.Hostname()) {
		return errors.New("mcp: remote HTTP MCP endpoints must use https (plaintext http is limited to loopback)")
	}
	return nil
}

func isLoopbackMCPHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runMCPNative(native mcpNativeCommand) error {
	path, err := exec.LookPath(native.bin)
	if err != nil {
		return fmt.Errorf("mcp: %s CLI not found in PATH", native.bin)
	}
	cmd := exec.Command(path, native.args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mcp: %s command failed: %w", native.bin, err)
	}
	return nil
}
