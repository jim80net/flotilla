package dash

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

// TestGoalsLayoutMindmapOnly locks the mind-map-only Goals rendering (operator 2026-07-06):
// the tree/mind-map toggle was removed, so normalizeGoalsLayout REDIRECTS every seed (incl. a
// legacy "tree"/"org") to the mind map, the body always renders data-goals-layout="mindmap",
// and the assets carry no layout picker.
func TestGoalsLayoutMindmapOnly(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	// Every seed normalizes to mindmap — no dead tree/org layout target.
	for _, in := range []string{"", "org", "mindmap", "tree", "TREE", "bogus"} {
		if got := normalizeGoalsLayout(in); got != "mindmap" {
			t.Errorf("normalizeGoalsLayout(%q) = %q, want mindmap (mind-map-only)", in, got)
		}
	}

	// default → the index seeds mindmap.
	srv, _ := newTestServer(t, singleFleetRoster, now)
	if body := doGet(t, srv, "/").Body.String(); !strings.Contains(body, `data-goals-layout="mindmap"`) {
		t.Error("index must seed data-goals-layout=\"mindmap\"")
	}

	// A legacy tree-seeded Config is REDIRECTED to mindmap (no dead tree target left behind).
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(singleFleetRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	srv2, err := NewServer(Config{RosterPath: rosterPath, Bind: DefaultBind, GoalsLayout: "tree", Transport: stubTransport{}, WebTransport: stubTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	body2 := doGet(t, srv2, "/").Body.String()
	if !strings.Contains(body2, `data-goals-layout="mindmap"`) || strings.Contains(body2, `data-goals-layout="tree"`) {
		t.Error("a legacy tree-seeded Config must be REDIRECTED to data-goals-layout=\"mindmap\"")
	}

	// The layout picker is gone from the assets — no toggle markup, no toggle JS.
	html := doGet(t, srv, "/").Body.String()
	if strings.Contains(html, "goals-layout-toggle") || strings.Contains(html, "glayout-btn") {
		t.Error("index.html must not carry the removed layout toggle")
	}
	if js := doGet(t, srv, "/static/goals.js").Body.String(); strings.Contains(js, "setLayout") || strings.Contains(js, "wireLayoutToggle") {
		t.Error("goals.js must not carry the removed layout-toggle logic")
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
		// A deploy must not leave a stale asset cached — the served assets carry no-cache so
		// the browser revalidates (the goals-toggle regression was a stale-asset symptom).
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s Cache-Control = %q, want no-cache", path, cc)
		}
	}
	// the index page (static chrome) must also be no-cache so a deploy is picked up.
	if cc := doGet(t, srv, "/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", cc)
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

// TestConversationsWave2 locks #349 Inc 4: the drive-queue chip opens a full-item modal
// (E10) and the relay-ledger thread filter normalizes the "@name" address form on BOTH
// from and to (E11 filter half) so a desk's own OUTBOUND relay lines are no longer dropped.
func TestConversationsWave2(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	// E10: queue chip → modal (structured index, not raw text attr — #419).
	for _, marker := range []string{"openConvModal", "data-bq-open", "data-bq-index", "queueItems"} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must open a drive-queue item in the modal (missing %q) — #349 Inc 4 E10 / #419", marker)
		}
	}
	if strings.Contains(js, "data-bq-text") {
		t.Error("dash.js must not carry raw backlog text in data-bq-text (operator-facing modal — #419)")
	}
	// cubic #361 P2: the conv-modal must trap Tab focus (like the goals-modal) so Tab can't
	// escape behind the backdrop, and must return focus to the opening chip on close.
	if !strings.Contains(js, "convModalReturn") {
		t.Error("conv-modal must return focus to the opening chip on close (convModalReturn) — cubic #361 P2")
	}
	if ci := strings.Index(js, "wireConvModal"); ci >= 0 {
		if !strings.Contains(js[ci:], `e.key !== "Tab"`) {
			t.Error("conv-modal must trap Tab focus inside the modal (mirrors the goals-modal trap) — cubic #361 P2")
		}
	}
	// E11 filter half: symmetric @-normalization (the prior code matched @-prefix on `to`
	// only, dropping a desk's own outbound relay lines from `from`).
	if !strings.Contains(js, "ledgerParticipant") {
		t.Error("dash.js must normalize @name symmetrically on ledger from/to (ledgerParticipant) — #349 Inc 4 E11")
	}
	if strings.Contains(js, `to === "@" + d`) {
		t.Error("dash.js must not carry the asymmetric @-only-on-`to` ledger match (the E11 filter bug) — #349 Inc 4")
	}
	// #370: rail selection is COMPOSITE (name + channel_id) — a desk name in several channels
	// must highlight only the picked copy, not every copy. Requires selectedChannel + a
	// data-channel attribute + the channel in the composite `on` check.
	if !strings.Contains(js, "selectedChannel") || !strings.Contains(js, "data-channel") {
		t.Error("dash.js rail selection must be channel-scoped (selectedChannel + data-channel) — #370")
	}
	if !strings.Contains(js, "grp.channel_id === selectedChannel") {
		t.Error("dash.js rail highlight must require the channel to match (composite key), not the desk name alone — #370")
	}
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, `id="conv-modal"`) {
		t.Error("index.html must carry the drive-queue item modal (#conv-modal) — #349 Inc 4 E10")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	// Wave 4 desktop-space audit widened the drive-queue context column 360→420px so a
	// queued line reads without clipping (the composite grid pins the exact value).
	if !strings.Contains(css, ".conv-modal.open") || !strings.Contains(css, "1fr) 420px") {
		t.Error("dash.css must style the conv-modal (E10) and set the context column to 420px (Wave 4 widen) — #349 Inc 4 / F#383")
	}
}

// TestWorkQueueModalOperatorFacing locks #419: the work-queue modal renders an
// operator-facing layer first (title/summary/status) and keeps internal ledger prose
// in a collapsed section — never as the primary view. Fail-closed render-lint.
func TestWorkQueueModalOperatorFacing(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	backlogPath := filepath.Join(dir, ".flotilla-state.md")
	jargonLine := "- [in-flight] Gate the watch scheduler :: Ready for merge tonight.\n"
	jargonLine += "- [in-flight] COS GATE PR #414 SHA c1d47a5837106354bf2654cccc7d03b473ffd1de cubic verdict pending\n"
	if err := os.WriteFile(backlogPath, []byte("## Backlog\n"+jargonLine), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/history")
	var doc HistoryDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Backlog.Unblocked) < 2 {
		t.Fatalf("unblocked = %+v", doc.Backlog.Unblocked)
	}
	for _, item := range doc.Backlog.Unblocked {
		if !TitleIsOperatorFacing(item.Title) {
			t.Errorf("API title not operator-facing: %q (raw=%q)", item.Title, item.Raw)
		}
	}
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"conv-modal-summary",
		"conv-modal-internal",
		"item.title",
		"item.internal",
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js missing operator-facing modal marker %q — #419", marker)
		}
	}
	if strings.Contains(js, `conv-modal-title").textContent = text`) {
		t.Error("dash.js must not assign raw backlog text directly to conv-modal-title — #419")
	}
	html := doGet(t, srv, "/").Body.String()
	for _, marker := range []string{
		`id="conv-modal-summary"`,
		`id="conv-modal-internal"`,
		"Internal detail",
	} {
		if !strings.Contains(html, marker) {
			t.Errorf("index.html missing operator-facing modal element %q — #419", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if strings.Contains(css, ".conv-modal-title {") {
		block := css[strings.Index(css, ".conv-modal-title {"):]
		if !strings.Contains(block[:min(200, len(block))], "text-transform: none") {
			t.Error("conv-modal-title must not uppercase the primary view — #419")
		}
	}
	if !strings.Contains(css, ".conv-modal-internal") || !strings.Contains(css, ".conv-modal-summary") {
		t.Error("dash.css must style operator-facing modal sections — #419")
	}
}

// TestDashOperatorUX421 locks operator feedback batch: scoped work queue, header
// decisions entry, goals kebab menu, DISABLE_AUTHENTICATION wiring.
func TestDashOperatorUX421(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"queueVisibleForDesk", "conv-queue-scope", "openDecisions",
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js missing #421 marker %q", marker)
		}
	}
	gjs := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{"gnode-kebab", "gnode-pop", "openDecisions", "hdr-decisions-count"} {
		if !strings.Contains(gjs, marker) {
			t.Errorf("goals.js missing #421 marker %q", marker)
		}
	}
	if strings.Contains(gjs, "gnode-godesk") {
		t.Error("goals.js must drop the wide →desk button (#421)")
	}
	html := doGet(t, srv, "/").Body.String()
	// #429: the decisions entry moved from a header button (id="hdr-decisions") to a
	// first-class tab; the always-reachable entry point is now the tab itself.
	if !strings.Contains(html, `id="tab-decisions"`) || !strings.Contains(html, `id="conv-queue-scope"`) {
		t.Error("index.html must carry the decisions tab + scoped queue labels — #421/#429")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".gnode-kebab") || !strings.Contains(css, ".hdr-decisions-count") {
		t.Error("dash.css must style kebab menu + the decisions awaiting-count badge — #421/#429")
	}
	item := ParseQueueItemDisplay("- [in-flight] Do thing @alpha")
	if item.Scope != "alpha" {
		t.Errorf("scope = %q, want alpha", item.Scope)
	}
}

