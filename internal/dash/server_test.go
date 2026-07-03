package dash

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// newTestServer builds a Server over a temp roster + artifacts, with a pinned
// clock, WITHOUT binding a socket (handlers are exercised via httptest).
func newTestServer(t *testing.T, rosterBody string, now time.Time) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{RosterPath: rosterPath, Bind: DefaultBind, Transport: stubTransport{}, WebTransport: stubTransport{}})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.now = func() time.Time { return now }
	return srv, dir
}

func writeSnapshot(t *testing.T, path string, snap watch.Snapshot, mtime time.Time) {
	t.Helper()
	if err := snap.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

const singleFleetRoster = `{
	"channel_id": "C1",
	"xo_agent": "xo",
	"heartbeat_interval": "20m",
	"agents": [{"name": "xo"}, {"name": "alpha", "surface": "aider"}]
}`

func TestHandleStatus_Fresh(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	writeSnapshot(t, filepath.Join(dir, "flotilla-detector-state.json"),
		watch.Snapshot{DeskStates: map[string]surface.State{"xo": surface.StateIdle, "alpha": surface.StateWorking}, XOSettled: true},
		now.Add(-30*time.Second))
	// ack file 5s old
	ackPath := filepath.Join(dir, "flotilla-xo-alive")
	if err := os.WriteFile(ackPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(ackPath, now.Add(-5*time.Second), now.Add(-5*time.Second))

	rec := doGet(t, srv, "/api/status")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var doc BoardDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Freshness.State != "fresh" {
		t.Errorf("freshness = %q, want fresh", doc.Freshness.State)
	}
	if doc.GeneratedAt == "" {
		t.Error("fresh snapshot must carry generated_at")
	}
	if len(doc.Agents) != 2 || doc.Agents[0].Role != "hub" {
		t.Errorf("agents = %+v", doc.Agents)
	}
	if !doc.XOLiveness.Acked || !doc.XOLiveness.Settled {
		t.Errorf("xo liveness = %+v", doc.XOLiveness)
	}
}

func TestHandleStatus_Absent(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	// No snapshot written.
	rec := doGet(t, srv, "/api/status")
	var doc BoardDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Freshness.State != "absent" {
		t.Errorf("freshness = %q, want absent", doc.Freshness.State)
	}
	for _, a := range doc.Agents {
		if a.State != "unknown" {
			t.Errorf("desk %q = %q, want unknown", a.Name, a.State)
		}
	}
}

func TestHandleStatus_Stale(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	// 20m heartbeat ⇒ 60m threshold; write a snapshot 2h old.
	writeSnapshot(t, filepath.Join(dir, "flotilla-detector-state.json"),
		watch.Snapshot{DeskStates: map[string]surface.State{"xo": surface.StateIdle}},
		now.Add(-2*time.Hour))
	rec := doGet(t, srv, "/api/status")
	var doc BoardDoc
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc.Freshness.State != "stale" {
		t.Errorf("freshness = %q, want stale", doc.Freshness.State)
	}
}

func TestHandleTopology(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	rec := doGet(t, srv, "/api/topology")
	var doc TopologyDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Channels) != 1 || doc.Channels[0].ChannelID != "C1" {
		t.Errorf("topology = %+v", doc)
	}
}

func TestHandleHistory(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	// Write a backlog file at the default path.
	backlogPath := filepath.Join(dir, ".flotilla-state.md")
	if err := os.WriteFile(backlogPath, []byte("## Backlog\n- [in-flight] ship dash\n- [done] design\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/history")
	var doc HistoryDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Backlog.Found || len(doc.Backlog.Unblocked) != 1 || doc.Backlog.Done != 1 {
		t.Errorf("history backlog = %+v", doc.Backlog)
	}
}

func TestHandleIndex(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	rec := doGet(t, srv, "/")
	if rec.Code != 200 {
		t.Fatalf("index code %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/static/dash.js") || !strings.Contains(body, "/static/dash.css") {
		t.Error("index must reference the static assets")
	}
	if !strings.Contains(body, "/static/tracker.js") || !strings.Contains(body, "/static/control.js") {
		t.Error("index must reference the tracker + control assets")
	}
	// The index is static chrome — it must NOT embed fleet data in a <script>.
	if strings.Contains(body, "agents") {
		t.Error("index page must not server-render fleet data (XSS surface)")
	}
}

func TestHandleStaticAssets(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	for _, path := range []string{"/static/dash.js", "/static/dash.css", "/static/tracker.js", "/static/control.js"} {
		rec := doGet(t, srv, path)
		if rec.Code != 200 {
			t.Errorf("%s code %d", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served empty", path)
		}
	}
}

// TestControlTargetsNotClobberedGuard is a source-presence regression lock for the
// #235 cubic P2: syncControlTargets() must NOT unconditionally overwrite the route/
// resume target fields on a background refresh (that silently misdirects a control
// action to a different desk than the operator typed). The fix guards refresh-time
// prefill behind a `controlTargetsTouched` flag set on operator input. There is no
// JS test runner in this repo, so — consistent with the other asset-content
// assertions above — this locks the guard's presence in the served dash.js: removing
// it (reintroducing the clobber) fails here.
func TestControlTargetsNotClobberedGuard(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	if !strings.Contains(js, "controlTargetsTouched") {
		t.Error("dash.js must guard control-target prefill with the controlTargetsTouched flag (#235: a refresh must not clobber operator input)")
	}
	// Assert BOTH call forms: the explicit desk-selection path passes true (set
	// authoritatively), and the refresh path calls the GUARDED no-arg form (prefill
	// only when untouched). Locking both is what keeps a future edit from either
	// dropping the explicit set OR reintroducing an unconditional refresh-time set.
	if !strings.Contains(js, "syncControlTargets(true)") {
		t.Error("dash.js must set targets authoritatively only on explicit desk-selection (syncControlTargets(true))")
	}
	if !strings.Contains(js, "syncControlTargets();") {
		t.Error("dash.js refresh path must call the GUARDED (non-explicit) syncControlTargets() — #235: a refresh must not force-set the target")
	}
	if !strings.Contains(js, `addEventListener("input"`) {
		t.Error("dash.js must mark control targets touched on operator input (an input listener)")
	}
}

// TestSessionMirrorGlance locks the session-mirror glance widget (design §2.5): the
// reader-map placeholder is replaced by a render that consumes /api/session-mirror.
// No JS test runner, so this asserts the served dash.js has the render + fetch and
// no longer carries the old placeholder.
func TestSessionMirrorGlance(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{"renderSessionMirror", "/api/session-mirror", "fetchMirror"} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must consume the session mirror (missing %q) — design §2.5", marker)
		}
	}
	if strings.Contains(js, "renderReaderMapPlaceholder") {
		t.Error("dash.js must replace the reader-map placeholder with the session-mirror glance — design §2.5")
	}
}

// TestGoalsCanvasAssets locks the Goals view's pan/zoom canvas (#280 Inc 1). The
// Goals view was ported from the merged flex-column layout to the operator-approved
// 2D Fleet Situation Map — an absolute tiered layout inside a transform-driven world
// with wheel/drag pan-zoom. There is no JS test runner, so — consistent with the
// other asset-content assertions — this locks (a) the canvas DOM the engine renders
// into is present in the served index, and (b) the pan/zoom engine is present in the
// served goals.js. Removing either (regressing to a static layout) fails here.
func TestGoalsCanvasAssets(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)

	rec := doGet(t, srv, "/static/goals.js")
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("/static/goals.js: code=%d len=%d (must be served)", rec.Code, rec.Body.Len())
	}
	js := rec.Body.String()
	for _, marker := range []string{"setupPanZoom", "applyTransform", "fitOverview", "drawEdges"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the pan/zoom canvas engine (missing %q)", marker)
		}
	}
	// #283: keyed/diffed updates — a structural signature drives an in-place refresh
	// that preserves element identity (focus + transient UI classes) across SSE
	// ticks. Lock the engine so a regression to full-teardown-per-tick fails here.
	for _, marker := range []string{"structuralSig", "updateInPlace"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the keyed-update engine (missing %q) — #283", marker)
		}
	}
	// Inc 2: node-detail drawer + hover chain-highlight + reapply-after-render.
	for _, marker := range []string{"openDrawer", "highlightChain", "reapplyTransient"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the detail-drawer / hover engine (missing %q) — Inc 2", marker)
		}
	}
	// Inc 4: dependency-line rendering + the conversation deep-link.
	for _, marker := range []string{"depEdges", "lightDeps", "gd-convo"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the dependency-line / deep-link engine (missing %q) — Inc 4", marker)
		}
	}
	// dash.js must expose the deep-link hook the Goals drawer calls.
	if !strings.Contains(doGet(t, srv, "/static/dash.js").Body.String(), "openConversation") {
		t.Error("dash.js must expose window.flotillaDash.openConversation for the Goals deep-link — Inc 4")
	}
	// #284 a11y: keyboard pan/zoom + focus-recenter + the aria-live announcer.
	for _, marker := range []string{"recenterOn", "nodeVisible", "updateLive"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the keyboard/a11y engine (missing %q) — #284", marker)
		}
	}

	body := doGet(t, srv, "/").Body.String()
	if !strings.Contains(body, "/static/goals.js") {
		t.Error("index must reference the goals asset")
	}
	// The canvas DOM the engine binds to: a pan/zoom viewport → transformed world →
	// (edges, tier labels, nodes) + zoom controls.
	for _, id := range []string{
		"goals-viewport", "goals-world", "goals-nodes", "goals-edges", "goals-tierlabels", "goals-zin", "goals-zout", "goals-zfit",
		"goals-drawer", "goals-drawer-body", "goals-drawer-close", "goals-help", // Inc 2: drawer + help tooltip
		"goals-live", // #284: aria-live status region
	} {
		if !strings.Contains(body, id) {
			t.Errorf("index must contain the goals canvas element #%s", id)
		}
	}
}

