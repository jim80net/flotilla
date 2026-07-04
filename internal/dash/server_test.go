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

// TestGoalsLayoutEnvDefault locks the #317 env-seeded layout default: the index renders
// the body's data-goals-layout from Config.GoalsLayout (org by default, tree when set),
// and goals.js reads that attribute so a deployment can seed the default (the live toggle
// still overrides).
func TestGoalsLayoutEnvDefault(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	// normalize: default MIND MAP (org retired from the UI); "tree" (any case) honored;
	// anything else — incl. a legacy "org" seed — maps to mindmap.
	for in, want := range map[string]string{"": "mindmap", "org": "mindmap", "mindmap": "mindmap", "tree": "tree", "TREE": "tree", "bogus": "mindmap"} {
		if got := normalizeGoalsLayout(in); got != want {
			t.Errorf("normalizeGoalsLayout(%q) = %q, want %q", in, got, want)
		}
	}

	// default (no env) → the index seeds mindmap.
	srv, _ := newTestServer(t, singleFleetRoster, now)
	if body := doGet(t, srv, "/").Body.String(); !strings.Contains(body, `data-goals-layout="mindmap"`) {
		t.Error("index must seed the goals layout (default mindmap) into the body attribute")
	}

	// tree seeded via Config → the index seeds tree.
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(singleFleetRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	srv2, err := NewServer(Config{RosterPath: rosterPath, Bind: DefaultBind, GoalsLayout: "tree", Transport: stubTransport{}, WebTransport: stubTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	if body := doGet(t, srv2, "/").Body.String(); !strings.Contains(body, `data-goals-layout="tree"`) {
		t.Error("a tree-seeded Config must render data-goals-layout=\"tree\"")
	}

	// goals.js consumes the attribute.
	if js := doGet(t, srv, "/static/goals.js").Body.String(); !strings.Contains(js, "data-goals-layout") {
		t.Error("goals.js must read the env-seeded default from data-goals-layout (#317)")
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

// TestConversationsWave2 locks #349 Inc 4: the drive-queue chip opens a full-item modal
// (E10) and the relay-ledger thread filter normalizes the "@name" address form on BOTH
// from and to (E11 filter half) so a desk's own OUTBOUND relay lines are no longer dropped.
func TestConversationsWave2(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	// E10: queue chip → modal.
	for _, marker := range []string{"openConvModal", "data-bq-open", "data-bq-text"} {
		if !strings.Contains(js, marker) {
			t.Errorf("dash.js must open a drive-queue item in the modal (missing %q) — #349 Inc 4 E10", marker)
		}
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
	for _, marker := range []string{"nodeActivate", "openModal", "gnode-respond"} {
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
	// org-graph v2 Inc B: the hub-and-spoke layout + its live tree⇄org toggle,
	// consuming layout.hub_center. drawEdges must branch on the mode (radial spokes vs
	// tiered beziers), and the toggle must force a rebuild.
	for _, marker := range []string{"layoutOrg", "goalsLayout", "hub_center", "setLayout", "glayout-btn"} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js must retain the org-graph v2 hub-spoke layout (missing %q) — Inc B", marker)
		}
	}
	// #324 Inc 1: org is the DEFAULT layout (operator UX blessing), and the org geometry
	// is content-aware — leaf-weight angular packing + per-ring radii from card extents
	// (no fixed RING_STEP), with narrower org cards.
	// The default is MIND MAP (operator retired org from the UI); env-seedable via the body
	// attribute (#317) — the IIFE reads data-goals-layout and falls back to "mindmap"; "tree"
	// is the only other selectable mode (org is dormant, no button, no default).
	if !strings.Contains(js, `v === "tree" ? "tree" : "mindmap"`) {
		t.Error("goals.js must seed goalsLayout from data-goals-layout, defaulting mindmap (org retired)")
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
	for _, marker := range []string{"goToDesk", "gnode-godesk", "restoreNode"} {
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
	// mobile clipped-button fix: the goals layout toggle (overflow:hidden) must NOT shrink and
	// clip its buttons when the squeezed header row crushes it (seen at 768) — flex:none on the
	// toggle itself + a wrapping goals panel-head so head-right drops below the title instead.
	// Scope the flex:none check to the .goals-layout-toggle rule body — other unrelated rules
	// also use flex:none, so a bare Contains would pass even if the toggle lost it (cubic #368).
	if !strings.Contains(css, ".goals-panel > .panel-head") {
		t.Error("dash.css must let the goals panel-head wrap so the toggle isn't crushed (.goals-panel > .panel-head)")
	}
	if i := strings.Index(css, ".goals-layout-toggle {"); i < 0 {
		t.Error("dash.css must define the .goals-layout-toggle rule")
	} else {
		block := css[i:]
		if end := strings.Index(block, "}"); end >= 0 {
			block = block[:end]
		}
		if !strings.Contains(block, "flex: none") {
			t.Error("the .goals-layout-toggle rule itself must be flex:none so its buttons never clip on a squeezed header (cubic #368)")
		}
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
	// mind-map (org v3): a third radial mode whose children fan LOCALLY from each parent
	// (limbs + sub-branches) with curved edges. Selectable via the toggle; org stays default.
	for _, marker := range []string{"layoutMindmap", "isRadial", `data-layout="mindmap"`} {
		if !strings.Contains(js+doGet(t, srv, "/").Body.String(), marker) {
			t.Errorf("goals map must carry the mind-map layout (missing %q)", marker)
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
	// the tree | mind map layout toggle chrome (org retired from the UI, so no org button).
	if !strings.Contains(body, "glayout-btn") || !strings.Contains(body, `data-layout="tree"`) || !strings.Contains(body, `data-layout="mindmap"`) {
		t.Error("index must carry the tree | mind map goals layout toggle (glayout-btn / data-layout)")
	}
	if strings.Contains(body, `data-layout="org"`) {
		t.Error("the org layout button must be retired from the UI (operator verdict) — no org button")
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

// --- helpers ---

func doGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	return rec
}
