package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/jim80net/flotilla/internal/dash"
)

// cmdDash starts the optional local web interface (`flotilla dash`). Phase 1 is a
// PURE READER: it serves a read-only fleet board, federation topology, and
// coordination history over the artifacts `flotilla watch` already writes (the
// detector snapshot, the XO ack file, the roster, the CoS ledger, the backlog).
// It starts no daemon, probes no panes, and writes no fleet state — `flotilla
// watch` stays the single writer (design §2).
//
// It mirrors `flotilla status`'s default-path resolution EXACTLY so the dash
// reads precisely what watch writes (same env vars, same <roster-dir>/… fallbacks).
// The default bind is loopback; Phase 1 serves loopback only (a non-loopback
// bind, which needs the token + SSE-cookie auth gate, lands with the control
// phase — the server fails closed on one until then).
func cmdDash(args []string) error {
	fs := flag.NewFlagSet("dash", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	snapshotPath := fs.String("snapshot-file", os.Getenv("FLOTILLA_SNAPSHOT_FILE"), "change-detector snapshot file (default <roster-dir>/flotilla-detector-state.json)")
	ackPath := fs.String("ack-file", os.Getenv("FLOTILLA_ACK_FILE"), "XO liveness ack file (default <roster-dir>/flotilla-xo-alive)")
	trackerPath := fs.String("tracker-file", os.Getenv("FLOTILLA_TRACKER_FILE"), "backlog markdown the history view reads (default <roster-dir>/.flotilla-state.md)")
	bind := fs.String("bind", dash.DefaultBind, "local listen address (loopback only in this phase)")
	// --repo is accepted now for forward-compatibility with the Phase 2 tracker;
	// it is unused in Phase 1 (the read surface has no GitHub coupling).
	_ = fs.String("repo", "", "GitHub repo for the issue tracker (owner/name; reserved for the tracker phase, unused here)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// NewServer loads + validates the roster (fail-closed), resolves the
	// <roster-dir>/… default paths, and validates the bind (loopback-only here).
	srv, err := dash.NewServer(dash.Config{
		RosterPath:   *rosterPath,
		SnapshotPath: *snapshotPath,
		AckPath:      *ackPath,
		BacklogPath:  *trackerPath,
		Bind:         *bind,
	})
	if err != nil {
		return err
	}

	// Serve until SIGINT/SIGTERM, then shut down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx)
}
