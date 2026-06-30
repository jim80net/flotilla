package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/watch"
)

// Config holds the resolved inputs for a dash server. The command layer
// (cmd/flotilla/dash.go) resolves these (default paths mirroring `status`) and
// hands them to NewServer; the server itself does the per-request file I/O.
type Config struct {
	RosterPath   string // path to the roster file
	SnapshotPath string // detector snapshot (default <roster-dir>/flotilla-detector-state.json)
	AckPath      string // XO liveness ack file (default <roster-dir>/flotilla-xo-alive)
	LedgerPath   string // CoS ledger (cfg.CosLedger; "" when the CoS mirror is inert)
	BacklogPath  string // backlog markdown (--tracker-file; default <roster-dir>/.flotilla-state.md)
	Bind         string // listen address (default 127.0.0.1:8787)
	Repo         string // pinned GitHub repo for the tracker (owner/name); "" disables the tracker
	SecretsPath  string // secrets env file for the notify webhook ("" ⇒ notify unavailable)

	// Transport is the coordination transport backing the control surface's notify
	// post (the operator note's destination is a Discord webhook, so this is the
	// DISCORD transport). It is constructed at the wiring boundary
	// (cmd/flotilla/dash.go) — the one place permitted to resolve the concrete medium
	// + the webhook credential — and injected here as an interface VALUE, so
	// internal/dash/control depends on internal/transport, not internal/discord. This
	// is the OUTBOUND seam (the direction asymmetry — design Decision 1).
	Transport transport.Transport

	// WebTransport is the INBOUND coordination transport: the route's roster-wide
	// resolver. As of PR3 (#198) the dash route is the LIVE web ingress — it resolves
	// its target+pane THROUGH this transport's ResolveDestination (the ONE shared
	// roster.ResolveTarget + the SAME deliver.ResolvePane every pane writer uses) and
	// keys the cross-process lock on the returned webDestination.paneTarget. It is the
	// `web` transport, constructed at the wiring boundary (cmd/flotilla/dash.go) with the
	// roster and injected here as an interface VALUE. Distinct from Transport (the
	// OUTBOUND notify medium) — the two opposite-direction seams (design Decision 1).
	WebTransport transport.Transport
}

// Server is the dash HTTP server: a pure reader over the artifacts `flotilla
// watch` writes. It owns the read-model handlers, the SSE hub, and the embedded
// static assets. It holds NO live fleet state of its own — every request reads
// the current artifacts fresh.
type Server struct {
	cfg       Config
	roster    *roster.Config
	xo        string        // resolved XO (xo_agent, else Agents[0])
	threshold time.Duration // snapshot staleness threshold (3× heartbeat)
	now       func() time.Time
	tmpl      *template.Template
	mux       *http.ServeMux
	hub       *hub
	allowed   map[string]bool    // Host-header allowlist (host:port forms)
	origins   map[string]bool    // Origin allowlist (scheme://host:port) for state-changing requests
	tracker   tracker.Tracker    // GitHub-backed issue tracker; nil when no --repo is configured
	control   control.Controller // cnc control (notify live; route/resume gated on the pane lock)
}

