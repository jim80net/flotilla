package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

// TestNewDashTransport_IsDiscordBacked pins the dash notify wiring (task 6.1/6.2):
// cmdDash constructs the DISCORD transport for the notify's outbound post — the
// note's destination is a Discord webhook (secrets.Webhook(xo)), so the injected
// transport is discord, NOT web (the web transport owns only inbound resolution —
// design Decision 1's direction asymmetry). This mirrors how watch.go constructs the
// discord transport (transport.Construct) and uses NewWebhookDestination at the post
// site. The transport carries the notify's Post + content cap; the credential is
// resolved by the control library and wrapped at Post-time (transport.NewWebhookDestination),
// so the empty Config here is correct — the transport needs no roster/secrets to
// post to a caller-resolved webhook destination.
func TestNewDashTransport_IsDiscordBacked(t *testing.T) {
	tr, err := newDashTransport()
	if err != nil {
		t.Fatalf("newDashTransport: %v", err)
	}
	if tr == nil {
		t.Fatal("newDashTransport must return a non-nil transport (NewServer fails closed on a nil Transport)")
	}
	if tr.Name() != transport.DefaultTransport {
		t.Errorf("dash notify transport = %q, want the discord default %q (the notify's destination is a Discord webhook)", tr.Name(), transport.DefaultTransport)
	}
	// The control library reads this cap at construction for the over-length guard;
	// the discord medium's cap is 2000 (the value the previous discord.MaxContentRunes
	// const carried — behavior preserved).
	if tr.MaxContentRunes() != 2000 {
		t.Errorf("dash notify transport cap = %d, want 2000 (the discord medium cap)", tr.MaxContentRunes())
	}
}

// dashWiringRoster writes a minimal roster file and loads it, for the web-transport
// wiring test (the web transport's Construct needs the roster — the resolver's source).
func dashWiringRoster(t *testing.T) *roster.Config {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "flotilla.json")
	const body = `{"channel_id":"C1","xo_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"},{"name":"alpha"}]}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

// TestNewDashWebTransport_IsWebBacked pins the dash route's INBOUND wiring (PR3 #198):
// cmdDash constructs the WEB transport for the route's roster-wide resolution — the dash
// route is now the LIVE web ingress, resolving its target+pane THROUGH this transport's
// ResolveDestination. Distinct from newDashTransport (the discord-backed OUTBOUND notify
// medium) — the two opposite-direction seams (design Decision 1). The web transport needs
// the roster (the resolver's source), so it is constructed with it.
func TestNewDashWebTransport_IsWebBacked(t *testing.T) {
	rc := dashWiringRoster(t)
	wt, err := newDashWebTransport(rc)
	if err != nil {
		t.Fatalf("newDashWebTransport: %v", err)
	}
	if wt == nil {
		t.Fatal("newDashWebTransport must return a non-nil transport (NewServer fails closed on a nil WebTransport)")
	}
	if wt.Name() != "web" {
		t.Errorf("dash route transport = %q, want the web transport (the inbound roster-wide resolver)", wt.Name())
	}
	// It owns no outbound post — the direction asymmetry: the web transport resolves
	// inbound; its Post rejects (the only outbound the dash does is the Discord notify,
	// posted by the discord transport, newDashTransport). This confirms newDashWebTransport
	// wired the WEB transport, not the discord default. (The roster-wide ResolveDestination
	// semantics are pinned in internal/transport/web_test.go with a fake pane resolver —
	// here the real deliver.ResolvePane would need a live tmux fleet, so this asserts the
	// medium identity, not a live pane resolution.)
	if err := wt.Post(transport.NewInboundTarget("xo", "%1"), "u", "c"); err == nil {
		t.Error("the dash route transport must be the WEB transport (Post rejects); a successful Post means the discord transport was wired by mistake")
	}
}
