package dash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
)

// maxRequestBody bounds a tracker write request's JSON body (titles/bodies are
// themselves capped in the tracker package; this is the transport-level guard so
// a client can never stream an unbounded body into the decoder).
const maxRequestBody = 128 * 1024

// --- request shapes (all tracker write inputs arrive as JSON) ---

type createIssueReq struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

type commentReq struct {
	Body string `json:"body"`
}

type labelReq struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// --- read handlers (open on loopback; Host-allowlist already applies) ---

// handleIssuesList serves GET /api/issues?state=&label=&limit=. The default is
// the repo's OPEN issues; ?label=operator-idea surfaces the XO's idea queue.
func (s *Server) handleIssuesList(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	filter := tracker.ListFilter{
		State: r.URL.Query().Get("state"),
		Label: r.URL.Query().Get("label"),
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			filter.Limit = n
		}
	}
	started := time.Now()
	issues, err := s.tracker.List(r.Context(), filter)
	w.Header().Set("Server-Timing", serverTiming("github-list", time.Since(started)))
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSON(w, issuesListDoc{Repo: s.repoName(), Issues: issues})
}

// handleWorkLedger serves the derived fleet-context view over the current tracker.
// Bodies are fetched only so flotilla attribution trailers can be parsed; the
// builder strips them from its list response.
func (s *Server) handleWorkLedger(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "all"
	}
	started := time.Now()
	listStarted := time.Now()
	issues, err := s.tracker.List(r.Context(), tracker.ListFilter{
		State: state, Label: r.URL.Query().Get("label"), Limit: 200, IncludeBody: true,
	})
	listElapsed := time.Since(listStarted)
	if err != nil {
		w.Header().Set("Server-Timing", serverTiming("github-list", listElapsed))
		writeTrackerError(w, err)
		return
	}
	deriveStarted := time.Now()
	doc := BuildWorkLedger(s.cfg.Repo, issues, s.loadGoalsFromIssues(issues), s.roster, s.now())
	w.Header().Set("Server-Timing", fmt.Sprintf("%s, %s, %s",
		serverTiming("github-list", listElapsed),
		serverTiming("derive", time.Since(deriveStarted)),
		serverTiming("total", time.Since(started))))
	writeJSON(w, doc)
}

// handleIssueGet serves GET /api/issues/{number} (body + comments).
func (s *Server) handleIssueGet(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	num, err := issueNumber(r)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	issue, err := s.tracker.Get(r.Context(), num)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSON(w, issue)
}

// --- write handlers (behind requireWrite: custom header + Origin gate) ---

// handleIssueCreate serves POST /api/issues (create a new issue).
func (s *Server) handleIssueCreate(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	var req createIssueReq
	if !decodeJSON(w, r, &req) {
		return
	}
	issue, err := s.tracker.Create(r.Context(), tracker.CreateInput{
		Title:  req.Title,
		Body:   req.Body,
		Labels: req.Labels,
	})
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, issue)
}

// handleIssueComment serves POST /api/issues/{number}/comments.
func (s *Server) handleIssueComment(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	num, err := issueNumber(r)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	var req commentReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.tracker.Comment(r.Context(), num, req.Body); err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSON(w, okDoc{OK: true})
}

// handleIssueLabel serves POST /api/issues/{number}/labels (add/remove labels).
func (s *Server) handleIssueLabel(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	num, err := issueNumber(r)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	var req labelReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.tracker.Label(r.Context(), num, req.Add, req.Remove); err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSON(w, okDoc{OK: true})
}

// handleIssueClose serves POST /api/issues/{number}/close (a DESTRUCTIVE verb —
// the UI confirms it explicitly before this is called).
func (s *Server) handleIssueClose(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		writeTrackerError(w, tracker.ErrNoRepo)
		return
	}
	num, err := issueNumber(r)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	if err := s.tracker.Close(r.Context(), num); err != nil {
		writeTrackerError(w, err)
		return
	}
	writeJSON(w, okDoc{OK: true})
}

// --- write-gate middleware (browser-CSRF defense, design §7) ---

