package main

import (
	"testing"

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