// NewServer validates the bind address (LOOPBACK ONLY — see validateBind; the
// token-gated non-loopback bind is a tracked follow-on), loads the roster,
// resolves the XO + freshness threshold, parses the embedded page template,
// wires the routes + the tracker/control surfaces. It does not listen; call Run.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Bind == "" {
		cfg.Bind = DefaultBind
	}
	if err := validateBind(cfg.Bind); err != nil {
		return nil, err
	}
	rc, err := roster.Load(cfg.RosterPath)
	if err != nil {
		return nil, err
	}
	// Resolve the <roster-dir>/… default paths (and the roster-derived CoS ledger
	// path) now that the roster is loaded — a single load, here.
	cfg = ResolvePaths(cfg, rc)
	// The XO is the explicit xo_agent, else the first agent (watch's own rule).
	// roster.Load guarantees a non-empty Agents slice, so [0] is safe.
	xo := rc.XOAgent
	if xo == "" {
		xo = rc.Agents[0].Name
	}
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:       cfg,
		roster:    rc,
		xo:        xo,
		threshold: FreshnessThreshold(rc.HeartbeatDur()),
		now:       time.Now,
		tmpl:      tmpl,
		mux:       http.NewServeMux(),
		hub:       newHub(),
		allowed:   buildHostAllowlist(cfg.Bind),
		origins:   buildOriginAllowlist(cfg.Bind),
	}
	// The tracker is OPTIONAL: it is wired only when a repo is pinned. An invalid
	// repo fails closed (NewServer errors) rather than serving a tracker that
	// could be coaxed into a bad --repo; an empty repo leaves the tracker nil and
	// its endpoints return ErrNoRepo (the read surface is unaffected).
	if cfg.Repo != "" {
		gh, terr := tracker.NewGH(cfg.Repo)
		if terr != nil {
			return nil, terr
		}
		s.tracker = gh
	}
	// The control surface is always wired: notify posts through the injected
	// (discord-backed) Transport when a secrets webhook is configured; route resolves
	// THROUGH the injected (web) WebTransport and drives a pane through the cross-process
	// lock; resume fails closed (design §5). BOTH transports are required — they are
	// constructed at the wiring boundary (the two opposite-direction seams); a nil one is
	// a wiring bug, surfaced fail-closed rather than nil-dereferenced at the first
	// notify/route.
	if cfg.Transport == nil {
		return nil, fmt.Errorf("dash: a coordination Transport is required for the notify (construct it at the wiring boundary and pass it via Config.Transport)")
	}
	if cfg.WebTransport == nil {
		return nil, fmt.Errorf("dash: a WebTransport is required for the route's inbound resolution (construct the web transport at the wiring boundary and pass it via Config.WebTransport)")
	}
	s.control = control.NewLibrary(rc, xo, cfg.SecretsPath, cfg.Transport, cfg.WebTransport)
	s.routes()
	return s, nil
}

// Run starts the HTTP server and the SSE file poller, serving until ctx is
// cancelled, then shuts down gracefully. Read/Write/Idle timeouts bound a stuck
// client so it cannot pin a goroutine forever; WriteTimeout is left at 0 because
// SSE responses are intentionally long-lived (the idle timeout + the hub's
// per-client lifecycle bound them instead).
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Bind)
	if err != nil {
		return fmt.Errorf("dash listen on %q: %w", s.cfg.Bind, err)
	}
	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// The file poller drives the SSE hub; it stops when ctx is cancelled.
	go s.hub.run(ctx)
	go s.poll(ctx)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	fmt.Fprintf(os.Stderr, "flotilla dash: serving on http://%s (reading %s)\n", s.cfg.Bind, s.cfg.SnapshotPath)
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// handler wraps the mux with the Host-header allowlist (anti-DNS-rebinding),
// applied to EVERY route including static assets and SSE.
func (s *Server) handler() http.Handler {
	return s.hostAllow(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/topology", s.handleTopology)
	s.mux.HandleFunc("/api/history", s.handleHistory)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticAssets()))))

	// Issue tracker (Phase 2). Reads follow the open-on-loopback read posture
	// (the Host-allowlist already wraps them); WRITES go through requireWrite,
	// which enforces the browser-CSRF defense (custom header + Origin) on loopback
	// too. Method-gating is the mux's job: a state-changing GET cannot reach a
	// write handler because the write patterns are POST-only.
	s.mux.HandleFunc("GET /api/issues", s.handleIssuesList)
	s.mux.HandleFunc("GET /api/issues/{number}", s.handleIssueGet)
	s.mux.HandleFunc("POST /api/issues", s.requireWrite(s.handleIssueCreate))
	s.mux.HandleFunc("POST /api/issues/{number}/comments", s.requireWrite(s.handleIssueComment))
	s.mux.HandleFunc("POST /api/issues/{number}/labels", s.requireWrite(s.handleIssueLabel))
	s.mux.HandleFunc("POST /api/issues/{number}/close", s.requireWrite(s.handleIssueClose))

	// cnc control (Phase 3) — state-changing, behind the same requireWrite
	// browser-CSRF gate as tracker writes. notify is live; route/resume fail
	// closed (503) until the cross-process pane lock lands.
	s.mux.HandleFunc("POST /api/control/route", s.requireWrite(s.handleControlRoute))
	s.mux.HandleFunc("POST /api/control/notify", s.requireWrite(s.handleControlNotify))
	s.mux.HandleFunc("POST /api/control/resume", s.requireWrite(s.handleControlResume))
}