// TestModalDesktopSpaceWave4 locks the Wave 4 (F#383) desktop-space + decision-log
// readability pass: the respond modal breathes on desktop (min(960px,94vw), a two-column
// brief|respond grid on wide viewports), the decision brief no longer sits in a nested
// 40vh scroll, the drive-queue item modal is widened, the conversations shell is not
// capped at the old 1500px, and an identical node/work-item brief is not rendered twice.
func TestModalDesktopSpaceWave4(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{
		"min(960px, 94vw)", // .gm-dialog widened from 520px
		`grid-template-areas: "title title" "brief respond"`, // desktop two-column layout
		"min(640px, 94vw)",  // .conv-modal-card widened from 460px
		"max-width: 1760px", // .conv-wrap uncapped from 1500px (desktop not capped for mobile)
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("dash.css must carry the Wave 4 desktop-space marker %q (F#383)", marker)
		}
	}
	// The decision brief must no longer be trapped in a nested 40vh scroll box (the modal's
	// own scroll owns the reading now) — guard against the old cramping regressing.
	if gi := strings.Index(css, ".gm-brief-full {"); gi >= 0 {
		block := css[gi:]
		if end := strings.Index(block, "}"); end >= 0 {
			block = block[:end]
		}
		if strings.Contains(block, "max-height: 40vh") {
			t.Error("dash.css .gm-brief-full must NOT re-introduce the nested 40vh scroll — the brief flows in the modal's own scroll (Wave 4)")
		}
	}
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, `class="gm-respond"`) {
		t.Error("index.html must wrap the response box in .gm-respond so the modal grid can place it beside the brief (Wave 4)")
	}
	js := doGet(t, srv, "/static/goals.js").Body.String()
	if !strings.Contains(js, "sameBrief") {
		t.Error("goals.js must de-duplicate an identical node/work-item brief in the respond modal (sameBrief) — Wave 4 readability")
	}
}

// TestConversationsCoordinatorPinWave4 locks F#383 criterion 1's rail half: the
// conversations rail pins the coordinator(s) as a first-class group even when neither is a
// channel xo_agent/member — so the CoS thread is always followable (the "I can't even see
// the CoS's conversation" gap). The identity half (BoardDoc.cos) is covered in readmodel_test.
func TestConversationsCoordinatorPinWave4(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"coordinatorNames",       // derives the coordinators (xo + distinct cos) from /api/status
		"conv-group-coordinator", // the pinned first-class group
		"st.cos",                 // reads the CoS identity the board now exposes
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must pin the coordinator thread first-class (missing %q) — F#383 criterion 1", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".chan-coordinator") {
		t.Error("dash.css must style the pinned coordinator group label (.chan-coordinator) — F#383")
	}
}

// TestThreadComposerAndOrderWave4 locks F#383 criteria 4 + 5: a composer on the thread
// (send to the selected desk/coordinator via the route-to-pane relay) and latest-at-bottom
// ordering with a jump-to-latest affordance. Without a composer the standalone-conversations
// test fails by definition — "a conversations page you cannot converse from."
func TestThreadComposerAndOrderWave4(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	html := doGet(t, srv, "/").Body.String()
	for _, marker := range []string{`id="thread-composer"`, `id="thread-composer-input"`, `id="thread-jump"`} {
		if !strings.Contains(html, marker) {
			t.Errorf("index.html must carry the thread composer/jump element %q — F#383 criteria 4/5", marker)
		}
	}
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"syncComposer",         // composer shown + labelled for the selected desk
		"scrollThreadToBottom", // latest-at-bottom pin
		"showThreadJump",       // jump-to-latest affordance
		`"/api/control/route"`, // the composer sends via the existing relay
		"inFlight",             // cubic P2: a single in-flight guard prevents a double-send on fast Enter
		"sameSel",              // cubic P3: the outcome binds to the desk the send targeted, not the new selection
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must wire the thread composer / scroll (missing %q) — F#383 criteria 4/5", marker)
		}
	}
	// The thread must sort ASCENDING (oldest first, latest at the bottom) — guard the exact
	// comparator so a refactor can't silently flip it back to newest-first.
	if !strings.Contains(js, "return at - bt;") || strings.Contains(js, "return bt - at;") {
		t.Error("dash.js renderThread must sort ascending (return at - bt) — latest-at-bottom, F#383 criterion 5")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".thread-composer") || !strings.Contains(css, ".thread-jump") {
		t.Error("dash.css must style the thread composer + jump chip (.thread-composer/.thread-jump) — F#383")
	}
}

// TestMobileTouchTargets397 locks issue #397 (mobile COMPELLING bar): the primary controls
// the ≤900px touch block previously missed — the drive-queue modal close, the jump-to-latest
// chip, and the (touch-only) pan-lock toggle — are all ≥44px hit targets, per design-book #330.
func TestMobileTouchTargets397(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	css := doGet(t, srv, "/static/dash.css").Body.String()
	// The conv-modal close joins its already-covered siblings at a 44x44 hit box.
	if !strings.Contains(css, ".gd-close, .gm-close, .conv-modal-x { width: 44px; height: 44px; }") {
		t.Error("dash.css must give the drive-queue modal close (.conv-modal-x) a 44x44 touch target — #397")
	}
	// The jump-to-latest chip is in the ≤900px 44px min-height set.
	if !strings.Contains(css, ".thread-jump,") {
		t.Error("dash.css must raise the jump-to-latest chip (.thread-jump) to the 44px touch block — #397")
	}
	// The pan-lock toggle (shown only on touch) is bumped 34px → 44px.
	if gi := strings.Index(css, ".gzoomctl .panlock {"); gi >= 0 {
		if end := strings.Index(css[gi:], "}"); end >= 0 {
			if rule := css[gi : gi+end]; !strings.Contains(rule, "min-height: 44px") {
				t.Errorf("dash.css .gzoomctl .panlock must be min-height 44px (was 34px) — #397; rule=%q", rule)
			}
		}
	} else {
		t.Error("dash.css must retain the .gzoomctl .panlock rule")
	}
}

