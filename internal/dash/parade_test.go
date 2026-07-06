package dash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadParades_NewestFirstWithReportAndAssets(t *testing.T) {
	dir := t.TempDir()
	// two dated parades + a non-date dir that must be ignored.
	mk := func(date, slides string, assets ...string) {
		pd := filepath.Join(dir, date)
		if err := os.MkdirAll(filepath.Join(pd, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if slides != "" {
			if err := os.WriteFile(filepath.Join(pd, "slides.md"), []byte(slides), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, a := range assets {
			if err := os.WriteFile(filepath.Join(pd, "assets", a), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("2026-07-03", "# older", "z.png", "a.png", "notes.txt") // notes.txt is not an image → dropped
	mk("2026-07-04", "# newer")                                // no assets
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := readParades(dir)
	if err != nil {
		t.Fatalf("readParades: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 parades (non-date dir ignored), got %d: %+v", len(got), got)
	}
	if got[0].Date != "2026-07-04" || got[1].Date != "2026-07-03" {
		t.Errorf("parades must be newest-first, got %q then %q", got[0].Date, got[1].Date)
	}
	if got[0].Slides != "# newer" {
		t.Errorf("slides.md not read: %q", got[0].Slides)
	}
	// legacy fallback: a parade with only report.md (no slides.md) is still read.
	legacy := filepath.Join(dir, "2026-07-05")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "report.md"), []byte("# legacy deck"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again, _ := readParades(dir); again[0].Date != "2026-07-05" || again[0].Slides != "# legacy deck" {
		t.Errorf("a legacy report.md (no slides.md) must fall back, got date=%q slides=%q", again[0].Date, again[0].Slides)
	}
	// assets: image files only, sorted; the .txt is excluded.
	if len(got[1].Assets) != 2 || got[1].Assets[0] != "a.png" || got[1].Assets[1] != "z.png" {
		t.Errorf("assets must be the sorted image files only, got %v", got[1].Assets)
	}
	if got[0].Assets == nil {
		t.Error("a parade with no assets must have an empty (non-nil) asset list")
	}
}

func TestReadParades_MissingDirIsEmptyButOtherErrorSurfaces(t *testing.T) {
	// absent archive ⇒ honest empty, no error.
	got, err := readParades(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil || len(got) != 0 {
		t.Errorf("a missing parade dir must yield an empty list + no error, got %d entries err=%v", len(got), err)
	}
	// a path that is a FILE (not a dir) is NOT "no parades" — the error must surface (#363).
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readParades(f); err == nil {
		t.Error("readParades on a non-directory must return an error, not a silent empty")
	}
}

func TestIsParadeImage(t *testing.T) {
	for _, ok := range []string{"a.png", "A.PNG", "b.jpg", "c.jpeg", "d.gif", "e.webp"} {
		if !isParadeImage(ok) {
			t.Errorf("%q should be an image", ok)
		}
	}
	// .svg is deliberately NOT allowed (active same-origin document — cubic #373).
	for _, no := range []string{"report.md", "notes.txt", "x", "y.pdf", "f.svg"} {
		if isParadeImage(no) {
			t.Errorf("%q should NOT be a served image", no)
		}
	}
}

func TestHandleParadeAsset_RejectsTraversalAndNonImage(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	srv.cfg.ParadesPath = filepath.Join(dir, "parades")
	// a real asset to prove the happy path resolves.
	pd := filepath.Join(srv.cfg.ParadesPath, "2026-07-04", "assets")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "shot.png"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	// also a secret OUTSIDE assets/ that traversal must never reach.
	if err := os.WriteFile(filepath.Join(srv.cfg.ParadesPath, "2026-07-04", "report.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if rec := doGet(t, srv, "/parade-assets/2026-07-04/shot.png"); rec.Code != 200 || rec.Body.String() != "PNGDATA" {
		t.Errorf("valid image asset must serve, got code=%d body=%q", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{
		"/parade-assets/2026-07-04/report.md",      // not an image
		"/parade-assets/2026-13-99/shot.png",       // bad date
		"/parade-assets/2026-07-04/..%2freport.md", // encoded traversal
		"/parade-assets/2026-07-04/sub%2fshot.png", // path separator in file
	} {
		if rec := doGet(t, srv, bad); rec.Code == 200 {
			t.Errorf("%s must NOT serve (got 200)", bad)
		}
	}

	// #373 P1: a SYMLINK dropped in assets/ that escapes to a host file must NOT serve.
	secret := filepath.Join(srv.cfg.ParadesPath, "2026-07-04", "report.md") // outside assets/
	link := filepath.Join(pd, "escape.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	if rec := doGet(t, srv, "/parade-assets/2026-07-04/escape.png"); rec.Code == 200 {
		t.Errorf("a symlink escaping assets/ must NOT serve, got 200 body=%q", rec.Body.String())
	}

	// #373 P2/P5: an asset whose name contains '&' must still serve (no double-escape).
	if err := os.WriteFile(filepath.Join(pd, "a&b.png"), []byte("AMP"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := doGet(t, srv, "/parade-assets/2026-07-04/a%26b.png"); rec.Code != 200 || rec.Body.String() != "AMP" {
		t.Errorf("an ampersand-named asset must serve, got code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// TestParadeDeckRenderMarkers locks the parade deck's render support: markdown links
// (dig-deeper sources) and the "> " decision-brief callout (parade v3). No JS runner, so
// this asserts the served assets carry the render — removing either fails here.
func TestParadeDeckRenderMarkers(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/parade.js").Body.String()
	if !strings.Contains(js, "rel=\"noopener\"") {
		t.Error("parade.js must render markdown links (dig-deeper sources) — parade v3 (a)")
	}
	if !strings.Contains(js, "pd-quote") {
		t.Error("parade.js must render a \"> \" blockquote as a decision-brief callout — parade v3 (c)")
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".pd-quote") {
		t.Error("dash.css must style the decision-brief callout (.pd-quote) — parade v3 (c)")
	}
}

// TestParadeTableRenderMarkers locks the deck renderer's TABLE support (#427/#428): GFM
// pipe-tables AND literal HTML <table> blocks both render as a real styled table, always
// rebuilt through the escape-then-inline path (cell markup stripped to text, never passed
// through). No JS runner, so this asserts the served assets carry the machinery.
func TestParadeTableRenderMarkers(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/parade.js").Body.String()
	for marker, why := range map[string]string{
		"isTableDelimiter": "GFM pipe-table delimiter detection (#428)",
		"pd-table":         "the shared styled-table emit (#428)",
		"parseHtmlTable":   "HTML <table> blocks parsed for their cell TEXT (#427)",
		"decodeEntities":   "source entities decoded before the render-side escape (#427)",
		"cellAlign":        "alignment restricted to a fixed keyword set (#427)",
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("parade.js missing table-render marker %q — %s", marker, why)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	if !strings.Contains(css, ".pd-table") {
		t.Error("dash.css must style the deck table (.pd-table) — #428")
	}
}
