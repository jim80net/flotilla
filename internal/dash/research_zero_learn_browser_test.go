package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestResearchZeroLearnRendered889 pins one coherent Learn truth across the
// index and desktop reader while preserving admitted showpieces and exact
// source-only deep links.
func TestResearchZeroLearnRendered889(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	writeResearchFixture(t, root, "source-only.md", `# Source-only field note

This generic source remains directly readable while it stays off Learn.
`, now)
	writeResearchFixture(t, root, "learn/SOURCE.md", `<!-- flotilla-publication
classification: research
reader-action: Compare the bounded example with the source note.
support: material
-->
# Admitted educational showpiece

This generic teaching note explains a bounded example.

[Supporting material](evidence.csv)
`, now.Add(-time.Minute))
	presentation := filepath.Join(root, "learn", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presentation, []byte("<!doctype html><title>Generic showpiece</title><p>Bounded teaching example.</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	evidenceDir := os.Getenv("FLOTILLA_BROWSER_EVIDENCE_DIR")
	if evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	script := `
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

url, evidence_dir = sys.argv[1], sys.argv[2]
zero_index = {
    "count": 1,
    "research": [{
        "id": "source-only.md",
        "title": "Source-only field note",
        "status": "research",
        "summary": "Generic source material.",
        "learn_ready": False,
        "presentation_ready": False,
        "diagnostics": [{"code": "metadata.missing", "message": "Publication metadata is not declared."}]
    }]
}
empty_goals = {"found": True, "goals": []}

def prepare(page, zero):
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.route("**/api/goals", lambda route: route.fulfill(
        status=200, content_type="application/json", body=json.dumps(empty_goals)))
    if zero:
        page.route("**/api/research", lambda route: route.fulfill(
            status=200, content_type="application/json", body=json.dumps(zero_index)))

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (360, 800), (1440, 900)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            prepare(page, True)
            page.goto(url + "/research?focus=learn", wait_until="domcontentloaded")

            expect(page.locator("#research-filter-status")).to_have_text("0 educational showpieces")
            expect(page.locator("#research-status-title")).to_have_text("No educational showpieces yet")
            expect(page.locator("#research-status")).to_contain_text("No educational showpieces are publication-ready yet.")
            expect(page.locator("#research-status")).to_contain_text("teaching and HTML5 showpiece contract")
            expect(page.locator("#research-reader-state-title")).to_have_text("No educational showpieces yet")
            expect(page.locator("#research-reader-state-detail")).to_contain_text("teaching and HTML5 showpiece contract")
            expect(page.locator("#research-reader-empty")).not_to_contain_text("Choose a waiting decision")
            expect(page.locator("#research-learn-list .research-card")).to_have_count(0)
            assert page.evaluate("document.documentElement.scrollWidth === document.documentElement.clientWidth")

            if evidence_dir:
                page.screenshot(path=os.path.join(evidence_dir, "research-zero-learn-%d.png" % width))

            # Decide owns a separate no-document prompt; it never inherits the
            # Learn publication contract.
            page.locator('[data-research-focus="decisions"]').click()
            expect(page.locator("#research-reader-state-title")).to_have_text("Choose a document")
            expect(page.locator("#research-reader-state-detail")).to_contain_text("Choose a waiting decision")
            expect(page.locator("#research-reader-state-detail")).not_to_contain_text("HTML5 showpiece contract")

            # Source-only provenance stays reachable even though it is not
            # admitted to the Learn shelf.
            page.goto(url + "/research/source-only.md", wait_until="domcontentloaded")
            expect(page.locator("#research-document")).to_be_visible()
            expect(page.locator("#research-title")).to_have_text("Source-only field note")
            expect(page.locator("#research-body")).to_contain_text("directly readable")
            assert page.url.endswith("/research/source-only.md")
            page.close()

        # The same runtime still admits a complete teaching + HTML5 package.
        populated = browser.new_page(viewport={"width": 1440, "height": 900})
        prepare(populated, False)
        populated.goto(url + "/research?focus=learn", wait_until="domcontentloaded")
        expect(populated.locator("#research-filter-status")).to_have_text("1 educational showpieces")
        expect(populated.locator("#research-learn-list .research-card")).to_have_count(1)
        expect(populated.locator("#research-learn-list")).to_contain_text("Admitted educational showpiece")
        expect(populated.locator("#research-reader-state-title")).to_have_text("Choose a document")
        expect(populated.locator("#research-reader-state-detail")).to_contain_text("Choose an educational showpiece")
        populated.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL, evidenceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered zero-Learn regression: %v\n%s", err, out)
	}
	for _, width := range []string{"390", "360", "1440"} {
		if evidenceDir == "" {
			break
		}
		path := filepath.Join(evidenceDir, "research-zero-learn-"+width+".png")
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("private zero-Learn evidence missing: %s (%v)", path, err)
		}
		t.Logf("private generic zero-Learn evidence: %s", path)
	}
}
