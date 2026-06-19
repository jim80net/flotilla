package dash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
)

// fakeTracker is an injectable Tracker double. It records the last write call so
// handler tests can assert the handler forwarded the right values, and returns
// canned results / errors. It implements tracker.Tracker.
type fakeTracker struct {
	issues []tracker.Issue
	issue  tracker.Issue
	err    error
	// recorded inputs
	lastFilter  tracker.ListFilter
	lastGet     int
	lastCreate  tracker.CreateInput
	lastComment struct {
		number int
		body   string
	}
	lastLabel struct {
		number      int
		add, remove []string
	}
	lastClose int
	calls     int
}

func (f *fakeTracker) List(_ context.Context, filter tracker.ListFilter) ([]tracker.Issue, error) {
	f.calls++
	f.lastFilter = filter
	return f.issues, f.err
}

func (f *fakeTracker) Get(_ context.Context, number int) (tracker.Issue, error) {
	f.calls++
	f.lastGet = number
	return f.issue, f.err
}

func (f *fakeTracker) Create(_ context.Context, in tracker.CreateInput) (tracker.Issue, error) {
	f.calls++
	f.lastCreate = in
	return f.issue, f.err
}

func (f *fakeTracker) Comment(_ context.Context, number int, body string) error {
	f.calls++
	f.lastComment.number = number
	f.lastComment.body = body
	return f.err
}

func (f *fakeTracker) Label(_ context.Context, number int, add, remove []string) error {
	f.calls++
	f.lastLabel.number = number
	f.lastLabel.add = add
	f.lastLabel.remove = remove
	return f.err
}

func (f *fakeTracker) Close(_ context.Context, number int) error {
	f.calls++
	f.lastClose = number
	return f.err
}

// trackerServer builds a test server with the fake tracker injected + a pinned
// repo name for the list doc.
func trackerServer(t *testing.T, f *fakeTracker) *Server {
	t.Helper()
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	srv.tracker = f
	srv.cfg.Repo = "jim80net/flotilla"
	return srv
}

// doWrite issues a POST through the full handler chain with the anti-CSRF custom
// header + a valid Origin set (the dash's own fetch sets these).
func doWrite(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("X-Flotilla-Dash", "1")
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	return rec
}

// --- read endpoints ---

func TestIssuesList_HappyPath(t *testing.T) {
	f := &fakeTracker{issues: []tracker.Issue{
		{Number: 116, Title: "a bug", State: "OPEN"},
		{Number: 115, Title: "an idea", State: "OPEN", Labels: []tracker.Label{{Name: "operator-idea"}}},
	}}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues?label=operator-idea&limit=10")
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var doc issuesListDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Repo != "jim80net/flotilla" || len(doc.Issues) != 2 {
		t.Fatalf("doc = %+v", doc)
	}
	if f.lastFilter.Label != "operator-idea" || f.lastFilter.Limit != 10 {
		t.Errorf("filter not forwarded: %+v", f.lastFilter)
	}
}

func TestIssuesList_NoRepoConfigured(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	// no tracker injected → nil
	rec := doGet(t, srv, "/api/issues")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no GitHub repo") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestIssueGet_HappyPath(t *testing.T) {
	f := &fakeTracker{issue: tracker.Issue{Number: 106, Title: "t", Body: "the body"}}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues/106")
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	if f.lastGet != 106 {
		t.Errorf("forwarded number = %d", f.lastGet)
	}
}

func TestIssueGet_InvalidNumber(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues/0")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", rec.Code)
	}
	if f.calls != 0 {
		t.Error("invalid number must not reach the tracker")
	}
}

func TestIssueGet_NotFound(t *testing.T) {
	f := &fakeTracker{err: tracker.ErrIssueNotFound}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues/999")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404", rec.Code)
	}
}

func TestIssuesList_GHErrorSurfaced(t *testing.T) {
	f := &fakeTracker{err: tracker.ErrUnauthenticated}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not authenticated") {
		t.Errorf("error not surfaced: %s", rec.Body.String())
	}
}

func TestIssuesList_RateLimited(t *testing.T) {
	f := &fakeTracker{err: tracker.ErrRateLimited}
	srv := trackerServer(t, f)
	rec := doGet(t, srv, "/api/issues")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code %d, want 429", rec.Code)
	}
}

