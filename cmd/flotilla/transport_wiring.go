package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/watch"
)

// This file holds the WIRING-LAYER adapters between the watch daemon's seams and the
// Transport SPI. cmd/flotilla is the one place permitted to construct the concrete
// transport and bridge its (opaque-Destination-keyed) capabilities to the watch
// package's medium-agnostic, string-channel-keyed seams — so the watch package never
// imports internal/discord and the reconcile logic is unchanged by the extraction.

// transportDestinations resolves one transport Destination per bound channel id (the
// subscribe + reconcile target set) by DELEGATING to the transport's own Destinations
// seam — so the channel-id→Destination construction lives inside the transport (per the
// SPI goal) and the wiring never constructs a medium-specific Destination itself.
func transportDestinations(tr transport.Transport, channelIDs []string) []transport.Destination {
	return tr.Destinations(channelIDs)
}

// destByChannel maps each channel id to its Destination, so the string-keyed watch
// poller (whose cursor space is per-channel-id) can recover the opaque Destination the
// transport's CatchUp capability requires.
func destByChannel(dests []transport.Destination, channelIDs []string) map[string]transport.Destination {
	m := make(map[string]transport.Destination, len(channelIDs))
	for i, id := range channelIDs {
		if i < len(dests) {
			m[id] = dests[i]
		}
	}
	return m
}

// transportCatchUpReader adapts the transport's CatchUp capability (keyed by an opaque
// Destination) to the watch poller's MessageReader seam (keyed by channel id). It is
// the bridge that lets the catch-up reconcile logic stay byte-identical (string
// channel ids, transport.Message) while the underlying read goes through the SPI.
type transportCatchUpReader struct {
	cap  transport.CatchUp
	dest map[string]transport.Destination
}

func (r *transportCatchUpReader) MessagesAfterPaged(channelID, afterID string, pageLimit, maxPages int) ([]transport.Message, bool, error) {
	d, ok := r.dest[channelID]
	if !ok {
		return nil, false, fmt.Errorf("no transport destination for channel %q", channelID)
	}
	return r.cap.MessagesAfter(d, afterID, pageLimit, maxPages)
}

func (r *transportCatchUpReader) Latest(channelID string) (transport.Message, bool, error) {
	d, ok := r.dest[channelID]
	if !ok {
		return transport.Message{}, false, fmt.Errorf("no transport destination for channel %q", channelID)
	}
	return r.cap.Latest(d)
}

// transportRecentReader adapts the transport's RecentHistory capability (keyed by
// an opaque Destination) to the un-acked backstop's RecentHistoryReader seam
// (keyed by channel id).
type transportRecentReader struct {
	cap  transport.RecentHistory
	dest map[string]transport.Destination
}

func (r *transportRecentReader) Recent(channelID string, limit int) ([]transport.Message, error) {
	d, ok := r.dest[channelID]
	if !ok {
		return nil, fmt.Errorf("no transport destination for channel %q", channelID)
	}
	return r.cap.Recent(d, limit)
}

func (r *transportRecentReader) RecentSince(channelID string, since time.Time) ([]transport.Message, error) {
	d, ok := r.dest[channelID]
	if !ok {
		return nil, fmt.Errorf("no transport destination for channel %q", channelID)
	}
	return r.cap.RecentSince(d, since)
}

// transportGateway adapts the transport's Subscribe (the inbound half) to the
// gatewayController seam the non-fatal-with-retry relayController consumes. Open
// subscribes (constructs+opens the live gateway as one attempt); Close tears the
// transport down. This preserves the 2026-06-10 crash-loop guard: a Subscribe failure
// is returned to relayController, which degrades to clock-only and retries — never
// fatal to the safety-critical clock.
type transportGateway struct {
	tr          transport.Transport
	ctx         context.Context
	dests       []transport.Destination
	handler     func(channelID, messageID, authorID, content string)
	onReconnect func()
}

// Open subscribes the transport to its destinations, wiring the relay handler (the
// medium-agnostic 4-field projection) and the reconnect→catch-up-kick coupling.
func (g *transportGateway) Open() error {
	return g.tr.Subscribe(g.ctx, g.dests, transport.MessageHandler(g.handler), g.onReconnect)
}

// Close tears down the transport's live gateway session. (The transport's own deferred
// Close in cmdWatch also covers process shutdown; this is the relayController's path.)
func (g *transportGateway) Close() error { return g.tr.Close() }

// compile-time assertions: the adapters satisfy the seams they bridge.
var (
	_ watch.MessageReader       = (*transportCatchUpReader)(nil)
	_ watch.RecentHistoryReader = (*transportRecentReader)(nil)
	_ gatewayController         = (*transportGateway)(nil)
)