// TestCoSThreadHonestRender405 locks the #406 fix-forward: the coordinator thread renders
// firewall-refused turns HONESTLY (a "withheld from public" badge keyed on the record's
// suppressed flag) and calibrates its forward-only history (backfill decision: don't pad a
// misleadingly-thin past — mark where the recorded history begins).
func TestCoSThreadHonestRender405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"coordinatorHistoryNote", // forward-only history calibration on the coordinator thread
		"m.suppressed",           // renders the withheld badge keyed on the record's suppressed flag
		"thread-withheld",        // the "withheld from public" badge
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must render the coordinator thread honestly (missing %q) — #406 fix-forward", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".thread-withheld") || !strings.Contains(css, ".thread-calib") {
		t.Error("dash.css must style the withheld badge (.thread-withheld) + the history calibration (.thread-calib) — #406")
	}
}

// TestSimplifyControlsQueue405 locks #405 Inc 4a: the CoS-internal control forms
// (route-to-desk / fleet-note / resume-crashed-desk) are dropped from the operator UI, and the
// jargon "Drive queue" is renamed to plain "Work queue". The operator sends to a desk from the
// thread composer, not a separate control column.
func TestSimplifyControlsQueue405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	html := doGet(t, srv, "/").Body.String()
	for _, gone := range []string{`id="route-form"`, `id="notify-form"`, `id="resume-form"`, "Drive queue"} {
		if strings.Contains(html, gone) {
			t.Errorf("index.html must DROP the CoS-internal control surface / jargon %q — #405 Inc 4", gone)
		}
	}
	if !strings.Contains(html, "Work queue") {
		t.Error("index.html must rename the drive queue to plain 'Work queue' — #405 Inc 4")
	}
	// The operator's per-thread composer must remain (that's how they message a desk now).
	if !strings.Contains(html, `id="thread-composer"`) {
		t.Error("index.html must keep the thread composer (the operator's message path) — #405 Inc 4")
	}
}

// TestHandleSessionMirror locks the /api/session-mirror contract the glance JS binds
// to: { agent, entries:[{ts, info, ...}] } with entries ascending (newest last). This
// guards the field names dash.js silently depends on (entries[last].ts / .info).
func TestHandleSessionMirror(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	mdir := filepath.Join(dir, "session-mirror")
	if err := os.MkdirAll(mdir, 0o700); err != nil {
		t.Fatal(err)
	}
	// two lines, oldest first (append order = ascending)
	lines := `{"ts":"2026-06-18T11:00:00Z","agent":"alpha","verbose":"v1","info":"older","debug":{"info":"older"},"suppressed":false}
{"ts":"2026-06-18T12:00:00Z","agent":"alpha","verbose":"v2","info":"newest","debug":{"info":"newest"},"suppressed":false}
`
	if err := os.WriteFile(filepath.Join(mdir, "alpha.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/session-mirror?agent=alpha&limit=100")
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var doc struct {
		Agent   string `json:"agent"`
		Entries []struct {
			TS   string `json:"ts"`
			Info string `json:"info"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Agent != "alpha" || len(doc.Entries) != 2 {
		t.Fatalf("doc = %+v", doc)
	}
	// ascending order — the LAST entry is the newest (what the glance renders)
	if doc.Entries[len(doc.Entries)-1].Info != "newest" {
		t.Errorf("entries must be ascending (newest last); got last=%q", doc.Entries[len(doc.Entries)-1].Info)
	}
}

// TestGoalsCellRenames405 locks #405 Inc 3 (Q2, operator-turned): "Pending"/"Aspirational" are
// renamed to plain language ("Blocked" / "Planned") that cures the confusion, kept distinct.
func TestGoalsCellRenames405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	if !strings.Contains(js, `k: "Blocked"`) || !strings.Contains(js, `k: "Planned"`) {
		t.Error("goals.js must rename the situation cells to 'Blocked' / 'Planned' — #405 Inc 3 Q2")
	}
	if strings.Contains(js, `k: "Pending"`) || strings.Contains(js, `k: "Aspirational"`) {
		t.Error("goals.js must drop the jargon labels 'Pending'/'Aspirational' from the cells — #405 Inc 3 Q2")
	}
	// The rename must be SWEPT across every surface, not just the tiles (#411 cubic): the pill
	// label + the legend read "planned", never the jargon "aspirational".
	if !strings.Contains(js, `aspirational: "planned"`) || strings.Contains(js, `aspirational: "aspirational"`) {
		t.Error("goals.js STATE_LABEL must map the aspirational state to 'planned' — #411 sweep")
	}
	if strings.Contains(js, `["aspirational", "aspirational"]`) {
		t.Error("goals.js legend must label the aspirational dot 'planned', not 'aspirational' — #411 sweep")
	}
	if html := doGet(t, srv, "/").Body.String(); strings.Contains(html, "ghosted aspirational") {
		t.Error("index.html help tooltip must read 'ghosted planned', not 'aspirational' — #411 sweep")
	}
}

// TestDecisionPage405 locks #405 Inc 2 (the operator's centerpiece) as reshaped by #429:
// the decision reading room is a first-class TAB rendering as a full page — no modal, no
// secondary scrollbar. It formats the canonical 6-element briefs with references (links)
// and demo images inline, each decision showing which goal it drives.
func TestDecisionPage405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, `id="view-decisions"`) || !strings.Contains(html, `id="gdec-list"`) {
		t.Error("index.html must carry the decision-page view (#view-decisions / #gdec-list) — #405 Inc 2 / #429")
	}
	// #429: the Decisions entry is a real role="tab" SPA tab, and the old modal is GONE —
	// a leftover #goals-decisions dialog would mean the refactor shipped both surfaces.
	if !strings.Contains(html, `id="tab-decisions" class="tab" data-view="decisions" role="tab"`) {
		t.Error(`index.html must carry the Decisions tab as a role="tab" SPA view switch — #429`)
	}
	if strings.Contains(html, `id="goals-decisions"`) || strings.Contains(html, "data-gdec-close") {
		t.Error("index.html must NOT carry the retired decisions modal (#goals-decisions) — #429")
	}
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"gatherDecisions",     // collects every open decision fleet-wide
		"openDecisions",       // per-open entry: instant paint + always refetch (#429)
		"paintDecisions",      // the pure painter, also driven by live ticks (#429)
		"decisionsVisible",    // live ticks reach the open tab (#429)
		"data-open-decisions", // the Awaiting-you tile trigger
		"gdec-ctx-link",       // "Drives" — which goal the decision drives (linked)
		"gm-brief-img",        // demo images rendered in a brief
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must implement the decision page (missing %q) — #405 Inc 2 / #429", marker)
		}
	}
	// #429 + the cubic #363 discipline: a failed goals load must surface an honest
	// unavailable state, never a clean-looking "nothing awaiting you".
	if !strings.Contains(js, "unavailable right now") {
		t.Error("goals.js paintDecisions must render an honest unavailable state on a failed goals load — #429")
	}
	// renderBrief must render reference links (the "references littered throughout" requirement).
	if !strings.Contains(js, `rel="noopener noreferrer"`) {
		t.Error("goals.js renderBrief must render reference links (http(s)-restricted anchors) — #405 Inc 2")
	}
	// #429: dash.js owns the view switch (showView("decisions") repaints the page).
	djs := doGet(t, srv, "/static/dash.js").Body.String()
	if !strings.Contains(djs, `"decisions"`) || !strings.Contains(djs, "openDecisionsView") {
		t.Error(`dash.js must route the decisions view (VIEWS + openDecisionsView) — #429`)
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".gdec-list") || !strings.Contains(css, ".gdec-card") {
		t.Error("dash.css must style the decision reading room (.gdec-list/.gdec-card) — #405 Inc 2")
	}
	// #429: no modal chrome and no inner scrollbar — the document owns the one scroll.
	if strings.Contains(css, ".gdec-sheet") || strings.Contains(css, ".gdec-backdrop") {
		t.Error("dash.css must NOT carry the retired decisions modal chrome (.gdec-sheet/.gdec-backdrop) — #429")
	}
	if gi := strings.Index(css, ".gdec-list"); gi >= 0 {
		if line := css[gi : gi+strings.Index(css[gi:], "\n")]; strings.Contains(line, "overflow-y") {
			t.Error("the decisions list must not own a secondary scrollbar (overflow-y) — #429")
		}
	}
}