// requireWrite wraps a state-changing handler with the browser-attacker defense
// that applies ON LOOPBACK TOO (the operator's own browser is untrusted):
//
//  1. A custom request header (X-Flotilla-Dash: 1) is REQUIRED. A cross-origin
//     page can only set a custom header via a CORS preflight, which the dash
//     never approves (it emits no Access-Control-Allow-* headers), so a forged
//     "simple request" POST from a malicious page is rejected. This is the
//     primary CSRF defense and it does NOT depend on a token.
//  2. The Origin (or, absent it, the Referer) is validated against the bind's
//     origin allowlist WHEN PRESENT — defense-in-depth. Browsers always attach
//     Origin to a non-GET request, so a cross-origin forgery carries the
//     attacker's Origin and is rejected here; a non-browser client (host-shell
//     trust on loopback) may omit it and is gated by the custom header alone.
//
// The bearer-token gate + the SSE session cookie that make a NON-loopback bind
// safe land with the control phase (Phase 3); Phase 2 is loopback-only.
func (s *Server) requireWrite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.DisableAuthentication {
			if r.Header.Get("X-Flotilla-Dash") != "1" {
				http.Error(w, "forbidden: missing X-Flotilla-Dash header (anti-CSRF)", http.StatusForbidden)
				return
			}
			if !s.originAllowed(r) {
				http.Error(w, "forbidden: Origin not allowed (anti-CSRF)", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// originAllowed validates the request's Origin (preferred) or Referer origin
// against the allowlist. It returns true when neither header is present (a
// non-browser client on loopback, already gated by the required custom header);
// when a header IS present it must match, so a cross-origin browser forgery is
// rejected.
func (s *Server) originAllowed(r *http.Request) bool {
	// A present Origin/Referer is validated against the CONFIGURED allowlist (the
	// loopback/bind forms plus the operator's declared AllowedOrigins) — NEVER against
	// the request Host header. Validating against the Host header re-opens DNS rebinding:
	// an attacker page whose DNS resolves its own domain to the dash's LAN IP sends a
	// matching Origin AND Host, so a Host-relative check would pass. A fixed allowlist
	// does not contain the attacker's domain, so the forged write is rejected — while the
	// operator's declared LAN origin (FLOTILLA_DASH_ALLOWED_ORIGINS) is accepted. A
	// missing Origin/Referer is a non-browser client (already gated by the required custom
	// header); DNS rebinding is a browser attack and always carries an Origin.
	if origin := r.Header.Get("Origin"); origin != "" {
		return s.origins[origin]
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return false
		}
		return s.origins[u.Scheme+"://"+u.Host]
	}
	return true
}

// --- helpers ---

// issuesListDoc wraps the list with the pinned repo so the UI can show which
// repo it is tracking (the repo is server-pinned, never client-selected).
type issuesListDoc struct {
	Repo   string          `json:"repo"`
	Issues []tracker.Issue `json:"issues"`
}

// okDoc is the minimal success body for a write with no resource to return.
type okDoc struct {
	OK bool `json:"ok"`
}

// errorDoc is the JSON error body the UI surfaces verbatim (never a swallowed
// failure, never an empty list masquerading as "no issues").
type errorDoc struct {
	Error string `json:"error"`
}

// repoName returns the pinned repo for display (server-pinned, never client-set).
func (s *Server) repoName() string {
	return s.cfg.Repo
}

// issueNumber extracts and validates the {number} path value as a positive int.
func issueNumber(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n <= 0 {
		return 0, tracker.ErrInvalidNumber
	}
	return n, nil
}

// decodeJSON reads a size-bounded JSON request body into v, writing a 400 on a
// malformed/oversized body. It returns false when it has already written the
// error response (the caller must return).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	// Reject trailing content after the JSON object (exactly one value expected).
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body: unexpected trailing content")
		return false
	}
	return true
}

// writeTrackerError maps a tracker typed error onto an HTTP status + an honest
// JSON message (always surfaced — the silent-failure discipline). An
// unrecognized gh failure is a 502 (the dash is a gateway to gh) — never a
// swallowed success. Gateway-class (5xx) failures are also logged to the dash's
// stderr so an operator can correlate a reported "tracker error" with a cause;
// on loopback the verbatim gh message to the client is acceptable (Phase 3's
// non-loopback bind should genericize the client message — TODO below).
func writeTrackerError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, tracker.ErrNoRepo),
		errors.Is(err, tracker.ErrGHMissing):
		status = http.StatusServiceUnavailable
	case errors.Is(err, tracker.ErrIssueNotFound),
		errors.Is(err, tracker.ErrRepoNotFound):
		status = http.StatusNotFound
	case errors.Is(err, tracker.ErrRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, tracker.ErrTimeout):
		status = http.StatusGatewayTimeout
	case errors.Is(err, tracker.ErrInvalidNumber),
		errors.Is(err, tracker.ErrEmptyTitle),
		errors.Is(err, tracker.ErrEmptyBody),
		errors.Is(err, tracker.ErrEmptyLabel),
		errors.Is(err, tracker.ErrTooLong),
		errors.Is(err, tracker.ErrNoLabelChange),
		errors.Is(err, tracker.ErrInvalidState),
		errors.Is(err, tracker.ErrInvalidRepo):
		status = http.StatusBadRequest
	}
	// TODO(dash, Phase 3): on a non-loopback bind, return a generic client
	// message for 5xx (the verbatim gh stderr may carry host paths) and keep the
	// detail server-side only. On loopback the operator IS the host owner, so the
	// real message aids debugging.
	if status >= 500 {
		fmt.Fprintf(os.Stderr, "flotilla dash: tracker error (%d): %v\n", status, err)
	}
	writeError(w, status, err.Error())
}

// writeError writes a JSON error body with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorDoc{Error: msg})
}
