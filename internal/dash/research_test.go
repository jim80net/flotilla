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

func TestResearchPublicationDirectiveAndDiagnostics858(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	valid := `<!-- flotilla-publication
classification: research
reader-action: Compare the evidence and choose the next experiment.
support: material
owner: grok-research
-->
# Evidence review

This substantive report compares the generic options and explains the measured outcome.

[Supporting dataset](evidence.csv)
`
	archival := `<!-- flotilla-publication
classification: archival
reader-action: Retain this rationale as historical context.
support: text-only
support-rationale: The contemporaneous rationale is the complete historical record.
-->
# Historical rationale

This note records why the reversible example was retained after the review.
`
	decision := `<!-- flotilla-publication
classification: decision
reader-action: Decide whether to run the reversible trial.
support: text-only
support-rationale: The bounded choice and rollback are fully stated here.
-->
# Reversible trial

**Status:** DESIGN ONLY — awaiting operator GO

The trial stays frozen until the operator makes an explicit decision.
`
	writeResearchFixture(t, root, "valid/SOURCE.md", valid, now)
	presentation := filepath.Join(root, "valid", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presentation, []byte("<!doctype html><title>Valid evidence showpiece</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeResearchFixture(t, root, "archival.md", archival, now.Add(-time.Minute))
	writeResearchFixture(t, root, "decision.md", decision, now.Add(-2*time.Minute))
	writeResearchFixture(t, root, "empty.md", "", now.Add(-3*time.Minute))
	writeResearchFixture(t, root, "title.md", "# Title only\n", now.Add(-4*time.Minute))
	writeResearchFixture(t, root, "boilerplate.md", "# Placeholder\n\nTODO: coming soon.\n", now.Add(-5*time.Minute))

	entries, err := readResearchIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("entries = %d, want all 6 retained for measurement", len(entries))
	}
	byID := map[string]ResearchEntry{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	if !byID["valid/SOURCE.md"].PublicationValid || len(byID["valid/SOURCE.md"].Diagnostics) != 0 || !byID["valid/SOURCE.md"].PresentationReady {
		t.Errorf("valid publication = %+v", byID["valid/SOURCE.md"])
	}
	if byID["valid/SOURCE.md"].Publication.Owner != "grok-research" {
		t.Errorf("publication owner = %q, want grok-research", byID["valid/SOURCE.md"].Publication.Owner)
	}
	if !byID["valid/SOURCE.md"].LearnReady {
		t.Errorf("complete explicit research showpiece must be Learn-ready: %+v", byID["valid/SOURCE.md"])
	}
	if !byID["archival.md"].Archival || byID["archival.md"].Decision || byID["archival.md"].Status != "archival" || !byID["archival.md"].PublicationValid {
		t.Errorf("archival publication = %+v", byID["archival.md"])
	}
	if byID["archival.md"].LearnReady || byID["decision.md"].LearnReady || byID["empty.md"].LearnReady {
		t.Errorf("Learn admitted archival, decision, or raw note: archival=%+v decision=%+v empty=%+v",
			byID["archival.md"], byID["decision.md"], byID["empty.md"])
	}
	if !byID["decision.md"].Decision || byID["decision.md"].Status != "design-only" || byID["decision.md"].PublicationValid {
		t.Errorf("decision publication = %+v", byID["decision.md"])
	}
	for id, contentCode := range map[string]string{
		"empty.md": "content.empty", "title.md": "content.title_only", "boilerplate.md": "content.boilerplate",
	} {
		codes := map[string]bool{}
		for _, diagnostic := range byID[id].Diagnostics {
			codes[diagnostic.Code] = true
		}
		for _, want := range []string{contentCode, "action.missing", "support.missing", "presentation.missing"} {
			if !codes[want] {
				t.Errorf("%s diagnostics missing %q: %+v", id, want, byID[id].Diagnostics)
			}
		}
	}
	summary := summarizeResearchDiagnostics(entries)
	if summary.Documents != 6 || summary.Valid != 2 || summary.NeedsAttention != 4 || summary.Showpieces != 1 || summary.SourceOnly != 5 {
		t.Errorf("diagnostics summary = %+v", summary)
	}
	if summary.ByCode["action.missing"] != 3 || summary.ByCode["support.missing"] != 3 {
		t.Errorf("diagnostics counts = %+v", summary.ByCode)
	}
	if summary.ByCode["presentation.missing"] != 4 {
		t.Errorf("presentation readiness count = %+v", summary.ByCode)
	}
}

func TestResearchPublicationOwnerFailsClosed(t *testing.T) {
	publication, diagnostics := parseResearchPublication(`<!-- flotilla-publication
classification: research
reader-action: Read the evidence.
support: material
owner: ../../cos
-->
# Unsafe owner

[Evidence](evidence.csv)
`)
	if publication.Owner != "../../cos" {
		t.Fatalf("owner = %q", publication.Owner)
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "metadata.owner" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v, want metadata.owner", diagnostics)
	}
}

func TestEducationalResearchReadyFailsClosed(t *testing.T) {
	base := ResearchEntry{
		Publication:       ResearchPublication{Explicit: true, Classification: "research"},
		PublicationValid:  true,
		PresentationReady: true,
	}
	if !educationalResearchReady(base) {
		t.Fatal("complete explicit research showpiece should enter Learn")
	}
	tests := map[string]ResearchEntry{
		"missing explicit publication": {
			Publication: ResearchPublication{Classification: "research"}, PublicationValid: true, PresentationReady: true,
		},
		"status note with no classification": {
			Publication: ResearchPublication{Explicit: true}, PublicationValid: true, PresentationReady: true,
		},
		"source only": {
			Publication: ResearchPublication{Explicit: true, Classification: "research"}, PublicationValid: false,
		},
		"decision showpiece": {
			Publication: ResearchPublication{Explicit: true, Classification: "decision"}, PublicationValid: true, PresentationReady: true, Decision: true,
		},
		"archival showpiece": {
			Publication: ResearchPublication{Explicit: true, Classification: "archival"}, PublicationValid: true, PresentationReady: true, Archival: true,
		},
	}
	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			if educationalResearchReady(entry) {
				t.Fatalf("non-educational entry admitted to Learn: %+v", entry)
			}
		})
	}
}