// TestDecisionsCountUnified451 locks the one-population rule: the tab badge showed the
// server's gated-NODE count (6) while the page header counted decision CARDS (3) —
// same screen, two numbers (#451). Every decisions surface (badge, Awaiting-you tile,
// page header, screen-reader announcement) now derives from ONE client-side count.
// #501 refined the population: the count is COMPLETE decisions only, with brief-less
// gated items VISIBLE in their own labeled "preparing" bucket (fail-closed) — still
// one derivation, never two disagreeing numbers.
func TestDecisionsCountUnified451(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"decisionsCount",                       // the single derivation every surface reads
		"two count sources may never disagree", // the #451 invariant, restated across the #501 refinement
		"produced NOTHING above",               // gatherDecisions' catch-all — no gated item can vanish
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must unify the decisions count (missing %q) — #451", marker)
		}
	}
	// The badge/tile/announcement must not read the server node-population count anymore.
	// Each marker is the EXACT old-code pattern (the badge read c.awaiting via a variable
	// indirection, `awaiting = c.awaiting || 0` — cubic #458 P3: a marker string that never
	// existed in the old code guards nothing).
	for _, stale := range []string{
		"awaiting = c.awaiting", // the badge's old source (var awaiting = c.awaiting || 0)
		`announce((c.awaiting`,  // the old screen-reader clause
		`v: c.awaiting`,         // the old Awaiting-you tile value
	} {
		if strings.Contains(js, stale) {
			t.Errorf("goals.js still reads counts.awaiting for a decisions surface (%q) — #451", stale)
		}
	}
}

// TestBriefTableRenderMarkers450 locks GFM pipe-table support in the BRIEF renderer
// (decision cards / respond modal / drawer): #447 gave the parade renderer tables, but
// briefs — the surface built for structured decision content (cost tables, tradeoff
// matrices) — still rendered raw pipes (#450). Same escape-first shape, ported.
func TestBriefTableRenderMarkers450(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"isTableDelimiter", // header+delimiter detection (never mistakes prose pipes)
		"splitTableRow",    // \|-aware cell splitting
		"tableAligns",      // fixed {left,center,right} alignment set
		"gm-table",         // the brief table emit
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js renderBrief must support GFM pipe tables (missing %q) — #450", marker)
		}
	}
	if css := doGet(t, srv, "/static/dash.css").Body.String(); !strings.Contains(css, ".gm-table") {
		t.Error("dash.css must style the brief table (.gm-table) — #450")
	}
}

// TestDashInc5Shell405 locks the three shell items from #405 Inc 5:
//   - Part A: Parade tab in the header nav that navigates to /parade (a navigation-out link).
//   - Part B: Unseen-content dot on each tab, driven by per-browser localStorage signatures.
//   - Part C: Hub-and-spoke brand mark SVG used consistently in the header.
func TestDashInc5Shell405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)

	// ── Part A: Parade tab ─────────────────────────────────────────────────────
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, `id="tab-parade"`) {
		t.Error(`index.html must carry a Parade tab with id="tab-parade" — #405 Inc 5 item 8`)
	}
	if !strings.Contains(html, `href="/parade"`) {
		t.Error(`index.html Parade tab must navigate to /parade (href="/parade") — #405 Inc 5 item 8`)
	}
	// Parade tab must NOT carry a data-view attribute (it is a navigation-out, not an SPA panel).
	if i := strings.Index(html, `id="tab-parade"`); i >= 0 {
		chunk := html[i : i+200]
		if strings.Contains(chunk, `data-view="parade"`) {
			t.Error(`Parade tab must not carry data-view="parade" — it is a nav-out link, not an SPA view — #405 Inc 5 item 8`)
		}
	}
	// /parade must respond 200 (the standalone parade page is served).
	if rec := doGet(t, srv, "/parade"); rec.Code != 200 {
		t.Errorf("/parade page code %d, want 200 — #405 Inc 5 item 8", rec.Code)
	}
	// dash.js must only wire button tabs (those with data-view) to showView —
	// the Parade <a> must not accidentally reach showView("parade") on click.
	js := doGet(t, srv, "/static/dash.js").Body.String()
	if !strings.Contains(js, `.tab[data-view]`) {
		t.Error(`dash.js must select only ".tab[data-view]" for the SPA click handler (not ".tab") so the Parade <a> is excluded — #405 Inc 5 item 8`)
	}
	// cubic #416 P2: the Parade defer-to-record-view path must NOT hijack modified
	// clicks (⌘/Ctrl/Shift/Alt or non-primary button) — those open /parade in a new
	// tab/window and must keep native browser behavior. The handler guards on the
	// modifier keys before any preventDefault.
	if !strings.Contains(js, `e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0`) {
		t.Error(`dash.js Parade tab click must let modified/non-primary clicks navigate natively (guard before preventDefault) — cubic #416 P2`)
	}
	// cubic #416 P1: only role="tab" elements may be direct children of role="tablist".
	// The Parade <a> is a navigation-out link, not a tab, so it must live OUTSIDE the
	// tablist — the three SPA view-tabs sit in an inner role="tablist" group, and the
	// Parade link is a sibling in the nav (still visually a tab).
	if strings.Contains(html, `class="tabs" role="tablist"`) {
		t.Error(`index.html: role="tablist" must NOT be on the .tabs nav that contains the Parade <a> — an <a> is not a valid tablist child (cubic #416 P1)`)
	}
	if !strings.Contains(html, `class="tab-group" role="tablist"`) {
		t.Error(`index.html: the three SPA view-tabs must be wrapped in an inner role="tablist" group (.tab-group) so only role="tab" elements are tablist children (cubic #416 P1)`)
	}
	// The tablist group must close BEFORE the Parade link — i.e. the Parade <a> is a
	// sibling of the group, not inside it. html/template strips the HTML comments, so
	// the group-closing </span> sits immediately before the Parade anchor (only
	// whitespace between). This regex confirms the group closes, THEN the Parade <a>
	// opens — the Parade link is outside role="tablist" (cubic #416 P1).
	if !regexp.MustCompile(`</span>\s*<a id="tab-parade"`).MatchString(html) {
		t.Error(`index.html: the Parade <a> must sit AFTER the tablist group closes (outside role="tablist") — cubic #416 P1`)
	}

	// ── Part B: Unseen-content dots ────────────────────────────────────────────
	// Each SPA tab must carry an unseen-dot span.
	for _, id := range []string{"dot-conversations", "dot-goals", "dot-issues", "dot-parade"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("index.html must carry unseen-dot element %q — #405 Inc 5 item 9", id)
		}
	}
	// dash.js must carry the unseen-dot module functions.
	for _, marker := range []string{
		"unseenKey",      // localStorage key helper
		"refreshDots",    // updates all dot visibility
		"markTabViewed",  // clears the dot + stores signature on tab open
		"computeConvSig", // derives conversations signature from cache.history
		"peekGoalsSig",   // peeks /api/goals for goals signature
		"peekIssuesSig",  // peeks /api/issues for issues signature
		"peekParadeSig",  // peeks /api/parades for parade signature
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must implement the unseen-dot module (missing %q) — #405 Inc 5 item 9", marker)
		}
	}
	// showView must call markTabViewed so the dot clears when the operator opens a tab.
	if si := strings.Index(js, "function showView"); si >= 0 {
		// use a generous window — the function body includes several long comment lines
		// (widened for the #429 decisions-tab branch added to the function body)
		if !strings.Contains(js[si:si+1600], "markTabViewed") {
			t.Error("dash.js showView must call markTabViewed to clear the unseen dot on tab open — #405 Inc 5 item 9")
		}
	}
	// cubic #416 P2: the issues signature must read the tracker's camelCase timestamp
	// fields (updatedAt / createdAt — the gh `--json` shape), NOT snake_case, or an
	// edit to an existing issue (count unchanged) never lights the dot.
	if strings.Contains(js, "updated_at") || strings.Contains(js, "created_at") {
		t.Error(`dash.js peekIssuesSig must read camelCase updatedAt/createdAt (not snake_case) — the /api/issues shape (cubic #416 P2)`)
	}
	if !strings.Contains(js, "updatedAt") {
		t.Error(`dash.js peekIssuesSig must read the issue's updatedAt field — cubic #416 P2`)
	}
	// cubic #416 P2: the Parade dot must clear even on a fast click before its sig loads —
	// the click handler defers navigation, peeks the sig, then stores it (pendingView +
	// deferred nav). Guard the pending mechanism + the deferred-navigation path.
	if !strings.Contains(js, "pendingView") {
		t.Error("dash.js must track a pending tab-view so a fast Parade click records once the sig loads (cubic #416 P2)")
	}
	if !strings.Contains(js, "window.location.href = href") {
		t.Error("dash.js Parade click must defer navigation until the sig is stored when it isn't yet loaded (cubic #416 P2)")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".unseen-dot") {
		t.Error("dash.css must define .unseen-dot — #405 Inc 5 item 9")
	}
	if !strings.Contains(css, `data-active="true"`) {
		t.Error(`dash.css must show the dot when data-active="true" — #405 Inc 5 item 9`)
	}
	// cubic #416 P3: the Parade link's visited colour must be the explicit tab token,
	// not `inherit` (which would pull the parent nav → body var(--ink-2), darkening it).
	if strings.Contains(css, ".tab-parade:visited { color: inherit") {
		t.Error(`dash.css .tab-parade:visited must not use color: inherit (renders var(--ink-2), darker than the tab base) — cubic #416 P3`)
	}
	if !strings.Contains(css, ".tab-group") {
		t.Error("dash.css must define .tab-group (the inner tablist flex group preserving the strip layout) — cubic #416 P1")
	}

	// ── Part C: Hub-and-spoke brand mark ──────────────────────────────────────
	// The brand mark must be an inline SVG (hub node + spokes + satellite nodes).
	if !strings.Contains(html, `class="brand-mark"`) {
		t.Error("index.html must carry the brand-mark element — #405 Inc 5 item 10")
	}
	// The SVG must include the hub circle and spoke lines (look for the characteristic
	// viewBox and at least one circle + line element within the brand-mark section).
	if !strings.Contains(html, `viewBox="0 0 24 24"`) {
		t.Error("index.html brand-mark SVG must have viewBox='0 0 24 24' — #405 Inc 5 item 10")
	}
	if !strings.Contains(html, `cx="12" cy="12"`) {
		t.Error("index.html brand-mark SVG must have a hub circle at (12,12) — #405 Inc 5 item 10")
	}
	// dash.css must style the brand-mark with fill/stroke (SVG, not border-based).
	if !strings.Contains(css, "fill: var(--cyan)") {
		t.Error("dash.css .brand-mark must use fill: var(--cyan) for the SVG icon — #405 Inc 5 item 10")
	}
	if strings.Contains(css, "border-left:") {
		if i := strings.Index(css, ".brand-mark"); i >= 0 {
			if strings.Contains(css[i:i+200], "border-left:") {
				t.Error("dash.css .brand-mark must not use border-left (replaced by SVG icon) — #405 Inc 5 item 10")
			}
		}
	}
	// The parade page must also carry the hub-and-spoke brand mark.
	paradeHTML := doGet(t, srv, "/parade").Body.String()
	if !strings.Contains(paradeHTML, `class="brand-mark"`) {
		t.Error("parade.html must carry the brand-mark SVG for consistent fleet identity — #405 Inc 5 item 10")
	}
	if !strings.Contains(paradeHTML, `cx="12" cy="12"`) {
		t.Error("parade.html brand-mark SVG must have the hub circle at (12,12) — #405 Inc 5 item 10")
	}
}

