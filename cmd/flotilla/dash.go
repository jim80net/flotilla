package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/dash/tracker"
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
	// --repo pins the issue tracker's GitHub repo (owner/name). When empty it is
	// resolved from the working directory the way `gh` does; if that fails the
	// tracker is simply disabled (the read surface is unaffected).
	repo := fs.String("repo", os.Getenv("FLOTILLA_DASH_REPO"), "GitHub repo for the issue tracker (owner/name; default: the working-dir repo as gh resolves it)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve the tracker repo at STARTUP (never request-derived). An empty/
	// unresolvable repo disables the tracker with a clear message rather than
	// failing the whole dash — graceful degradation of one optional feature.
	pinnedRepo := resolveTrackerRepo(*repo)

	// NewServer loads + validates the roster (fail-closed), resolves the
	// <roster-dir>/… default paths, validates the bind (loopback-only here), and
	// constructs the gh-backed tracker when a repo is pinned (fail-closed on a
	// malformed repo).
	srv, err := dash.NewServer(dash.Config{
		RosterPath:   *rosterPath,
		SnapshotPath: *snapshotPath,
		AckPath:      *ackPath,
		BacklogPath:  *trackerPath,
		Bind:         *bind,
		Repo:         pinnedRepo,
	})
	if err != nil {
		return err
	}

	// Serve until SIGINT/SIGTERM, then shut down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx)
}

// resolveTrackerRepo returns the pinned tracker repo. An explicit --repo is used
// verbatim; otherwise it asks `gh` for the working-dir repo. A resolution
// failure (cwd is not a gh repo, gh unauthenticated) is reported on stderr and
// the tracker is disabled (empty string) — the read surface still serves.
func resolveTrackerRepo(flagRepo string) string {
	if flagRepo != "" {
		return flagRepo
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, err := tracker.ResolveDefaultRepo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla dash: issue tracker disabled — could not resolve a GitHub repo (%v); pass --repo owner/name to enable it\n", err)
		return ""
	}
	return repo
}
