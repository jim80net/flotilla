package dash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/researchannotation"
)

const annotationMarkdown = "# Field note\n\nAlpha café target sentence. Omega.\n"

func annotationServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	srv, dir := newTestServer(t, singleFleetRoster, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC))
	researchRoot := filepath.Join(dir, "research")
	if err := os.MkdirAll(filepath.Join(researchRoot, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(researchRoot, "notes", "field.md")
	if err := os.WriteFile(docPath, []byte(annotationMarkdown), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.cfg.ResearchPath = researchRoot
	srv.cfg.ResearchAnnotationsPath = filepath.Join(dir, "research-annotations")
	return srv, docPath, srv.cfg.ResearchAnnotationsPath
}

func decodeAnnotationResponse(t *testing.T, rec *httptest.ResponseRecorder) researchAnnotationsResponse {
	t.Helper()
	var response researchAnnotationsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return response
}

func createAnnotationBody(t *testing.T, generation uint64, digest, comment string, anchor *researchannotation.Anchor) string {
	t.Helper()
	raw, err := json.Marshal(researchAnnotationCreateRequest{
		Generation: &generation, DocumentDigest: digest, Comment: comment, Anchor: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestResearchAnnotationReadCreateIsGatedPrivateAndImmutable(t *testing.T) {
	srv, docPath, storeRoot := annotationServer(t)
	before, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}

	initial := doGet(t, srv, "/api/research-annotations/notes/field.md")
	if initial.Code != http.StatusOK {
		t.Fatalf("initial status=%d body=%s", initial.Code, initial.Body.String())
	}
	state := decodeAnnotationResponse(t, initial)
	if state.Generation != 0 || state.DocumentID != "notes/field.md" || len(state.Annotations) != 0 || !researchannotation.ValidDigest(state.DocumentDigest) {
		t.Fatalf("initial state = %+v", state)
	}
	anchor := &researchannotation.Anchor{Quote: "café target", Prefix: "Alpha ", Suffix: " sentence", Start: 20, End: 31}
	body := createAnnotationBody(t, 0, state.DocumentDigest, `<script>alert("private-body")</script>`, anchor)

	ungated := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/research-annotations/notes/field.md", strings.NewReader(body))
	blocked := httptest.NewRecorder()
	srv.handler().ServeHTTP(blocked, ungated)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("ungated status=%d", blocked.Code)
	}
	if _, err := os.Stat(storeRoot); !os.IsNotExist(err) {
		t.Fatalf("write gate touched store: %v", err)
	}

	var audit []string
	srv.researchAnnotationAudit = func(event researchAnnotationAuditEvent) { audit = append(audit, formatResearchAnnotationAudit(event)) }
	created := doWrite(t, srv, http.MethodPost, "/api/research-annotations/notes/field.md", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	response := decodeAnnotationResponse(t, created)
	if response.Generation != 1 || response.Created == nil || response.Created.Author != "operator" || response.Created.AnchorResolution == nil || response.Created.AnchorResolution.State != researchannotation.AnchorAttached {
		t.Fatalf("created response = %+v", response)
	}
	if got := response.Created.Comments[0].Text; got != `<script>alert("private-body")</script>` {
		t.Fatalf("comment = %q", got)
	}
	if strings.Contains(created.Body.String(), "<script>") || !strings.Contains(created.Body.String(), `\u003cscript\u003e`) {
		t.Fatalf("response did not HTML-escape annotation: %s", created.Body.String())
	}
	auditText := strings.Join(audit, "")
	if strings.Contains(auditText, "private-body") || strings.Contains(auditText, "café target") || !strings.Contains(auditText, response.Created.ID) {
		t.Fatalf("unsafe/incomplete audit: %q", auditText)
	}
	after, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("source Markdown was mutated")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(docPath), "annotations.json")); !os.IsNotExist(err) {
		t.Fatalf("sidecar was written beside Markdown: %v", err)
	}
	if info, err := os.Stat(researchannotation.StorePath(storeRoot)); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("private store info=%v err=%v", info, err)
	}
}

func TestResearchAnnotationCreateRejectsStaleAndMalformedInputs(t *testing.T) {
	srv, _, _ := annotationServer(t)
	initial := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	good := createAnnotationBody(t, 0, initial.DocumentDigest, "first", nil)
	if rec := doWrite(t, srv, http.MethodPost, "/api/research-annotations/notes/field.md", good); rec.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", rec.Code, rec.Body.String())
	}

	for name, body := range map[string]string{
		"stale generation":   good,
		"stale digest":       createAnnotationBody(t, 1, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "second", nil),
		"missing generation": fmt.Sprintf(`{"document_digest":%q,"comment":"x"}`, initial.DocumentDigest),
		"oversized comment":  createAnnotationBody(t, 1, initial.DocumentDigest, strings.Repeat("x", researchannotation.MaxCommentRunes+1), nil),
		"ambiguous anchor":   createAnnotationBody(t, 1, initial.DocumentDigest, "x", &researchannotation.Anchor{Quote: "a", Start: 0, End: 1}),
		"unknown field":      fmt.Sprintf(`{"generation":1,"document_digest":%q,"comment":"x","secret":"no"}`, initial.DocumentDigest),
	} {
		t.Run(name, func(t *testing.T) {
			rec := doWrite(t, srv, http.MethodPost, "/api/research-annotations/notes/field.md", body)
			want := http.StatusBadRequest
			if strings.HasPrefix(name, "stale") {
				want = http.StatusConflict
			}
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
		})
	}
	final := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	if final.Generation != 1 || len(final.Annotations) != 1 {
		t.Fatalf("rejected writes changed state: %+v", final)
	}
}