// --- write endpoints (happy path) ---

func TestIssueCreate_HappyPath(t *testing.T) {
	f := &fakeTracker{issue: tracker.Issue{Number: 130, URL: "https://github.com/jim80net/flotilla/issues/130", State: "OPEN"}}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues", `{"title":"new","body":"b","labels":["operator-idea"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastCreate.Title != "new" || f.lastCreate.Body != "b" || len(f.lastCreate.Labels) != 1 {
		t.Errorf("create input not forwarded: %+v", f.lastCreate)
	}
}

func TestIssueComment_HappyPath(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues/5/comments", `{"body":"hello"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastComment.number != 5 || f.lastComment.body != "hello" {
		t.Errorf("comment not forwarded: %+v", f.lastComment)
	}
}

func TestIssueLabel_HappyPath(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues/7/labels", `{"add":["operator-idea"],"remove":["bug"]}`)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastLabel.number != 7 || f.lastLabel.add[0] != "operator-idea" || f.lastLabel.remove[0] != "bug" {
		t.Errorf("label not forwarded: %+v", f.lastLabel)
	}
}

func TestIssueClose_HappyPath(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues/9/close", ``)
	if rec.Code != 200 {
		t.Fatalf("code %d; body=%s", rec.Code, rec.Body.String())
	}
	if f.lastClose != 9 {
		t.Errorf("close not forwarded: %d", f.lastClose)
	}
}

// --- write-gate: the browser-CSRF defense (custom header + Origin) ---

func TestWriteGate_MissingCustomHeaderRejected(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/issues", strings.NewReader(`{"title":"x"}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	// NO X-Flotilla-Dash header.
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", rec.Code)
	}
	if f.calls != 0 {
		t.Error("a CSRF-rejected write must NOT reach the tracker (no gh call)")
	}
}

func TestWriteGate_CrossOriginRejected(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/issues", strings.NewReader(`{"title":"x"}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("X-Flotilla-Dash", "1")
	req.Header.Set("Origin", "http://evil.example.com") // attacker origin
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403", rec.Code)
	}
	if f.calls != 0 {
		t.Error("a cross-origin write must NOT reach the tracker")
	}
}

func TestWriteGate_NonBrowserNoOriginAllowed(t *testing.T) {
	// A non-browser client (host-shell trust on loopback) may omit Origin; the
	// required custom header is the gate. This must be ALLOWED.
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/issues/9/close", nil)
	req.Host = "127.0.0.1:8787"
	req.Header.Set("X-Flotilla-Dash", "1")
	// No Origin / Referer.
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d, want 200 (custom header suffices for non-browser)", rec.Code)
	}
}

func TestWriteGate_RefererFallbackValidated(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	// Origin absent but a cross-origin Referer present → reject.
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/issues/9/close", nil)
	req.Host = "127.0.0.1:8787"
	req.Header.Set("X-Flotilla-Dash", "1")
	req.Header.Set("Referer", "http://evil.example.com/page")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code %d, want 403 (cross-origin Referer)", rec.Code)
	}
}

// --- method-gating: a state-changing GET cannot reach a write handler ---

func TestWrite_GETOnWriteRouteIsNotAccepted(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	// GET /api/issues/9/close — no such GET route; the mux returns 405.
	rec := doGet(t, srv, "/api/issues/9/close")
	if rec.Code == 200 {
		t.Fatalf("a GET on a write route must not succeed (code %d)", rec.Code)
	}
	if f.calls != 0 {
		t.Error("a GET must never reach a write handler")
	}
}

// --- malformed body ---

func TestIssueCreate_MalformedBodyRejected(t *testing.T) {
	f := &fakeTracker{}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues", `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", rec.Code)
	}
	if f.calls != 0 {
		t.Error("a malformed body must not reach the tracker")
	}
}

func TestIssueCreate_ValidationErrorFromTracker(t *testing.T) {
	// The handler forwards to the tracker, whose empty-title validation returns a
	// 400-class typed error.
	f := &fakeTracker{err: tracker.ErrEmptyTitle}
	srv := trackerServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/issues", `{"title":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
