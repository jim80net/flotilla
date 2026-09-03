package dash

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestResearchPresentationFullScreenLinkRendered932 pins the operator's small
// link contract: presentation-ready research and decision papers expose the
// bare package URL in both per-document locations; source-only papers do not.
func TestResearchPresentationFullScreenLinkRendered932(t *testing.T) {
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
	for _, fixture := range []struct {
		dir, classification, title string
	}{
		{dir: "research-ready", classification: "research", title: "Ready research paper"},
		{dir: "decision-ready", classification: "decision", title: "Ready decision paper"},
	} {
		writeResearchFixture(t, root, fixture.dir+"/SOURCE.md", `<!-- flotilla-publication
classification: `+fixture.classification+`
reader-action: Read the presentation and choose the next reversible step.
support: material
-->
# `+fixture.title+`

The source remains available inside the reading room.
`, time.Now())
		presentation := filepath.Join(root, fixture.dir, "presentation", "index.html")
		if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(presentation, []byte("<!doctype html><title>"+fixture.title+"</title><main>Standalone package</main>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeResearchFixture(t, root, "stylesheet-hidden/SOURCE.md", "# Stylesheet hidden paper\n\nThe source remains available when the presentation is not visible.\n", time.Now())
	hiddenPresentation := filepath.Join(root, "stylesheet-hidden", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(hiddenPresentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hiddenPresentation, []byte("<!doctype html><style>main { display:none }</style><main>Invisible styled evidence</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeResearchFixture(t, root, "source-only.md", "# Source-only paper\n\nNo presentation package exists.\n", time.Now())

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	script := `
import json
import sys
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]

def prepare(page):
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined; window.FLOTILLA_PRESENTATION_TIMEOUT_MS = 100")
    page.route("**/api/goals", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"found": True, "goals": []})))

def assert_ready(page, document_id, expected_path):
    page.goto(url + "/research/" + document_id, wait_until="domcontentloaded")
    expect(page.locator("#research-document")).to_be_visible()
    expect(page.locator("#research-presentation")).to_be_visible()
    links = page.locator(".research-full-screen-link")
    expect(links).to_have_count(2)
    for link in links.all():
        expect(link).to_be_visible()
        expect(link).to_have_attribute("href", expected_path)
        expect(link).to_have_attribute("target", "_blank")
        expect(link).to_have_attribute("rel", "noopener")
        box = link.bounding_box()
        assert box and box["height"] >= 44 and box["x"] >= 0
        assert box["x"] + box["width"] <= page.viewport_size["width"] + 0.5, box
    bare = page.request.get(url + expected_path)
    assert bare.ok, bare.status
    assert "Standalone package" in bare.text()
    assert "flotilla R&D" not in bare.text()

with sync_playwright() as p:
    browser = p.chromium.launch()
    try:
        for width, height in [(390, 844), (1440, 900)]:
            page = browser.new_page(viewport={"width": width, "height": height})
            prepare(page)
            assert_ready(page, "research-ready/SOURCE.md", "/research-presentations/research-ready/presentation/index.html")
            assert_ready(page, "decision-ready/SOURCE.md", "/research-presentations/decision-ready/presentation/index.html")

            page.route("**/research-presentations/decision-ready/presentation/index.html", lambda route: route.abort())
            page.goto(url + "/research/decision-ready/SOURCE.md", wait_until="domcontentloaded")
            expect(page.locator("#research-presentation-status")).to_contain_text("Presentation preview unavailable")
            expect(page.locator("#research-presentation")).to_be_hidden()
            expect(page.locator("#research-presentation-stage")).to_be_visible()
            expect(page.locator("#research-body")).to_be_visible()
            expect(page.locator("#research-body")).to_contain_text("The source remains available")

            page.goto(url + "/research/stylesheet-hidden/SOURCE.md", wait_until="domcontentloaded")
            expect(page.locator("#research-presentation-status")).to_contain_text("Presentation preview unavailable")
            expect(page.locator("#research-presentation")).to_be_hidden()
            expect(page.locator("#research-presentation-stage")).to_be_visible()
            expect(page.locator("#research-body")).to_be_visible()
            expect(page.locator("#research-body")).to_contain_text("The source remains available")

            page.goto(url + "/research/source-only.md", wait_until="domcontentloaded")
            expect(page.locator("#research-document")).to_be_visible()
            expect(page.locator("#research-presentation-stage")).to_be_hidden()
            expect(page.locator("#research-full-screen-header")).to_be_hidden()
            expect(page.locator("#research-full-screen-canvas")).to_be_hidden()
            assert page.evaluate("document.documentElement.scrollWidth === innerWidth")
            page.close()
    finally:
        browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research full-screen link regression: %v\n%s", err, out)
	}
}