func TestResearchAnnotationReadReanchorsOnlyUniqueContext(t *testing.T) {
	srv, docPath, _ := annotationServer(t)
	initial := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	anchor := &researchannotation.Anchor{Quote: "café target", Prefix: "Alpha ", Suffix: " sentence", Start: 20, End: 31}
	if rec := doWrite(t, srv, http.MethodPost, "/api/research-annotations/notes/field.md", createAnnotationBody(t, 0, initial.DocumentDigest, "note", anchor)); rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := os.WriteFile(docPath, []byte("Preface. "+annotationMarkdown), 0o600); err != nil {
		t.Fatal(err)
	}
	moved := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	resolution := moved.Annotations[0].AnchorResolution
	if moved.DocumentDigest == initial.DocumentDigest || resolution == nil || resolution.State != researchannotation.AnchorAttached || resolution.Start != 29 {
		t.Fatalf("moved state = %+v", moved)
	}
	duplicate := annotationMarkdown + "\nAlpha café target sentence. Omega.\n"
	if err := os.WriteFile(docPath, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	needsReview := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	if got := needsReview.Annotations[0].AnchorResolution; got == nil || got.State != researchannotation.AnchorNeedsReview {
		t.Fatalf("ambiguous state = %+v", got)
	}
}

func TestResearchAnnotationGenerationCASOverHTTP(t *testing.T) {
	srv, _, _ := annotationServer(t)
	initial := decodeAnnotationResponse(t, doGet(t, srv, "/api/research-annotations/notes/field.md"))
	body := createAnnotationBody(t, 0, initial.DocumentDigest, "concurrent", nil)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/research-annotations/notes/field.md", strings.NewReader(body))
			req.Host = "127.0.0.1:8787"
			req.Header.Set("X-Flotilla-Dash", "1")
			req.Header.Set("Origin", "http://127.0.0.1:8787")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.handler().ServeHTTP(rec, req)
			codes <- rec.Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	seen := map[int]int{}
	for code := range codes {
		seen[code]++
	}
	if seen[http.StatusCreated] != 1 || seen[http.StatusConflict] != 1 {
		t.Fatalf("codes = %v", seen)
	}
}

func TestResearchAnnotationTraversalAndUnsafeStorageFailClosed(t *testing.T) {
	srv, _, root := annotationServer(t)
	for _, path := range []string{"/api/research-annotations/../flotilla.json", "/api/research-annotations/%2e%2e%2fflotilla.json", "/api/research-annotations/.hidden.md"} {
		rec := doGet(t, srv, path)
		if rec.Code == http.StatusOK {
			t.Fatalf("unsafe path %q returned 200: %s", path, rec.Body.String())
		}
	}
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/research-annotations/notes/field.md")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), root) {
		t.Fatalf("unsafe storage response status=%d body=%s", rec.Code, rec.Body.String())
	}
}
