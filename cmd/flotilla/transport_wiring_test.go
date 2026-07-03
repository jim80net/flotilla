package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/transport"
)

// fakeOutboundTransport is a test Transport whose Subscribe outcome is controllable,
// so the NON-FATAL-DEGRADE invariant can be pinned end-to-end through the production
// wiring (transportGateway → relayController), not just via the abstract gateway seam.
type fakeOutboundTransport struct {
	subscribeErr error
	closed       bool
}

func (f *fakeOutboundTransport) Name() string { return "fake" }
func (f *fakeOutboundTransport) Subscribe(context.Context, []transport.Destination, transport.MessageHandler, func()) error {
	return f.subscribeErr
}
func (f *fakeOutboundTransport) Destinations(channelIDs []string) []transport.Destination {
	out := make([]transport.Destination, 0, len(channelIDs))
	for range channelIDs {
		out = append(out, transport.NewWebhookDestination(""))
	}
	return out
}
func (f *fakeOutboundTransport) Post(transport.Destination, string, string) error { return nil }
func (f *fakeOutboundTransport) PostWithAttachments(transport.Destination, string, string, []string) error {
	return nil
}
func (f *fakeOutboundTransport) ResolveDestination(string, string) (transport.Destination, string, bool) {
	return nil, "", false
}
func (f *fakeOutboundTransport) MaxContentRunes() int       { return 2000 }
func (f *fakeOutboundTransport) Chunk(text string) []string { return []string{text} }
func (f *fakeOutboundTransport) Close() error               { f.closed = true; return nil }

// TestTransportSubscribeFailureDegradesNonFatally is the load-bearing invariant-3
// test: a transport Subscribe failure (the cold-boot DNS blip) must degrade to
// clock-only and NEVER crash the daemon — the safety-critical clock keeps ticking.
// It drives the REAL production path (transportGateway.Open → tr.Subscribe, wrapped by
// relayController.Start), proving the extracted transport preserves watch.go's
// non-fatal posture rather than making Subscribe fatal.
func TestTransportSubscribeFailureDegradesNonFatally(t *testing.T) {
	warn, note, esc := &recorder{}, &recorder{}, &recorder{}
	tr := &fakeOutboundTransport{subscribeErr: errors.New("dns blip: name resolution failed")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The SAME factory shape watch.go wires: Open() is the transport's Subscribe.
	factory := func() (gatewayController, error) {
		ctrl := &transportGateway{tr: tr, ctx: ctx, dests: nil, handler: func(string, string, string, string) {}, onReconnect: nil}
		if err := ctrl.Open(); err != nil {
			return nil, err
		}
		return ctrl, nil
	}

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, esc.record)
	// Start MUST NOT panic or block — a failed Subscribe is a degraded-but-running
	// daemon, not a dead one (Start has no error to abort cmdWatch with).
	rc.Start(ctx)

	if rc.done == nil {
		t.Fatal("a failed transport Subscribe must spawn a background retry goroutine (clock-only degrade), not crash")
	}
	if !waitFor(func() bool { return warn.any("running CLOCK-ONLY") }, 200*time.Millisecond) {
		t.Errorf("expected a degraded clock-only warning after a Subscribe failure; got %v", warn.lines)
	}
	if note.any("inbound relay active") {
		t.Errorf("relay must NOT report active while Subscribe is failing; got %v", note.lines)
	}

	rc.Shutdown() // clean join, no leak
}

// TestTransportSubscribeSuccessActivatesRelay is the positive control: when Subscribe
// succeeds, the relay reports active and Shutdown closes the transport — the same
// gatewayController lifecycle the discord transport follows.
func TestTransportSubscribeSuccessActivatesRelay(t *testing.T) {
	warn, note, esc := &recorder{}, &recorder{}, &recorder{}
	tr := &fakeOutboundTransport{subscribeErr: nil}

	factory := func() (gatewayController, error) {
		ctrl := &transportGateway{tr: tr, ctx: context.Background(), handler: func(string, string, string, string) {}}
		if err := ctrl.Open(); err != nil {
			return nil, err
		}
		return ctrl, nil
	}

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, esc.record)
	rc.Start(context.Background())

	if rc.done != nil {
		t.Error("a clean Subscribe must NOT spawn a retry goroutine")
	}
	if !note.any("inbound relay active") {
		t.Errorf("expected 'inbound relay active' on a clean Subscribe; got %v", note.lines)
	}
	rc.Shutdown()
	if !tr.closed {
		t.Error("Shutdown must close the transport (gatewayController.Close → transport.Close)")
	}
}