// --- Host-allowlist (anti-DNS-rebinding) ---

func TestHostAllowlist(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	cases := []struct {
		host string
		want int
	}{
		{"127.0.0.1:8787", 200},
		{"localhost:8787", 200},
		{"[::1]:8787", 200},
		{"evil.example.com", http.StatusForbidden},
		{"evil.example.com:8787", http.StatusForbidden},
		{"127.0.0.1:9999", http.StatusForbidden}, // right host, wrong port
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "http://x/api/status", nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		srv.handler().ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("Host %q → code %d, want %d", c.host, rec.Code, c.want)
		}
	}
}

// --- bind validation (permits IP or localhost; non-loopback allowed on a
// trusted private network per the operator override — see validateBind /
// bindIsNonLoopback. Only a non-IP, non-localhost host is rejected.) ---

func TestValidateBind(t *testing.T) {
	ok := []string{
		"127.0.0.1:8787", "localhost:8787", "[::1]:8080", "127.0.0.1:0",
		// non-loopback binds are permitted (operator override, trusted LAN)
		"0.0.0.0:8787", "192.168.1.5:8787", "10.0.0.1:8080",
	}
	for _, b := range ok {
		if err := validateBind(b); err != nil {
			t.Errorf("validateBind(%q) = %v, want nil", b, err)
		}
	}
	bad := []string{"example.com:8787", "not-an-ip:8787"} // non-IP, non-localhost host
	for _, b := range bad {
		if err := validateBind(b); err == nil {
			t.Errorf("validateBind(%q) = nil, want an error (non-IP host)", b)
		}
	}
}

