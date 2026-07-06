package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #418 done-history: the log, the recorder's transition semantics, the attachment,
// and the end-to-end /api/goals stamp.

func TestReadDoneLog_MissingBadAndLatestWins(t *testing.T) {
	// missing file → empty history, no error.
	got, err := ReadDoneLog(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || len(got) != 0 {
		t.Fatalf("missing log must read empty, got %d err=%v", len(got), err)
	}
	// latest entry per id wins; a bad line is skipped (per-row recovery).
	p := filepath.Join(t.TempDir(), "goals-done.jsonl")
	lines := strings.Join([]string{
		`{"id":"a","ts":"2026-07-01T00:00:00Z","achieved":true,"seed":true}`,
		`NOT JSON AT ALL`,
		`{"id":"b","ts":"2026-07-02T00:00:00Z","achieved":true}`,
		`{"id":"a","ts":"2026-07-03T00:00:00Z","achieved":false}`,
		`{"ts":"2026-07-03T00:00:00Z","achieved":true}`, // no id → skipped
		// A corrupt ts must not become b's LATEST entry (it would erase the valid stamp
		// and hide b from bounded windows) — skipped like any other bad row (cubic #449 P2).
		`{"id":"b","ts":"yesterday-ish","achieved":false}`,
	}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ReadDoneLog(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 ids, got %d: %+v", len(got), got)
	}
	if a := got["a"]; a.Achieved || a.TS != "2026-07-03T00:00:00Z" {
		t.Errorf("latest entry for a must win (regressed at 07-03), got %+v", a)
	}
	if b := got["b"]; !b.Achieved || b.Seed {
		t.Errorf("b must be a non-seed achieve, got %+v", b)
	}
}

// achievedDoc builds a found goals doc with the given id→status_display pairs, in order.
func achievedDoc(pairs ...string) GoalsDoc {
	doc := GoalsDoc{Found: true}
	for i := 0; i+1 < len(pairs); i += 2 {
		doc.Goals = append(doc.Goals, RenderedGoal{ID: pairs[i], Title: "T-" + pairs[i], StatusDisplay: pairs[i+1]})
	}
	return doc
}

func TestDoneRecorder_TransitionLifecycle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "goals-done.jsonl")
	r := newDoneRecorder(p)
	t0 := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	// First-ever observation on a fresh log: the already-achieved goal is a SEED.
	app := r.observe(achievedDoc("a", "achieved", "b", "in-flight"), t0)
	if len(app) != 1 || app[0].ID != "a" || !app[0].Achieved || !app[0].Seed {
		t.Fatalf("first observation must seed the pre-achieved goal, got %+v", app)
	}
	// Re-observing the same state appends NOTHING (no duplicates).
	if app = r.observe(achievedDoc("a", "achieved", "b", "in-flight"), t0.Add(time.Minute)); app != nil {
		t.Fatalf("unchanged state must append nothing, got %+v", app)
	}
	// b achieves later — a real (non-seed) transition.
	app = r.observe(achievedDoc("a", "achieved", "b", "achieved"), t0.Add(2*time.Minute))
	if len(app) != 1 || app[0].ID != "b" || !app[0].Achieved || app[0].Seed {
		t.Fatalf("later achieve must be a non-seed entry, got %+v", app)
	}
	// a regresses — a tombstone; then re-achieves — a fresh stamp.
	app = r.observe(achievedDoc("a", "in-flight", "b", "achieved"), t0.Add(3*time.Minute))
	if len(app) != 1 || app[0].ID != "a" || app[0].Achieved {
		t.Fatalf("regress must record achieved:false, got %+v", app)
	}
	app = r.observe(achievedDoc("a", "achieved", "b", "achieved"), t0.Add(4*time.Minute))
	if len(app) != 1 || app[0].ID != "a" || !app[0].Achieved || app[0].Seed ||
		app[0].TS != "2026-07-06T12:04:00Z" {
		t.Fatalf("re-achieve must record a fresh non-seed stamp, got %+v", app)
	}

	// A not-found doc records nothing (an absent goals file is not "all regressed").
	if app = r.observe(GoalsDoc{Found: false}, t0.Add(5*time.Minute)); app != nil {
		t.Fatalf("not-found doc must be a no-op, got %+v", app)
	}
	// Roster-materialized desk cards are presence, not authored goals — never recorded.
	desk := GoalsDoc{Found: true, Goals: []RenderedGoal{{ID: "desk-x", StatusDisplay: "achieved", Source: "roster"}}}
	if app = r.observe(desk, t0.Add(6*time.Minute)); app != nil {
		t.Fatalf("roster-source nodes must not be recorded, got %+v", app)
	}

	// A FRESH recorder over the same file reconstructs the same view (durability).
	hist := newDoneRecorder(p).history()
	if e := hist["a"]; !e.Achieved || e.TS != "2026-07-06T12:04:00Z" || e.Seed {
		t.Errorf("reloaded history for a = %+v", e)
	}
	if e := hist["b"]; !e.Achieved || e.Seed {
		t.Errorf("reloaded history for b = %+v", e)
	}
	// And the file has exactly the 4 appended lines.
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 4 {
		t.Errorf("log must hold exactly 4 entries, got %d:\n%s", n, b)
	}
}