// TestMindmapLimbHue locks the mind-map per-limb hue (operator polish track): each top-level
// limb (a hub child/root and its subtree) is coloured with a distinct hue that rides on the
// branch EDGES, so the limbs are visually traceable while node cards keep their status colour.
func TestMindmapLimbHue(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"computeLimbHues",           // assigns a hue per limb (mind-map only)
		"limbStroke",                // resolves a node's limb colour
		"gedge-limb",                // the limb-coloured branch edge
		`goalsLayout !== "mindmap"`, // guarded: no-op for tree/org (status edges preserved)
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must carry the mind-map per-limb hue (missing %q)", marker)
		}
	}
	if css := doGet(t, srv, "/static/dash.css").Body.String(); !strings.Contains(css, ".gedge-limb") {
		t.Error("dash.css must style the limb-coloured branch edge (.gedge-limb)")
	}
}

// TestMindmapSequenceOrder locks F12: the mind map lays sibling branches out in the authored
// `after` sequence so a limb reads as a roadmap. The server validation (sibling-scoped, acyclic)
// is covered in goals_test; this locks the frontend ordering half.
func TestMindmapSequenceOrder(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"function sequenceOrder", // the stable topological sort by `after`
		"n.after",                // it reads each node's authored after list
		"sequenceOrder(ring1)",   // applied to the top-level limbs
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must order mind-map siblings by the authored `after` sequence (missing %q) — F12", marker)
		}
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
	// #302: node click → Conversations (nodeActivate), the ⚠ respond modal (openModal),
	// and the per-node control chips.
	for _, marker := range []string{"nodeActivate", "openModal", "gnode-kebab"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the #302 interaction (missing %q)", marker)
		}
	}
	// org-graph v2 Inc A (#312 schema): harness badge + priorities/milestones lists,
	// and the v2 scope labels (scopeNoun reads flotilla/desk, not the retired
	// fleet/project tokens). Also lock the enrichment into structuralSig so a
	// height-affecting change forces a rebuild (#283).
	for _, marker := range []string{"gnode-harness", "gnode-prios", "gnode-miles", "n.priorities", "n.milestones"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the org-graph v2 enrichment (missing %q) — Inc A", marker)
		}
	}
	if !strings.Contains(js, `"flotilla"`) || !strings.Contains(js, `s === "desk"`) {
		t.Error("goals.js scopeNoun must read the v2 scope tokens (flotilla/desk) — #312")
	}
	// The org-graph v2 hub-spoke GEOMETRY stays in the code, dormant — the tree/mind-map/org
	// LAYOUT PICKER was removed (setLayout/glayout-btn), but layoutOrg + hub_center remain so
	// this is a view-picker simplification, not a rebuild of the map.
	for _, marker := range []string{"layoutOrg", "goalsLayout", "hub_center"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the dormant org-graph v2 geometry (missing %q)", marker)
		}
	}
	// Mind-map-only (operator 2026-07-06): goalsLayout is a constant "mindmap" — no tree/org
	// seed read, and the toggle logic is gone.
	if !strings.Contains(js, `var goalsLayout = "mindmap"`) {
		t.Error("goals.js must hardcode goalsLayout to the mind map (mind-map-only)")
	}
	if strings.Contains(js, "setLayout") || strings.Contains(js, "wireLayoutToggle") {
		t.Error("goals.js must not carry the removed layout-toggle logic")
	}
	// #324 Inc 2: a roster-materialized desk (source==="roster") is a live entity, never
	// ghosted as aspirational even when it has no work/children.
	if !strings.Contains(js, `n.source !== "roster"`) {
		t.Error("visToken must treat a roster-materialized desk as live, not aspirational (#324 Inc 2)")
	}
	// #324 Inc 3: collaboration containers — the doc's collaborations are consumed, desks in
	// a group are clustered adjacent, and a dotted container is drawn around them.
	for _, marker := range []string{"collaborations", "clusterAdjacent", "collabMarkup", "gcollab"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the collaboration-container engine (missing %q) — #324 Inc 3", marker)
		}
	}
	// #347: the respond modal renders a gated item's decision package (brief markdown) in
	// full, with an honest empty state when the desk hasn't attached one.
	for _, marker := range []string{"renderBrief", "gm-brief-full", "No decision brief yet"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must render the decision brief in the respond modal (missing %q) — #347", marker)
		}
	}
	// #349 A2: cell-click SWAP — the node body opens the drawer (nodeActivate → openDrawer),
	// the conversation jump is a distinct → desk button (goToDesk / gnode-godesk); and the
	// drawer participates in browser history (restoreNode for the popstate restore).
	for _, marker := range []string{"gnode-kebab", "gnode-pop", "restoreNode"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the #349 nav cell-swap + history hook (missing %q)", marker)
		}
	}
	// dash.js owns the history controller: pushState per view/desk change + a popstate
	// restore so navigation is reversible (#349 A1).
	dashJS := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{"pushNav", "popstate", "history.pushState"} {
		if !strings.Contains(dashJS, marker) {
			t.Errorf("dash.js must retain the #349 browser-history controller (missing %q)", marker)
		}
	}
	// cubic #354 P2: applyNav must reset the restoringNav guard in a finally — a throwing
	// restore must never permanently suppress pushNav (history silently dead for the session).
	if ai := strings.Index(dashJS, "function applyNav"); ai >= 0 {
		if fi := strings.Index(dashJS[ai:], "} finally {"); fi < 0 || fi > 1600 {
			t.Error("applyNav must wrap its body in try/finally so the restoringNav guard always resets (cubic #354 P2)")
		}
	}
	// cubic #354 P2: the modal anchors focus-restore by NODE id (re-queried live on close),
	// so an in-modal drill-in re-render can't leave close() focusing a detached element.
	if !strings.Contains(js, "modalReturnId") {
		t.Error("goals.js must anchor modal focus-restore by node id (modalReturnId) — cubic #354 P2")
	}
	// #349 B — click-through completeness: gated items click through to their target
	// (gm-item-link), an aggregate node routes to its DOWNSTREAM decisions (downstreamGated),
	// and the status pill opens the blockers list.
	for _, marker := range []string{"downstreamGated", "gm-item-link", "Downstream decisions", "data-goto-desk", "data-open-node"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the #349 click-through completeness (missing %q)", marker)
		}
	}
	// #349 Inc 3 — taxonomy: a dependency-gated goal reads as PENDING (calm violet, tied to
	// the depends_on arcs), distinct from decision-gated (awaiting/amber) and blocked (red).
	// The client carries the legend/pill token; the server relabels it (see goals_test.go).
	for _, marker := range []string{"pending", "waiting on a dependency"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must carry the #349 pending taxonomy token (missing %q)", marker)
		}
	}
	// #349 Inc 5 F13 — history of done: realized goals gathered into a dedicated list
	// (a row opens the goal's drawer). The client renders it; the CSS + HTML host it.
	for _, marker := range []string{"renderDoneHistory", "gdone-row", "goals-done-list"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must render the #349 Inc 5 history-of-done (missing %q)", marker)
		}
	}
	// cubic #363 P2: the done panel must distinguish a load ERROR (found===false) from a
	// genuine empty — an unavailable state, not "No realized goals yet" dressing an error.
	if !strings.Contains(js, "Realized goals are unavailable") {
		t.Error("renderDoneHistory must show an honest unavailable state when the goals doc fails to load (cubic #363 P2)")
	}
	// mobile-QA #330: the node controls counter-scale the fit-to-view zoom (--ctl-scale)
	// so they stay screen-constant (tappable) on phone, and the css reveals ⓘ on touch.
	if !strings.Contains(js, "--ctl-scale") {
		t.Error("applyTransform must set --ctl-scale so node controls stay screen-constant under zoom (#330)")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{".gpill-pending", ".gedge-pending", ".gnode.state-pending", ".gdot-pending"} {
		if !strings.Contains(css, marker) {
			t.Errorf("dash.css must style the #349 pending taxonomy state (missing %q)", marker)
		}
	}
	// #349 Inc 5 F13: the history-of-done panel is hosted in the page + styled.
	if h := doGet(t, srv, "/").Body.String(); !strings.Contains(h, `id="goals-done"`) {
		t.Error("index.html must host the #349 Inc 5 history-of-done panel (#goals-done)")
	}
	if !strings.Contains(css, ".gdone-row") || !strings.Contains(css, ".goals-done-list") {
		t.Error("dash.css must style the #349 Inc 5 history-of-done (.gdone-row/.goals-done-list)")
	}
	if !strings.Contains(css, "var(--ctl-scale") || !strings.Contains(css, "@media (hover: none)") {
		t.Error("dash.css must counter-scale .gnode-ctl and reveal ⓘ on touch (@media hover:none) — #330")
	}
	// mobile-QA #330: deliberate-pan gate — a touch-drag scrolls the PAGE through the map
	// (touch-action:pan-y) until the operator toggles "move map" (pan-active → touch-action
	// none). Mouse panning is unchanged.
	if !strings.Contains(js, `e.pointerType === "touch"`) || !strings.Contains(js, "touchPanActive") {
		t.Error("goals.js must gate touch panning behind a deliberate toggle (#330 nested-scroll trap)")
	}
	if !strings.Contains(css, "touch-action: pan-y") || !strings.Contains(css, ".pan-active") {
		t.Error("dash.css must default the viewport to touch-action:pan-y and reclaim it on .pan-active (#330)")
	}
	// The goals panel-head still wraps so head-right (the legend) drops below the title on a
	// squeezed header. The layout toggle it once held was removed (mind-map-only 2026-07-06),
	// so its .goals-layout-toggle / .glayout-btn CSS must be gone.
	if !strings.Contains(css, ".goals-panel > .panel-head") {
		t.Error("dash.css must let the goals panel-head wrap (.goals-panel > .panel-head)")
	}
	if strings.Contains(css, ".goals-layout-toggle {") || strings.Contains(css, ".glayout-btn ") || strings.Contains(css, ".glayout-btn{") || strings.Contains(css, ".glayout-btn,") {
		t.Error("dash.css must not retain the removed layout-toggle rules (.goals-layout-toggle / .glayout-btn)")
	}
	// leafWeights (not leafCount): #364 extracted the shared leaf-weight helper used by org + mindmap.
	for _, marker := range []string{"leafWeights", "reach(", "nodeW", "RING_GAP"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the #324 content-aware org geometry (missing %q)", marker)
		}
	}
	if strings.Contains(js, "RING_STEP") {
		t.Error("goals.js must drop the fixed RING_STEP — org radii are content-aware (#324)")
	}
	// mind-map: the SOLE radial rendering (children fan LOCALLY from each parent — limbs +
	// sub-branches — with curved edges). The layout functions stay; the picker is gone.
	for _, marker := range []string{"layoutMindmap", "isRadial"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must carry the mind-map layout (missing %q)", marker)
		}
	}
	// mind-map geometry must hold at real fleet depth: disjoint angular sectors + a
	// collision-relaxation pass (the hub pinned) so 19+ nodes / deep chains don't overlap.
	if !strings.Contains(js, "Collision relaxation") {
		t.Error("layoutMindmap must run a collision-relaxation pass so real-depth fleets don't overlap")
	}
	// structuralSig must include the enrichment (priorities/milestones/harness) so an
	// add/remove of a height-affecting field triggers a full rebuild, not a stale
	// in-place text swap. Guard the index BEFORE slicing (a missing function must
	// fail the test, not panic on js[-1:]) — cubic #315 P3.
	sigStart := strings.Index(js, "function structuralSig")
	if sigStart < 0 {
		t.Fatal("goals.js must define structuralSig")
	}
	sigEnd := strings.Index(js[sigStart:], "function updateInPlace")
	if sigEnd < 0 {
		t.Fatal("structuralSig must precede updateInPlace in goals.js")
	}
	sig := js[sigStart : sigStart+sigEnd]
	for _, f := range []string{"n.priorities", "n.milestones", "harness"} {
		if !strings.Contains(sig, f) {
			t.Errorf("structuralSig must include enrichment field %q (#283 height contract)", f)
		}
	}
	// #324 Inc 3: collaboration membership drives clusterAdjacent (it MOVES nodes), so a
	// lane change is structural — structuralSig must fold in collaborations, or a
	// collaborations-only change would ride the in-place fast path and never re-cluster
	// (cubic #335 P2).
	if !strings.Contains(sig, "collab") || !strings.Contains(sig, "collaborations.map") {
		t.Error("structuralSig must fold in collaborations so a lane change forces a re-layout (#335 P2)")
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
		"goals-live",                                           // #284: aria-live status region
		"goals-modal", "goals-modal-input", "goals-modal-send", // #302: intervention modal
	} {
		if !strings.Contains(body, id) {
			t.Errorf("index must contain the goals canvas element #%s", id)
		}
	}
	// The layout picker is GONE — the map is mind-map-only (operator 2026-07-06). No toggle
	// chrome of any layout (tree/mindmap/org) may remain in the markup.
	if strings.Contains(body, "glayout-btn") || strings.Contains(body, "goals-layout-toggle") || strings.Contains(body, "data-layout=") {
		t.Error("index must NOT carry the removed goals layout toggle (mind-map-only)")
	}
}

