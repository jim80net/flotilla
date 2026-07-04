package dash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadParades_NewestFirstWithReportAndAssets(t *testing.T) {
	dir := t.TempDir()
	// two dated parades + a non-date dir that must be ignored.
	mk := func(date, report string, assets ...string) {
		pd := filepath.Join(dir, date)
		if err := os.MkdirAll(filepath.Join(pd, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if report != "" {
			if err := os.WriteFile(filepath.Join(pd, "report.md"), []byte(report), 0o644); err != nil {
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

	got := readParades(dir)
	if len(got) != 2 {
		t.Fatalf("expected 2 parades (non-date dir ignored), got %d: %+v", len(got), got)
	}
	if got[0].Date != "2026-07-04" || got[1].Date != "2026-07-03" {
		t.Errorf("parades must be newest-first, got %q then %q", got[0].Date, got[1].Date)
	}
	if got[0].Report != "# newer" {
		t.Errorf("report.md not read: %q", got[0].Report)
	}
	// assets: image files only, sorted; the .txt is excluded.
	if len(got[1].Assets) != 2 || got[1].Assets[0] != "a.png" || got[1].Assets[1] != "z.png" {
		t.Errorf("assets must be the sorted image files only, got %v", got[1].Assets)
	}
	if got[0].Assets == nil {
		t.Error("a parade with no assets must have an empty (non-nil) asset list")
	}
}

func TestReadParades_MissingDirIsEmpty(t *testing.T) {
	got := readParades(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("a missing parade dir must yield an empty list, got %+v", got)
	}
}

func TestIsParadeImage(t *testing.T) {
	for _, ok := range []string{"a.png", "A.PNG", "b.jpg", "c.jpeg", "d.gif", "e.webp", "f.svg"} {
		if !isParadeImage(ok) {
			t.Errorf("%q should be an image", ok)
		}
	}
	for _, no := range []string{"report.md", "notes.txt", "x", "y.pdf", ".png"} {
		if no != ".png" && isParadeImage(no) {
			t.Errorf("%q should NOT be an image", no)
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
		"/parade-assets/2026-07-04/report.md",        // not an image
		"/parade-assets/2026-13-99/shot.png",         // bad date
		"/parade-assets/2026-07-04/..%2freport.md",   // encoded traversal
		"/parade-assets/2026-07-04/sub%2fshot.png",   // path separator in file
	} {
		if rec := doGet(t, srv, bad); rec.Code == 200 {
			t.Errorf("%s must NOT serve (got 200)", bad)
		}
	}
}