func TestDoneRecorder_SecondProcessDoesNotReseed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "goals-done.jsonl")
	t0 := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	newDoneRecorder(p).observe(achievedDoc("a", "achieved"), t0)
	// A later process (log file EXISTS) observing a newly-achieved goal must NOT seed it.
	app := newDoneRecorder(p).observe(achievedDoc("a", "achieved", "c", "achieved"), t0.Add(time.Hour))
	if len(app) != 1 || app[0].ID != "c" || app[0].Seed {
		t.Fatalf("existing-log process must record a real (non-seed) transition, got %+v", app)
	}
}

func TestAttachDoneHistory(t *testing.T) {
	doc := achievedDoc("a", "achieved", "b", "in-flight", "c", "achieved")
	AttachDoneHistory(&doc, map[string]DoneEntry{
		"a": {ID: "a", TS: "2026-07-05T00:00:00Z", Achieved: true, Seed: true},
		"b": {ID: "b", TS: "2026-07-04T00:00:00Z", Achieved: true}, // regressed since → currently in-flight
		// c achieved but not yet observed → no stamp.
	})
	if doc.Goals[0].AchievedAt != "2026-07-05T00:00:00Z" || !doc.Goals[0].AchievedSeed {
		t.Errorf("a must carry its seed stamp, got %+v", doc.Goals[0])
	}
	if doc.Goals[1].AchievedAt != "" {
		t.Errorf("a non-achieved goal must carry NO stamp, got %q", doc.Goals[1].AchievedAt)
	}
	if doc.Goals[2].AchievedAt != "" {
		t.Errorf("an unobserved achieved goal must carry NO stamp yet, got %q", doc.Goals[2].AchievedAt)
	}
}

func TestHandleGoals_AchievedAtStamped(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	goals := `{
	  "version": 1,
	  "goals": [
	    {"id": "product", "title": "Product", "scope": "fleet"},
	    {"id": "shipped", "title": "Shipped", "scope": "project", "parent": "product", "status": "achieved"}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "fleet-goals.json"), []byte(goals), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/goals")
	var doc GoalsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var shipped *RenderedGoal
	for i := range doc.Goals {
		if doc.Goals[i].ID == "shipped" {
			shipped = &doc.Goals[i]
		}
	}
	if shipped == nil || shipped.StatusDisplay != "achieved" {
		t.Fatalf("fixture must roll up achieved, got %+v", shipped)
	}
	// The FIRST load both records and stamps: achieved_at present, seeded (the log was
	// born with this goal already achieved — its true achieve time is unknown).
	if shipped.AchievedAt != "2026-07-06T12:00:00Z" || !shipped.AchievedSeed {
		t.Errorf("first load must stamp a seeded achieved_at, got at=%q seed=%v", shipped.AchievedAt, shipped.AchievedSeed)
	}
	// The log landed at the resolved default path.
	if _, err := os.Stat(filepath.Join(dir, "goals-done.jsonl")); err != nil {
		t.Errorf("done-log must exist at <roster-dir>/goals-done.jsonl: %v", err)
	}
}

// TestRealizedSliderMarkers418 locks the LIVE look-back UI (#418): the slider control,
// the windowed count that reads achieved_at and excludes seeds, and the done-history
// row's recorded-time stamp. No JS runner — asserts the served assets carry the render.
func TestRealizedSliderMarkers418(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/goals.js").Body.String()
	for _, marker := range []string{
		"injectRealizedSlider", // the segment control (1d/7d/30d/all)
		"realizedInWindow",     // windowed count over achieved_at
		"achieved_seed",        // seeds excluded — unknown achieve times never count as recent
		"gdone-when",           // done-history rows show the recorded achieve time
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("goals.js missing #418 marker %q", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".grealized-btn") || !strings.Contains(css, ".gdone-when") {
		t.Error("dash.css must style the realized slider + done-row time — #418")
	}
}