// TestConversationsFormatting locks the #302 Conversations rendering: the thread is
// colour-coded by speaker (speakerHue → --spk) and the drive queue formats each
// backlog line into a status chip (backlogItem → .bq-marker) instead of a raw blob.
func TestConversationsFormatting(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{"speakerHue", "thread-from", "backlogItem", "bq-marker"} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must retain the #302 conversations formatting (missing %q)", marker)
		}
	}
	// speakerHue must normalize like thread identity (case-insensitive, trimmed)
	// so casing/spacing variants of one speaker share a colour (cubic #308 P2).
	if !strings.Contains(js, `.trim().toLowerCase()`) {
		t.Error("speakerHue must normalize the name (trim + lowercase) before hashing")
	}
}

// TestRailRegroupAndQueueFormat405 locks #405 Inc 4b:
//   - Part A (item 2a): the work queue renders each item as a structured row
//     (bq-row grid: state-chip column + text column), not a text blob. Timestamps
//     are NOT rendered (the backlog data carries none).
//   - Part B (item 4): buildRailGroups regroups the rail into Fleet Command (XOs)
//   - per-flotilla groups with the CoS filtered from every project channel. The
//     coordinator-pin logic is preserved so the CoS thread stays reachable.
func TestRailRegroupAndQueueFormat405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()

	// Part A — structured work-queue rows.
	for _, marker := range []string{
		"bq-row",       // CSS class that drives the grid layout on each item
		"bq-marker",    // the state chip column
		"bq-text-wide", // no-marker items span both columns so text is not cramped (#415 review)
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js backlogItem must render structured rows (missing %q) — #405 Inc 4b item 2a", marker)
		}
	}
	// The structured row must NOT render timestamps — the backlog data has none and
	// the decision was explicitly NO-timestamps.
	if strings.Contains(js, "bq-time") || strings.Contains(js, "bq-ts") {
		t.Error("backlogItem must not render timestamps — backlog items carry no per-item timestamp (#405 Inc 4b item 2a)")
	}

	// Part B — rail regroup (fleet-command + per-flotilla, CoS filtered).
	for _, marker := range []string{
		"cosKeys",          // filters coordinator names from per-flotilla member lists
		"fleetCmdChannels", // fleet-command channel separation
		"projectChannels",  // project/flotilla channel separation
		"Fleet Command",    // label string for the Fleet Command group
		"fleet-command",    // role string used in the regroup + CSS class
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js buildRailGroups must implement the fleet-command regroup (missing %q) — #405 Inc 4b item 4", marker)
		}
	}
	// The coordinator-pin logic must remain (CoS must still be reachable even if not
	// in any channel — the "coordinator" group is the fallback).
	if !strings.Contains(js, "conv-group-coordinator") {
		t.Error("buildRailGroups must retain the coordinator-pin group (conv-group-coordinator) — F#383 criterion 1")
	}
	// Fleet Command group must get a distinct CSS class separate from coordinator.
	if !strings.Contains(js, "conv-group-fleet-command") {
		t.Error("buildRailGroups must assign .conv-group-fleet-command to fleet-command groups — #405 Inc 4b item 4")
	}

	css := doGet(t, srv, "/static/dash.css").Body.String()
	// Part A CSS: bq-row grid layout.
	if !strings.Contains(css, ".backlog-item.bq-row") {
		t.Error("dash.css must style the structured work-queue rows (.backlog-item.bq-row) — #405 Inc 4b item 2a")
	}
	if !strings.Contains(css, "grid-template-columns") {
		t.Error("dash.css .bq-row must use a grid layout (grid-template-columns) to align chip + text — #405 Inc 4b")
	}
	// A no-marker item's text must span both columns so it fills full width instead of
	// being cramped into the 5.5rem chip column (#415 review).
	if !strings.Contains(css, ".bq-text-wide") {
		t.Error("dash.css must let no-marker items span both columns (.bq-text-wide grid-column) — #415 review")
	}
	// Part B CSS: Fleet Command group header style.
	if !strings.Contains(css, ".chan-fleet-command") {
		t.Error("dash.css must style the Fleet Command group label (.chan-fleet-command) — #405 Inc 4b item 4")
	}
	if !strings.Contains(css, ".conv-group-fleet-command") {
		t.Error("dash.css must style the Fleet Command group container (.conv-group-fleet-command) — #405 Inc 4b item 4")
	}
}

