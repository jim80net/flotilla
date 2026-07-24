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
)

func writeResearchFixture(t *testing.T, root, rel, body string, mod time.Time) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestResearchVideoAssetRangeAndBoundary(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	videoPath := filepath.Join(root, "papers", "media", "briefing.mp4")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("0123456789-video-bytes")
	if err := os.WriteFile(videoPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	full := doGet(t, srv, "/research-assets/papers/media/briefing.mp4")
	if full.Code != http.StatusOK || full.Body.String() != string(payload) {
		t.Fatalf("research video = %d %q", full.Code, full.Body.String())
	}
	if got := full.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("content type = %q, want video/mp4", got)
	}
	if got := full.Header().Get("Content-Disposition"); got != "inline" {
		t.Errorf("content disposition = %q, want inline", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/research-assets/papers/media/briefing.mp4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rangeRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rangeRec, req)
	if rangeRec.Code != http.StatusPartialContent || rangeRec.Body.String() != "2345" {
		t.Fatalf("research video range = %d %q", rangeRec.Code, rangeRec.Body.String())
	}

	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("PRIVATE_VIDEO_SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.mp4")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, bad := range []string{
		"/research-assets/leak.mp4",
		"/research-assets/.hidden.mp4",
		"/research-assets/papers/notes.txt",
		"/research-assets/%2e%2e%2foutside.mp4",
		"/research-assets/papers%5cmedia%5cbriefing.mp4",
	} {
		rec := doGet(t, srv, bad)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "PRIVATE_VIDEO_SENTINEL") {
			t.Errorf("unsafe research video path %q served status=%d body=%q", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestReadResearchIndexDecisionShelfAndBoundary(t *testing.T) {
	root := t.TempDir()
	older := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	writeResearchFixture(t, root, "authorization-design.md", "# Authorization Domains\n\n**Status:** DESIGN ONLY — awaiting operator design-review GO\n", older)
	writeResearchFixture(t, root, "notes/field-note.md", "# Field note\n\nA useful ordinary research summary.\n", newer)
	writeResearchFixture(t, root, ".hidden.md", "# hidden", newer)
	writeResearchFixture(t, root, ".private/secret.md", "# secret", newer)
	if err := os.WriteFile(filepath.Join(root, "flotilla-secrets.env"), []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ledger.json"), []byte(`{"credential":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("HOST_SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := readResearchIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("research index len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "authorization-design.md" || !got[0].Decision || got[0].Status != "design-only" {
		t.Errorf("decision shelf entry = %+v", got[0])
	}
	if got[0].Title != "Authorization Domains" {
		t.Errorf("heading-derived title = %q", got[0].Title)
	}
	if got[1].ID != "notes/field-note.md" || got[1].Decision || got[1].Summary != "A useful ordinary research summary." {
		t.Errorf("ordinary entry = %+v", got[1])
	}
	for _, entry := range got {
		if strings.Contains(entry.ID, "secret") || strings.Contains(entry.ID, "ledger") || strings.Contains(entry.ID, "leak") {
			t.Errorf("non-publication artifact entered research index: %+v", entry)
		}
	}
}

func TestResearchMissingRootIsEmptyAndBadRootErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	got, err := readResearchIndex(missing)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing research root = %+v, %v; want honest empty", got, err)
	}
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readResearchIndex(file); err == nil {
		t.Error("file-valued research root must error, not become an empty library")
	}
}

func TestResearchAPIIndexBodyDeepLinkAndTraversal(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	writeResearchFixture(t, root, "design.md", "# Safe design\n\n**Status:** awaiting-auth\n\nBody text.\n", time.Now())
	writeResearchFixture(t, root, "nested/note.md", "# Nested note\n\nNested body.\n", time.Now())
	outside := filepath.Join(t.TempDir(), "host-secret.md")
	if err := os.WriteFile(outside, []byte("HOST_SECRET_SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	indexRec := doGet(t, srv, "/api/research")
	if indexRec.Code != http.StatusOK {
		t.Fatalf("research index status = %d: %s", indexRec.Code, indexRec.Body.String())
	}
	var index struct {
		Research []ResearchEntry `json:"research"`
	}
	if err := json.Unmarshal(indexRec.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Research) != 2 || index.Research[0].ID != "design.md" {
		t.Errorf("research API index = %+v", index.Research)
	}
	if strings.Contains(indexRec.Body.String(), "HOST_SECRET_SENTINEL") || strings.Contains(indexRec.Body.String(), "leak.md") {
		t.Error("research index exposed a symlinked host file")
	}

	bodyRec := doGet(t, srv, "/api/research/nested/note.md")
	if bodyRec.Code != http.StatusOK {
		t.Fatalf("research body status = %d: %s", bodyRec.Code, bodyRec.Body.String())
	}
	var doc ResearchDocument
	if err := json.Unmarshal(bodyRec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ID != "nested/note.md" || doc.Markdown != "# Nested note\n\nNested body.\n" {
		t.Errorf("research document = %+v", doc)
	}
	if doc.Digest != researchDigest(doc.Markdown) || !strings.HasPrefix(doc.Digest, "sha256:") {
		t.Errorf("research digest = %q", doc.Digest)
	}
	if got := bodyRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("research body cache control = %q", got)
	}
	if page := doGet(t, srv, "/research/nested/note.md"); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `/static/research.js`) {
		t.Errorf("research deep-link page = %d, body marker=%v", page.Code, strings.Contains(page.Body.String(), `/static/research.js`))
	}

	for _, bad := range []string{
		"/api/research/leak.md",
		"/api/research/flotilla-secrets.env",
		"/api/research/.hidden.md",
		"/api/research/%2e%2e%2fhost-secret.md",
		"/api/research/nested/%2e%2e/%2e%2e/host-secret.md",
		"/api/research/nested%5cnote.md",
	} {
		rec := doGet(t, srv, bad)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "HOST_SECRET_SENTINEL") {
			t.Errorf("unsafe research path %q served status=%d body=%q", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestResearchPageAndDashboardNavMarkers(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	index := doGet(t, srv, "/").Body.String()
	if !strings.Contains(index, `id="tab-decisions"`) || !strings.Contains(index, `href="/research?focus=decisions"`) || !strings.Contains(index, `R&amp;D`) {
		t.Error("dashboard must expose the combined R&D navigation link with decision focus")
	}
	page := doGet(t, srv, "/research").Body.String()
	for _, marker := range []string{"Decide · investigate · learn", "R&amp;D", "Waiting on you", `id="research-reader"`, `id="research-search"`, `data-research-focus="decisions"`, `data-research-focus="library"`, `data-research-focus="all"`, `id="research-decision-more"`, `id="research-library-more"`, `id="research-toc-count"`, `id="research-document-comment"`, `id="research-annotation-panel"`, `/static/research.js`} {
		if !strings.Contains(page, marker) {
			t.Errorf("research page missing %q", marker)
		}
	}
	js := doGet(t, srv, "/static/research.js").Body.String()
	for _, marker := range []string{"function esc(value)", "renderMarkdown", "documentWithoutDuplicateTitle", "research-decision-strip", "collectionWindow = 6", "decisionWindow = 3", "filteredEntries", "setFocus", "tocRestoreY", "researchVideoURL", "data-research-video-fullscreen", "anchorForQuote", "X-Flotilla-Dash", "draft is still here"} {
		if !strings.Contains(js, marker) {
			t.Errorf("research renderer missing %q", marker)
		}
	}
}