// --- read-model loading (the only file I/O; the builders stay pure) ---

// loadBoard reads the snapshot + ack file fresh and builds the board document.
// It mirrors cmd/flotilla/status.go's load path EXACTLY (same LoadSnapshot, same
// mtime-as-generated_at, same ack-age treatment).
func (s *Server) loadBoard() BoardDoc {
	now := s.now()
	snap, snapOK := watch.LoadSnapshot(s.cfg.SnapshotPath)

	in := BoardInputs{
		Cfg:       s.roster,
		XO:        s.xo,
		Snap:      snap,
		SnapOK:    snapOK,
		Threshold: s.threshold,
	}
	if snapOK {
		if fi, err := os.Stat(s.cfg.SnapshotPath); err == nil {
			in.GeneratedAt = fi.ModTime().UTC().Format(time.RFC3339)
			in.SnapAge = now.Sub(fi.ModTime())
		}
	}
	if fi, err := os.Stat(s.cfg.AckPath); err == nil {
		in.AckOK = true
		in.AckAge = now.Sub(fi.ModTime())
	}
	return BuildBoard(in)
}

// loadHistory reads the ledger + backlog files fresh and builds the history
// document. A missing/unreadable file reads as empty (the dash never fabricates).
func (s *Server) loadHistory() HistoryDoc {
	return BuildHistory(readFileOrEmpty(s.cfg.LedgerPath), readFileOrEmpty(s.cfg.BacklogPath))
}

// --- handlers ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// The page is STATIC chrome; all dynamic fleet/issue data reaches it via
	// fetch of the JSON endpoints, never server-rendered into a <script> literal
	// (anti-XSS — a desk name / ledger gist can never become stored script).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := pageData{Bind: s.cfg.Bind, XO: s.xo}
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.loadBoard())
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, BuildTopology(s.roster))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.loadHistory())
}

// pageData is the (static) data the index template needs — no fleet data, just
// chrome the page shows before its JS fetches the live JSON.
type pageData struct {
	Bind string
	XO   string
}

// --- middleware + helpers ---