// TestThreadMerge locks the session-mirror ↔ CoS-ledger interleave (design §2.4,
// UI Inc 2): renderThread merges the desk's own session-mirror turn-finals with
// the relay ledger into one chronological timeline, with the cross-desk identity
// guard, session-output styling, and the re-announce dedup. There is no JS test
// runner in this repo, so — like the other asset-content locks — this asserts the
// merge engine's presence in the served dash.js + css so a regression to
// ledger-only (or a dropped guard/dedup) fails here.
func TestThreadMerge(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"mirrorEntriesForSelected", // guarded read of cache.mirror.entries for the selected desk
		"threadMirrorMsg",          // session-output turn renderer
		"threadLedgerMsg",          // relay-line renderer
		"thread-mirror",            // session-output styling hook
		"lastThreadKey",            // re-announce / scroll-reset dedup
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must retain the §2.4 thread-merge engine (missing %q)", marker)
		}
	}
	// The interleave must SORT the two streams (ledger is newest-first, mirror
	// newest-last) — a merge that forgot to sort would render out of order.
	if !strings.Contains(js, "items.sort(") {
		t.Error("renderThread must sort the merged ledger+mirror items chronologically")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".thread-mirror") {
		t.Error("dash.css must style the session-output turn (.thread-mirror)")
	}
}