func TestNewServer_PermitsNonLoopbackBind(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	_ = os.WriteFile(rosterPath, []byte(singleFleetRoster), 0o600)
	_, err := NewServer(Config{RosterPath: rosterPath, Bind: "0.0.0.0:8787"})
	// Operator override: a non-loopback bind is permitted, so NewServer must NOT
	// reject it as loopback-only. (It may still error on other missing config —
	// e.g. a coordination Transport — but that is not the bind gate.)
	if err != nil && strings.Contains(err.Error(), "loopback") {
		t.Errorf("NewServer must permit a non-loopback bind, got loopback rejection: %v", err)
	}
}

func TestResolvePaths(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	// cos_agent set ⇒ roster defaults CosLedger to <dir>/context-ledger.md.
	body := `{"xo_agent":"xo","cos_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"}]}`
	_ = os.WriteFile(rosterPath, []byte(body), 0o600)
	rc, err := loadInlineRosterAt(t, rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ResolvePaths(Config{RosterPath: rosterPath}, rc)
	if cfg.SnapshotPath != filepath.Join(dir, "flotilla-detector-state.json") {
		t.Errorf("snapshot path = %q", cfg.SnapshotPath)
	}
	if cfg.AckPath != filepath.Join(dir, "flotilla-xo-alive") {
		t.Errorf("ack path = %q", cfg.AckPath)
	}
	if cfg.BacklogPath != filepath.Join(dir, ".flotilla-state.md") {
		t.Errorf("backlog path = %q", cfg.BacklogPath)
	}
	if cfg.GoalsPath != filepath.Join(dir, "fleet-goals.json") {
		t.Errorf("goals path = %q (should default to <roster-dir>/fleet-goals.json)", cfg.GoalsPath)
	}
	if cfg.GoalsYAMLPath != filepath.Join(dir, "fleet-goals.yaml") {
		t.Errorf("goals yaml path = %q (should default to <roster-dir>/fleet-goals.yaml)", cfg.GoalsYAMLPath)
	}
	if cfg.LedgerPath != filepath.Join(dir, "context-ledger.md") {
		t.Errorf("ledger path = %q (should inherit roster CosLedger)", cfg.LedgerPath)
	}
}

// --- helpers ---

func doGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	return rec
}
