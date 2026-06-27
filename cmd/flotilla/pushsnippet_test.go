package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func mixedRoster() *roster.Config {
	return &roster.Config{
		XOAgent: "xo",
		Agents: []roster.Agent{
			{Name: "xo"},                          // the XO (claude-code)
			{Name: "oc-dev", Surface: "opencode"}, // a non-claude desk
			{Name: "pair", Surface: "aider"},      // a non-claude desk
		},
	}
}

func TestBuildPushSnippet(t *testing.T) {
	cfg := mixedRoster()
	out, err := buildPushSnippet(cfg, "oc-dev")
	if err != nil {
		t.Fatalf("buildPushSnippet: %v", err)
	}

	// The push command is `flotilla send` to the XO, with the desk's + XO's names filled.
	if !strings.Contains(out, "flotilla send --from oc-dev xo") {
		t.Errorf("snippet must instruct `flotilla send --from <desk> <xo>`; got:\n%s", out)
	}
	// It names the desk's native identity file (opencode → AGENTS.md).
	if !strings.Contains(out, "AGENTS.md") {
		t.Errorf("snippet must name the desk's identity file (opencode → AGENTS.md); got:\n%s", out)
	}
	// It tells the desk NOT to use notify / secrets (the security boundary), and warns
	// against provisioning $FLOTILLA_SECRETS.
	if !strings.Contains(out, "Do NOT run \"flotilla notify\"") {
		t.Error("snippet must instruct the desk NOT to use flotilla notify")
	}
	if !strings.Contains(out, "$FLOTILLA_SECRETS") {
		t.Error("snippet must warn against provisioning $FLOTILLA_SECRETS to the desk")
	}

	// THE SECURITY INVARIANT: the output cannot contain a secret — buildPushSnippet only
	// takes the roster (no secrets param). Assert no webhook URL / bot-token shape leaks.
	for _, leak := range []string{"discord.com/api/webhooks", "FLOTILLA_BOT_TOKEN", "FLOTILLA_WEBHOOK_", "Bot "} {
		if strings.Contains(out, leak) {
			t.Errorf("snippet leaked a secret-shaped token %q:\n%s", leak, out)
		}
	}

	// The aider desk resolves to CONVENTIONS.md (per-surface identity file).
	outAider, err := buildPushSnippet(cfg, "pair")
	if err != nil {
		t.Fatalf("buildPushSnippet(aider): %v", err)
	}
	if !strings.Contains(outAider, "CONVENTIONS.md") {
		t.Errorf("aider desk snippet must name CONVENTIONS.md; got:\n%s", outAider)
	}
}

func TestBuildPushSnippetRejectsNonRosterDeskAndXO(t *testing.T) {
	cfg := mixedRoster()
	// LOW-3: a desk not in the roster is rejected (a provisioning typo must not silently
	// produce a bogus-sender report).
	if _, err := buildPushSnippet(cfg, "ghost-desk"); err == nil {
		t.Error("buildPushSnippet(non-roster desk) = nil error, want a rejection")
	}
	// Provisioning the XO itself for push is a mistake (it reports TO the XO).
	if _, err := buildPushSnippet(cfg, "xo"); err == nil {
		t.Error("buildPushSnippet(the XO) = nil error, want a rejection (the XO is not a push-desk)")
	}
}

func TestBuildPushSnippetXOFallbackToFirstAgent(t *testing.T) {
	// systems-review LOW-1: no XOAgent set → the XO resolves to Agents[0], and the snippet
	// instructs `send` to that agent.
	cfg := &roster.Config{
		Agents: []roster.Agent{
			{Name: "lead"},                        // Agents[0] → the de-facto XO
			{Name: "oc-dev", Surface: "opencode"}, // the desk
		},
	}
	out, err := buildPushSnippet(cfg, "oc-dev")
	if err != nil {
		t.Fatalf("buildPushSnippet: %v", err)
	}
	if !strings.Contains(out, "flotilla send --from oc-dev lead") {
		t.Errorf("with no XOAgent, the snippet must target Agents[0] (lead); got:\n%s", out)
	}
}

func TestBuildPushSnippetUnknownSurfaceErrors(t *testing.T) {
	// systems-review LOW-2: an unregistered surface has no identity-file convention → error
	// (never a guessed file).
	cfg := &roster.Config{
		XOAgent: "xo",
		Agents: []roster.Agent{
			{Name: "xo"},
			{Name: "weird", Surface: "made-up"},
		},
	}
	if _, err := buildPushSnippet(cfg, "weird"); err == nil {
		t.Error("buildPushSnippet(unknown surface) = nil error, want an identity-file error")
	}
}