// TestDebugTier locks the session-mirror debug tier (design §2.3 UI half, UI Inc 3):
// a live info⇄debug toggle reveals each mirror entry's collapsible debug detail
// (reader-map envelope, mirror note, firewall warn-terms). The payload is always in
// the ledger, so this is a render toggle — no dormant env gate. As with the other
// asset-content locks, this asserts the engine's presence in the served assets so a
// regression (dropping the tier or the toggle) fails here.
func TestDebugTier(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	for _, marker := range []string{
		"debugBlock",         // per-entry debug renderer
		"setMirrorVerbosity", // the toggle handler
		"mirrorVerbosity",    // the detail-level state (also folded into the dedup keys)
		"thread-debug",       // the collapsible detail element
		"warn_terms",         // firewall diag surfaced
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must retain the §2.3 debug-tier engine (missing %q)", marker)
		}
	}
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, "mv-btn") || !strings.Contains(html, `data-verbosity="debug"`) {
		t.Error("index must carry the info⇄debug verbosity toggle (mv-btn / data-verbosity)")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".thread-debug") {
		t.Error("dash.css must style the debug tier (.thread-debug)")
	}
	// cubic #309 P3: the mirror-body typography is folded into the shared thread-gist
	// rule, not duplicated. Assert the combined selector whitespace-insensitively (the
	// two selectors sit on separate lines in source) so a CSS reformat can't spuriously
	// fail this — collapse all whitespace, then check the comma-adjacent selector.
	cssNoWS := strings.Join(strings.Fields(css), "")
	if !strings.Contains(cssNoWS, ".thread-gist,.thread-mirror-body{") {
		t.Error("dash.css must share the thread-gist / thread-mirror-body base rule (no duplication — #309 P3)")
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
	if cfg.SessionMirrorDir != filepath.Join(dir, "session-mirror") {
		t.Errorf("session mirror dir = %q (should default to <roster-dir>/session-mirror)", cfg.SessionMirrorDir)
	}
	if cfg.ParadesPath != filepath.Join(dir, "parades") {
		t.Errorf("parades path = %q (should default to <roster-dir>/parades)", cfg.ParadesPath)
	}
	if cfg.LedgerPath != filepath.Join(dir, "context-ledger.md") {
		t.Errorf("ledger path = %q (should inherit roster CosLedger)", cfg.LedgerPath)
	}

	// #376: roster already in state/ — default must be state/parades, not state/state/parades.
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stateRoster := filepath.Join(stateDir, "flotilla.json")
	if err := os.WriteFile(stateRoster, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rc2, err := loadInlineRosterAt(t, stateRoster)
	if err != nil {
		t.Fatal(err)
	}
	cfg2 := ResolvePaths(Config{RosterPath: stateRoster}, rc2)
	wantParades := filepath.Join(stateDir, "parades")
	if cfg2.ParadesPath != wantParades {
		t.Errorf("state roster parades path = %q, want %q", cfg2.ParadesPath, wantParades)
	}
	if strings.Contains(cfg2.ParadesPath, filepath.Join("state", "state")) {
		t.Errorf("parades path must not double state/: %q", cfg2.ParadesPath)
	}
}

// TestGoalsCellDrillins405 locks #405 Inc 3 Items 5+6: stat-cell click-to-highlight,
// realized look-back slider, and graph-node hover tooltip. These are the three
// drill-in interaction features in the Goals view's situation strip and node canvas.
func TestGoalsCellDrillins405(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	css := doGet(t, srv, "/static/dash.css").Body.String()

	// ── Item 5: stat-cell click-to-highlight ──────────────────────────────────
	// The four filterable tiles carry data-filter-tone so click delegation knows
	// which nodes to highlight; the helper functions drive the DOM mutations.
	for _, marker := range []string{
		"activeCellTone",   // module-level state variable: which tone is active (null = none)
		"TONE_TO_SEL",      // map from tile tone → CSS selector for matching nodes
		"applyFilter",      // adds .gcell-focus + .gnode-hl to matching nodes
		"clearFilter",      // removes .gcell-focus / .gnode-hl / .gcell-active
		"data-filter-tone", // attribute on filterable tiles (click delegation key)
		"gcell-active",     // CSS class on the pressed tile
		"gnode-hl",         // CSS class on highlighted nodes
		"gcell-focus",      // CSS class on #goals-nodes when a filter is active
		"reapplyTransient", // called after every render — must re-apply activeCellTone
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must implement stat-cell highlight (missing %q) — #405 Inc 3 Item 5", marker)
		}
	}
	// CSS must dim non-matching nodes and restore matching ones.
	if !strings.Contains(css, "gcell-focus") {
		t.Error("dash.css must carry the .gcell-focus opacity rule — #405 Inc 3 Item 5")
	}
	if !strings.Contains(css, "gcell-active") {
		t.Error("dash.css must style the active-tile ring (.gcell-active) — #405 Inc 3 Item 5")
	}
	// The Flotillas filter must match BOTH the v2 flotilla class and the legacy
	// v1 fleet class, so older/compat inputs still highlight (cubic #405 P2).
	if !strings.Contains(js, ".gnode-flotilla, .gnode-fleet") {
		t.Error("goals.js Flotillas filter must match both .gnode-flotilla and legacy .gnode-fleet — cubic #405 P2")
	}

	// ── Item 6a: realized look-back slider — LIVE as of #418 ─────────────────
	// The slider was stripped while dormant (no achieved_at data — ship-live rule);
	// #418's done-history data layer un-deferred it. The control must now ship AND
	// count real history: the guard flips from absence to liveness — a slider whose
	// window doesn't read achieved_at would be dormant UI sneaking back in.
	for _, marker := range []string{"grealized-slider", "grealized-btn", "injectRealizedSlider", "realizedWindow", "realizedInWindow"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must ship the LIVE realized slider (missing %q) — #418", marker)
		}
	}
	if !strings.Contains(css, ".grealized-") {
		t.Error("dash.css must style the realized slider (.grealized-*) — #418")
	}

	// ── Item 6b: graph-node hover tooltip ────────────────────────────────────
	for _, marker := range []string{
		"gnode-tip", // tooltip element id / CSS class
		"showTip",   // function: positions and populates the tooltip
		"hideTip",   // function: hides the tooltip on mouseout
		"ensureTip", // lazy singleton: injects the fixed-position overlay once
		"gnt-scope", // CSS class on the scope line (Flotilla / Desk / Task)
		"gnt-meta",  // CSS class on the status/owner/activity meta line
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must implement the node hover tooltip (missing %q) — #405 Inc 3 Item 6b", marker)
		}
	}
	if !strings.Contains(css, ".gnode-tip") {
		t.Error("dash.css must style the node hover tooltip (.gnode-tip) — #405 Inc 3 Item 6b")
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

// TestDecisionResponseLoop501 locks the #501 decision-response loop's UI half: every
// rendered decision carries an inline respond affordance wired to /api/control/respond;
// brief-less gated items render in the fail-closed "preparing" bucket (never as
// decisions); and the old prototype stub ("not sent — wiring is a follow-on") is GONE —
// a stub the operator can click is a walk failure.
func TestDecisionResponseLoop501(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"gdec-respond",          // the per-card response affordance
		"sendDecisionResponse",  // the ONE reply path (cards + modal share it)
		"/api/control/respond",  // wired to the real control endpoint
		"renderPreparingRow",    // the fail-closed brief-less rendering
		"gdec-prep",             // the preparing bucket
		"Briefs being prepared", // its labeled, honest header
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must implement the decision-response loop (missing %q) — #501", marker)
		}
	}
	if strings.Contains(js, "prototype stub") || strings.Contains(js, "not sent (wiring") {
		t.Error("goals.js must NOT ship the reply-path stub — the loop is wired end-to-end (#501)")
	}
	html := doGet(t, srv, "/").Body.String()
	if !strings.Contains(html, `data-xo=`) {
		t.Error("index.html body must carry data-xo (the ownerless-goal response fallback target) — #501")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".gdec-respond") || !strings.Contains(css, ".gdec-prep") {
		t.Error("dash.css must style the respond affordance + preparing bucket — #501")
	}
}
