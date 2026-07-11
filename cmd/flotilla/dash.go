package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/dash"
	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

// cmdDash starts the optional local web interface (`flotilla dash`). The fleet
// view is a PURE READER over the artifacts `flotilla watch` already writes (the
// detector snapshot, the XO ack file, the roster, the CoS ledger, the backlog) —
// it starts no daemon, probes no panes, and writes no fleet state (`flotilla
// watch` stays the single writer, design §2). The dash also serves the native
// issue tracker (via `gh`) and the cnc control surface (notify live;
// route/resume gated on the cross-process pane lock).
//
// It mirrors `flotilla status`'s default-path resolution EXACTLY so the dash
// reads precisely what watch writes (same env vars, same <roster-dir>/… fallbacks).
// The default bind is loopback; the dash serves loopback only (a non-loopback
// bind, which needs the bearer-token + SSE-cookie auth gate, is a tracked
// follow-on — the server fails closed on one until then).
func cmdDash(args []string) error {
	fs := flag.NewFlagSet("dash", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	snapshotPath := fs.String("snapshot-file", os.Getenv("FLOTILLA_SNAPSHOT_FILE"), "change-detector snapshot file (default <roster-dir>/flotilla-detector-state.json)")
	ackPath := fs.String("ack-file", os.Getenv("FLOTILLA_ACK_FILE"), "XO liveness ack file (default <roster-dir>/flotilla-xo-alive)")
	trackerPath := fs.String("tracker-file", os.Getenv("FLOTILLA_TRACKER_FILE"), "backlog markdown the history view reads (default <roster-dir>/.flotilla-state.md)")
	goalsPath := fs.String("goals-file", os.Getenv("FLOTILLA_GOALS_FILE"), "goals file the Goals view reads (default <roster-dir>/fleet-goals.json)")
	orgFile := fs.String("org-file", os.Getenv("FLOTILLA_ORG_FILE"), "optional org-truth file (default <roster-dir>/fleet-org.yaml when present; env FLOTILLA_ORG_FILE)")
	bind := fs.String("bind", dash.DefaultBind, "local listen address (loopback only in this phase)")
	// --repo pins the issue tracker's GitHub repo (owner/name). When empty it is
	// resolved from the working directory the way `gh` does; if that fails the
	// tracker is simply disabled (the read surface is unaffected).
	repo := fs.String("repo", os.Getenv("FLOTILLA_DASH_REPO"), "GitHub repo for the issue tracker (owner/name; default: the working-dir repo as gh resolves it)")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file for the notify webhook (optional; notify is disabled without it)")
	goalsLayout := fs.String("goals-layout", os.Getenv("FLOTILLA_DASH_GOALS_LAYOUT"), "DEPRECATED — the Goals map is mind-map-only; any value is redirected to the mind map")
	paradesDir := fs.String("parades-dir", os.Getenv("FLOTILLA_DASH_PARADES_DIR"), "parade archive dir the /parade page reads (default <roster-dir>/parades)")
	doneLogPath := fs.String("done-log", os.Getenv("FLOTILLA_DASH_DONE_LOG"), "goals done-history JSONL the server appends + the Realized window reads (default <roster-dir>/goals-done.jsonl)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve the tracker repo at STARTUP (never request-derived). An empty/
	// unresolvable repo disables the tracker with a clear message rather than
	// failing the whole dash — graceful degradation of one optional feature.
	pinnedRepo := resolveTrackerRepo(*repo)

	// Construct the coordination transport that backs the notify's outbound post —
	// the DISCORD transport (the operator-note destination is a Discord webhook). The
	// wiring boundary is the one place permitted to resolve the concrete medium; the
	// credential itself is resolved by the control library (from --secrets) and wrapped
	// at the post site (transport.NewWebhookDestination), so the transport needs no
	// roster/secrets to post to a caller-resolved webhook — exactly the
	// Construct + NewWebhookDestination pattern watch.go uses for its down-alert post.
	tr, err := newDashTransport()
	if err != nil {
		return err
	}

	// Construct the INBOUND web transport — the route's roster-wide resolver. As of PR3
	// (#198) the dash route is the LIVE web ingress: it resolves its target+pane THROUGH
	// this transport's ResolveDestination (the ONE shared roster.ResolveTarget + the SAME
	// deliver.ResolvePane every pane writer uses). It needs the roster (the resolver's
	// source), so we load it here at the wiring boundary — the one place permitted to
	// resolve the concrete media. (NewServer loads the roster again to validate it +
	// resolve default paths; both read the same file, identical content. The web
	// transport is the INBOUND seam, distinct from tr, the OUTBOUND notify medium —
	// design Decision 1's direction asymmetry.) A construction failure is surfaced
	// (fail-closed) rather than serving a dash whose route would nil-deref.
	rc, err := roster.LoadWith(*rosterPath, roster.LoadOptions{OrgFile: *orgFile})
	if err != nil {
		return fmt.Errorf("dash: load roster for the web transport: %w", err)
	}
	webTr, err := newDashWebTransport(rc)
	if err != nil {
		return err
	}

	// NewServer loads + validates the roster (fail-closed), resolves the
	// <roster-dir>/… default paths, validates the bind (loopback-only here), and
	// constructs the gh-backed tracker when a repo is pinned (fail-closed on a
	// malformed repo).
	srv, err := dash.NewServer(dash.Config{
		RosterPath:            *rosterPath,
		OrgFile:               *orgFile,
		SnapshotPath:          *snapshotPath,
		AckPath:               *ackPath,
		BacklogPath:           *trackerPath,
		GoalsPath:             *goalsPath,
		ParadesPath:           *paradesDir,
		DoneLogPath:           *doneLogPath,
		Bind:                  *bind,
		Repo:                  pinnedRepo,
		SecretsPath:           *secretsPath,
		GoalsLayout:           *goalsLayout,
		DisableAuthentication: dashEnvTruthy("DISABLE_AUTHENTICATION"),
		AllowedOrigins:        dashEnvList("FLOTILLA_DASH_ALLOWED_ORIGINS"),
		Transport:             tr,
		WebTransport:          webTr,
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

// newDashTransport constructs the DISCORD coordination transport that backs the dash
// notify's outbound post. The note's destination is a Discord webhook, so this is the
// discord transport (transport.DefaultTransport), obtained from the registry via
// Construct — the SAME mechanism watch.go uses (cmd/flotilla/watch.go) for its
// down-alert post. An empty Config is correct: the control library resolves the XO's
// webhook from --secrets and wraps it in a transport.NewWebhookDestination at the post
// site, so the transport needs no roster/secrets to post to that caller-resolved
// destination; it supplies only Post + the medium's content cap (MaxContentRunes). The
// WEB transport — the dash's INBOUND roster-wide resolver — is registered and selected
// separately; it is NOT the notify's post medium (the direction asymmetry, design
// Decision 1). A construction failure is surfaced (fail-closed) rather than serving a
// dash whose notify would nil-deref.
func newDashTransport() (transport.Transport, error) {
	tr, err := transport.Construct(transport.DefaultTransport, transport.Config{})
	if err != nil {
		return nil, fmt.Errorf("dash: construct the notify transport: %w", err)
	}
	return tr, nil
}

// newDashWebTransport constructs the WEB coordination transport that backs the dash
// route's INBOUND resolution. As of PR3 (#198) the dash route is the LIVE web ingress: it
// resolves its target+pane THROUGH this transport's ResolveDestination — the ONE shared
// roster.ResolveTarget (so the dash route + the web transport cannot drift) plus the SAME
// deliver.ResolvePane every other pane writer uses (so the cross-process per-pane lock keys
// on the IDENTICAL resolved target — the serialization contract, design Decision 4 / §5).
// Unlike the notify transport (the OUTBOUND discord medium), the web transport NEEDS the
// roster — it is the resolver's source — so it is constructed with it (transport.Config.Roster).
// The web factory fails closed without a roster; a construction failure is surfaced
// (fail-closed) rather than serving a dash whose route would nil-deref.
func newDashWebTransport(rc *roster.Config) (transport.Transport, error) {
	wt, err := transport.Construct("web", transport.Config{Roster: rc})
	if err != nil {
		return nil, fmt.Errorf("dash: construct the web (route) transport: %w", err)
	}
	return wt, nil
}

// dashEnvTruthy reports whether an env var is set to a truthy value (1/true/yes/on).
func dashEnvTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// dashEnvList parses a comma-separated env var into a trimmed, non-empty list — used for
// FLOTILLA_DASH_ALLOWED_ORIGINS (the operator's declared write-gate origins for a LAN bind).
func dashEnvList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
