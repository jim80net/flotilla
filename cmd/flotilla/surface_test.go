package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestValidateAgentSurfaces(t *testing.T) {
	// surface-driver-aider §6.2: a roster mixing the default, the new "aider"
	// driver, and an explicit "claude-code" passes startup validation; an
	// unregistered surface is a clear startup error (never a silent mis-drive).
	ok := &roster.Config{Agents: []roster.Agent{
		{Name: "xo"}, // empty → default claude-code
		{Name: "backend", Surface: "claude-code"}, // explicit default
		{Name: "pair", Surface: "aider"},          // the aider driver
		{Name: "oc", Surface: "opencode"},         // the opencode driver
		{Name: "gk", Surface: "grok"},             // the grok driver
		{Name: "cx", Surface: "codex"},            // the codex driver
	}}
	if err := validateAgentSurfaces(ok); err != nil {
		t.Fatalf("validateAgentSurfaces(aider+default) = %v, want nil", err)
	}

	bad := &roster.Config{Agents: []roster.Agent{
		{Name: "pair", Surface: "aider"},
		{Name: "oops", Surface: "nope"}, // unregistered
	}}
	err := validateAgentSurfaces(bad)
	if err == nil {
		t.Fatal("validateAgentSurfaces(nope) = nil, want an unknown-surface error")
	}
	if !strings.Contains(err.Error(), "oops") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q should name the offending agent and surface", err)
	}
}