func TestResearchPublicationMetadataCannotInferAuthorizationGO858(t *testing.T) {
	body := `<!-- flotilla-publication
classification: decision
reader-action: Review the design; explicit authorization remains separate.
support: text-only
support-rationale: This is a design argument for operator review.
-->
# Authorization Domains

**Status:** DESIGN ONLY — awaiting operator design-review GO

No authorization is granted by this publication metadata.
`
	entry := researchEntry("authorization-domains.md", body, time.Now())
	if entry.Status != "design-only" || !entry.Decision {
		t.Fatalf("explicit decision publication = %+v", entry)
	}
	if strings.Contains(strings.ToLower(entry.Status), "go") || entry.Publication.ReaderAction == "" {
		t.Errorf("publication metadata inferred GO or lost explicit action: %+v", entry)
	}
	if !strings.Contains(body, "DESIGN ONLY") {
		t.Fatal("fixture must retain the frozen design marker")
	}
}

func TestResearchIndexPrefersCanonicalHTML5Showpiece(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	writeResearchFixture(t, root, "buzz/SOURCE.md", "# Buzz research\n\nA complete source paper.\n", now)
	writeResearchFixture(t, root, "buzz.md", "# Legacy Buzz research\n\nSuperseded flat source.\n", now.Add(2*time.Hour))
	writeResearchFixture(t, root, "source-only/SOURCE.md", "# Source only\n\nStill awaiting its presentation.\n", now.Add(time.Hour))
	presentation := filepath.Join(root, "buzz", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presentation, []byte("<!doctype html><title>Buzz showpiece</title>"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := readResearchIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("research index = %+v, want ready package to suppress same-stem legacy Markdown", entries)
	}
	if got := entries[0]; got.ID != "buzz/SOURCE.md" || !got.PresentationReady ||
		got.PublicationState != "showpiece" || got.PresentationURL != "/research-presentations/buzz/presentation/index.html" {
		t.Fatalf("showpiece entry = %+v", got)
	}
	if got := entries[1]; got.ID != "source-only/SOURCE.md" || got.PresentationReady ||
		got.PublicationState != "source-only" || got.PresentationURL != "" {
		t.Fatalf("source-only entry = %+v", got)
	}
}

func TestResearchPresentationServesOnlyCanonicalPackageAssets(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	writeResearchFixture(t, root, "buzz/SOURCE.md", "# Buzz\n\nSource evidence.\n", time.Now())
	for name, body := range map[string]string{
		"buzz/presentation/index.html":     `<!doctype html><script src="assets/app.js"></script>`,
		"buzz/presentation/assets/app.js":  `document.body.dataset.ready = "true";`,
		"buzz/presentation/assets/art.svg": `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"buzz/presentation/media/demo.mp4": "video-bytes",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	html := doGet(t, srv, "/research-presentations/buzz/presentation/index.html")
	if html.Code != http.StatusOK || !strings.Contains(html.Body.String(), "assets/app.js") {
		t.Fatalf("presentation HTML = %d %q", html.Code, html.Body.String())
	}
	if got := html.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'none'") || !strings.Contains(got, "frame-ancestors 'self'") {
		t.Errorf("presentation CSP = %q", got)
	}
	if got := html.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("presentation nosniff = %q", got)
	}
	if js := doGet(t, srv, "/research-presentations/buzz/presentation/assets/app.js"); js.Code != http.StatusOK || !strings.Contains(js.Body.String(), "dataset.ready") {
		t.Fatalf("presentation JS = %d %q", js.Code, js.Body.String())
	}
	if media := doGet(t, srv, "/research-presentations/buzz/presentation/media/demo.mp4"); media.Code != http.StatusOK || media.Body.String() != "video-bytes" {
		t.Fatalf("presentation media = %d %q", media.Code, media.Body.String())
	}
	if source := doGet(t, srv, "/research-presentations/buzz/SOURCE.md"); source.Code != http.StatusOK || !strings.Contains(source.Body.String(), "Source evidence") {
		t.Fatalf("presentation-relative source = %d %q", source.Code, source.Body.String())
	}

	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("HOST_SECRET_SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "buzz", "presentation", "assets", "leak.js")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, bad := range []string{
		"/research-presentations/buzz/presentation/assets/leak.js",
		"/research-presentations/buzz/presentation/.hidden.js",
		"/research-presentations/buzz/presentation/notes.md",
		"/research-presentations/%2e%2e%2foutside.js",
		"/research-presentations/orphan/presentation/index.html",
	} {
		rec := doGet(t, srv, bad)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "HOST_SECRET_SENTINEL") {
			t.Errorf("unsafe presentation path %q served status=%d body=%q", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestResearchStatusUsesExactAwaitingAuthMarkers(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		markdown string
		status   string
		decision bool
	}{
		{"frontmatter", "Design", "---\nstatus: awaiting-auth\n---\nBody", "awaiting-auth", true},
		{"bold metadata", "Design", "**Status:** awaiting-auth — operator GO\n", "awaiting-auth", true},
		{"exact ledger token", "Design", "## Gate\n\n- [awaiting-auth] operator GO\n", "awaiting-auth", true},
		{"loop posture is not authorization", "Fleet posture", "status: research\n\nRaw loop posture: awaiting-authority\n", "research", false},
		{"near miss is not authorization", "Fleet posture", "# Fleet posture awaiting-authorization\n", "research", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, decision, _ := researchStatus(tc.title, tc.markdown, ResearchPublication{})
			if status != tc.status || decision != tc.decision {
				t.Fatalf("researchStatus = (%q, %v), want (%q, %v)", status, decision, tc.status, tc.decision)
			}
		})
	}
}

func TestResearchPublicationExcludesOperationalArtifacts(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeResearchFixture(t, root, "papers/real-paper.md", "# Real paper\n\nSubstantive research.\n", now)
	for _, id := range []string{
		"walk-20260724/captures.md",
		"packages/product-walk-20260724/manifest.md",
		"scorecards/fleet-seven-c-scorecard.md",
		"demo/findings.md",
		"dumps/process-dump-20260724.md",
		"state/session-mirror/transcript.md",
	} {
		writeResearchFixture(t, root, id, "# Operational artifact\n\nNot a publication.\n", now)
	}

	entries, err := readResearchIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "papers/real-paper.md" {
		t.Fatalf("publication index = %+v, want only the real paper", entries)
	}
	for _, id := range []string{"demo/findings.md", "dumps/process-dump-20260724.md"} {
		if _, found, err := readResearchDocument(root, id); err != nil || found {
			t.Fatalf("excluded document %q = found %v, err %v; want unavailable", id, found, err)
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
		Research    []ResearchEntry            `json:"research"`
		Diagnostics ResearchDiagnosticsSummary `json:"diagnostics"`
	}
	if err := json.Unmarshal(indexRec.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Research) != 2 || index.Research[0].ID != "design.md" {
		t.Errorf("research API index = %+v", index.Research)
	}
	if index.Diagnostics.Documents != 2 || index.Diagnostics.NeedsAttention != 2 {
		t.Errorf("research API diagnostics = %+v", index.Diagnostics)
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
	for _, marker := range []string{"Depth · decisions", "R&amp;D", "Waiting on you", "What we learned", "not status notes", `id="research-reader"`, `id="research-search"`, `data-research-focus="decisions"`, `data-research-focus="learn"`, `id="research-decision-more"`, `id="research-learn-more"`, `id="research-toc-count"`, `id="research-publication-state"`, `id="research-document-comment"`, `id="research-annotation-panel"`, `id="research-presentation"`, `sandbox="allow-scripts"`, `/static/research.js`} {
		if !strings.Contains(page, marker) {
			t.Errorf("research page missing %q", marker)
		}
	}
	js := doGet(t, srv, "/static/research.js").Body.String()
	for _, marker := range []string{"function esc(value)", "renderMarkdown", "documentWithoutDuplicateTitle", "documentWithoutPublicationDirective", "educationalResearch", "learn_ready", "research-publication-state", "research-decision-strip", "collectionWindow = 6", "decisionWindow = 3", "filteredEntries", "setFocus", "tocRestoreY", "researchVideoURL", "data-research-video-fullscreen", "anchorForQuote", "X-Flotilla-Dash", "draft is still here", `detail === "awaiting-auth"`, "item.paper_id || paperIDFromBrief", "HTML5 showpiece", "renderPresentation"} {
		if !strings.Contains(js, marker) {
			t.Errorf("research renderer missing %q", marker)
		}
	}
}
