package dash

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeResearchFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResearchLibraryDecisionShelfAndBody(t *testing.T) {
	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	root := filepath.Join(dir, "research")
	writeResearchFile(t, root, "notes/general.md", "# General note\n\nReference material discussing why another artifact is design only.")
	writeResearchFile(t, root, "authorization-domains-design-for-operator-review-20260719.md", "# Authorization Domains\n\n**Status:** DESIGN ONLY — awaiting operator review\n\n<script>alert(1)</script>")
	writeResearchFile(t, root, "held.md", "# Held finding\n\nStatus: awaiting authority")
	writeResearchFile(t, root, "ignore.txt", "not markdown")

	rec := doGet(t, srv, "/api/research")
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d: %s", rec.Code, rec.Body.String())
	}
	var index struct {
		Documents []researchEntry `json:"documents"`
		Count     int             `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if index.Count != 3 || len(index.Documents) != 3 {
		t.Fatalf("index count = %d/%d, want 3", index.Count, len(index.Documents))
	}
	if index.Documents[0].Path != "authorization-domains-design-for-operator-review-20260719.md" || index.Documents[0].State != "design_only" {
		t.Fatalf("first decision document = %#v", index.Documents[0])
	}
	if index.Documents[1].State != "awaiting_authority" {
		t.Fatalf("second document state = %q, want awaiting_authority", index.Documents[1].State)
	}
	if index.Documents[0].Body != "" {
		t.Fatal("index must not embed full research bodies")
	}

	docRec := doGet(t, srv, "/api/research/"+index.Documents[0].ID)
	if docRec.Code != http.StatusOK {
		t.Fatalf("document status = %d: %s", docRec.Code, docRec.Body.String())
	}
	var body struct {
		Document researchEntry `json:"document"`
	}
	if err := json.Unmarshal(docRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Document.Body, "<script>alert(1)</script>") {
		t.Fatalf("API must preserve markdown text for escape-first client rendering: %q", body.Document.Body)
	}
}

func TestResearchLibraryTraversalAndSymlinkFailClosed(t *testing.T) {
	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	root := filepath.Join(dir, "research")
	writeResearchFile(t, root, "safe.md", "# Safe")
	outside := filepath.Join(dir, "outside-secret.md")
	if err := os.WriteFile(outside, []byte("must not be served"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}

	var index struct {
		Documents []researchEntry `json:"documents"`
	}
	rec := doGet(t, srv, "/api/research")
	if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Documents) != 1 || index.Documents[0].Path != "safe.md" {
		t.Fatalf("symlink/non-library content escaped into index: %#v", index.Documents)
	}
	for _, path := range []string{
		"/api/research/not-an-id",
		"/api/research/%2e%2e%2foutside-secret.md",
		"/api/research/../../outside-secret.md",
	} {
		rec := doGet(t, srv, path)
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "must not be served") {
			t.Fatalf("traversal %q returned %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestResearchLibraryEmptyErrorAndPageMarkers(t *testing.T) {
	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	rec := doGet(t, srv, "/api/research")
	var empty struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || empty.Count != 0 {
		t.Fatalf("missing library must be honest empty: %d %s", rec.Code, rec.Body.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "research"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = doGet(t, srv, "/api/research")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "could not be read") {
		t.Fatalf("unreadable library must be loud: %d %s", rec.Code, rec.Body.String())
	}

	page := doGet(t, srv, "/research")
	if page.Code != http.StatusOK {
		t.Fatalf("research page status = %d", page.Code)
	}
	for _, marker := range []string{"Research", "research-list", "research-reader", "/static/research.js"} {
		if !strings.Contains(page.Body.String(), marker) {
			t.Errorf("research page missing %q", marker)
		}
	}
	js := doGet(t, srv, "/static/research.js").Body.String()
	for _, marker := range []string{"function esc", "renderMarkdown", "safeHref", "renderMarkdown(doc.body)"} {
		if !strings.Contains(js, marker) {
			t.Errorf("research.js missing escape/render marker %q", marker)
		}
	}
}