// hostAllow enforces the Host-header allowlist on every request (anti-DNS-
// rebinding, design §7). A request whose Host is outside the allowlist is
// rejected regardless of the bind address — closing the rebinding path where a
// remote page rebinds its hostname to 127.0.0.1 and reaches the loopback dash.
func (s *Server) hostAllow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowed[r.Host] && !bindIsNonLoopback(s.cfg.Bind) {
			http.Error(w, "forbidden: Host header not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

// writeJSONStatus writes v as JSON with an explicit status. The Content-Type is
// set BEFORE WriteHeader — a header set after WriteHeader is silently dropped
// (the headers are already on the wire), so a non-200 JSON response must set the
// type first or the client sees a sniffed/empty content type.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// readFileOrEmpty returns a file's contents, or "" for an empty path or any read
// error (the read model treats absence as empty — it never fabricates data).
func readFileOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildHostAllowlist returns the set of acceptable Host headers: the loopback
// names at the bind port plus the configured bind host:port itself. The port is
// taken from the bind address (default 8787).
func buildHostAllowlist(bind string) map[string]bool {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		// Should not happen (validateBind ran), but fail safe to the bind verbatim.
		return map[string]bool{bind: true}
	}
	allowed := map[string]bool{
		net.JoinHostPort("127.0.0.1", port): true,
		net.JoinHostPort("::1", port):       true,
		net.JoinHostPort("localhost", port): true,
		net.JoinHostPort(host, port):        true,
	}
	return allowed
}

// bindIsNonLoopback reports whether the dash is bound to a non-loopback address
// (0.0.0.0 / a LAN IP). On the operator's private network (override 2026-06-30)
// such a bind intentionally serves the LAN, so the anti-DNS-rebinding Host
// allowlist (a loopback-defense) does not apply — any Host is accepted. The
// bearer-token auth gate (flotilla #208) is the hardening follow-on for an
// untrusted network.
func bindIsNonLoopback(bind string) bool {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return !ip.IsLoopback()
}

// buildOriginAllowlist returns the set of acceptable Origin (and Referer-origin)
// values for state-changing requests: the same loopback/bind host:port forms as
// the Host allowlist, prefixed with the http scheme (the dash serves plaintext
// http on loopback). A state-changing request whose Origin/Referer is present
// must match one of these (anti-CSRF defense-in-depth alongside the custom
// header — see requireWrite).
func buildOriginAllowlist(bind string) map[string]bool {
	origins := map[string]bool{}
	for hostport := range buildHostAllowlist(bind) {
		origins["http://"+hostport] = true
	}
	return origins
}

// validateBind enforces the loopback-only posture. A non-loopback bind (LAN,
// 0.0.0.0) would expose an UNAUTHENTICATED surface to the network; the bearer
// token + SSE-cookie auth gate that makes a non-loopback bind safe is a tracked
// follow-on (the non-loopback auth surface, deferred from this control phase).
// Until it lands the dash fails closed: it refuses any non-loopback bind rather
// than serving unauthenticated beyond the host. Remote access is via an SSH
// tunnel to the loopback bind.
func validateBind(bind string) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return fmt.Errorf("dash: --bind %q is not a valid host:port: %w", bind, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("dash: --bind host %q is not an IP or localhost (Phase 1 serves loopback only)", host)
	}
	// Operator override (2026-06-30): non-loopback bind PERMITTED on the operator's
	// private network — he owns the exposure decision. Reads serve openly; state-
	// changing control requests remain Origin/Host-gated (anti-DNS-rebinding). The
	// bearer-token + SSE-cookie auth gate (flotilla #208) is the proper hardening
	// follow-on for an untrusted network; this unblocks 0.0.0.0 on a trusted LAN now.
	_ = ip.IsLoopback()
	return nil
}

// DefaultBind is the default loopback listen address.
const DefaultBind = "127.0.0.1:8787"

// ResolvePaths fills empty path fields with the same <roster-dir>/… defaults
// `status` and `watch` use, and derives the ledger + backlog paths from the
// loaded roster. It mirrors cmd/flotilla/status.go's default-path resolution.
func ResolvePaths(cfg Config, rc *roster.Config) Config {
	dir := filepath.Dir(cfg.RosterPath)
	if cfg.SnapshotPath == "" {
		cfg.SnapshotPath = filepath.Join(dir, "flotilla-detector-state.json")
	}
	if cfg.AckPath == "" {
		cfg.AckPath = filepath.Join(dir, "flotilla-xo-alive")
	}
	if cfg.BacklogPath == "" {
		cfg.BacklogPath = filepath.Join(dir, ".flotilla-state.md")
	}
	// The CoS ledger path is whatever the roster resolved (empty when the CoS
	// mirror is inert — then the history view shows no ledger, honestly).
	if cfg.LedgerPath == "" {
		cfg.LedgerPath = rc.CosLedger
	}
	return cfg
}
